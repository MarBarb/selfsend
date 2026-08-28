<div align="center">
  <img src="frontend/public/icon.svg" width="88" alt="SelfSend logo">
  <h1>SelfSend</h1>
  <p><strong>像聊天一样，给自己发文件。</strong></p>
  <p>A tiny, self-hosted personal file timeline. No app store, no account service, no official cloud.</p>
</div>

SelfSend 把“文件传输助手”的体验从聊天软件里单独拿出来：在手机或电脑浏览器中选择文件，它就会出现在一条只属于你的时间线上。项目只提供软件，不运营任何文件存储或中转服务。

> **当前状态：v0.1 开发预览。** 已经可以部署和传文件，但在公开到互联网前，请先阅读下面的安全说明。

## 特点

- 手机、平板、电脑都使用响应式网页，无需上架 App Store
- PWA 支持，可添加到主屏幕
- 单用户设计，没有开放注册和多租户复杂度
- 4 MiB 分片上传，网络中断后重新选择同一文件可以续传
- 文件时间线、图片预览、Range 下载、永久删除
- Go 单二进制，Vue 前端直接嵌入其中
- 默认 SQLite + 本地磁盘，一个数据目录即可备份
- 不连接 SelfSend 官方服务器，不包含遥测

## 30 秒启动

需要 Docker 和 Docker Compose：

```bash
git clone https://github.com/MarBarb/selfsend.git
cd selfsend
docker compose up -d --build
```

打开 `http://localhost:8080`，首次进入时创建管理员密码。局域网中的其他设备可通过运行 SelfSend 的电脑 IP 访问，例如 `http://192.168.1.20:8080`。

首次构建需要下载 Go 模块。`compose.yaml` 在大陆网络下默认使用 `https://goproxy.cn,direct`；如果需要使用官方模块代理：

```bash
GOPROXY=https://proxy.golang.org,direct docker compose up -d --build
```

如果看到 `Docker Compose requires buildx plugin`，这是 Docker 回退到旧构建器的警告，不会阻止构建。安装 buildx 可以消除警告并获得更好的缓存，但不是运行 SelfSend 的必要条件。

数据保存在仓库目录下的 `data/` 中：

```text
data/
├── selfsend.db       # 设置、会话和时间线
├── selfsend.db-wal
├── uploads/          # 未完成的上传
└── blobs/            # 已完成的文件
```

升级前请完整备份这个目录，包括可能存在的 `-wal` 和 `-shm` 文件。

## 公网部署与 HTTPS

不要把 `8080` 端口直接暴露到公网。推荐在 SelfSend 前放置 Caddy、Nginx 或 Traefik，并使用有效 HTTPS 证书。反向代理终止 TLS 时设置：

```yaml
environment:
  SELFSEND_TRUST_PROXY: "true"
```

Caddy 的最小配置：

```caddyfile
selfsend.example.com {
    reverse_proxy selfsend:8080
}
```

PWA、Service Worker 和一部分浏览器安全 API 需要可信 HTTPS。局域网 HTTP 可以传文件，但高级 PWA 能力可能不可用。

## 手机浏览器限制

SelfSend 不会假装网页拥有原生 App 的全部后台权限：

- iPhone 锁屏、切换应用或系统回收页面后，大文件上传可能暂停
- 上传期间请保持页面打开
- 页面被彻底关闭后，需要重新选择原文件；服务器会从已保存的分片继续
- iOS 不允许网页任意写入系统文件夹，下载行为由浏览器控制
- 第一版不承诺从其他 App 的系统分享菜单直接分享到 SelfSend

## 配置

| 环境变量 | 默认值 | 说明 |
|---|---:|---|
| `SELFSEND_LISTEN` | `:8080` | HTTP 监听地址 |
| `SELFSEND_DATA_DIR` | `./data` | 数据库与文件目录 |
| `SELFSEND_ADMIN_PASSWORD` | 空 | 仅在实例尚未初始化时设置初始密码 |
| `SELFSEND_MAX_UPLOAD_SIZE` | `21474836480` | 单文件上限，单位为字节（默认 20 GiB） |
| `SELFSEND_TRUST_PROXY` | `false` | 信任反向代理的 HTTPS 标头并设置 Secure Cookie |

命令行也支持：

```bash
selfsend -listen :8080 -data ./data -max-upload-size 21474836480
```

## 从源码开发

需要 Go 1.25+、Node.js 24+：

```bash
cd frontend
npm install
npm run build
cd ..
go run ./cmd/selfsend
```

前后端联调时可以分别运行 `go run ./cmd/selfsend` 和 `npm run dev`。Vite 会把 `/api` 代理到 `localhost:8080`。

完整检查：

```bash
make check
```

## 安全与责任边界

SelfSend 仅提供可自行部署的软件，不运营账号、文件存储、传输、中转、推送或设备发现服务。部署者负责自己的服务器、域名、HTTPS、访问控制、备份、安全更新以及适用的合规要求。

当前威胁模型是“信任自己控制的服务器”：文件落盘后没有端到端加密。请使用磁盘加密、HTTPS 和强密码。不要把它部署到不受信任的共享主机上，也不要把它描述为端到端加密产品。

如果发现安全问题，请不要公开创建 Issue，按 [SECURITY.md](SECURITY.md) 中的方式联系维护者。

## 路线图

- [x] 单用户初始化和登录
- [x] 聊天式文件时间线
- [x] 分片上传、续传、下载和删除
- [x] PWA 外壳和自托管容器
- [ ] 文本备注、链接和搜索
- [ ] 二维码配对和局域网引导
- [ ] 存储配额、保留策略和孤立分片清理
- [ ] 可选 S3 兼容存储
- [ ] 经评审的客户端加密格式

## License

[MIT](LICENSE). SelfSend 按“现状”提供，不附带任何明示或暗示担保。
