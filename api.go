package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// setupSessionHandlers configures the onCall, onResponse, and onEvent handlers for a session's client.
func setupSessionHandlers(session *Session) {
	client := session.Client
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

		session.wsMu.Lock()
		if session.isDslRunning {
			session.lastLogs = append(session.lastLogs, message)
		}
		session.wsMu.Unlock()

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

		session.wsMu.Lock()
		if session.isDslRunning {
			session.lastLogs = append(session.lastLogs, message)
		}
		session.wsMu.Unlock()

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

			message, _ := json.Marshal(map[string]interface{}{"type": "screencast", "sessionId": s.ID, "data": frame.Data})
			select {
			case s.broadcast <- message:
			default:
				fmt.Printf("Session %s: broadcast channel full, dropping screencast frame.\n", s.ID)
			}
			go client.Call("Page.screencastFrameAck", map[string]interface{}{"sessionId": frame.SessionID})
		}
	}
	client.Call("Page.startScreencast", map[string]interface{}{"format": "jpeg", "quality": 50})
}

func create(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	isPersistent := r.URL.Query().Get("persistent") == "true"

	if isPersistent {
		var parentSession *Session
		for _, s := range sessions {
			if s.IsPersistent && s.Cmd != nil { // Find an existing master persistent session
				parentSession = s
				break
			}
		}

		if parentSession != nil {
			// Found an existing persistent browser, create a new tab in it.
			log.Printf("在已有的持久化浏览器 %s 中创建新标签页", parentSession.ID)

			newTarget, err := parentSession.Client.Call("Target.createTarget", map[string]interface{}{"url": "about:blank", "newWindow": false})
			if err != nil {
				http.Error(w, "创建新标签页失败: "+err.Error(), http.StatusInternalServerError)
				return
			}

			var targetInfo struct {
				TargetID string `json:"targetId"`
			}
			json.Unmarshal(newTarget, &targetInfo)
			if targetInfo.TargetID == "" {
				http.Error(w, "获取新标签页ID失败", http.StatusInternalServerError)
				return
			}

			// A new tab is created, now get its specific websocket URL
			wsURL, err := getWSURLForTarget(parentSession.Port, targetInfo.TargetID)
			if err != nil {
				parentSession.Client.Call("Target.closeTarget", map[string]interface{}{"targetId": targetInfo.TargetID})
				http.Error(w, "获取新标签页连接信息失败: "+err.Error(), http.StatusInternalServerError)
				return
			}

			client, err := NewClient(wsURL)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			client.Call("Page.enable", nil)

			id := fmt.Sprintf("%s-tab-%d", parentSession.ID, time.Now().UnixNano())
			session := &Session{
				ID:            id,
				Port:          parentSession.Port,
				ParentClient:  parentSession.Client,
				TargetID:      targetInfo.TargetID,
				IsPersistent:  true,
				Client:        client,
				Cmd:           nil,
				UserDataDir:   parentSession.UserDataDir,
				WS:            make(map[*websocket.Conn]bool),
				broadcast:     make(chan []byte, 256),
				userInputChan: make(chan string, 1),
			}
			go session.broadcastLoop()
			client.onClose = func() { deleteSession(id) }
			setupSessionHandlers(session)
			sessions[id] = session
			json.NewEncoder(w).Encode(map[string]interface{}{"sessionId": id, "isPersistent": true})
			return
		}
	}

	var userDataDir string
	var id string
	if isPersistent {
		select {
		case dir := <-persistentDirPool:
			userDataDir = dir
			log.Printf("从池中复用持久化目录: %s", userDataDir)
		default:
			// Pool is empty, create a new directory for a persistent session
			userDataDir = filepath.Join(persistentDataPath, fmt.Sprintf("session-%d", time.Now().UnixNano()))
			log.Printf("池为空，新建持久化目录: %s", userDataDir)
		}
		id = filepath.Base(userDataDir) // Use directory name as ID for simplicity
	} else {
		id = fmt.Sprintf("temp-%d", time.Now().UnixNano()) // Fallback to creating a new browser
		userDataDir = filepath.Join(os.TempDir(), "chrome-"+id)
	}

	port := nextPort
	nextPort++

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
		ID:            id,
		Port:          port,
		IsPersistent:  isPersistent,
		Client:        client,
		Cmd:           cmd,
		UserDataDir:   userDataDir,
		WS:            make(map[*websocket.Conn]bool),
		broadcast:     make(chan []byte, 256),
		userInputChan: make(chan string, 1),
	}
	go session.broadcastLoop()

	client.onClose = func() {
		deleteSession(id)
	}

	setupSessionHandlers(session)

	sessions[id] = session

	json.NewEncoder(w).Encode(map[string]interface{}{"sessionId": id, "isPersistent": isPersistent})
}

