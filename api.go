package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func create(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	port := nextPort
	nextPort++

	userDataDir := filepath.Join(os.TempDir(), "chrome-"+id)
	cmd := launchChrome(chromeExecutablePath, port, userDataDir)
	wsURL, err := getWSURL(port)
	if err != nil {
		cmd.Process.Kill()
		os.RemoveAll(userDataDir)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	client, err := NewClient(wsURL)
	if err != nil {
		cmd.Process.Kill()
		os.RemoveAll(userDataDir)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 必须 enable
	client.Call("Page.enable", nil)
	// client.Call("Page.navigate", map[string]interface{}{"url": "about:blank"})

	session := &Session{
		ID:          id,
		Client:      client,
		Cmd:         cmd,
		UserDataDir: userDataDir,
		WS:          make(map[*websocket.Conn]bool),
		broadcast:   make(chan []byte, 256),
	}
	go session.broadcastLoop()

	client.onClose = func() {
		deleteSession(id)
	}

	// Callback for outgoing DevTools Protocol calls
	client.onCall = func(id int, method string, params interface{}) {
		if method == "Page.screencastFrameAck" { // 不记录发出的 ACK 消息
			return
		}
		logPayload := map[string]interface{}{
			"id":     id,
			"method": method,
			"params": params,
		}
		message, _ := json.Marshal(map[string]interface{}{
			"type":      "log",
			"logType":   "request",
			"sessionId": session.ID,
			"payload":   logPayload,
		})

		select {
		case session.broadcast <- message:
		default:
			fmt.Printf("Session %s: broadcast channel full, dropping request log.\n", session.ID)
		}
	}

	// Callback for incoming DevTools Protocol responses
	client.onResponse = func(id int, method string, rawResponse map[string]json.RawMessage) {
		// 排除 Page.screencastFrameAck 的响应，这类消息过于频繁
		if method == "Page.screencastFrameAck" {
			return
		}
		message, _ := json.Marshal(map[string]interface{}{
			"type":      "log",
			"logType":   "response",
			"sessionId": session.ID,
			"payload":   rawResponse,
		})

		select {
		case session.broadcast <- message:
		default:
			fmt.Printf("Session %s: broadcast channel full, dropping response log.\n", session.ID)
		}
	}

	// 监听帧
	client.onEvent = func(method string, params json.RawMessage) {
		if method == "Page.screencastFrame" {
			var frame struct {
				Data      string `json:"data"`
				SessionID int    `json:"sessionId"`
			}
			json.Unmarshal(params, &frame)

			s := session // capture session for the closure
			s.wsMu.Lock()
			s.lastFrame = frame.Data
			s.wsMu.Unlock()

			message, _ := json.Marshal(map[string]interface{}{
				"type":      "screencast",
				"sessionId": s.ID,
				"data":      frame.Data,
			})

			select {
			case s.broadcast <- message:
			default:
				fmt.Printf("Session %s: broadcast channel full, dropping screencast frame.\n", s.ID)
			}

			// 必须为收到的每一个 frame 进行 ACK，否则浏览器会停止发送。
			go client.Call("Page.screencastFrameAck", map[string]interface{}{"sessionId": frame.SessionID})
		}
	}

	// 👉 放在 navigate 后
	client.Call("Page.startScreencast", map[string]interface{}{
		"format":  "jpeg",
		"quality": 50,
	})

	sessions[id] = session

	json.NewEncoder(w).Encode(map[string]string{"sessionId": id})
}

// DSLAction 定义了单个自动化操作的结构
type DSLAction struct {
	Type      string      `json:"type"`                // 操作类型: navigate, input, click, select, wait, wait_selector, eval, keypress
	URL       string      `json:"url,omitempty"`       // navigate 的参数
	Selector  string      `json:"selector,omitempty"`  // CSS 选择器
	Value     string      `json:"value,omitempty"`     // 填入的值
	Ms        int         `json:"ms,omitempty"`        // 等待毫秒数
	Script    string      `json:"script,omitempty"`    // 要执行的自定义 JS
	Condition string      `json:"condition,omitempty"` // if 的判断条件 (JS 表达式)
	Then      []DSLAction `json:"then,omitempty"`      // 条件为真时执行的动作
	Else      []DSLAction `json:"else,omitempty"`      // 条件为假时执行的动作
}

func runDSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	mu.Lock()
	s, ok := sessions[id]
	mu.Unlock()

	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	var actions []DSLAction
	if err := json.NewDecoder(r.Body).Decode(&actions); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.wsMu.Lock()
	s.cancelDSL = cancel
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		s.cancelDSL = nil
		s.wsMu.Unlock()
		cancel()
	}()

	executeDSLActions(ctx, s, actions)

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func stopDSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	mu.Lock()
	s, ok := sessions[id]
	mu.Unlock()

	if ok {
		s.wsMu.Lock()
		if s.cancelDSL != nil {
			s.cancelDSL() // 发送取消信号
		}
		s.wsMu.Unlock()
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func executeDSLActions(ctx context.Context, s *Session, actions []DSLAction) {
	for _, action := range actions {
		// 每次执行指令前检查是否收到了终止信号
		if ctx.Err() != nil {
			return
		}

		switch action.Type {
		case "navigate":
			s.Client.Call("Page.navigate", map[string]interface{}{"url": action.URL})

			// 轮询等待页面完全加载 (document.readyState === 'complete')
			for j := 0; j < 100; j++ { // 最多等待 20 秒
				if ctx.Err() != nil {
					return
				}
				raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": "document.readyState"})
				if err == nil {
					var res struct {
						Result struct {
							Value string `json:"value"`
						} `json:"result"`
					}
					json.Unmarshal(raw, &res)
					if res.Result.Value == "complete" {
						break
					}
				}
				select {
				case <-time.After(200 * time.Millisecond):
				case <-ctx.Done():
					return
				}
			}
			// 额外给前端框架 500ms 的宽限期，用于完成复杂的事件绑定(Hydration)
			time.Sleep(500 * time.Millisecond)
		case "input":
			expr := fmt.Sprintf("document.querySelector(`%s`).value = `%s`", action.Selector, action.Value)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
		case "click":
			expr := fmt.Sprintf("document.querySelector(`%s`).click()", action.Selector)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
		case "select":
			expr := fmt.Sprintf("var el = document.querySelector(`%s`); el.value = `%s`; el.dispatchEvent(new Event('change', {bubbles: true}));", action.Selector, action.Value)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
		case "keypress":
			// 1. 如果传入了选择器，敲击前先强制让该元素获取焦点
			if action.Selector != "" {
				expr := fmt.Sprintf(`var el = document.querySelector("%s"); if(el) el.focus();`, action.Selector)
				s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
				time.Sleep(50 * time.Millisecond) // 等待 focus 真正生效
			}

			// 2. 针对 Enter 键，需要附带 text 属性才能被部分框架正确捕获
			text := ""
			if action.Value == "Enter" {
				text = "\r"
			}

			// 模拟真实按键按下
			s.Client.Call("Input.dispatchKeyEvent", map[string]interface{}{
				"type": "keyDown",
				"key":  action.Value,
				"code": action.Value,
				"text": text,
			})

			time.Sleep(50 * time.Millisecond) // 稍微停顿，模拟人手按压的微小延迟

			// 模拟真实按键抬起
			s.Client.Call("Input.dispatchKeyEvent", map[string]interface{}{
				"type": "keyUp",
				"key":  action.Value,
				"code": action.Value,
			})
		case "wait":
			select {
			case <-time.After(time.Duration(action.Ms) * time.Millisecond):
			case <-ctx.Done():
				return
			}
		case "eval":
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": action.Script})
		case "wait_selector":
			// 轮询等待元素出现（最多等待 10 秒）
			expr := fmt.Sprintf("!!document.querySelector(`%s`)", action.Selector)
			for j := 0; j < 50; j++ {
				if ctx.Err() != nil {
					return
				}

				raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
				if err == nil {
					var res struct {
						Result struct {
							Value bool `json:"value"`
						} `json:"result"`
					}
					json.Unmarshal(raw, &res)
					if res.Result.Value {
						break // 元素出现了，跳出等待循环
					}
				}
				select {
				case <-time.After(200 * time.Millisecond):
				case <-ctx.Done():
					return
				}
			}
		case "if":
			raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": action.Condition})
			if err == nil {
				var res struct {
					Result struct {
						Value bool `json:"value"`
					} `json:"result"`
				}
				fmt.Println("raw", raw)
				json.Unmarshal(raw, &res)
				if res.Result.Value {
					executeDSLActions(ctx, s, action.Then)
				} else {
					executeDSLActions(ctx, s, action.Else)
				}
			} else {
				fmt.Println("if err", err)
			}
		}
	}
}

func del(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	deleteSession(id)
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("sessionId")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// The connection is automatically closed by the server when this handler returns.
	// We will keep it open by not returning, but we need a mechanism to close it.
	// The broadcast loop will close it on write error.

	mu.Lock()
	s, ok := sessions[id]
	mu.Unlock()

	if !ok {
		conn.Close()
		return
	}

	s.wsMu.Lock()
	lastFrame := s.lastFrame
	s.wsMu.Unlock()

	if lastFrame != "" {
		message, _ := json.Marshal(map[string]interface{}{
			"type":      "screencast",
			"sessionId": id,
			"data":      lastFrame,
		})
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			conn.Close()
			return
		}
	}

	s.wsMu.Lock()
	s.WS[conn] = true
	s.wsMu.Unlock()
}

type SessionInfo struct {
	ID        string `json:"id"`
	IsRunning bool   `json:"isRunning"`
}

func listSessions(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	var infos []SessionInfo
	for id, s := range sessions {
		s.wsMu.Lock()
		isRunning := s.cancelDSL != nil
		s.wsMu.Unlock()

		infos = append(infos, SessionInfo{ID: id, IsRunning: isRunning})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": infos})
}

// --- DSL Management CRUD ---
func apiListDSL(w http.ResponseWriter, r *http.Request) {
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)
	rows, err := db.Query("SELECT id, name, content, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM dsl_scripts WHERE creator_id = ? ORDER BY id DESC", userInfo.OaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var dsls []DSL
	for rows.Next() {
		var d DSL
		// Assuming new records will have these fields populated.
		if err := rows.Scan(&d.ID, &d.Name, &d.Content, &d.CreatorID, &d.CreatedAt); err == nil {
			dsls = append(dsls, d)
		}
	}
	if dsls == nil {
		dsls = []DSL{}
	}
	json.NewEncoder(w).Encode(dsls)
}

func apiSaveDSL(w http.ResponseWriter, r *http.Request) {
	var dsl DSL
	if err := json.NewDecoder(r.Body).Decode(&dsl); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	if dsl.ID == 0 {
		res, _ := db.Exec("INSERT INTO dsl_scripts (name, content, creator_id) VALUES (?, ?, ?)", dsl.Name, dsl.Content, userInfo.OaID)
		id, _ := res.LastInsertId()
		dsl.ID = int(id)
	} else {
		// Security check: ensure user owns this DSL
		var owner string
		err := db.QueryRow("SELECT creator_id FROM dsl_scripts WHERE id=?", dsl.ID).Scan(&owner)
		if err != nil || owner != userInfo.OaID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		db.Exec("UPDATE dsl_scripts SET name=?, content=? WHERE id=? AND creator_id=?", dsl.Name, dsl.Content, dsl.ID, userInfo.OaID)
	}

	var finalDsl DSL
	db.QueryRow("SELECT id, name, content, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM dsl_scripts WHERE id=?", dsl.ID).Scan(&finalDsl.ID, &finalDsl.Name, &finalDsl.Content, &finalDsl.CreatorID, &finalDsl.CreatedAt)
	json.NewEncoder(w).Encode(finalDsl)
}

func apiDeleteDSL(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	var owner string
	err := db.QueryRow("SELECT creator_id FROM dsl_scripts WHERE id=?", id).Scan(&owner)
	if err != nil || owner != userInfo.OaID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	db.Exec("DELETE FROM dsl_scripts WHERE id=? AND creator_id=?", id, userInfo.OaID)
	w.Write([]byte(`{"status":"ok"}`))
}

// --- 定时任务管理调度 ---

type CronTask struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	DSLID     int    `json:"dslId"`
	Status    int    `json:"status"` // 0: 停止, 1: 运行中
	CreatorID string `json:"creatorId,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

var activeCronJobs = make(map[int]context.CancelFunc)
var cronMu sync.Mutex

func initCronTasks() {
	db.Exec(`CREATE TABLE IF NOT EXISTS cron_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		schedule TEXT,
		dsl_id INTEGER,
		status INTEGER DEFAULT 0,
		creator_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)

	// 重启应用时，自动恢复状态为“运行中”的定时任务
	rows, err := db.Query("SELECT id, name, schedule, dsl_id, status FROM cron_tasks WHERE status=1")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var task CronTask
			rows.Scan(&task.ID, &task.Name, &task.Schedule, &task.DSLID, &task.Status)
			startCronJob(task)
		}
	}
}

