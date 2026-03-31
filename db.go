package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var db *sql.DB

type DSL struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	CreatorID string `json:"creatorId,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func initDB() {
	var err error
	// 数据库文件将存放在当前运行目录
	db, err = sql.Open("sqlite", "./antinvo.db")
	if err != nil {
		log.Fatal("无法连接数据库:", err)
	}

	// 自动建表
	sqlStmt := `
	CREATE TABLE IF NOT EXISTS dsl_scripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		content TEXT NOT NULL,
		creator_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Fatalf("创建数据库表失败: %q", err)
	}

	// 新增：创建用户会话表
	sessionTableStmt := `
	CREATE TABLE IF NOT EXISTS user_sessions (
		session_id TEXT PRIMARY KEY,
		user_info TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err = db.Exec(sessionTableStmt)
	if err != nil {
		log.Fatalf("创建用户会话表失败: %q", err)
	}
}
