import SwiftUI
import UIKit

struct RootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Group {
            switch model.phase {
            case .noServer:
                ServerPickerView()
            case .connecting:
                ProgressView("正在连接 SelfSend…")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(.systemGroupedBackground))
            case .setup:
                InitializeServerView()
            case .login:
                LoginView()
            case .ready:
                MainView()
            case .failed(let message):
                ConnectionErrorView(message: message)
            }
        }
        .alert("提示", isPresented: Binding(
            get: { model.alertMessage != nil },
            set: { if !$0 { model.alertMessage = nil } }
        )) {
            Button("好", role: .cancel) { model.alertMessage = nil }
        } message: {
            Text(model.alertMessage ?? "")
        }
        .sheet(item: $model.sharedFileURL) { url in
            ShareSheet(items: [url])
        }
    }
}

private struct ConnectionErrorView: View {
    @EnvironmentObject private var model: AppModel
    let message: String

    var body: some View {
        ContentUnavailableView {
            Label("无法连接服务器", systemImage: "wifi.exclamationmark")
        } description: {
            Text(message)
        } actions: {
            if let server = model.currentServer {
                Button("重新连接") { Task { await model.connect(to: server) } }
                    .buttonStyle(.borderedProminent)
            }
            Button("选择其他服务器") { model.chooseAnotherServer() }
        }
    }
}

struct ShareSheet: UIViewControllerRepresentable {
    let items: [Any]

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: items, applicationActivities: nil)
    }

    func updateUIViewController(_ uiViewController: UIActivityViewController, context: Context) {}
}

extension URL: @retroactive Identifiable {
    public var id: String { absoluteString }
}