func startCronJob(task CronTask) {
	dur, err := time.ParseDuration(task.Schedule)
	if err != nil {
		fmt.Println("启动失败, 无效的时间格式:", task.Schedule)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cronMu.Lock()
	if oldCancel, exists := activeCronJobs[task.ID]; exists {
		oldCancel() // 取消可能正在运行的旧实例
	}
	activeCronJobs[task.ID] = cancel
	cronMu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(dur):
				// 每次到期后，抛入后台独立运行，避免阻塞下一个周期
				go runCronDSL(task.DSLID)
			}
		}
	}()
}

func stopCronJob(id int) {
	cronMu.Lock()
	if cancel, exists := activeCronJobs[id]; exists {
		cancel()
		delete(activeCronJobs, id)
	}
	cronMu.Unlock()
}

func runCronDSL(dslID int) {
	var content string
	err := db.QueryRow("SELECT content FROM dsl_scripts WHERE id=?", dslID).Scan(&content)
	if err != nil {
		return
	}
	var actions []DSLAction
	json.Unmarshal([]byte(content), &actions)

	// 1. 在后台初始化一个新的无头浏览器实例
	id := fmt.Sprintf("cron-%d", time.Now().UnixNano())
	mu.Lock()
	port := nextPort
	nextPort++
	mu.Unlock()

	userDataDir := filepath.Join(os.TempDir(), "chrome-"+id)
	cmd := launchChrome(chromeExecutablePath, port, userDataDir)
	wsURL, err := getWSURL(port)
	if err != nil {
		cmd.Process.Kill()
		os.RemoveAll(userDataDir)
		return
	}
	client, err := NewClient(wsURL)
	if err != nil {
		cmd.Process.Kill()
		os.RemoveAll(userDataDir)
		return
	}

	client.Call("Page.enable", nil)
	session := &Session{ID: id, Client: client, Cmd: cmd, UserDataDir: userDataDir, WS: make(map[*websocket.Conn]bool)}
	client.onClose = func() { deleteSession(id) }

	// 监听帧回传
	client.onEvent = func(method string, params json.RawMessage) {
		if method == "Page.screencastFrame" {
			var frame struct {
				Data      string `json:"data"`
				SessionID int    `json:"sessionId"`
			}
			json.Unmarshal(params, &frame)

			session.wsMu.Lock()
			session.lastFrame = frame.Data
			for ws := range session.WS {
				err := ws.WriteJSON(map[string]interface{}{
					"sessionId": id,
					"data":      frame.Data,
				})
				if err != nil {
					ws.Close()
					delete(session.WS, ws) // 自动清理死连接
				}
			}

			session.wsMu.Unlock()
			// 必须为收到的每一个 frame 进行 ACK，否则浏览器会停止发送。
			go client.Call("Page.screencastFrameAck", map[string]interface{}{"sessionId": frame.SessionID})
		}
	}

	client.Call("Page.startScreencast", map[string]interface{}{"format": "jpeg", "quality": 50})

	mu.Lock()
	sessions[id] = session
	mu.Unlock()

	broadcastSessionEvent("session_added", SessionInfo{ID: id, IsRunning: true})

	// 无论执行成功或发生错误，最终销毁关闭该次临时浏览器
	defer func() {
		deleteSession(id)
		broadcastSessionEvent("session_removed", map[string]string{"id": id})
	}()

	// 2. 注入上下文并执行 DSL (防止僵尸任务，强制最多10分钟超时)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	session.wsMu.Lock()
	session.cancelDSL = cancel
	session.wsMu.Unlock()

	executeDSLActions(ctx, session, actions)
}

