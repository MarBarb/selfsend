<div align="center">
  <img src="frontend/public/icon.svg" width="88" alt="SelfSend logo">
  <h1>SelfSend</h1>
  <p><strong>把自己的每台设备变成好友，像聊天一样传文字和文件。</strong></p>
  <p>A tiny, self-hosted personal file timeline. No app store, no account service, no official cloud.</p>
</div>

SelfSend 把“文件传输助手”的体验从聊天软件里单独拿出来：每台浏览器设备拥有独立账号，通过一次性二维码加入后，会自动出现在该实例所有设备的消息列表中，可以双向发送文字、照片或文件。项目只提供软件，不运营任何文件存储或中转服务。

> **当前状态：v0.1 开发预览。** 已经可以部署和传文件，但在公开到互联网前，请先阅读下面的安全说明。

## 特点

- 手机、平板、电脑都使用响应式网页，无需上架 App Store
- PWA 支持，可添加到主屏幕
- 单用户设计，没有开放注册和多租户复杂度
- 一次性二维码或链接添加设备，加入后自动连接所有已有设备
- 每台设备拥有独立登录身份，聊天记录可区分收发方向
- 设备名称就是账号名，重名时自动添加 `(2)`、`(3)` 后缀
- 可从已有设备中选择至少两台发起群聊，群成员共同收发文字和文件
- 4 MiB 分片上传，网络中断后重新选择同一文件可以续传
- 微信式消息首页、文字与文件聊天、图片预览、Range 下载、永久删除
- Go 单二进制，Vue 前端直接嵌入其中
- 默认 SQLite + 本地磁盘，一个数据目录即可备份
- GitHub Container Registry 提供 AMD64 与 ARM64 预构建镜像
- 可在局域网内把整个 SelfSend 迁移到另一台空白服务器
- 迁移支持自动发现、加密分块续传、完整性校验和设备身份交接
- 不连接 SelfSend 官方服务器，不包含遥测
- 界面图标使用 MIT 许可的 [Heroicons](https://heroicons.com/)

## 30 秒启动

需要 Docker 和 Docker Compose：

```bash
git clone https://github.com/MarBarb/selfsend.git
cd selfsend
docker compose up -d
```

打开 `http://localhost:8080`，首次进入时创建管理员密码。局域网中的其他设备可通过运行 SelfSend 的电脑 IP 访问，例如 `http://192.168.1.20:8080`。

创建第一台设备账号后，点击消息页右上角的加号生成邀请二维码。请使用局域网 IP 地址打开 SelfSend 后再生成二维码；`localhost` 只代表当前设备，手机无法通过它访问电脑。另一台设备扫码加入后，会自动出现在所有已有设备的消息列表中，无需逐台添加或确认。

默认会从 GitHub Container Registry 拉取公开镜像 `ghcr.io/marbarb/selfsend:latest`。部署设备无需安装 Go 或 Node.js，也无需登录 GitHub。

升级到最新版本：

```bash
docker compose pull
docker compose up -d
```

正式环境建议在 `.env` 中固定版本，例如 `SELFSEND_VERSION=v0.1.0`，确认升级后再修改版本号。

数据保存在仓库目录下的 `data/` 中：

```text
data/
├── selfsend.db       # 设置、会话和时间线
├── selfsend.db-wal
├── uploads/          # 未完成的上传
└── blobs/            # 已完成的文件
```

升级前请完整备份这个目录，包括可能存在的 `-wal` 和 `-shm` 文件。

## NAS 部署

支持能够运行 64 位 Docker 的 AMD64 或 ARM64 NAS，例如群晖、威联通及通用 Linux NAS。通过 SSH 登录 NAS 后运行：

```bash
mkdir -p selfsend && cd selfsend
curl -fsSL https://raw.githubusercontent.com/MarBarb/selfsend/main/compose.nas.yaml -o compose.yaml
docker compose up -d
```

然后访问 `http://NAS局域网IP:8080`。NAS 配置使用名为 `selfsend-data` 的 Docker 命名卷，避免不同 NAS 系统上的宿主机目录权限差异。删除或重建容器不会删除该数据卷；不要执行 `docker compose down -v`，除非确定要删除全部 SelfSend 数据。

升级 NAS 部署：

```bash
cd selfsend
docker compose pull
docker compose up -d
```

若 NAS 使用旧版 Compose，可以把命令中的 `docker compose` 替换为 `docker-compose`。局域网自动发现需要允许 UDP `38081`，无法使用广播时仍可手动复制迁移链接。

## 更换本地服务器

“更换服务器”会迁移整个 SelfSend 实例，而不只是修改界面里的服务器名称。设备账号、私聊、群聊、文字、文件、管理员密码和实例标识都会保留。

1. 在新电脑或 NAS 上使用一个新的空目录启动 SelfSend。
2. 首次打开新服务器，选择“从另一台服务器迁入”，填写新服务器的设备名称。
3. 复制新服务器显示的一次性迁移链接。
4. 在任意已连接设备打开“我 → 服务器 → 更换服务器”，粘贴链接并输入管理员密码。
5. 等待新服务器完成校验和自动重启，然后页面会跳转到新地址。

迁移仅允许连接局域网、环回或 `.local` 地址。传输使用 4 MiB 分块，可以从已经确认的偏移继续；即使局域网只使用 HTTP，迁移分块也会通过一次性凭证派生的 AES-GCM 密钥加密和认证。

迁移时需要注意：

- 新服务器必须是尚未初始化的空白 SelfSend，不能覆盖另一个已有实例。
- 两台服务器都需要额外临时磁盘空间。新服务器建议至少保留迁移数据量两倍以上的可用空间。
- 开始制作一致性快照后，旧服务器会暂时停止发送消息、修改设置和上传文件。
- 任何已连接设备都可以发起迁移或导出备份，但执行前必须重新验证管理员密码。
- 新服务器完整校验数据库、文件清单和 SHA-256 后才会激活。
- 旧服务器不会删除原数据，而是进入只读迁移状态；新服务器也会保存其初始化前的数据副本。
- 不要让旧、新服务器在迁移完成后同时恢复写入。SelfSend 不会尝试合并两份已经分叉的数据。

SelfSend 使用 UDP `38081` 在局域网中寻找处于接收模式的新服务器。Docker Compose 已默认映射这个端口；若路由器、主机防火墙或 Docker 网络不支持广播，仍可直接复制迁移链接完成迁移。

在“我 → 服务器”也可以导出完整 `.tar` 备份。要恢复备份，在空白服务器选择“从另一台服务器迁入”，进入接收页面后选择备份文件。备份上传同样支持分块续传，并在激活前校验内部文件清单。

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
| `SELFSEND_DISCOVERY` | `true` | 启用 UDP 38081 局域网服务器发现 |
| `SELFSEND_CANONICAL_URL` | 空 | 可选的稳定局域网访问地址，用于迁移后的设备跳转 |

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

需要从源码构建本地 Docker 镜像时：

```bash
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

`compose.build.yaml` 在大陆网络下默认使用 `https://goproxy.cn,direct`，可以通过宿主机 `GOPROXY` 环境变量覆盖。

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
- [x] 文本备注
- [x] 设备账号、消息首页与“我”页面
- [ ] 链接识别和搜索
- [x] 一次性二维码添加设备、全设备自动连接和双向聊天
- [x] 多设备群聊和群文件共享
- [x] 局域网服务器自动发现和完整实例迁移
- [x] 完整备份导出与空白服务器恢复
- [ ] 存储配额、保留策略和孤立分片清理
- [ ] 可选 S3 兼容存储
- [ ] 经评审的客户端加密格式

## License

[MIT](LICENSE). SelfSend 按“现状”提供，不附带任何明示或暗示担保。
