# Contributing to SelfSend

感谢你愿意帮助 SelfSend。第一阶段请保持项目的基本边界：单用户、自托管、浏览器优先、没有官方云服务。

## 开始开发

1. 安装 Go 1.25+ 和 Node.js 24+。
2. 在 `frontend/` 运行 `npm install && npm run build`。
3. 在仓库根目录运行 `go run ./cmd/selfsend`。
4. 提交前运行 `make check`。

请为行为变化补充测试，并避免引入必须长期运行的新服务。大型功能建议先创建 Discussion 或 Issue 说明使用场景和自托管成本。
