package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"sync"

	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

var userSessions sync.Map

func main() {
	const port = ":8080"

	initDB()                // 初始化数据库表
	loadUserSessions()      // 从数据库加载用户会话
	initPersistentDirPool() // 初始化持久化目录池
	go runGlobalWsHub()     // 启动全局事件广播 Hub
	initCronTasks()         // 初始化定时任务表并启动活跃任务

	// 初始化密码保险箱
	encryptionKeyStr := os.Getenv("APP_ENCRYPTION_KEY")
	if encryptionKeyStr == "" {
		log.Println("警告: 环境变量 APP_ENCRYPTION_KEY 未设置。密码保险箱功能将不可用，且相关API会返回错误。")
	} else {
		if err := initEncryption(encryptionKeyStr); err != nil {
			log.Fatalf("启动失败: 无效的加密密钥: %v", err)
		}
		log.Println("密码保险箱功能已启用。")
	}

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
	http.HandleFunc("/run_dsl_bulk", runDSLBulk) // 新增：批量执行 DSL 接口
	http.HandleFunc("/run_dsl", runDSL)
	http.HandleFunc("/stop_dsl", stopDSL)
	http.HandleFunc("/user_input", userInput)
	http.HandleFunc("/api/dsl/list", authMiddleware(apiListDSL))
	http.HandleFunc("/api/dsl/save", authMiddleware(apiSaveDSL))
	http.HandleFunc("/api/dsl/delete", authMiddleware(apiDeleteDSL))

	// 批量 DSL API
	http.HandleFunc("/api/batch_dsl/list", authMiddleware(apiListBatchDSL))
	http.HandleFunc("/api/batch_dsl/save", authMiddleware(apiSaveBatchDSL))
	http.HandleFunc("/api/batch_dsl/delete", authMiddleware(apiDeleteBatchDSL))

	http.HandleFunc("/api/generate_dsl", apiGenerateDSL) // 允许不强制登录调用，或者按需包上 authMiddleware

	http.HandleFunc("/api/cron/list", authMiddleware(apiListCron))
	http.HandleFunc("/api/cron/save", authMiddleware(apiSaveCron))
	http.HandleFunc("/api/cron/delete", authMiddleware(apiDeleteCron))
	http.HandleFunc("/api/cron/toggle", authMiddleware(apiToggleCron))

	// 密码保险箱 API
	http.HandleFunc("/api/secret/list", authMiddleware(apiListSecrets))
	http.HandleFunc("/api/secret/save", authMiddleware(apiSaveSecret))
	http.HandleFunc("/api/secret/delete", authMiddleware(apiDeleteSecret))

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
