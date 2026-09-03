import SwiftUI

struct MeView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingServers = false
    @State private var showingEditor = false

    var body: some View {
        NavigationStack {
            List {
                if let device = model.currentDevice {
                    Section {
                        Button { showingEditor = true } label: {
                            HStack(spacing: 14) {
                                AvatarView(value: device.avatar, size: 64)
                                Text(device.name).font(.title3.weight(.semibold)).foregroundStyle(.primary)
                                Spacer()
                                Image(systemName: "chevron.right").font(.caption).foregroundStyle(.tertiary)
                            }
                            .padding(.vertical, 7)
                        }
                    }
                }
                Section("当前服务器") {
                    if let server = model.currentServer {
                        LabeledContent("名称", value: server.name)
                        LabeledContent("类型", value: server.deploymentType.title + (server.deploymentType == .nas ? "（实验性）" : ""))
                        LabeledContent("地址") {
                            Text(server.baseURL.absoluteString).lineLimit(1).foregroundStyle(.secondary)
                        }
                    }
                    if let details = model.serverDetails {
                        LabeledContent("文件存储", value: ByteCountFormatter.string(fromByteCount: details.totalBytes, countStyle: .file))
                        LabeledContent("版本", value: details.version)
                    }
                    Button { showingServers = true } label: { Label("管理与切换服务器", systemImage: "server.rack") }
                }
                Section {
                    Button("退出当前服务器", role: .destructive) { Task { await model.logout() } }
                }
            }
            .navigationTitle("我")
            .sheet(isPresented: $showingServers) { ServerManagerView() }
            .sheet(isPresented: $showingEditor) { EditDeviceView() }
            .task { await model.loadServerDetails() }
        }
    }
}

private struct EditDeviceView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var avatar = "📱"
    @State private var saving = false
    private let avatars = ["📱", "💻", "🖥️", "📂", "🟢", "🐼", "🐱"]

    var body: some View {
        NavigationStack {
            Form {
                Section("头像") {
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: 7), spacing: 8) {
                        ForEach(avatars, id: \.self) { value in
                            Button { avatar = value } label: {
                                Text(value)
                                    .font(.title2)
                                    .frame(width: 38, height: 38)
                                    .background(avatar == value ? Color.accentColor.opacity(0.16) : Color.clear, in: RoundedRectangle(cornerRadius: 8))
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.vertical, 5)
                }
                Section("名称") {
                    TextField("设备名称", text: $name)
                }
            }
            .navigationTitle("编辑设备账号")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("取消") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("保存") {
                        saving = true
                        Task {
                            if await model.updateCurrentDevice(name: name.trimmingCharacters(in: .whitespacesAndNewlines), avatar: avatar) { dismiss() }
                            saving = false
                        }
                    }
                    .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || saving)
                }
            }
            .onAppear {
                name = model.currentDevice?.name ?? "iPhone"
                avatar = model.currentDevice?.avatar ?? "📱"
            }
        }
    }
}
