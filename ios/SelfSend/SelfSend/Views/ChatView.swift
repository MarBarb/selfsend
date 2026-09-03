import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

struct ChatView: View {
    @EnvironmentObject private var model: AppModel
    let conversation: Conversation
    @State private var text = ""
    @State private var showingAttachments = false
    @State private var showingFileImporter = false
    @State private var showingPhotoPicker = false
    @State private var selectedPhoto: PhotosPickerItem?
    @State private var pendingDelete: TimelineItem?

    var body: some View {
        VStack(spacing: 0) {
            messages
            if model.isUploading {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text("正在上传，请保持 SelfSend 打开").font(.caption).foregroundStyle(.secondary)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 7)
                .background(.thinMaterial)
            }
            composer
        }
        .background(Color(.systemGray6))
        .navigationTitle(conversation.name)
        .navigationBarTitleDisplayMode(.inline)
        .task(id: conversation.conversationID) {
            await model.loadTimeline(conversationID: conversation.conversationID, showErrors: true)
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(3))
                await model.loadTimeline(conversationID: conversation.conversationID)
            }
        }
        .confirmationDialog("发送文件", isPresented: $showingAttachments, titleVisibility: .visible) {
            Button("照片") { showingPhotoPicker = true }
            Button("文件") { showingFileImporter = true }
            Button("取消", role: .cancel) {}
        }
        .photosPicker(isPresented: $showingPhotoPicker, selection: $selectedPhoto, matching: .images)
        .onChange(of: selectedPhoto) { _, item in
            guard let item else { return }
            Task { await uploadPhoto(item) }
        }
        .fileImporter(isPresented: $showingFileImporter, allowedContentTypes: [.item]) { result in
            guard case .success(let url) = result else { return }
            let accessing = url.startAccessingSecurityScopedResource()
            Task {
                defer { if accessing { url.stopAccessingSecurityScopedResource() } }
                await model.upload(fileURL: url, fileName: url.lastPathComponent, mimeType: UTType(filenameExtension: url.pathExtension)?.preferredMIMEType, conversationID: conversation.conversationID)
            }
        }
        .confirmationDialog("永久删除这条消息？", isPresented: Binding(
            get: { pendingDelete != nil },
            set: { if !$0 { pendingDelete = nil } }
        )) {
            Button("删除", role: .destructive) {
                if let item = pendingDelete { Task { await model.delete(item, conversationID: conversation.conversationID) } }
                pendingDelete = nil
            }
            Button("取消", role: .cancel) { pendingDelete = nil }
        }
    }

    private var messages: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    ForEach(model.timeline) { item in
                        MessageRow(item: item, isMine: item.senderDeviceID == model.currentDevice?.id) {
                            pendingDelete = item
                        }
                        .id(item.id)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 16)
            }
            .defaultScrollAnchor(.bottom)
            .onChange(of: model.timeline.count) { _, _ in
                if let last = model.timeline.last { withAnimation { proxy.scrollTo(last.id, anchor: .bottom) } }
            }
        }
    }

    private var composer: some View {
        HStack(alignment: .bottom, spacing: 9) {
            TextField("输入消息", text: $text, axis: .vertical)
                .lineLimit(1...5)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 8))
                .submitLabel(.send)
                .onSubmit(send)
            if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                Button { showingAttachments = true } label: {
                    Image(systemName: "plus.circle").font(.system(size: 29, weight: .regular))
                }
                .foregroundStyle(.primary)
                .disabled(model.isUploading)
            } else {
                Button("发送", action: send)
                    .buttonStyle(.borderedProminent)
                    .buttonBorderShape(.roundedRectangle(radius: 7))
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(.bar)
    }

    private func send() {
        let sending = text
        Task {
            if await model.sendText(sending, conversationID: conversation.conversationID) { text = "" }
        }
    }

    private func uploadPhoto(_ item: PhotosPickerItem) async {
        defer { selectedPhoto = nil }
        do {
            guard let data = try await item.loadTransferable(type: Data.self) else { return }
            let type = item.supportedContentTypes.first ?? .jpeg
            let ext = type.preferredFilenameExtension ?? "jpg"
            let url = FileManager.default.temporaryDirectory.appendingPathComponent("SelfSend-\(UUID().uuidString).\(ext)")
            try data.write(to: url, options: .atomic)
            defer { try? FileManager.default.removeItem(at: url) }
            await model.upload(fileURL: url, fileName: "照片.\(ext)", mimeType: type.preferredMIMEType, conversationID: conversation.conversationID)
        } catch {
            model.alertMessage = error.localizedDescription
        }
    }
}

private struct MessageRow: View {
    @EnvironmentObject private var model: AppModel
    let item: TimelineItem
    let isMine: Bool
    let requestDelete: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            if isMine { Spacer(minLength: 42) }
            if !isMine { AvatarView(value: senderAvatar, size: 36) }
            VStack(alignment: isMine ? .trailing : .leading, spacing: 4) {
                if !isMine && senderName != "" { Text(senderName).font(.caption2).foregroundStyle(.secondary) }
                content
                Text(time).font(.caption2).foregroundStyle(.tertiary)
            }
            if isMine { AvatarView(value: model.currentDevice?.avatar ?? "📱", size: 36) }
            if !isMine { Spacer(minLength: 42) }
        }
        .contextMenu {
            if isMine { Button("删除", systemImage: "trash", role: .destructive, action: requestDelete) }
        }
    }

    @ViewBuilder private var content: some View {
        switch item {
        case .text(let message):
            Text(message.text)
                .font(.body)
                .foregroundStyle(.primary)
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(isMine ? SelfSendTheme.bubble : Color(.systemBackground), in: RoundedRectangle(cornerRadius: 7))
        case .file(let file):
            Button { Task { await model.download(file) } } label: {
                HStack(spacing: 11) {
                    Image(systemName: fileSymbol(file.mimeType))
                        .font(.title2)
                        .foregroundStyle(.white)
                        .frame(width: 44, height: 48)
                        .background(SelfSendTheme.green.gradient, in: RoundedRectangle(cornerRadius: 8))
                    VStack(alignment: .leading, spacing: 4) {
                        Text(file.fileName).font(.subheadline.weight(.medium)).foregroundStyle(.primary).lineLimit(2)
                        Text(ByteCountFormatter.string(fromByteCount: file.size, countStyle: .file)).font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 4)
                    Image(systemName: "arrow.down.circle").foregroundStyle(SelfSendTheme.green)
                }
                .frame(maxWidth: 280)
                .padding(9)
                .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 10))
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
        Date(timeIntervalSince1970: TimeInterval(item.createdAt) / 1000).formatted(date: .omitted, time: .shortened)
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
