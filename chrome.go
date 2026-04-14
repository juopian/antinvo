package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

func getChromePath() (string, error) {
	var paths []string
	switch runtime.GOOS {
	case "linux": // Add common paths for Docker containers
		paths = []string{"/usr/bin/google-chrome", "/usr/bin/chromium-browser", "/usr/bin/chrome", "/opt/google/chrome/chrome", "/usr/bin/google-chrome-stable"}
	case "darwin":
		paths = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	case "windows":
		paths = []string{
			"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
			"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
			os.Getenv("USERPROFILE") + "\\AppData\\Local\\Google\\Chrome\\Application\\chrome.exe",
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Check if CHROME_PATH environment variable is set (useful for Docker or custom setups)
	if envPath := os.Getenv("CHROME_PATH"); envPath != "" {
		paths = append([]string{envPath}, paths...)
	}

	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		return path, nil
	}

	return "", fmt.Errorf("cannot find Chrome executable in default paths")
}

func launchChrome(chromePath string, port int, dir string) *exec.Cmd {
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", dir),
		"--headless=new",
		"--window-size=1500,1000",     // 设置窗口尺寸以匹配前端卡片6:4的宽高比，避免滚动条
		"--hide-scrollbars",           // 强制隐藏滚动条
		"--disable-gpu",               // 在无头模式下通常建议禁用 GPU 以提高稳定性
		"--ignore-certificate-errors", // 在某些企业代理环境下，这可能是必需的
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-popup-blocking",                           // 禁用弹出窗口拦截
		"--disable-web-security",                             // 禁用同源策略，允许跨域访问 iframe
		"--disable-site-isolation-trials",                    // 禁用站点隔离
		"--disable-features=IsolateOrigins,site-per-process", // 彻底允许跨域 iframe 的 DOM 穿透
		"--disable-background-timer-throttling",              // 禁用后台定时器节流
		"--disable-backgrounding-occluded-windows",           // 禁用遮挡窗口后台化
		"--disable-renderer-backgrounding",                   // 彻底禁用渲染器后台运行限制
		"--disable-ipc-flooding-protection",                  // 禁用 IPC 泛洪保护，防止高频通信被掐断
		"--disable-blink-features=AutomationControlled",      // 隐藏 navigator.webdriver 标志，防止 API 接口被反爬拦截
	}
	if proxyServer := os.Getenv("HTTP_PROXY"); proxyServer != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", proxyServer))
	} else if proxyServer := os.Getenv("HTTPS_PROXY"); proxyServer != "" {
		args = append(args, fmt.Sprintf("--proxy-server=%s", proxyServer))
	}
	cmd := exec.Command(
		chromePath,
		args...,
	)
	cmd.Start()
	return cmd
}

func getWSURL(port int) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/json/list", port)
	var resp *http.Response
	var err error

	// 轮询等待浏览器启动，最多等待 5 秒 (50 * 100ms)
	for i := 0; i < 50; i++ {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err != nil {
		return "", fmt.Errorf("无法连接到 Chrome: %w", err)
	}
	defer resp.Body.Close()

	var pages []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}

	for _, page := range pages {
		if page["type"] == "page" {
			if wsURL, ok := page["webSocketDebuggerUrl"].(string); ok {
				return wsURL, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 webSocketDebuggerUrl")
}

func getWSURLForTarget(port int, targetId string) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/json/list", port)
	var resp *http.Response
	var err error

	// It might take a moment for the new target to appear in the list
	for i := 0; i < 20; i++ {
		resp, err = http.Get(url)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		var pages []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("解析 JSON 失败: %w", err)
		}
		resp.Body.Close()

		for _, page := range pages {
			if page["type"] == "page" {
				if id, ok := page["id"].(string); ok && id == targetId {
					if wsURL, ok := page["webSocketDebuggerUrl"].(string); ok {
						return wsURL, nil
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	return "", fmt.Errorf("未找到 targetId %s 的 webSocketDebuggerUrl", targetId)
}
