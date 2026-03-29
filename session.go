package main

import (
	"context"
	"log"
	"os/exec"
	"sync"

	"github.com/gorilla/websocket"
)

type Session struct {
	ID        string
	Client    *Client
	Cmd       *exec.Cmd
	WS        map[*websocket.Conn]bool
	wsMu      sync.Mutex
	lastFrame string
	cancelDSL context.CancelFunc
}

var sessions = map[string]*Session{}
var mu sync.Mutex
var nextPort = 9000
var chromeExecutablePath string

func deleteSession(id string) {
	mu.Lock()
	defer mu.Unlock()

	if s, ok := sessions[id]; ok {
		s.Client.conn.Close()
		if s.Cmd != nil && s.Cmd.Process != nil {
			s.Cmd.Process.Kill()
		}

		s.wsMu.Lock()
		for ws := range s.WS {
			ws.Close()
		}
		s.wsMu.Unlock()

		delete(sessions, id)
		log.Println("已清理关闭的浏览器资源, Session ID:", id)
	}
}
