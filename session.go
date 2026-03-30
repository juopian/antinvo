package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"

	"github.com/gorilla/websocket"
)

type Session struct {
	ID          string
	Client      *Client
	Cmd         *exec.Cmd
	UserDataDir string
	WS          map[*websocket.Conn]bool
	wsMu        sync.Mutex
	lastFrame   string
	cancelDSL   context.CancelFunc
	broadcast   chan []byte
}

var sessions = map[string]*Session{}
var mu sync.Mutex
var nextPort = 9000
var chromeExecutablePath string

func deleteSession(id string) {
	mu.Lock()
	defer mu.Unlock()

	if s, ok := sessions[id]; ok {
		close(s.broadcast)
		s.Client.conn.Close()
		if s.Cmd != nil && s.Cmd.Process != nil {
			s.Cmd.Process.Kill()
		}

		// Delete user data directory
		if s.UserDataDir != "" {
			os.RemoveAll(s.UserDataDir)
			log.Printf("已删除临时数据目录: %s", s.UserDataDir)
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

func (s *Session) broadcastLoop() {
	for message := range s.broadcast {
		s.wsMu.Lock()
		for ws := range s.WS {
			// NOTE: This write happens inside the lock. For a large number of clients,
			// it might be better to copy the list of connections and release the lock.
			// But for this application, this is simpler and likely sufficient.
			if err := ws.WriteMessage(websocket.TextMessage, message); err != nil {
				ws.Close()
				delete(s.WS, ws)
			}
		}
		s.wsMu.Unlock()
	}
}
