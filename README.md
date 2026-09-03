<div align="center">
  <img src="frontend/public/icon.svg" width="88" alt="SelfSend logo">
  <h1>SelfSend</h1>
  <p><strong>把自己的每台设备变成好友，像聊天一样传文字和文件。</strong></p>
  <p>A tiny, self-hosted personal file timeline. No app store, no account service, no official cloud.</p>
</div>

SelfSend 把“文件传输助手”的体验从聊天软件里单独拿出来：每台浏览器设备拥有独立账号，通过一次性二维码加入后，会自动出现在该实例所有设备的消息列表中，可以双向发送文字、照片或文件。项目只提供软件，不运营任何文件存储或中转服务。

> **当前状态：v0.1 开发预览。** 已经可以部署和传文件，但在公开到互联网前，请先阅读下面的安全说明。

## 特点

- 手机、平板、电脑均可使用响应式网页，无需安装客户端
- 提供原生 SwiftUI iOS 客户端（开发预览），网页端仍可独立使用
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

## 服务器类型

SelfSend 始终只连接一台服务器。服务器可以采用三种部署方式：

- **本地服务器**：运行在 Windows、macOS 或 Linux 电脑上，主要通过局域网访问。
- **云端服务器**：运行在用户自行购买和维护的云服务器上，通过公网 HTTPS 访问。
- **NAS 服务器（实验性）**：运行在支持 Docker 的 NAS 上，数据保存在 NAS 文件夹中；厂商远程访问对第三方容器的支持需要逐项验证。

类型只描述运行环境，不会创建多份数据，也不会在服务器之间持续同步。

## 多服务器切换

同一个浏览器可以保存多台彼此独立的 SelfSend 服务器。在消息页顶部打开服务器菜单，可以添加、切换、改名或移除服务器入口。切换会直接进入目标服务器；不同服务器拥有各自的设备账号、会话、文件、管理员密码和登录状态，不会合并聊天列表，也不会同步消息或文件。

服务器目录只包含名称、地址和部署类型，不保存密码或登录凭证。目录保存在当前浏览器中，并在切换时通过 URL 片段带到目标 SelfSend 页面；URL 片段不会发送给 Web 服务器。换用新的浏览器或设备时，需要重新添加服务器入口。

公网服务器必须使用 HTTPS。本地 HTTP 地址只允许使用局域网 IP、`localhost`、`.local`、`.home` 或 `.lan` 主机名。其他服务器的实时在线状态不会被跨域探测，界面只显示上次成功连接时间。

## NAS 部署

支持能够运行 64 位 Docker 的 AMD64 或 ARM64 NAS，例如群晖、威联通及通用 Linux NAS。通过 SSH 登录 NAS 后运行：

```bash
mkdir -p selfsend && cd selfsend
curl -fsSL https://raw.githubusercontent.com/MarBarb/selfsend/main/compose.nas.yaml -o compose.yaml
docker compose up -d
```

然后访问 `http://NAS局域网IP:8080`。NAS 配置默认使用名为 `selfsend-data` 的 Docker 命名卷，避免不同 NAS 系统上的宿主机目录权限差异。删除或重建容器不会删除该数据卷；不要执行 `docker compose down -v`，除非确定要删除全部 SelfSend 数据。

如果希望数据保存在 NAS 文件管理器中可见的专用目录，可以先创建目录，并在 `.env` 中写入它的绝对路径：

```dotenv
SELFSEND_DATA_PATH=/NAS上的实际路径/SelfSend
```

随后重新运行 `docker compose up -d`。该目录会映射为容器中的 `/data`，其中包含数据库、上传临时文件和全部聊天文件。这个目录由 SelfSend 管理，不要在服务运行时手动移动或删除内部文件。

升级 NAS 部署：

```bash
cd selfsend
docker compose pull
docker compose up -d
```

若 NAS 使用旧版 Compose，可以把命令中的 `docker compose` 替换为 `docker-compose`。局域网自动发现需要允许 UDP `38081`，无法使用广播时仍可手动复制迁移链接。

### 成品 NAS 部署（实验性）

极空间、群晖、威联通、飞牛、绿联以及其他支持 64 位 Docker 的 NAS 可以共用在线 NAS 配置，把文件直接保存在用户选择的 NAS 文件夹中：

```bash
mkdir -p selfsend && cd selfsend
curl -fsSL https://raw.githubusercontent.com/MarBarb/selfsend/main/compose.online-nas.yaml -o compose.yaml
printf 'SELFSEND_DATA_PATH=%s\n' '/NAS中复制的文件夹绝对路径' > .env
docker compose up -d
```

