package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type CDPRequest struct {
	ID     int         `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

type pendingRequest struct {
	ch     chan map[string]json.RawMessage
	method string
}

type Client struct {
	conn    *websocket.Conn        // WebSocket 连接
	nextID  int                    // 下一个请求 ID
	pending map[int]pendingRequest // 待处理的请求及其方法
	mu      sync.Mutex             // 保护 pending 和 nextID
	writeMu sync.Mutex             // 保护 conn.WriteJSON

	onEvent    func(string, json.RawMessage)
	onCall     func(id int, method string, params interface{})                     // 收到发出的 CDP Call 时回调
	onResponse func(id int, method string, rawResponse map[string]json.RawMessage) // 收到 CDP 响应时回调，带上原始方法名
	onClose    func()
}

func NewClient(wsURL string) (*Client, error) {
	if wsURL == "" {
		return nil, fmt.Errorf("websocket URL is empty")
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:    conn,
		pending: make(map[int]pendingRequest),
	}

	go c.readLoop()
	go c.keepAlive()
	return c, nil
}

func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	// 存储请求的 ID 和对应的方法
	c.pending[id] = pendingRequest{ch: make(chan map[string]json.RawMessage, 1), method: method}
	c.mu.Unlock()

	if c.onCall != nil && method != "Page.screencastFrameAck" {
		c.onCall(id, method, params)
	}

	c.writeMu.Lock()
	err := c.conn.WriteJSON(CDPRequest{ID: id, Method: method, Params: params})
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-c.pending[id].ch: // 从通道读取响应
		if errRaw, ok := resp["error"]; ok {
			return nil, fmt.Errorf("CDP error: %s", string(errRaw))
		}
		return resp["result"], nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout: %s", method)
	}
}

func (c *Client) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.writeMu.Lock()
		err := c.conn.WriteMessage(websocket.PingMessage, nil)
		c.writeMu.Unlock()
		if err != nil {
			return // 连接已断开，退出心跳协程
		}
	}
}

func (c *Client) readLoop() {
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if c.onClose != nil {
				c.onClose()
			}
			return
		}

		var raw map[string]json.RawMessage
		json.Unmarshal(data, &raw)

		if idRaw, ok := raw["id"]; ok {
			var id int
			json.Unmarshal(idRaw, &id)

			if c.onResponse != nil {
				c.mu.Lock() // 需要先锁定，以便访问 pending map 获取 method
				pendingReq, ok := c.pending[id]
				c.mu.Unlock()
				if ok {
					// 仅在找到了对应 ID 的请求时，才触发 onResponse
					c.onResponse(id, pendingReq.method, raw)
				}
			}
			c.mu.Lock() // 再次锁定以更新 pending map
			if pendingReq, ok := c.pending[id]; ok {
				pendingReq.ch <- raw
				// ch <- raw
				delete(c.pending, id)
			}
			c.mu.Unlock()
			continue
		}

		if methodRaw, ok := raw["method"]; ok {
			var method string
			json.Unmarshal(methodRaw, &method)

			if c.onEvent != nil {
				c.onEvent(method, raw["params"])
			}
		}
	}
}
