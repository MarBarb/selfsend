# SelfSend Windows

SelfSend 的轻量 Windows 客户端，使用 Tauri 2、Vue 3 和系统 WebView2。

## 开发

需要 Node.js 24+、Rust stable、Microsoft C++ Build Tools 和 WebView2 Runtime：

```powershell
cd windows\SelfSend
npm install
npm run dev
```

## 构建安装包

```powershell
npm run build
```

NSIS 安装包会生成到 `src-tauri\target\release\bundle\nsis`。正式发布前必须为安装包和可执行文件配置 Windows 代码签名。

## 第一版能力

- 服务器地址和邀请链接连接
- 初始化、登录、设备注册
- 会话列表、文字消息、文件上传和下载
- 4 MiB 分片上传与中断续传
- SSE 后台事件和 Windows 原生通知
- 系统托盘、单实例、关闭窗口后继续运行
- 用户登录后自动启动；后台启动不创建 WebView 窗口

Windows Credential Manager 保存会话凭证，普通设置保存在当前用户的应用配置目录中。