// DSLAction 定义了单个自动化操作的结构
type DSLAction struct {
	Type            string      `json:"type"`                      // 操作类型: navigate, input, click, select, checkbox, radio, wait, wait_selector, eval, keypress, wait_for_input, wait_for_qrcode_scan
	URL             string      `json:"url,omitempty"`             // navigate 的参数
	Selector        string      `json:"selector,omitempty"`        // CSS 选择器
	Value           string      `json:"value,omitempty"`           // 填入的值
	Ms              int         `json:"ms,omitempty"`              // 等待毫秒数
	Timeout         int         `json:"timeout,omitempty"`         // 通用超时时间(秒), e.g. for wait_selector, wait_for_qrcode_scan
	Script          string      `json:"script,omitempty"`          // 要执行的自定义 JS
	Condition       string      `json:"condition,omitempty"`       // if 的判断条件 (JS 表达式)
	Then            []DSLAction `json:"then,omitempty"`            // 条件为真时执行的动作
	Else            []DSLAction `json:"else,omitempty"`            // 条件为假时执行的动作
	InputType       string      `json:"inputType,omitempty"`       // wait_for_input 的类型, e.g., "prompt"
	Prompt          string      `json:"prompt,omitempty"`          // wait_for_input 的提示信息
	VariableName    string      `json:"variableName,omitempty"`    // wait_for_input 存储用户输入的变量名
	SuccessSelector string      `json:"successSelector,omitempty"` // wait_for_qrcode_scan 的成功标识元素
	CaptchaSelector string      `json:"captchaSelector,omitempty"` // wait_for_captcha 的图形验证码元素选择器
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

	// Read the raw body to check for secrets before decoding
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	containsSecret := bytes.Contains(bodyBytes, []byte("{{secret."))

	var creatorID string
	var userIsAuthenticated bool

	// Manually check for authentication, similar to authMiddleware but without failing on unauth
	cookie, err := r.Cookie("session_id")
	if err == nil {
		sessionID := cookie.Value
		userInfoVal, ok := userSessions.Load(sessionID)
		if ok {
			userInfo, castOk := userInfoVal.(UserInfo)
			if castOk {
				userIsAuthenticated = true
				creatorID = userInfo.OaID
			}
		}
	}

	if containsSecret && !userIsAuthenticated {
		http.Error(w, "未登录用户无法在DSL中使用密码保险箱功能。", http.StatusForbidden)
		return
	}

	// Restore the body so it can be read again by json.NewDecoder
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// 清理旧日志，并标记 DSL 开始执行
	s.wsMu.Lock()
	s.isDslRunning = true
	s.lastLogs = nil
	s.wsMu.Unlock()

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
		s.isDslRunning = false
		s.cancelDSL = nil
		s.wsMu.Unlock()
		cancel()
	}()

	variables := make(map[string]string)
	executeDSLActions(ctx, s, actions, variables, creatorID)

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// DSLBulkRequest 定义了批量 DSL 执行的请求体结构
type DSLBulkRequest [][]DSLAction