// --- CRUD ---
func apiListCron(w http.ResponseWriter, r *http.Request) {
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)
	rows, err := db.Query("SELECT id, name, schedule, dsl_id, status, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM cron_tasks WHERE creator_id = ? ORDER BY id DESC", userInfo.OaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []CronTask
	for rows.Next() {
		var t CronTask
		// Assuming new records will have these fields populated.
		if err := rows.Scan(&t.ID, &t.Name, &t.Schedule, &t.DSLID, &t.Status, &t.CreatorID, &t.CreatedAt); err == nil {
			tasks = append(tasks, t)
		}
	}
	if tasks == nil {
		tasks = []CronTask{}
	}
	json.NewEncoder(w).Encode(tasks)
}

func apiSaveCron(w http.ResponseWriter, r *http.Request) {
	var t CronTask
	json.NewDecoder(r.Body).Decode(&t)
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	// 安全检查：确保所选的 DSL 脚本属于当前用户
	if t.DSLID > 0 {
		var dslOwner string
		err := db.QueryRow("SELECT creator_id FROM dsl_scripts WHERE id=?", t.DSLID).Scan(&dslOwner)
		if err != nil || dslOwner != userInfo.OaID {
			http.Error(w, "Forbidden: selected DSL does not belong to you.", http.StatusForbidden)
			return
		}
	}

	if t.ID == 0 {
		res, _ := db.Exec("INSERT INTO cron_tasks (name, schedule, dsl_id, status, creator_id) VALUES (?, ?, ?, 0, ?)", t.Name, t.Schedule, t.DSLID, userInfo.OaID)
		id, _ := res.LastInsertId()
		t.ID = int(id)
	} else {
		// 安全检查：确保当前用户是此定时任务的所有者
		var cronOwner string
		var currentStatus int
		err := db.QueryRow("SELECT creator_id, status FROM cron_tasks WHERE id=?", t.ID).Scan(&cronOwner, &currentStatus)
		if err != nil || cronOwner != userInfo.OaID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		db.Exec("UPDATE cron_tasks SET name=?, schedule=?, dsl_id=? WHERE id=? AND creator_id=?", t.Name, t.Schedule, t.DSLID, t.ID, userInfo.OaID)

		// 如果任务原本就在运行，则重启以应用新的时间间隔
		if currentStatus == 1 {
			var updatedTask CronTask
			db.QueryRow("SELECT id, name, schedule, dsl_id, status FROM cron_tasks WHERE id=?", t.ID).Scan(&updatedTask.ID, &updatedTask.Name, &updatedTask.Schedule, &updatedTask.DSLID, &updatedTask.Status)
			stopCronJob(t.ID)
			startCronJob(updatedTask)
		}
	}

	// 重新从数据库获取完整的任务信息并返回给前端
	var finalTask CronTask
	db.QueryRow("SELECT id, name, schedule, dsl_id, status, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM cron_tasks WHERE id=?", t.ID).Scan(&finalTask.ID, &finalTask.Name, &finalTask.Schedule, &finalTask.DSLID, &finalTask.Status, &finalTask.CreatorID, &finalTask.CreatedAt)
	json.NewEncoder(w).Encode(finalTask)
}

