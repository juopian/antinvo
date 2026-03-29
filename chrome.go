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
	case "darwin":
		paths = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
	case "linux":
		paths = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	case "windows":
		paths = []string{
			"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
			"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
			os.Getenv("USERPROFILE") + "\\AppData\\Local\\Google\\Chrome\\Application\\chrome.exe",
		}
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
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
	cmd := exec.Command(
		chromePath,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", dir),
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
	)
	cmd.Start()
	return cmd
}

func getWSURL(port int) (string, error) {
	url := fmt.Sprintf("http://localhost:%d/json", port)
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