// runDSLBulk 处理批量执行 DSL 的请求
func runDSLBulk(w http.ResponseWriter, r *http.Request) {
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

	// Read the raw body to check for secrets before decoding
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	containsSecret := bytes.Contains(bodyBytes, []byte("{{secret."))

	var creatorID string
	var userIsAuthenticated bool

	// Manually check for authentication, similar to authMiddleware but without failing on unauth
	cookie, err := r.Cookie("session_id")
	if err == nil {
		sessionID := cookie.Value
		userInfoVal, ok := userSessions.Load(sessionID)
		if ok {
			userInfo, castOk := userInfoVal.(UserInfo)
			if castOk {
				userIsAuthenticated = true
				creatorID = userInfo.OaID
			}
		}
	}

	if containsSecret && !userIsAuthenticated {
		http.Error(w, "未登录用户无法在批量DSL中使用密码保险箱功能。", http.StatusForbidden)
		return
	}

	// Restore the body so it can be read again by json.NewDecoder
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var bulkActions DSLBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&bulkActions); err != nil {
		http.Error(w, "Invalid JSON for bulk DSL", http.StatusBadRequest)
		return
	}

	// 清理旧日志，并标记 DSL 开始执行
	s.wsMu.Lock()
	s.isDslRunning = true // 标记会话正在运行批量DSL
	s.lastLogs = nil
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		s.isDslRunning = false
		s.cancelDSL = nil // 确保在批量任务结束后取消函数也被清理
		s.wsMu.Unlock()
	}()

	for i, actions := range bulkActions {
		log.Printf("Session %s: 开始执行批量 DSL 任务批次 %d/%d", s.ID, i+1, len(bulkActions))
		// 为每次 DSL 序列创建独立的 Context.WithCancel，但共用会话
		ctx, cancel := context.WithCancel(context.Background())
		s.wsMu.Lock()
		s.cancelDSL = cancel // 允许停止整个批量任务
		s.wsMu.Unlock()

		variables := make(map[string]string)
		executeDSLActions(ctx, s, actions, variables, creatorID)

		if ctx.Err() != nil {
			log.Printf("Session %s: 批量 DSL 任务在批次 %d/%d 处被终止: %v", s.ID, i+1, len(bulkActions), ctx.Err())
			http.Error(w, fmt.Sprintf("批量 DSL 任务在批次 %d 处被终止: %v", i+1, ctx.Err()), http.StatusInternalServerError)
			cancel() // 确保上下文被取消
			return
		}
		cancel() // 正常完成，清理当前批次的上下文
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "批量 DSL 任务执行完成。"})
}

func userInput(w http.ResponseWriter, r *http.Request) {
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	select {
	case s.userInputChan <- string(body):
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "Not waiting for input", http.StatusConflict)
	}
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
		// 发送消息以确保前端交互UI被清理
		finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
		s.broadcast <- finishMsg
		s.wsMu.Unlock()
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func pollForSuccess(ctx context.Context, s *Session, expression string, timeout time.Duration) error {
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		// Check for cancellation at the start of each iteration
		select {
		case <-pollCtx.Done():
			return fmt.Errorf("polling timed out or was cancelled")
		default:
		}

		raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expression})
		if err == nil {
			var res struct {
				Result struct {
					Value bool `json:"value"`
				} `json:"result"`
			}
			json.Unmarshal(raw, &res)
			if res.Result.Value {
				return nil // Success
			}
		}

		// Wait before next poll
		select {
		case <-time.After(200 * time.Millisecond):
		case <-pollCtx.Done():
			return fmt.Errorf("polling timed out or was cancelled during wait")
		}
	}
}

var varRegex = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)

