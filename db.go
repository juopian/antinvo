package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var db *sql.DB

type DSL struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
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
		content TEXT NOT NULL
	);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Fatalf("创建数据库表失败: %q", err)
	}
}
