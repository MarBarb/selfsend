import SwiftUI

private struct AuthShell<Content: View>: View {
    let title: String
    let subtitle: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(spacing: 24) {
            Spacer()
            Text("S")
                .font(.system(size: 34, weight: .bold, design: .rounded))
                .foregroundStyle(.white)
                .frame(width: 72, height: 72)
                .background(SelfSendTheme.green.gradient, in: RoundedRectangle(cornerRadius: 20))
            VStack(spacing: 7) {
                Text(title).font(.title2.bold())
                Text(subtitle).font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
            content
                .frame(maxWidth: 420)
            Spacer()
        }
        .padding(24)
        .background(Color(.systemGroupedBackground).ignoresSafeArea())
    }
}

struct LoginView: View {
    @EnvironmentObject private var model: AppModel
    @State private var password = ""

    var body: some View {
        AuthShell(title: model.currentServer?.name ?? "SelfSend", subtitle: "输入这台服务器的管理员密码") {
            VStack(spacing: 14) {
                SecureField("管理员密码", text: $password)
                    .textContentType(.password)
                    .submitLabel(.go)
                    .onSubmit(login)
                    .padding(13)
                    .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 11))
                Button(action: login) {
                    if model.isWorking { ProgressView().tint(.white) }
                    else { Text("进入 SelfSend") }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .frame(maxWidth: .infinity)
                .disabled(password.isEmpty || model.isWorking)
                Button("选择其他服务器") { model.chooseAnotherServer() }
                    .font(.footnote)
            }
        }
    }

    private func login() {
        guard !password.isEmpty else { return }
        Task { await model.login(password: password) }
    }
}

struct InitializeServerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var password = ""
    @State private var confirmation = ""
    @State private var localError: String?

    var body: some View {
        AuthShell(title: "初始化服务器", subtitle: "这是一台尚未设置的 SelfSend 服务器") {
            VStack(spacing: 13) {
                SecureField("创建管理员密码（至少 10 个字符）", text: $password)
                    .textContentType(.newPassword)
                    .padding(13)
                    .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 11))
                SecureField("再次输入", text: $confirmation)
                    .textContentType(.newPassword)
                    .padding(13)
                    .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 11))
                if let localError { Text(localError).font(.footnote).foregroundStyle(.red) }
                Button(action: initialize) {
                    if model.isWorking { ProgressView().tint(.white) }
                    else { Text("创建私人空间") }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .frame(maxWidth: .infinity)
                .disabled(model.isWorking)
                Button("选择其他服务器") { model.chooseAnotherServer() }
                    .font(.footnote)
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