// --- Global WebSocket Hub for Events ---

var globalWsHub = struct {
	clients    map[*websocket.Conn]bool
	mu         sync.Mutex
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan []byte
}{
	clients:    make(map[*websocket.Conn]bool),
	register:   make(chan *websocket.Conn),
	unregister: make(chan *websocket.Conn),
	broadcast:  make(chan []byte, 128), // Buffered channel to prevent blocking
}

func runGlobalWsHub() {
	for {
		select {
		case conn := <-globalWsHub.register:
			globalWsHub.mu.Lock()
			globalWsHub.clients[conn] = true
			globalWsHub.mu.Unlock()
		case conn := <-globalWsHub.unregister:
			globalWsHub.mu.Lock()
			if _, ok := globalWsHub.clients[conn]; ok {
				delete(globalWsHub.clients, conn)
				conn.Close()
			}
			globalWsHub.mu.Unlock()
		case message := <-globalWsHub.broadcast:
			globalWsHub.mu.Lock()
			for conn := range globalWsHub.clients {
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					// On error, assume client disconnected, unregister them.
					go func(c *websocket.Conn) {
						globalWsHub.unregister <- c
					}(conn)
				}
			}
			globalWsHub.mu.Unlock()
		}
	}
}

func globalEventsWsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	globalWsHub.register <- conn

	// When the client disconnects, unregister them.
	defer func() { globalWsHub.unregister <- conn }()

	// Keep the connection alive by reading messages (and discarding them).
	// This loop will exit when the client disconnects, triggering the defer.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func broadcastSessionEvent(eventType string, payload interface{}) {
	message, err := json.Marshal(map[string]interface{}{
		"type":    eventType,
		"payload": payload,
	})
	if err != nil {
		fmt.Println("Error marshalling broadcast message:", err)
		return
	}

	// Non-blocking send
	select {
	case globalWsHub.broadcast <- message:
	default:
		fmt.Println("Global hub broadcast channel is full. Message dropped.")
	}
}

