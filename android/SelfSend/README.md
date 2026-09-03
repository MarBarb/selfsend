# SelfSend for Android

SelfSend 的原生 Android 客户端，使用 Kotlin 与 Jetpack Compose，兼容 Android 8.0（API 26）及以上版本。

## 已实现

- 连接本地 HTTP、NAS 或公网 HTTPS SelfSend 服务器
- 初始化新服务器、管理员密码登录、按服务器保存 Cookie 会话
- 自动注册 Android 设备、编辑设备名称和头像
- 私聊与群聊列表、创建群聊、分享或粘贴一次性设备邀请
- 文字消息、文件选择、4 MiB 分片断点上传、系统文件保存下载、删除自己发送的消息
- 前台轮询新消息，以及迁移后跳转到新服务器

## 构建

在 Android Studio 中打开本目录，或执行：

```bash
./gradlew :app:assembleDebug
```

Debug APK 生成在 `app/build/outputs/apk/debug/app-debug.apk`。
