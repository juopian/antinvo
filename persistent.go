package main

import (
	"log"
	"os"
	"path/filepath"
)

const persistentDataPath = "./persistent_data"
const maxPersistentSessions = 50 // Max number of persistent sessions to pool

// persistentDirPool holds available user data directories for reuse.
var persistentDirPool chan string

func initPersistentDirPool() {
	persistentDirPool = make(chan string, maxPersistentSessions)

	// Ensure the base directory for persistent data exists.
	if err := os.MkdirAll(persistentDataPath, 0755); err != nil {
		log.Fatalf("无法创建持久化数据目录: %v", err)
	}

	// Scan for existing directories to populate the pool.
	// This is useful if the application restarts and we want to reuse old sessions.
	items, err := os.ReadDir(persistentDataPath)
	if err != nil {
		log.Printf("扫描持久化目录失败: %v", err)
		return
	}

	log.Println("正在扫描可复用的持久化浏览器数据...")
	for _, item := range items {
		if item.IsDir() {
			// A simple check to see if the session is active is to see if a lockfile exists.
			// Chrome creates a "SingletonLock" file. If it's not there, we can probably reuse it.
			lockfilePath := filepath.Join(persistentDataPath, item.Name(), "SingletonLock")
			if _, err := os.Stat(lockfilePath); os.IsNotExist(err) {
				fullPath := filepath.Join(persistentDataPath, item.Name())
				select {
				case persistentDirPool <- fullPath:
					log.Printf("发现可用目录，已添加到池中: %s", fullPath)
				default:
					log.Printf("持久化目录池已满，跳过: %s", fullPath)
				}
			}
		}
	}
	log.Printf("持久化目录池初始化完毕，可用数量: %d", len(persistentDirPool))
}
