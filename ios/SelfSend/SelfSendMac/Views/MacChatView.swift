import SwiftUI
import UniformTypeIdentifiers

struct MacChatView: View {
    @EnvironmentObject private var model: AppModel
    let conversation: Conversation
    @State private var text = ""
    @State private var showingFileImporter = false
    @State private var pendingDelete: TimelineItem?
    @State private var isDropTargeted = false

    var body: some View {
        VStack(spacing: 0) {
            messages
            if model.isUploading {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("正在上传文件…").font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 7)
                .background(.thinMaterial)
            }
            composer
        }
        .background(Color(nsColor: .underPageBackgroundColor))
        .navigationTitle(conversation.name)
        .toolbar {
            ToolbarItemGroup {
                Button { Task { await model.loadTimeline(conversationID: conversation.conversationID, showErrors: true) } } label: {
                    Label("刷新", systemImage: "arrow.clockwise")
                }
                .keyboardShortcut("r", modifiers: .command)
                Button { showingFileImporter = true } label: {
                    Label("发送文件", systemImage: "paperclip")
                }
                .keyboardShortcut("u", modifiers: .command)
                .disabled(model.isUploading)
            }
        }
        .overlay {
            if isDropTargeted {
                RoundedRectangle(cornerRadius: 14)
                    .stroke(SelfSendTheme.green, style: StrokeStyle(lineWidth: 3, dash: [8]))
                    .padding(10)
                    .allowsHitTesting(false)
            }
        }
        .dropDestination(for: URL.self) { urls, _ in
            guard !urls.isEmpty else { return false }
            Task { await upload(urls) }
            return true
        } isTargeted: { isTargeted in
            isDropTargeted = isTargeted
        }
        .fileImporter(
            isPresented: $showingFileImporter,
            allowedContentTypes: [.item],
            allowsMultipleSelection: true
        ) { result in
            guard case .success(let urls) = result else { return }
            Task { await upload(urls) }
        }
        .task(id: conversation.conversationID) {
            await model.loadTimeline(conversationID: conversation.conversationID, showErrors: true)
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                await model.loadTimeline(conversationID: conversation.conversationID)
            }
        }
        .confirmationDialog("永久删除这条消息？", isPresented: Binding(
            get: { pendingDelete != nil },
            set: { if !$0 { pendingDelete = nil } }
        )) {
            Button("删除", role: .destructive) {
                if let item = pendingDelete {
                    Task { await model.delete(item, conversationID: conversation.conversationID) }
                }
                pendingDelete = nil
            }
            Button("取消", role: .cancel) { pendingDelete = nil }
        }
    }

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 13) {
                    if model.timeline.isEmpty {
                        ContentUnavailableView(
                            "还没有消息",
                            systemImage: "tray",
                            description: Text("输入文字，或把文件拖到这里发送")
                        )
                        .frame(minHeight: 360)
                    } else {
                        ForEach(model.timeline) { item in
                            MacMessageRow(
                                item: item,
                                isMine: item.senderDeviceID == model.currentDevice?.id
                            ) {
                                pendingDelete = item
                            }
                            .id(item.id)
                        }
                    }
                }
                .padding(.horizontal, 22)
                .padding(.vertical, 18)
            }
            .defaultScrollAnchor(.bottom)
            .onChange(of: model.timeline.count) { _, _ in
                if let last = model.timeline.last {
                    withAnimation { proxy.scrollTo(last.id, anchor: .bottom) }
                }
            }
        }
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 10) {
            Button { showingFileImporter = true } label: {
                Image(systemName: "plus.circle").font(.title2)
            }
            .buttonStyle(.borderless)
            .disabled(model.isUploading)

            TextField("输入消息", text: $text, axis: .vertical)
                .lineLimit(1...5)
                .textFieldStyle(.plain)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(Color(nsColor: .textBackgroundColor), in: RoundedRectangle(cornerRadius: 9))
                .overlay(RoundedRectangle(cornerRadius: 9).stroke(.separator.opacity(0.55)))
                .onSubmit(send)

            Button("发送", action: send)
                .buttonStyle(.borderedProminent)
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(.bar)
    }

    private func send() {
        let sending = text
        Task {
            if await model.sendText(sending, conversationID: conversation.conversationID) {
                text = ""
            }
        }
    }

    private func upload(_ urls: [URL]) async {
        for url in urls where url.isFileURL {
            let accessing = url.startAccessingSecurityScopedResource()
            defer { if accessing { url.stopAccessingSecurityScopedResource() } }
            await model.upload(
                fileURL: url,
                fileName: url.lastPathComponent,
                mimeType: UTType(filenameExtension: url.pathExtension)?.preferredMIMEType,
                conversationID: conversation.conversationID
            )
        }
    }
}

private struct MacMessageRow: View {
    @EnvironmentObject private var model: AppModel
    let item: TimelineItem
    let isMine: Bool
    let requestDelete: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 9) {
            if isMine { Spacer(minLength: 90) }
            if !isMine { MacAvatarView(value: senderAvatar, size: 34) }
            VStack(alignment: isMine ? .trailing : .leading, spacing: 4) {
                if !isMine && !senderName.isEmpty {
                    Text(senderName).font(.caption2).foregroundStyle(.secondary)
                }
                content
                Text(time).font(.caption2).foregroundStyle(.tertiary)
            }
            if isMine { MacAvatarView(value: model.currentDevice?.avatar ?? "💻", size: 34) }
            if !isMine { Spacer(minLength: 90) }
        }
        .contextMenu {
            if isMine {
                Button("删除", systemImage: "trash", role: .destructive, action: requestDelete)
            }
        }
    }

    @ViewBuilder private var content: some View {
        switch item {
        case .text(let message):
            Text(message.text)
                .textSelection(.enabled)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(
                    isMine ? SelfSendTheme.bubble : Color(nsColor: .controlBackgroundColor),
                    in: RoundedRectangle(cornerRadius: 8)
                )
        case .file(let file):
            Button { Task { await model.download(file) } } label: {
                HStack(spacing: 12) {
                    Image(systemName: fileSymbol(file.mimeType))
                        .font(.title2)
                        .foregroundStyle(.white)
                        .frame(width: 44, height: 48)
                        .background(SelfSendTheme.green.gradient, in: RoundedRectangle(cornerRadius: 8))
                    VStack(alignment: .leading, spacing: 4) {
                        Text(file.fileName).font(.callout.weight(.medium)).foregroundStyle(.primary).lineLimit(2)
                        Text(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 12)
                    Image(systemName: "arrow.down.circle").foregroundStyle(SelfSendTheme.green)
                }
                .frame(width: 330)
                .padding(10)
                .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 10))
            }
            .buttonStyle(.plain)
        }
    }

    private var senderName: String {
        switch item { case .text(let value): value.senderName; case .file(let value): value.senderName }
    }

    private var senderAvatar: String {
        switch item { case .text(let value): value.senderAvatar; case .file(let value): value.senderAvatar }
    }

    private var time: String {
        Date(timeIntervalSince1970: TimeInterval(item.createdAt) / 1000)
            .formatted(date: .omitted, time: .shortened)
    }

    private func fileSymbol(_ mimeType: String) -> String {
        if mimeType.hasPrefix("image/") { return "photo" }
        if mimeType.hasPrefix("video/") { return "film" }
        if mimeType.hasPrefix("audio/") { return "waveform" }
        if mimeType.contains("pdf") { return "doc.richtext" }
        if mimeType.contains("zip") || mimeType.contains("compressed") { return "archivebox" }
        return "doc"
    }
}