先通过局域网地址确认 SelfSend 正常工作并完成迁移，再尝试使用 NAS 厂商的远程访问功能或反向代理，为 `8080` 对应的网页服务配置公网入口。在线使用必须采用 HTTPS；不要把 SMB、NFS、FTP 或未加密的 `8080` 端口直接暴露到公网。

若获得稳定的 HTTPS 地址，可以追加到 `.env` 后重建容器：

```dotenv
SELFSEND_PUBLIC_URL=https://你的稳定公网地址
```

在线 NAS 配置已经启用 `SELFSEND_TRUST_PROXY`。反向代理需要正确传递 `X-Forwarded-Proto: https`，否则安全 Cookie 和公网地址识别可能不正确。不同厂商的远程访问可能对上传大小、长连接或请求超时有限制，迁移大量数据前建议先上传和下载一个普通文件验证。

品牌选择只影响安装提示，不影响 SelfSend 的数据格式或迁移协议。SelfSend 不调用任何 NAS 厂商私有 API，因此同一套镜像和 Compose 配置可以跨品牌使用。

所选目录必须允许容器读写。若容器反复重启并提示 `permission denied`，请在 NAS 的容器管理界面为该目录授予读写权限，再重新启动 SelfSend；不要为了省事把整个存储池开放给容器。

## 云端服务器部署

在自行购买的 Linux 云服务器上运行：

```bash
mkdir -p selfsend && cd selfsend
curl -fsSL https://raw.githubusercontent.com/MarBarb/selfsend/main/compose.cloud.yaml -o compose.yaml
docker compose up -d
```

云端配置只把 SelfSend 绑定到 `127.0.0.1:8080`。必须再使用 Caddy、Nginx 或云厂商网关配置域名和 HTTPS，不能直接通过公网暴露该端口。服务器费用、系统更新、防火墙、域名、证书和备份由部署者负责。

## 迁移服务器

“迁移服务器”会迁移整个 SelfSend 实例，不同于消息页顶部不移动数据的“切换服务器”。设备账号、私聊、群聊、文字、文件、管理员密码和实例标识都会保留。

1. 在新电脑或 NAS 上使用一个新的空目录启动 SelfSend。
2. 首次打开新服务器，选择“从另一台服务器迁入”，填写新服务器的设备名称。
3. 复制新服务器显示的一次性迁移链接。
4. 在任意已连接设备打开“我 → 服务器 → 迁移服务器”，选择本地服务器、云端服务器或实验性的 NAS 服务器，粘贴链接并输入管理员密码。
5. 等待新服务器完成校验和自动重启，然后页面会跳转到新地址。

本地迁移仅允许连接局域网、环回或 `.local` 地址；云端迁移只允许解析到公网地址的 HTTPS 目标；NAS 迁移优先接受局域网地址，也允许公网 HTTPS 地址。传输使用 4 MiB 分块，可以从已经确认的偏移继续；迁移分块还会通过一次性凭证派生的 AES-GCM 密钥加密和认证。

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

## iOS 客户端（开发预览）

原生 SwiftUI 工程位于 `ios/SelfSend/`，最低支持 iOS 17。它直接连接用户自行部署的 SelfSend，不经过官方服务器，目前支持：

- 保存和切换多台彼此独立的本地、云端或 NAS 服务器
- 初始化空白服务器、管理员密码登录和自动注册当前 iPhone 设备
- 查看消息列表和聊天记录、发送文字
- 从照片或“文件”中选择内容，以 4 MiB 分片上传
- 下载文件并调用 iOS 系统分享面板
- 修改当前设备名称与头像、查看服务器信息和退出登录

使用 Xcode 打开：

```bash
open ios/SelfSend/SelfSend.xcodeproj
```

在真机运行前，需要在 Xcode 的 Signing & Capabilities 中选择自己的开发团队。首次连接局域网服务器时请允许“本地网络”权限；真机不能用 `localhost` 访问 Mac 上的服务器，应填写 Mac 的局域网 IP，例如 `http://192.168.1.20:8080`。公网服务器必须使用 HTTPS。

工程使用 `project.yml` 维护，也可以安装 XcodeGen 后重新生成：

```bash
cd ios/SelfSend
xcodegen generate
```

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
| `SELFSEND_CANONICAL_URL` | 空 | 可选的稳定访问地址，用于迁移后的设备跳转 |
| `SELFSEND_DEPLOYMENT_TYPE` | 空 | 部署类型：`local`、`cloud` 或 `nas` |
| `SELFSEND_PROVIDER` | 空 | 运行环境或 NAS 品牌标识，仅用于界面展示和安装引导 |

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