func substituteVariables(text string, variables map[string]string, creatorID string) string {
	return varRegex.ReplaceAllStringFunc(text, func(match string) string {
		key := match[2 : len(match)-2]
		if strings.HasPrefix(key, "secret.") {
			if len(encryptionKey) == 0 {
				log.Printf("警告: 尝试使用密码 '%s'，但密码保险箱未配置 (APP_ENCRYPTION_KEY 缺失)。", key)
				return match // Return original if vault is not configured
			}
			secretName := strings.TrimPrefix(key, "secret.")
			var encryptedValue string
			err := db.QueryRow("SELECT value FROM secrets WHERE name = ? AND creator_id = ?", secretName, creatorID).Scan(&encryptedValue)
			if err != nil {
				log.Printf("无法找到密码 '%s' (用户: %s): %v", secretName, creatorID, err)
				return match // Return original if secret not found
			}

			decryptedValue, err := decrypt(encryptedValue)
			if err != nil {
				log.Printf("解密密码 '%s' 失败 (用户: %s): %v", secretName, creatorID, err)
				return match // Return original on decryption failure
			}
			return decryptedValue
		}

		if val, ok := variables[key]; ok {
			return val
		}
		return match // Return original if not found
	})
}

func executeDSLActions(ctx context.Context, s *Session, actions []DSLAction, variables map[string]string, creatorID string) {
	for _, action := range actions {
		// 每次执行指令前检查是否收到了终止信号
		if ctx.Err() != nil {
			return
		}

		// 替换所有可能包含变量的字段
		action.URL = substituteVariables(action.URL, variables, creatorID)
		action.Selector = substituteVariables(action.Selector, variables, creatorID)
		action.Value = substituteVariables(action.Value, variables, creatorID)
		action.Script = substituteVariables(action.Script, variables, creatorID)
		action.Condition = substituteVariables(action.Condition, variables, creatorID)
		action.Prompt = substituteVariables(action.Prompt, variables, creatorID)
		action.SuccessSelector = substituteVariables(action.SuccessSelector, variables, creatorID)

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
			focusExpr := fmt.Sprintf("document.querySelector(`%s`).focus()", action.Selector)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": focusExpr})

			if action.Value != "" {
				s.Client.Call("Input.insertText", map[string]interface{}{
					"text": action.Value,
				})
			}
		case "checkbox": // 使用CDP先检查状态，再决定是否点击，更接近原生操作
			getCheckedStateExpr := fmt.Sprintf("document.querySelector(`%s`) ? document.querySelector(`%s`).checked : null", action.Selector, action.Selector)
			raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": getCheckedStateExpr, "returnByValue": true})
			if err != nil {
				log.Printf("Session %s: checkbox state check failed for '%s': %v", s.ID, action.Selector, err)
				return
			}
			var res struct {
				Result struct {
					Value *bool `json:"value"`
				} `json:"result"`
			}
			json.Unmarshal(raw, &res)

			if res.Result.Value != nil {
				currentlyChecked := *res.Result.Value
				shouldBeChecked := action.Value == "true"
				if currentlyChecked != shouldBeChecked {
					clickExpr := fmt.Sprintf("document.querySelector(`%s`).click()", action.Selector)
					s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": clickExpr})
				}
			} else {
				log.Printf("Session %s: checkbox element not found '%s'", s.ID, action.Selector)
			}
		case "radio": // 改造为使用更精确的组合选择器直接点击，而不是JS循环
			// 这种方式假定选择器能定位到 radio 组 (例如, 'input[name="group"]'),
			// 然后通过 action.Value 来指定具体要点击的那个选项的 value.
			clickExpr := fmt.Sprintf("document.querySelector(`%s[value='%s']`).click()", action.Selector, action.Value)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": clickExpr})
		case "click":
			expr := fmt.Sprintf("document.querySelector(`%s`).click()", action.Selector)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
		case "select":
			// 改造为更稳健的方式，在设置 value 后，同时触发 input 和 change 事件，以兼容各类前端框架
			expr := fmt.Sprintf(`
				const select = document.querySelector('%s');
				if (select && select.value !== '%s') {
					select.value = '%s';
					select.dispatchEvent(new Event('input', { bubbles: true }));
					select.dispatchEvent(new Event('change', { bubbles: true }));
				}
			`, action.Selector, action.Value, action.Value)
			s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": expr})
		case "keypress":
			// 1. 如果传入了选择器，敲击前先强制让该元素获取焦点
			if action.Selector != "" {
				focusExpr := fmt.Sprintf(`document.querySelector("%s").focus()`, action.Selector)
				s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": focusExpr})

				// 等待元素真正获得焦点
				pollExpr := fmt.Sprintf("document.activeElement === document.querySelector(`%s`)", action.Selector)
				if err := pollForSuccess(ctx, s, pollExpr, 5*time.Second); err != nil {
					log.Printf("Session %s: keypress 操作无法聚焦于元素 '%s': %v", s.ID, action.Selector, err)
				}
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
			timeout := 10 * time.Second
			if action.Timeout > 0 {
				timeout = time.Duration(action.Timeout) * time.Second
			}
			expr := fmt.Sprintf("!!document.querySelector(`%s`)", action.Selector)
			err := pollForSuccess(ctx, s, expr, timeout)
			if err != nil {
				log.Printf("Session %s: wait_selector for '%s' failed: %v", s.ID, action.Selector, err)
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
					executeDSLActions(ctx, s, action.Then, variables, creatorID)
				} else {
					executeDSLActions(ctx, s, action.Else, variables, creatorID)
				}
			} else {
				fmt.Println("if err", err)
			}
		case "wait_for_input":
			if action.InputType == "prompt" {
				interactionPayload := map[string]interface{}{
					"type":    "user_interaction_required",
					"payload": map[string]string{"inputType": "prompt", "prompt": action.Prompt},
				}
				msg, _ := json.Marshal(interactionPayload)
				s.broadcast <- msg

				select {
				case userInput := <-s.userInputChan:
					if action.VariableName != "" {
						variables[action.VariableName] = userInput
					}
					finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
					s.broadcast <- finishMsg
				case <-ctx.Done():
					finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
					s.broadcast <- finishMsg
					return
				}
			}
		case "wait_for_captcha":
			// 1. 提取图形验证码的 src 或 data URL
			getCaptchaExpr := fmt.Sprintf(`
				(() => {
					const el = document.querySelector('%s');
					if (!el) return null;
					if (el.tagName.toLowerCase() === 'img') return el.src;
					if (el.tagName.toLowerCase() === 'canvas') return el.toDataURL();
					return null;
				})()
			`, action.CaptchaSelector)

			raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": getCaptchaExpr, "returnByValue": true})
			if err != nil {
				log.Printf("Session %s: 获取图形验证码失败: %v", s.ID, err)
				return
			}

			var res struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			json.Unmarshal(raw, &res)
			captchaData := res.Result.Value

			if captchaData == "" {
				log.Printf("Session %s: 未找到图形验证码元素或元素内容为空 (%s)", s.ID, action.CaptchaSelector)
				return
			}

			// 2. 将图形验证码和输入框广播到前端
			prompt := "请输入验证码"
			if action.Prompt != "" {
				prompt = action.Prompt
			}
			interactionPayload := map[string]interface{}{
				"type": "user_interaction_required",
				"payload": map[string]string{
					"inputType":   "captcha",
					"captchaData": captchaData,
					"prompt":      prompt,
				},
			}
			msg, _ := json.Marshal(interactionPayload)
			s.broadcast <- msg

			// 3. 等待用户输入
			select {
			case userInput := <-s.userInputChan:
				if action.VariableName != "" {
					variables[action.VariableName] = userInput
				}
				finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
				s.broadcast <- finishMsg
			case <-ctx.Done():
				finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
				s.broadcast <- finishMsg
				return
			}
		case "wait_for_qrcode_scan":
			// 1. 提取二维码的 src 或 data URL
			getQrCodeExpr := fmt.Sprintf(`
				(() => {
					const el = document.querySelector('%s');
					if (!el) return null;
					if (el.tagName.toLowerCase() === 'img') return el.src;
					if (el.tagName.toLowerCase() === 'canvas') return el.toDataURL();
					const svg = el.querySelector('svg');
					if (svg) {
						const xml = new XMLSerializer().serializeToString(svg);
						return 'data:image/svg+xml;base64,' + window.btoa(xml);
					}
					return null; 
				})()
			`, action.Selector)

			raw, err := s.Client.Call("Runtime.evaluate", map[string]interface{}{"expression": getQrCodeExpr, "returnByValue": true})
			if err != nil {
				log.Printf("Session %s: 获取二维码失败: %v", s.ID, err)
				return
			}

			var res struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			json.Unmarshal(raw, &res)
			qrCodeData := res.Result.Value

			if qrCodeData == "" {
				log.Printf("Session %s: 未找到二维码元素或元素内容为空 (%s)", s.ID, action.Selector)
				return
			}

			// 2. 将二维码广播到前端
			prompt := "请使用手机扫码登录"
			if action.Prompt != "" {
				prompt = action.Prompt
			}
			interactionPayload := map[string]interface{}{
				"type": "user_interaction_required",
				"payload": map[string]string{
					"inputType":  "qrcode",
					"qrCodeData": qrCodeData,
					"prompt":     prompt,
				},
			}
			msg, _ := json.Marshal(interactionPayload)
			s.broadcast <- msg

			// 3. 等待扫描完成 (通过轮询成功标识元素)
			timeout := 60 // 默认超时60秒
			if action.Timeout > 0 {
				timeout = action.Timeout
			}
			successPollExpr := fmt.Sprintf("!!document.querySelector(`%s`)", action.SuccessSelector)
			log.Printf("Session %s: 等待扫码登录, 轮询目标元素: %s", s.ID, action.SuccessSelector)
			err = pollForSuccess(ctx, s, successPollExpr, time.Duration(timeout)*time.Second)

			// 4. 通知前端交互结束
			finishMsg, _ := json.Marshal(map[string]interface{}{"type": "user_interaction_finished"})
			s.broadcast <- finishMsg

			if err != nil {
				log.Printf("Session %s: 等待扫码结果时出错: %v", s.ID, err)
			} else {
				log.Printf("Session %s: 扫码成功, 找到目标元素.", s.ID)
			}
		}
	}
}

