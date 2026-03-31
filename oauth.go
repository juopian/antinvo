package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
)

const (
	clientID = "boston" // Replace with your actual client ID
)

// UserInfoKey is the key for storing UserInfo in a request context.
type UserInfoKey struct{}

// UserInfo 对应 OAuth2 接口返回中的 user_info 字段
type UserInfo struct {
	UniEmail string `json:"uni_email"`
	OaID     string `json:"oa_id"`
	FullName string `json:"full_name"`
	Email    string `json:"email"`
	Mobile   string `json:"mobile"`
	Duty     string `json:"duty"`
	BcmiID   int    `json:"bcmi_id"`
}

// OAuth2UserInfoResponse 对应 OAuth2 获取用户资料接口的完整响应
type OAuth2UserInfoResponse struct {
	ClientID          string   `json:"client_id"`
	Sub               string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Email             string   `json:"email"`
	OpenID            string   `json:"openid"`
	UserInfo          UserInfo `json:"user_info"`
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	state := generateRandomString(8)
	nonce := generateRandomString(4)

	// 将 state 写入 cookie 以便在 callback 中进行 CSRF 强校验
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
	})

	// 动态获取当前的 Scheme 和 Host 拼接回调地址
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	dynamicRedirectURI := fmt.Sprintf("%s://%s/callback", scheme, r.Host)

	v := url.Values{}
	v.Set("response_type", "id_token")
	v.Set("client_id", clientID)
	v.Set("redirect_uri", dynamicRedirectURI)
	v.Set("state", state)
	v.Set("nonce", nonce)
	v.Set("scope", "")

	authorizationEndpoint := os.Getenv("OAUTH2_AUTH_ENDPOINT")
	if authorizationEndpoint == "" {
		http.Error(w, "Authorization endpoint not configured", http.StatusInternalServerError)
		return
	}

	authURL := authorizationEndpoint + "?" + v.Encode()
	fmt.Println("Redirecting to:", authURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func callbackHandler(w http.ResponseWriter, r *http.Request) {
	// Handle the callback from the authorization server
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	idToken := r.FormValue("id_token")
	state := r.FormValue("state")

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || state != stateCookie.Value {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	if idToken == "" {
		http.Error(w, "Missing id_token", http.StatusBadRequest)
		return
	}

	// 获取用户资料
	userInfoEndpoint := os.Getenv("OAUTH2_USERINFO_ENDPOINT")
	if userInfoEndpoint == "" {
		http.Error(w, "User info endpoint not configured", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest("GET", userInfoEndpoint, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+idToken)

	// 创建一个自定义的 Transport 来禁用 HTTP/2，这通常能解决与某些服务器通信时遇到的 "EOF" 错误
	tr := &http.Transport{
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error fetching user info:", err)
		http.Error(w, "Failed to fetch user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Failed to read user info", http.StatusInternalServerError)
		return
	}

	var userResp OAuth2UserInfoResponse
	if err := json.Unmarshal(body, &userResp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 生成 session ID 并保存用户信息到内存和数据库
	sessionID := generateRandomString(32)

	userInfoJSON, err := json.Marshal(userResp.UserInfo)
	if err != nil {
		http.Error(w, "无法序列化用户信息", http.StatusInternalServerError)
		return
	}

	// 持久化到数据库
	_, err = db.Exec("INSERT INTO user_sessions (session_id, user_info) VALUES (?, ?)", sessionID, string(userInfoJSON))
	if err != nil {
		log.Printf("保存用户会话到数据库失败: %v", err)
		http.Error(w, "无法保存会话", http.StatusInternalServerError)
		return
	}
	userSessions.Store(sessionID, userResp.UserInfo)

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true, // 仅HTTP，防止客户端脚本访问
	})

	// 认证成功后重定向回首页
	http.Redirect(w, r, "/", http.StatusFound)
}

// authMiddleware 验证用户是否已登录
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized: Missing session cookie", http.StatusUnauthorized)
			return
		}

		sessionID := cookie.Value
		userInfo, ok := userSessions.Load(sessionID)
		if !ok {
			http.Error(w, "Unauthorized: Invalid session", http.StatusUnauthorized)
			return
		}

		// 将用户信息添加到请求上下文中，以便后续处理程序使用
		ctx := context.WithValue(r.Context(), UserInfoKey{}, userInfo)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// 获取当前登录用户信息
func userInfoHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	val, ok := userSessions.Load(cookie.Value)
	if !ok {
		http.Error(w, "Invalid session", http.StatusUnauthorized)
		return
	}

	userInfo, ok := val.(UserInfo)
	if !ok {
		http.Error(w, "Invalid user info", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userInfo)
}

// 登出处理
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		sessionID := cookie.Value
		userSessions.Delete(sessionID) // 从内存中清除 Session

		// 从数据库中清除 Session
		_, err := db.Exec("DELETE FROM user_sessions WHERE session_id = ?", sessionID)
		if err != nil {
			log.Printf("从数据库删除会话失败: %v", err)
		}
	}

	// 清除客户端 Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1, // 设为负数立刻过期
		HttpOnly: true,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

// loadUserSessions 从数据库加载所有用户会话到内存中
func loadUserSessions() {
	rows, err := db.Query("SELECT session_id, user_info FROM user_sessions")
	if err != nil {
		log.Printf("启动时加载用户会话失败: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var sessionID string
		var userInfoJSON string
		if err := rows.Scan(&sessionID, &userInfoJSON); err != nil {
			log.Printf("扫描会话行失败: %v", err)
			continue
		}

		var userInfo UserInfo
		if err := json.Unmarshal([]byte(userInfoJSON), &userInfo); err != nil {
			log.Printf("解析会话中的用户信息失败: %v", err)
			continue
		}
		userSessions.Store(sessionID, userInfo)
		count++
	}
	log.Printf("从数据库加载了 %d 个用户会话。", count)
}
