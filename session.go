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
	ID            string
	Port          int
	ParentClient  *Client
	TargetID      string
	IsPersistent  bool
	Client        *Client
	Cmd           *exec.Cmd
	UserDataDir   string
	WS            map[*websocket.Conn]bool
	wsMu          sync.Mutex
	lastFrame     string
	lastLogs      [][]byte
	isDslRunning  bool
	userInputChan chan string
	cancelDSL     context.CancelFunc
	broadcast     chan []byte
}

var sessions = map[string]*Session{}
var mu sync.Mutex
var nextPort = 9000
var chromeExecutablePath string

func deleteSession(id string) {
	mu.Lock()
	s, ok := sessions[id]
	if !ok {
		mu.Unlock()
		return
	}
	delete(sessions, id)
	mu.Unlock()

	close(s.broadcast)
	// The connection is already closed or closing, which is what triggers this function.

	if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
	}

	// Delete user data directory
	if s.UserDataDir != "" {
		if s.IsPersistent {
			// Only master sessions (which own a Cmd) return their dir to the pool.
			if s.Cmd != nil {
				select {
				case persistentDirPool <- s.UserDataDir:
					log.Printf("已释放持久化数据目录到池中: %s", s.UserDataDir)
				default:
					log.Printf("持久化目录池已满，未释放: %s", s.UserDataDir)
				}
			}
		} else {
			os.RemoveAll(s.UserDataDir)
			log.Printf("已删除临时数据目录: %s", s.UserDataDir)
		}
	}

	s.wsMu.Lock()
	for ws := range s.WS {
		ws.Close()
	}
	s.wsMu.Unlock()

	log.Println("已清理关闭的浏览器资源, Session ID:", id)
	broadcastSessionEvent("session_removed", map[string]string{"id": id})
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
