# Antinvo - Remote Chrome Control

Antinvo 是一个基于 Go 语言和 Chrome DevTools Protocol (CDP) 实现的远程浏览器控制服务。它允许你通过 Web 界面创建多个 Chrome 浏览器实例，实时查看浏览器画面（Screencast），并与页面进行交互（如跳转、点击、输入等）。

## 特性 (Features)

- **实时画面回传**：通过 WebSocket 实时接收 Chrome 的屏幕广播流。
- **多会话管理**：支持同时创建和管理多个独立的 Chrome 浏览器实例。
- **DOM 交互**：支持通过 CSS 选择器在页面中填入内容、点击按钮以及选择下拉框（Select）。
- **单文件部署**：前端静态文件已通过 `//go:embed` 嵌入，编译后只需一个独立的可执行文件即可运行。
- **自动资源清理**：浏览器关闭或崩溃时，后端能自动回收 WebSocket 连接和释放端口资源。

## 环境要求 (Prerequisites)

1. **Go 环境**：Go 1.16 或更高版本（本项目基于 Go 1.26.1）。
2. **Google Chrome**：运行该程序的宿主机上必须已安装 Google Chrome 浏览器。

> **⚠️ 重要提示：跨平台运行前请修改 Chrome 路径**  
## 本地运行和编译 (Development and Build)

在项目根目录下，直接运行所有相关的 Go 文件：

```bash
go run .
```
程序启动后，访问 `http://localhost:8080` 即可看到操作界面。

## 编译指南 (Build & Cross-Compile)
在项目根目录下，执行go build .
得益于 Go 语言强大的交叉编译能力，你可以很容易地将程序编译到不同的操作系统和架构。前端静态资源已经被自动打包进二进制文件中。

### 编译到 macOS

**Apple Silicon (M1/M2/M3)**:
```bash
GOOS=darwin GOARCH=arm64 go build -o antinvo-mac-arm64 .
```

**Intel Mac**:
```bash
GOOS=darwin GOARCH=amd64 go build -o antinvo-mac-amd64 .
```

### 编译到 Linux

```bash
GOOS=linux GOARCH=amd64 go build -o antinvo-linux-amd64 .
```

### 编译到 Windows


GOOS=windows GOARCH=amd64 go build -o antinvo-windows.exe .
```

## 使用说明 (Usage)

1. **启动服务**：运行编译后的可执行文件（如 `./antinvo-mac-arm64`）。
2. **访问控制台**：打开浏览器访问 `http://localhost:8080`。
3. **创建实例**：点击页面上的 **“新建浏览器”** 按钮，后端将启动一个独立的 Chrome 进程。
4. **页面导航**：
   - 在“输入网址...”框中输入带协议的完整 URL（例如 `https://www.baidu.com`）。
   - 点击 **“跳转”** 按钮。
5. **页面交互**：
   - **填入内容**：在“CSS选择器”框输入目标输入框的选择器（如 `#kw`），在“输入内容”框填入文字，点击 **“填入内容”**。
   - **点击元素**：在“CSS选择器”框输入目标按钮的选择器（如 `#su`），点击 **“点击元素”**。
   - **选择Option**：在“CSS选择器”框输入 `<select>` 的选择器，在“Option Value”框输入目标 `<option>` 的 value 值，点击 **“选择Option”**。

### build image 

docker build --platform=linux/amd64 -t antinvo-go:latest .

docker run -p 8081:8080 \
  --platform=linux/amd64 \
  -e OAUTH2_AUTH_ENDPOINT="YOUR_AUTH_ENDPOINT" \
  -e OAUTH2_USERINFO_ENDPOINT="YOUR_USERINFO_ENDPOINT" \
  antinvo-go:latest

### sample DSL

`
[
  {
    "type": "navigate",
    "url": "https://www.baidu.com/"
  },
  {
    "type": "wait",
    "ms": 300
  },
  {
    "type": "click",
    "selector": "a[name=tj_login]"
  },
  {
    "type": "wait",
    "ms": 1000
  },
  {
    "type": "click",
    "selector": ".login-type-tab .switch-item:last-child"
  },
  {
    "type": "input",
    "selector": ".pass-text-input-smsPhone",
    "value": "18620011202"
  },
  {
    "type": "click",
    "selector": "button.pass-item-timer"
  },
  {
    "type": "wait",
    "ms": 1000
  },
  {
    "type": "wait_for_input",
    "inputType": "prompt",
    "prompt": "请输入6位短信验证码",
    "variableName": "smsCode"
  },
  {
    "type": "input",
    "selector": ".pass-text-input-smsVerifyCode",
    "value": "{{smsCode}}"
  },
  {
    "type": "click",
    "selector": ".pass-button-submit"
  }
]
`