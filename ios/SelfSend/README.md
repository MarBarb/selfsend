# SelfSend Apple 客户端

同一个 Xcode 工程包含两个原生客户端：

- `SelfSend`：iPhone 与 iPad，最低 iOS 17。
- `SelfSendMac`：Mac，最低 macOS 14。

两端共享 `Models`、`Networking`、`Persistence` 和 `AppModel`，界面代码分别位于 `SelfSend/Views` 与 `SelfSendMac/Views`。

## 打开和运行 macOS 客户端

```bash
cd ios/SelfSend
xcodegen generate
open SelfSend.xcodeproj
```

在 Xcode 顶部选择 `SelfSendMac` scheme 和 `My Mac`，然后运行。首次启动后添加现有 SelfSend 服务器地址；局域网服务器可以使用 `http://192.168.x.x:8080`，公网服务器必须使用 HTTPS。

命令行验证：

```bash
xcodebuild -project SelfSend.xcodeproj -scheme SelfSendMac -destination 'platform=macOS' test
```

`SelfSend.xcodeproj` 由 `project.yml` 生成。修改 target、构建设置或文件结构后，应重新运行 `xcodegen generate`，不要只修改生成的工程文件。
