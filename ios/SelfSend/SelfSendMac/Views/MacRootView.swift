import AppKit
import SwiftUI

struct MacRootView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Group {
            switch model.phase {
            case .noServer:
                MacServerPickerView()
            case .connecting:
                ProgressView("正在连接 SelfSend…")
                    .controlSize(.large)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .background(Color(nsColor: .windowBackgroundColor))
            case .setup:
                MacInitializeServerView()
            case .login:
                MacLoginView()
            case .ready:
                MacMainView()
            case .failed(let message):
                MacConnectionErrorView(message: message)
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
            MacDownloadReadyView(url: url)
        }
    }
}

private struct MacConnectionErrorView: View {
    @EnvironmentObject private var model: AppModel
    let message: String

    var body: some View {
        ContentUnavailableView {
            Label("无法连接服务器", systemImage: "wifi.exclamationmark")
        } description: {
            Text(message)
        } actions: {
            HStack {
                if let server = model.currentServer {
                    Button("重新连接") { Task { await model.connect(to: server) } }
                        .buttonStyle(.borderedProminent)
                }
                Button("选择其他服务器") { model.chooseAnotherServer() }
            }
        }
    }
}

private struct MacDownloadReadyView: View {
    @Environment(\.dismiss) private var dismiss
    let url: URL

    var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: 44))
                .foregroundStyle(SelfSendTheme.green)
            VStack(spacing: 6) {
                Text("下载完成").font(.title2.bold())
                Text(url.lastPathComponent)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                    .multilineTextAlignment(.center)
            }
            HStack {
                Button("在 Finder 中显示") {
                    NSWorkspace.shared.activateFileViewerSelecting([url])
                    dismiss()
                }
                Button("打开") {
                    NSWorkspace.shared.open(url)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .padding(28)
        .frame(width: 420)
    }
}

extension URL: @retroactive Identifiable {
    public var id: String { absoluteString }
}
