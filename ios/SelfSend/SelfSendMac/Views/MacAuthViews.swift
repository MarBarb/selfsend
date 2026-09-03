import SwiftUI

private struct MacAuthShell<Content: View>: View {
    let title: String
    let subtitle: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            Text("S")
                .font(.system(size: 38, weight: .bold, design: .rounded))
                .foregroundStyle(.white)
                .frame(width: 78, height: 78)
                .background(SelfSendTheme.green.gradient, in: RoundedRectangle(cornerRadius: 20))
                .shadow(color: SelfSendTheme.green.opacity(0.22), radius: 18, y: 8)
            VStack(spacing: 7) {
                Text(title).font(.title2.bold())
                Text(subtitle)
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            content.frame(width: 360)
            Spacer()
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
    }
}

struct MacLoginView: View {
    @EnvironmentObject private var model: AppModel
    @State private var password = ""

    var body: some View {
        MacAuthShell(
            title: model.currentServer?.name ?? "SelfSend",
            subtitle: "输入这台服务器的管理员密码"
        ) {
            VStack(spacing: 14) {
                SecureField("管理员密码", text: $password)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit(login)
                Button(action: login) {
                    if model.isWorking { ProgressView().controlSize(.small) }
                    else { Text("进入 SelfSend") }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .frame(maxWidth: .infinity)
                .disabled(password.isEmpty || model.isWorking)
                Button("选择其他服务器") { model.chooseAnotherServer() }
                    .buttonStyle(.link)
            }
        }
    }

    private func login() {
        guard !password.isEmpty else { return }
        Task { await model.login(password: password) }
    }
}

struct MacInitializeServerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var password = ""
    @State private var confirmation = ""
    @State private var localError: String?

    var body: some View {
        MacAuthShell(title: "初始化服务器", subtitle: "这是一台尚未设置的 SelfSend 服务器") {
            VStack(spacing: 12) {
                SecureField("创建管理员密码（至少 10 个字符）", text: $password)
                    .textFieldStyle(.roundedBorder)
                SecureField("再次输入", text: $confirmation)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit(initialize)
                if let localError {
                    Text(localError).font(.footnote).foregroundStyle(.red)
                }
                Button(action: initialize) {
                    if model.isWorking { ProgressView().controlSize(.small) }
                    else { Text("创建私人空间") }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .frame(maxWidth: .infinity)
                .disabled(model.isWorking)
                Button("选择其他服务器") { model.chooseAnotherServer() }
                    .buttonStyle(.link)
            }
        }
    }

    private func initialize() {
        if password.count < 10 { localError = "密码至少需要 10 个字符"; return }
        if password != confirmation { localError = "两次输入的密码不一致"; return }
        localError = nil
        Task { await model.initializeServer(password: password) }
    }
}
