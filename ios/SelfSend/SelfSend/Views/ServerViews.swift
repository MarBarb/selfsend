import SwiftUI

struct ServerPickerView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingAddServer = false

    var body: some View {
        NavigationStack {
            Group {
                if model.servers.isEmpty {
                    ContentUnavailableView {
                        Label("添加 SelfSend 服务器", systemImage: "server.rack")
                    } description: {
                        Text("iOS 客户端不会托管数据，请连接你自己部署的服务器。")
                    } actions: {
                        Button("添加服务器") { showingAddServer = true }
                            .buttonStyle(.borderedProminent)
                    }
                } else {
                    List {
                        Section("选择服务器") {
                            ForEach(model.servers) { server in
                                Button {
                                    Task { await model.connect(to: server) }
                                } label: {
                                    ServerLabel(server: server)
                                }
                                .buttonStyle(.plain)
                            }
                            .onDelete { offsets in
                                for index in offsets { model.removeServer(model.servers[index]) }
                            }
                        }
                        Section {
                            Button { showingAddServer = true } label: {
                                Label("添加服务器", systemImage: "plus.circle")
                            }
                        }
                    }
                }
            }
            .navigationTitle("SelfSend")
            .toolbar {
                if !model.servers.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button { showingAddServer = true } label: { Image(systemName: "plus") }
                    }
                }
            }
            .sheet(isPresented: $showingAddServer) { AddServerView() }
        }
    }
}

struct AddServerView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var address = ""
    @State private var deploymentType: DeploymentType = .local
    @State private var errorMessage: String?
    @State private var saving = false

    var body: some View {
        NavigationStack {
            Form {
                Section("服务器类型") {
                    Picker("服务器类型", selection: $deploymentType) {
                        ForEach(DeploymentType.allCases) { type in
                            Label(type.shortTitle, systemImage: type.symbol).tag(type)
                        }
                    }
                    .pickerStyle(.segmented)
                    if deploymentType == .nas {
                        Label("NAS 支持目前是实验性功能", systemImage: "flask")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                Section {
                    TextField("显示名称（可选）", text: $name)
                    TextField(deploymentType == .cloud ? "https://send.example.com" : "http://192.168.1.20:8080", text: $address)
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                        .autocorrectionDisabled()
                    if let errorMessage {
                        Text(errorMessage).foregroundStyle(.red).font(.footnote)
                    }
                } header: {
                    Text("连接信息")
                } footer: {
                    Text("服务器之间不会同步设备、聊天记录或文件。公网地址必须使用 HTTPS。")
                }
            }
            .navigationTitle("添加服务器")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("连接") { connect() }
                        .disabled(address.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || saving)
                }
            }
        }
    }

    private func connect() {
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

struct ServerLabel: View {
    let server: ServerProfile
    var isCurrent = false

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: server.deploymentType.symbol)
                .font(.title3)
                .foregroundStyle(.white)
                .frame(width: 42, height: 42)
                .background(iconColor.gradient, in: RoundedRectangle(cornerRadius: 9))
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 6) {
                    Text(server.name).foregroundStyle(.primary).lineLimit(1)
                    if server.deploymentType == .nas {
                        Text("实验性").font(.caption2).foregroundStyle(.orange)
                    }
                }
                Text(isCurrent ? "当前服务器" : server.baseURL.absoluteString)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            if isCurrent { Image(systemName: "checkmark").foregroundStyle(SelfSendTheme.green) }
        }
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
