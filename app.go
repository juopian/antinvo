package main

import (
	"embed"
	"io/fs"
	"log"
	"sync"

	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

var userSessions sync.Map

func main() {
	const port = ":8080"

	initDB()            // 初始化DSL
	go runGlobalWsHub() // 启动全局事件广播 Hub
	initCronTasks()     // 初始化定时任务表并启动活跃任务

	var err error
	chromeExecutablePath, err = getChromePath()
	if err != nil {
		log.Fatalf("启动失败: 找不到 Chrome 浏览器。请确保已安装或检查 chrome.go 中的路径。错误: %v", err)
	}
	log.Printf("找到 Chrome 浏览器: %s", chromeExecutablePath)

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", http.FileServer(http.FS(staticFS)))
	http.HandleFunc("/create", create)
	http.HandleFunc("/delete", del)
	http.HandleFunc("/ws/events", globalEventsWsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/list", listSessions)
	http.HandleFunc("/run_dsl", runDSL)
	http.HandleFunc("/stop_dsl", stopDSL)
	http.HandleFunc("/api/dsl/list", authMiddleware(apiListDSL))
	http.HandleFunc("/api/dsl/save", authMiddleware(apiSaveDSL))
	http.HandleFunc("/api/dsl/delete", authMiddleware(apiDeleteDSL))
	http.HandleFunc("/api/cron/list", authMiddleware(apiListCron))
	http.HandleFunc("/api/cron/save", authMiddleware(apiSaveCron))
	http.HandleFunc("/api/cron/delete", authMiddleware(apiDeleteCron))
	http.HandleFunc("/api/cron/toggle", authMiddleware(apiToggleCron))

	// OAuth2 Handlers
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/callback", callbackHandler)
	http.HandleFunc("/logout", logoutHandler)
	http.HandleFunc("/api/user/info", userInfoHandler) // Public API to check login status

	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatal(err)
	} else {
		log.Printf("Server listening on port %s", port)
	}
}
