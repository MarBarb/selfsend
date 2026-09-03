import SwiftUI

struct MacServerPickerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingAddServer = false

    var body: some View {
        VStack(spacing: 20) {
            Spacer()
            Image(systemName: "paperplane.circle.fill")
                .font(.system(size: 64))
                .foregroundStyle(SelfSendTheme.green)
            VStack(spacing: 6) {
                Text("SelfSend").font(.largeTitle.bold())
                Text("连接你自己部署的服务器")
                    .foregroundStyle(.secondary)
            }

            if model.servers.isEmpty {
                Button("添加服务器") { showingAddServer = true }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
            } else {
                VStack(spacing: 8) {
                    ForEach(model.servers) { server in
                        Button {
                            Task { await model.connect(to: server) }
                        } label: {
                            MacServerLabel(server: server)
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(10)
                .frame(width: 440)
                .background(.background, in: RoundedRectangle(cornerRadius: 14))
                .overlay(RoundedRectangle(cornerRadius: 14).stroke(.separator.opacity(0.5)))

                Button("添加另一台服务器") { showingAddServer = true }
            }
            Spacer()
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(nsColor: .windowBackgroundColor))
        .sheet(isPresented: $showingAddServer) { MacAddServerView() }
    }
}

struct MacAddServerView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var address = ""
    @State private var deploymentType: DeploymentType = .local
    @State private var errorMessage: String?
    @State private var saving = false

    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack {
                Text("添加服务器").font(.title2.bold())
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
            }

            Picker("服务器类型", selection: $deploymentType) {
                ForEach(DeploymentType.allCases) { type in
                    Label(type.shortTitle, systemImage: type.symbol).tag(type)
                }
            }
            .pickerStyle(.segmented)

            Grid(alignment: .leading, horizontalSpacing: 14, verticalSpacing: 14) {
                GridRow {
                    Text("显示名称")
                    TextField("可选", text: $name).textFieldStyle(.roundedBorder)
                }
                GridRow {
                    Text("服务器地址")
                    TextField(
                        deploymentType == .cloud ? "https://send.example.com" : "http://192.168.1.20:8080",
                        text: $address
                    )
                    .textFieldStyle(.roundedBorder)
                    .onSubmit(connect)
                }
            }

            if deploymentType == .nas {
                Label("NAS 支持目前是实验性功能", systemImage: "flask")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if let errorMessage {
                Text(errorMessage).font(.footnote).foregroundStyle(.red)
            }
            Text("本地地址可以使用 HTTP；公网服务器必须使用 HTTPS。")
                .font(.footnote)
                .foregroundStyle(.secondary)

            HStack {
                Spacer()
                Button("连接") { connect() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || saving)
            }
        }
        .padding(24)
        .frame(width: 520)
    }

    private func connect() {
        guard !address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        saving = true
        errorMessage = nil
        Task {
            do {
                try await model.addServer(name: name, address: address, deploymentType: deploymentType)
                dismiss()
            } catch {
                errorMessage = error.localizedDescription
            }
            saving = false
        }
    }
}

struct MacServerManagerView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var showingAddServer = false

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("服务器").font(.title2.bold())
                Spacer()
                Button { showingAddServer = true } label: { Label("添加", systemImage: "plus") }
                Button("完成") { dismiss() }.keyboardShortcut(.cancelAction)
            }
            .padding(18)

            Divider()

            List {
                ForEach(model.servers) { server in
                    HStack {
                        Button {
                            guard server.id != model.currentServer?.id else { return }
                            dismiss()
                            Task { await model.connect(to: server) }
                        } label: {
                            MacServerLabel(server: server, isCurrent: server.id == model.currentServer?.id)
                        }
                        .buttonStyle(.plain)
                        Spacer()
                        if server.id != model.currentServer?.id {
                            Button(role: .destructive) { model.removeServer(server) } label: {
                                Image(systemName: "trash")
                            }
                            .buttonStyle(.borderless)
                            .help("移除服务器")
                        }
                    }
                    .padding(.vertical, 3)
                }
            }
        }
        .frame(width: 560, height: 400)
        .sheet(isPresented: $showingAddServer) { MacAddServerView() }
    }
}

struct MacServerLabel: View {
    let server: ServerProfile
    var isCurrent = false

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: server.deploymentType.symbol)
                .font(.title3)
                .foregroundStyle(.white)
                .frame(width: 42, height: 42)
                .background(iconColor.gradient, in: RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(server.name).foregroundStyle(.primary).lineLimit(1)
                    if server.deploymentType == .nas {
                        Text("实验性").font(.caption2).foregroundStyle(.orange)
                    }
                }
                Text(server.baseURL.absoluteString)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            if isCurrent { Image(systemName: "checkmark").foregroundStyle(SelfSendTheme.green) }
        }
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
    }

    private var iconColor: Color {
        switch server.deploymentType {
        case .local: SelfSendTheme.green
        case .cloud: .blue
        case .nas: .brown
        }
    }
}