func del(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	mu.Lock()
	s, ok := sessions[id]
	mu.Unlock()

	if !ok {
		return
	}

	if s.Cmd != nil { // This is a master session, closing it will kill the process.
		s.Client.conn.Close()
	} else if s.ParentClient != nil && s.TargetID != "" { // This is a tab session.
		// Closing the target should automatically close its dedicated websocket,
		// which will trigger the client.onClose for the tab's client.
		s.ParentClient.Call("Target.closeTarget", map[string]interface{}{"targetId": s.TargetID})
	} else {
		s.Client.conn.Close() // Fallback for tab sessions without parent info.
	}
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
	lastLogs := s.lastLogs
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

	// 如果有缓存的日志，也一并发送给新连接的前端
	if len(lastLogs) > 0 {
		for _, logMsg := range lastLogs {
			if err := conn.WriteMessage(websocket.TextMessage, logMsg); err != nil {
				conn.Close()
				return
			}
		}
	}

	s.wsMu.Lock()
	s.WS[conn] = true
	s.wsMu.Unlock()
}

type SessionInfo struct {
	ID           string `json:"id"`
	IsRunning    bool   `json:"isRunning"`
	IsPersistent bool   `json:"isPersistent"`
}

func listSessions(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	var infos []SessionInfo
	for id, s := range sessions {
		s.wsMu.Lock()
		isRunning := s.cancelDSL != nil
		s.wsMu.Unlock()

		infos = append(infos, SessionInfo{ID: id, IsRunning: isRunning, IsPersistent: s.IsPersistent})
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

// --- Batch DSL Management CRUD ---

func apiListBatchDSL(w http.ResponseWriter, r *http.Request) {
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)
	rows, err := db.Query("SELECT id, name, content, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM batch_dsl_scripts WHERE creator_id = ? ORDER BY id DESC", userInfo.OaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var dsls []DSL
	for rows.Next() {
		var d DSL
		if err := rows.Scan(&d.ID, &d.Name, &d.Content, &d.CreatorID, &d.CreatedAt); err == nil {
			dsls = append(dsls, d)
		}
	}
	if dsls == nil {
		dsls = []DSL{}
	}
	json.NewEncoder(w).Encode(dsls)
}

func apiSaveBatchDSL(w http.ResponseWriter, r *http.Request) {
	var dsl DSL
	if err := json.NewDecoder(r.Body).Decode(&dsl); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	if dsl.ID == 0 {
		res, _ := db.Exec("INSERT INTO batch_dsl_scripts (name, content, creator_id) VALUES (?, ?, ?)", dsl.Name, dsl.Content, userInfo.OaID)
		id, _ := res.LastInsertId()
		dsl.ID = int(id)
	} else {
		var owner string
		err := db.QueryRow("SELECT creator_id FROM batch_dsl_scripts WHERE id=?", dsl.ID).Scan(&owner)
		if err != nil || owner != userInfo.OaID {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		db.Exec("UPDATE batch_dsl_scripts SET name=?, content=? WHERE id=? AND creator_id=?", dsl.Name, dsl.Content, dsl.ID, userInfo.OaID)
	}

	var finalDsl DSL
	db.QueryRow("SELECT id, name, content, creator_id, strftime('%Y-%m-%d %H:%M:%S', created_at) FROM batch_dsl_scripts WHERE id=?", dsl.ID).Scan(&finalDsl.ID, &finalDsl.Name, &finalDsl.Content, &finalDsl.CreatorID, &finalDsl.CreatedAt)
	json.NewEncoder(w).Encode(finalDsl)
}

func apiDeleteBatchDSL(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	var owner string
	err := db.QueryRow("SELECT creator_id FROM batch_dsl_scripts WHERE id=?", id).Scan(&owner)
	if err != nil || owner != userInfo.OaID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	db.Exec("DELETE FROM batch_dsl_scripts WHERE id=? AND creator_id=?", id, userInfo.OaID)
	w.Write([]byte(`{"status":"ok"}`))
}

// --- AI / LLM DSL Generator ---

type GenerateDSLRequest struct {
	Prompt string `json:"prompt"`
}

func apiGenerateDSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		http.Error(w, "未配置 LLM_API_KEY 环境变量，无法使用 AI 生成功能。", http.StatusServiceUnavailable)
		return
	}

	baseURL := os.Getenv("LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/chat/completions" // 默认使用 OpenAI，你可以替换为 DeepSeek 等
	}
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	var reqData GenerateDSLRequest
	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil || reqData.Prompt == "" {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	systemPrompt := `你是一个RPA(机器人流程自动化)脚本生成专家。
请根据用户的自然语言指令，生成对应的 JSON 格式的 DSL 脚本。
仅输出合法的 JSON 数组，不要包含任何 markdown 标记 (如 ` + "```" + `json) 或其他说明文字。

支持的常用操作类型 (type) 及其参数如下：
- navigate: {"type": "navigate", "url": "网页地址"}
- input: {"type": "input", "selector": "CSS选择器", "value": "要输入的文本"}
- click: {"type": "click", "selector": "CSS选择器"}
- wait: {"type": "wait", "ms": 毫秒数}
- wait_selector: {"type": "wait_selector", "selector": "CSS选择器", "timeout": 等待超时秒数}
- keypress: {"type": "keypress", "selector": "CSS选择器", "value": "按键名，如Enter"}

示例：
用户：打开百度，搜索“golang”，然后点击搜索按钮
输出：
[
  {"type": "navigate", "url": "https://www.baidu.com"},
  {"type": "wait_selector", "selector": "#kw", "timeout": 5},
  {"type": "input", "selector": "#kw", "value": "golang"},
  {"type": "click", "selector": "#su"}
]`

	payload := map[string]interface{}{
		"model":       model,
		"temperature": 0.1, // 较低的温度以确保输出稳定的 JSON 格式
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": reqData.Prompt},
		},
	}
	bodyBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequest("POST", baseURL, bytes.NewReader(bodyBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		http.Error(w, "调用大模型 API 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var respData struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	// if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil || len(respData.Choices) == 0 {
	if err := json.Unmarshal(body, &respData); err != nil || len(respData.Choices) == 0 {
		http.Error(w, "大模型 API 返回格式解析失败", http.StatusInternalServerError)
		return
	}

	// 清理 Markdown JSON 代码块符号（应对模型不听话的情况）
	content := strings.TrimSpace(respData.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
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
	var content, creatorID string
	err := db.QueryRow("SELECT content, creator_id FROM dsl_scripts WHERE id=?", dslID).Scan(&content, &creatorID)
	if err != nil {
		log.Printf("定时任务执行失败: 无法获取 DSL 脚本 %d: %v", dslID, err)
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

	broadcastSessionEvent("session_added", SessionInfo{ID: id, IsRunning: true, IsPersistent: session.IsPersistent})

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

	executeDSLActions(ctx, session, actions, make(map[string]string), creatorID)
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

// --- 密码保险箱 CRUD ---

// Secret 定义了密码保险箱中的一个条目
type Secret struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`       // 在列出时不返回 value
	Description string `json:"description,omitempty"` // 说明
}

func apiListSecrets(w http.ResponseWriter, r *http.Request) {
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)
	rows, err := db.Query("SELECT name, description FROM secrets WHERE creator_id = ? ORDER BY name", userInfo.OaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		var s Secret
		if err := rows.Scan(&s.Name, &s.Description); err == nil {
			secrets = append(secrets, s)
		}
	}
	if secrets == nil {
		secrets = []Secret{}
	}
	json.NewEncoder(w).Encode(secrets)
}

func apiSaveSecret(w http.ResponseWriter, r *http.Request) {
	if len(encryptionKey) == 0 {
		http.Error(w, "密码保险箱功能未在服务器上配置。", http.StatusServiceUnavailable)
		return
	}

	var secret Secret
	if err := json.NewDecoder(r.Body).Decode(&secret); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if secret.Name == "" || secret.Value == "" {
		http.Error(w, "密码的名称和值不能为空", http.StatusBadRequest)
		return
	}

	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	encryptedValue, err := encrypt(secret.Value)
	if err != nil {
		http.Error(w, "加密密码失败", http.StatusInternalServerError)
		return
	}

	// 使用 INSERT OR REPLACE (UPSERT) 逻辑
	_, err = db.Exec("INSERT INTO secrets (name, value, description, creator_id) VALUES (?, ?, ?, ?) ON CONFLICT(name, creator_id) DO UPDATE SET value=excluded.value, description=excluded.description",
		secret.Name, encryptedValue, secret.Description, userInfo.OaID)

	if err != nil {
		http.Error(w, "保存密码失败", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

func apiDeleteSecret(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "缺少密码名称参数", http.StatusBadRequest)
		return
	}
	userInfo := r.Context().Value(UserInfoKey{}).(UserInfo)

	_, err := db.Exec("DELETE FROM secrets WHERE name=? AND creator_id=?", name, userInfo.OaID)
	if err != nil {
		http.Error(w, "删除密码失败", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
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
