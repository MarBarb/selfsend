import SwiftUI

struct ConversationListView: View {
    @EnvironmentObject private var model: AppModel
    @State private var showingAddServer = false
    @State private var showingServerManager = false

    var body: some View {
        NavigationStack {
            Group {
                if model.conversations.isEmpty {
                    ContentUnavailableView("还没有其他设备", systemImage: "iphone.and.arrow.forward", description: Text("先在网页端生成邀请，再把这台 iPhone 添加为设备。"))
                } else {
                    List(model.conversations) { conversation in
                        NavigationLink {
                            ChatView(conversation: conversation)
                        } label: {
                            ConversationRow(conversation: conversation)
                        }
                    }
                    .listStyle(.plain)
                    .refreshable { await model.refreshConversations(showErrors: true) }
                }
            }
            .navigationTitle("消息")
            .toolbar {
                ToolbarItem(placement: .principal) {
                    serverMenu
                }
            }
            .sheet(isPresented: $showingAddServer) { AddServerView() }
            .sheet(isPresented: $showingServerManager) { ServerManagerView() }
            .task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(5))
                    await model.refreshConversations()
                }
            }
        }
    }

    private var serverMenu: some View {
        Menu {
            Section("切换服务器") {
                ForEach(model.servers) { server in
                    Button {
                        guard server.id != model.currentServer?.id else { return }
                        Task { await model.connect(to: server) }
                    } label: {
                        Label {
                            Text(server.name)
                        } icon: {
                            Image(systemName: server.id == model.currentServer?.id ? "checkmark" : server.deploymentType.symbol)
                        }
                    }
                }
            }
            Button { showingAddServer = true } label: { Label("添加服务器", systemImage: "plus.circle") }
            Button { showingServerManager = true } label: { Label("管理服务器", systemImage: "server.rack") }
        } label: {
            VStack(spacing: 1) {
                HStack(spacing: 4) {
                    Text(model.currentServer?.name ?? "当前服务器")
                        .font(.headline)
                        .lineLimit(1)
                    Image(systemName: "chevron.down").font(.caption2)
                }
                Text(model.currentServer?.deploymentType.title ?? "")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: 210)
        }
    }
}

private struct ConversationRow: View {
    let conversation: Conversation

    var body: some View {
        HStack(spacing: 12) {
            AvatarView(value: conversation.avatar, size: 50)
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text(conversation.name).font(.body).lineLimit(1)
                    Spacer()
                    if conversation.lastMessageAt > 0 {
                        Text(timestamp(conversation.lastMessageAt)).font(.caption2).foregroundStyle(.tertiary)
                    }
                }
                Text(conversation.preview).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
            }
        }
        .padding(.vertical, 4)
    }

    private func timestamp(_ milliseconds: Int64) -> String {
        let date = Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1000)
        if Calendar.current.isDateInToday(date) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.month().day())
    }
}

struct AvatarView: View {
    let value: String
    let size: CGFloat

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.16)
                .fill(SelfSendTheme.green.gradient)
            Text(value.hasPrefix("data:image/") ? "设备" : value)
                .font(.system(size: size * 0.42))
                .minimumScaleFactor(0.45)
                .lineLimit(1)
                .foregroundStyle(.white)
        }
        .frame(width: size, height: size)
    }
}

struct ServerManagerView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var showingAddServer = false

    var body: some View {
        NavigationStack {
            List {
                ForEach(model.servers) { server in
                    Button {
                        if server.id != model.currentServer?.id {
                            dismiss()
                            Task { await model.connect(to: server) }
                        }
                    } label: {
                        ServerLabel(server: server, isCurrent: server.id == model.currentServer?.id)
                    }
                    .buttonStyle(.plain)
                    .swipeActions {
                        if server.id != model.currentServer?.id {
                            Button("移除", role: .destructive) { model.removeServer(server) }
                        }
                    }
                }
            }
            .navigationTitle("服务器")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("完成") { dismiss() } }
                ToolbarItem(placement: .primaryAction) { Button { showingAddServer = true } label: { Image(systemName: "plus") } }
            }
            .sheet(isPresented: $showingAddServer) { AddServerView() }
        }
    }
}
