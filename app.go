package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
	// 初始化数据库
	initDB()
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
	http.HandleFunc("/navigate", navigate)
	http.HandleFunc("/delete", del)
	http.HandleFunc("/ws/events", globalEventsWsHandler)
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/list", listSessions)
	http.HandleFunc("/click", click)
	http.HandleFunc("/input", input)
	http.HandleFunc("/selectOption", selectOption)
	http.HandleFunc("/run_dsl", runDSL)
	http.HandleFunc("/stop_dsl", stopDSL)
	http.HandleFunc("/api/dsl/list", apiListDSL)
	http.HandleFunc("/api/dsl/save", apiSaveDSL)
	http.HandleFunc("/api/dsl/delete", apiDeleteDSL)
	http.HandleFunc("/api/cron/list", apiListCron)
	http.HandleFunc("/api/cron/save", apiSaveCron)
	http.HandleFunc("/api/cron/delete", apiDeleteCron)
	http.HandleFunc("/api/cron/toggle", apiToggleCron)

	log.Println("http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
