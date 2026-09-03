import SwiftUI

struct MacSettingsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingEditor = false
    @State private var showingServers = false

    var body: some View {
        Form {
            if let device = model.currentDevice {
                Section("这台 Mac") {
                    HStack(spacing: 12) {
                        MacAvatarView(value: device.avatar, size: 46)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(device.name).font(.headline)
                            Text("设备账号").font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("编辑…") { showingEditor = true }
                    }
                }
            }

            Section("当前服务器") {
                if let server = model.currentServer {
                    LabeledContent("名称", value: server.name)
                    LabeledContent("类型", value: server.deploymentType.title)
                    LabeledContent("地址") {
                        Text(server.baseURL.absoluteString)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                } else {
                    Text("尚未连接服务器").foregroundStyle(.secondary)
                }
                if let details = model.serverDetails {
                    LabeledContent(
                        "文件存储",
                        value: ByteCountFormatter.string(fromByteCount: details.totalBytes, countStyle: .file)
                    )
                    LabeledContent("版本", value: details.version)
                }
                Button("管理与切换服务器…") { showingServers = true }
            }

            if model.phase == .ready {
                Section {
                    Button("退出当前服务器", role: .destructive) { Task { await model.logout() } }
                }
            }
        }
        .formStyle(.grouped)
        .padding(16)
        .frame(width: 520, height: 430)
        .task { if model.phase == .ready { await model.loadServerDetails() } }
        .sheet(isPresented: $showingEditor) { MacEditDeviceView() }
        .sheet(isPresented: $showingServers) { MacServerManagerView() }
    }
}

private struct MacEditDeviceView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var avatar = "💻"
    @State private var saving = false
    private let avatars = ["💻", "🖥️", "📱", "📂", "🟢", "🐼", "🐱"]

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("编辑设备账号").font(.title2.bold())
            Text("头像").font(.headline)
            HStack(spacing: 8) {
                ForEach(avatars, id: \.self) { value in
                    Button { avatar = value } label: {
                        Text(value)
                            .font(.title2)
                            .frame(width: 40, height: 40)
                            .background(
                                avatar == value ? Color.accentColor.opacity(0.18) : Color.clear,
                                in: RoundedRectangle(cornerRadius: 8)
                            )
                    }
                    .buttonStyle(.plain)
                }
            }
            Text("名称").font(.headline)
            TextField("设备名称", text: $name).textFieldStyle(.roundedBorder)
            HStack {
                Spacer()
                Button("取消") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("保存") { save() }
                    .buttonStyle(.borderedProminent)
                    .keyboardShortcut(.defaultAction)
                    .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || saving)
            }
        }
        .padding(24)
        .frame(width: 430)
        .onAppear {
            name = model.currentDevice?.name ?? "Mac"
            avatar = model.currentDevice?.avatar ?? "💻"
        }
    }

    private func save() {
        saving = true
        Task {
            if await model.updateCurrentDevice(
                name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                avatar: avatar
            ) {
                dismiss()
            }
            saving = false
        }
    }
}
