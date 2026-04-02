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

	// 新增：批量DSL脚本表
	batchDslTableStmt := `
	CREATE TABLE IF NOT EXISTS batch_dsl_scripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		content TEXT NOT NULL,
		creator_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	_, err = db.Exec(batchDslTableStmt)
	if err != nil {
		log.Fatalf("创建批量DSL脚本表失败: %q", err)
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

	// 新增：创建密码保险箱表
	secretsTableStmt := `
	CREATE TABLE IF NOT EXISTS secrets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		value TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		creator_id TEXT NOT NULL,
		UNIQUE(name, creator_id)
	);
	`
	_, err = db.Exec(secretsTableStmt)
	if err != nil {
		log.Fatalf("创建密码保险箱表失败: %q", err)
	}

	// --- 数据库迁移逻辑 ---
	// 检查 secrets 表是否缺少 description 字段，如果缺少则添加
	rows, err := db.Query("PRAGMA table_info(secrets)")
	if err != nil {
		log.Fatalf("检查 secrets 表结构失败: %q", err)
	}

	var hasDescriptionColumn bool
	for rows.Next() {
		var (
			cid        int
			name       string
			type_      string
			notnull    int
			dflt_value sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &type_, &notnull, &dflt_value, &pk); err == nil && name == "description" {
			hasDescriptionColumn = true
			break
		}
	}
	rows.Close()

	if !hasDescriptionColumn {
		log.Println("数据库迁移: 为 secrets 表添加 description 字段...")
		_, err = db.Exec("ALTER TABLE secrets ADD COLUMN description TEXT NOT NULL DEFAULT ''")
		if err != nil {
			log.Fatalf("为 secrets 表添加 description 字段失败: %q", err)
		}
	}
}
