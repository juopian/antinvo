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

type Client struct {
	conn    *websocket.Conn
	nextID  int
	pending map[int]chan map[string]json.RawMessage
	mu      sync.Mutex
	writeMu sync.Mutex

	onEvent func(string, json.RawMessage)
	onClose func()
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
		pending: make(map[int]chan map[string]json.RawMessage),
	}

	go c.readLoop()
	go c.keepAlive()
	return c, nil
}

func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan map[string]json.RawMessage, 1)
	//print id and params
	fmt.Println("call: id:", id, "method:", method, "params:", params)
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	err := c.conn.WriteJSON(CDPRequest{ID: id, Method: method, Params: params})
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
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

			fmt.Println("data:", string(data))

			c.mu.Lock()
			if ch, ok := c.pending[id]; ok {
				ch <- raw
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
