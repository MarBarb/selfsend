import SwiftUI

struct MacMainView: View {
    @EnvironmentObject private var model: AppModel
    @State private var selectedConversationID: String?
    @State private var showingAddServer = false
    @State private var showingServerManager = false

    private var selectedConversation: Conversation? {
        model.conversations.first { $0.id == selectedConversationID }
    }

    var body: some View {
        NavigationSplitView {
            VStack(spacing: 0) {
                serverHeader
                Divider()
                conversationList
                Divider()
                accountFooter
            }
            .navigationSplitViewColumnWidth(min: 240, ideal: 290, max: 360)
        } detail: {
            if let selectedConversation {
                MacChatView(conversation: selectedConversation)
                    .id(selectedConversation.conversationID)
            } else {
                ContentUnavailableView(
                    "选择一段对话",
                    systemImage: "bubble.left.and.bubble.right",
                    description: Text("从左侧选择一台设备或群聊")
                )
            }
        }
        .sheet(isPresented: $showingAddServer) { MacAddServerView() }
        .sheet(isPresented: $showingServerManager) { MacServerManagerView() }
        .task {
            selectFirstConversationIfNeeded()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                await model.refreshConversations()
            }
        }
        .onChange(of: model.conversations) { _, _ in selectFirstConversationIfNeeded() }
        .onChange(of: model.currentServer?.id) { _, _ in selectedConversationID = nil }
    }

    private var serverHeader: some View {
        Menu {
            Section("切换服务器") {
                ForEach(model.servers) { server in
                    Button {
                        guard server.id != model.currentServer?.id else { return }
                        Task { await model.connect(to: server) }
                    } label: {
                        Label(
                            server.name,
                            systemImage: server.id == model.currentServer?.id ? "checkmark" : server.deploymentType.symbol
                        )
                    }
                }
            }
            Divider()
            Button("添加服务器…", systemImage: "plus.circle") { showingAddServer = true }
            Button("管理服务器…", systemImage: "server.rack") { showingServerManager = true }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: model.currentServer?.deploymentType.symbol ?? "server.rack")
                    .foregroundStyle(SelfSendTheme.green)
                VStack(alignment: .leading, spacing: 2) {
                    Text(model.currentServer?.name ?? "当前服务器").font(.headline).lineLimit(1)
                    Text(model.currentServer?.deploymentType.title ?? "")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "chevron.up.chevron.down").font(.caption2).foregroundStyle(.secondary)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .menuStyle(.borderlessButton)
        .padding(.horizontal, 4)
    }

    @ViewBuilder private var conversationList: some View {
        if model.conversations.isEmpty {
            ContentUnavailableView {
                Label("还没有其他设备", systemImage: "laptopcomputer.and.iphone")
            } description: {
                Text("在已有设备上生成邀请，再把这台 Mac 添加为设备。")
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            List(model.conversations, selection: $selectedConversationID) { conversation in
                MacConversationRow(conversation: conversation)
                    .tag(conversation.id)
            }
            .listStyle(.sidebar)
        }
    }

    private var accountFooter: some View {
        HStack(spacing: 10) {
            MacAvatarView(value: model.currentDevice?.avatar ?? "💻", size: 30)
            Text(model.currentDevice?.name ?? "这台 Mac")
                .font(.callout)
                .lineLimit(1)
            Spacer()
            SettingsLink {
                Image(systemName: "gearshape")
            }
            .buttonStyle(.borderless)
            .help("设置")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
    }

    private func selectFirstConversationIfNeeded() {
        guard selectedConversation == nil else { return }
        selectedConversationID = model.conversations.first?.id
    }
}

private struct MacConversationRow: View {
    let conversation: Conversation

    var body: some View {
        HStack(spacing: 11) {
            MacAvatarView(value: conversation.avatar, size: 42)
            VStack(alignment: .leading, spacing: 5) {
                HStack {
                    Text(conversation.name).font(.body.weight(.medium)).lineLimit(1)
                    Spacer()
                    if conversation.lastMessageAt > 0 {
                        Text(timestamp).font(.caption2).foregroundStyle(.tertiary)
                    }
                }
                Text(conversation.preview)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 4)
    }

    private var timestamp: String {
        let date = Date(timeIntervalSince1970: TimeInterval(conversation.lastMessageAt) / 1000)
        if Calendar.current.isDateInToday(date) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.month().day())
    }
}

struct MacAvatarView: View {
    let value: String
    let size: CGFloat

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.18).fill(SelfSendTheme.green.gradient)
            Text(value.hasPrefix("data:image/") ? "设备" : value)
                .font(.system(size: size * 0.42))
                .minimumScaleFactor(0.45)
                .lineLimit(1)
                .foregroundStyle(.white)
        }
        .frame(width: size, height: size)
    }
}