func apiDeleteCron(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	var owner string
	err := db.QueryRow("SELECT creator_id FROM cron_tasks WHERE id=?", idStr).Scan(&owner)
	if err != nil || owner != userInfo.OaID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var id int
	fmt.Sscanf(idStr, "%d", &id)
	stopCronJob(id) // 先停止可能在运行的任务
	db.Exec("DELETE FROM cron_tasks WHERE id=? AND creator_id=?", id, userInfo.OaID)
	w.Write([]byte(`{"status":"ok"}`))
}

func apiToggleCron(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	var owner string
	err := db.QueryRow("SELECT creator_id FROM cron_tasks WHERE id=?", idStr).Scan(&owner)
	if err != nil || owner != userInfo.OaID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var id int
	fmt.Sscanf(idStr, "%d", &id)

	var t CronTask
	db.QueryRow("SELECT id, name, schedule, dsl_id, status FROM cron_tasks WHERE id=?", id).Scan(&t.ID, &t.Name, &t.Schedule, &t.DSLID, &t.Status)

	if t.Status == 0 {
		db.Exec("UPDATE cron_tasks SET status=1 WHERE id=?", id)
		startCronJob(t)
	} else {
		db.Exec("UPDATE cron_tasks SET status=0 WHERE id=?", id)
		stopCronJob(id)
	}

	var finalTask CronTask
	db.QueryRow("SELECT id, name, schedule, dsl_id, status, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM cron_tasks WHERE id=?", t.ID).Scan(&finalTask.ID, &finalTask.Name, &finalTask.Schedule, &finalTask.DSLID, &finalTask.Status, &finalTask.CreatorID, &finalTask.CreatedAt)
	json.NewEncoder(w).Encode(finalTask)
}
