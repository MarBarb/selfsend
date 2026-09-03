import Foundation
#if canImport(Combine)
import Combine
#endif
#if os(iOS)
import UIKit
#elseif os(macOS)
import AppKit
#endif

enum AppPhase: Equatable {
    case noServer
    case connecting
    case setup
    case login
    case ready
    case failed(String)
}

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var phase: AppPhase = .connecting
    @Published private(set) var servers: [ServerProfile]
    @Published private(set) var currentServer: ServerProfile?
    @Published private(set) var status: InstanceStatus?
    @Published private(set) var conversations: [Conversation] = []
    @Published private(set) var timeline: [TimelineItem] = []
    @Published private(set) var serverDetails: ServerDetails?
    @Published var isWorking = false
    @Published var isUploading = false
    @Published var alertMessage: String?
    @Published var sharedFileURL: URL?

    private var client: APIClient?
    private var activeTimelineConversationID: String?

    init() {
        let stored = ServerStore.load()
        self.servers = stored
        if let selectedID = ServerStore.selectedID, let selected = stored.first(where: { $0.id == selectedID }) {
            Task { await connect(to: selected) }
        } else if let first = stored.first {
            Task { await connect(to: first) }
        } else {
            phase = .noServer
        }
    }

    var currentDevice: Device? { status?.device }

    func addServer(name: String, address: String, deploymentType: DeploymentType) async throws {
        let url = try ServerStore.normalizedURL(address, deploymentType: deploymentType)
        let profile = ServerProfile(name: name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? deploymentType.title : name.trimmingCharacters(in: .whitespacesAndNewlines), baseURL: url, deploymentType: deploymentType)
        servers = ServerStore.upsert(profile, into: servers)
        ServerStore.save(servers)
        await connect(to: profile)
    }

    func connect(to profile: ServerProfile) async {
        phase = .connecting
        currentServer = profile
        ServerStore.selectedID = profile.id
        client = APIClient(baseURL: profile.baseURL)
        conversations = []
        timeline = []
        activeTimelineConversationID = nil
        serverDetails = nil
        do {
            let loaded = try await requireClient().status()
            status = loaded
            remember(identity: loaded.server, fallback: profile)
            if loaded.setupRequired { phase = .setup }
            else if loaded.authenticated { try await enterAuthenticated(status: loaded) }
            else { phase = .login }
        } catch {
            phase = .failed(message(for: error))
        }
    }

    func chooseAnotherServer() {
        phase = .noServer
        currentServer = nil
        client = nil
        status = nil
    }

    func initializeServer(password: String) async {
        await authenticate {
            try await self.requireClient().setup(password: password)
        }
    }

    func login(password: String) async {
        await authenticate {
            try await self.requireClient().login(password: password)
        }
    }

    private func authenticate(_ operation: () async throws -> Void) async {
        guard !isWorking else { return }
        isWorking = true
        defer { isWorking = false }
        do {
            try await operation()
            let loaded = try await requireClient().status()
            status = loaded
            try await enterAuthenticated(status: loaded)
        } catch {
            alertMessage = message(for: error)
        }
    }

    private func enterAuthenticated(status initialStatus: InstanceStatus) async throws {
        var loaded = initialStatus
        if loaded.device == nil {
            let instanceKey = loaded.server?.instanceID ?? currentServer?.baseURL.absoluteString ?? "default"
            let defaultsKey = "selfsend.\(Self.platformKey).device.\(instanceKey)"
            let deviceID = UserDefaults.standard.string(forKey: defaultsKey) ?? UUID().uuidString.lowercased()
            UserDefaults.standard.set(deviceID, forKey: defaultsKey)
            _ = try await requireClient().registerDevice(
                id: deviceID,
                name: Self.defaultDeviceName,
                avatar: Self.defaultDeviceAvatar
            )
            loaded = try await requireClient().status()
        }
        status = loaded
        remember(identity: loaded.server, fallback: currentServer)
        conversations = try await requireClient().conversations()
        phase = .ready
    }

    func refreshConversations(showErrors: Bool = false) async {
        guard phase == .ready else { return }
        do { conversations = try await requireClient().conversations() }
        catch { if showErrors { alertMessage = message(for: error) } }
    }

    func loadTimeline(conversationID: String, showErrors: Bool = false) async {
        guard phase == .ready else { return }
        if activeTimelineConversationID != conversationID {
            activeTimelineConversationID = conversationID
            timeline = []
        }
        do {
            let loaded = try await requireClient().timeline(conversationID: conversationID).items.sorted { $0.createdAt < $1.createdAt }
            guard activeTimelineConversationID == conversationID else { return }
            timeline = loaded
        }
        catch { if showErrors { alertMessage = message(for: error) } }
    }

    func sendText(_ text: String, conversationID: String) async -> Bool {
        let value = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return false }
        do {
            _ = try await requireClient().sendText(conversationID: conversationID, text: value)
            await loadTimeline(conversationID: conversationID, showErrors: true)
            await refreshConversations()
            return true
        } catch {
            alertMessage = message(for: error)
            return false
        }
    }

    func upload(fileURL: URL, fileName: String, mimeType: String?, conversationID: String) async {
        guard !isUploading else { return }
        isUploading = true
        defer { isUploading = false }
        do {
            try await requireClient().upload(fileURL: fileURL, fileName: fileName, mimeType: mimeType, conversationID: conversationID)
            await loadTimeline(conversationID: conversationID, showErrors: true)
            await refreshConversations()
        } catch {
            alertMessage = message(for: error)
        }
    }

    func download(_ item: FileItem) async {
        do { sharedFileURL = try await requireClient().download(item: item) }
        catch { alertMessage = message(for: error) }
    }

    func delete(_ item: TimelineItem, conversationID: String) async {
        do {
            try await requireClient().deleteItem(id: item.id)
            timeline.removeAll { $0.id == item.id }
            await refreshConversations()
        } catch { alertMessage = message(for: error) }
    }

    func updateCurrentDevice(name: String, avatar: String) async -> Bool {
        guard let device = currentDevice else { return false }
        do {
            let updated = try await requireClient().updateDevice(id: device.id, name: name, avatar: avatar)
            if var currentStatus = status {
                currentStatus = InstanceStatus(
                    setupRequired: currentStatus.setupRequired,
                    authenticated: currentStatus.authenticated,
                    maxUploadSize: currentStatus.maxUploadSize,
                    version: currentStatus.version,
                    itemCount: currentStatus.itemCount,
                    totalBytes: currentStatus.totalBytes,
                    device: updated,
                    server: currentStatus.server
                )
                status = currentStatus
            }
            return true
        } catch {
            alertMessage = message(for: error)
            return false
        }
    }

    func loadServerDetails() async {
        do { serverDetails = try await requireClient().serverDetails() }
        catch { alertMessage = message(for: error) }
    }

    func logout() async {
        do { try await requireClient().logout() } catch { /* Local state still returns to login. */ }
        status = nil
        conversations = []
        timeline = []
        activeTimelineConversationID = nil
        phase = .login
    }

    func removeServer(_ profile: ServerProfile) {
        guard profile.id != currentServer?.id else { return }
        servers.removeAll { $0.id == profile.id }
        ServerStore.save(servers)
    }

    private func remember(identity: ServerIdentity?, fallback: ServerProfile?) {
        guard var profile = fallback ?? currentServer else { return }
        if let identity {
            profile.instanceID = identity.instanceID
            if profile.name == profile.deploymentType.title || profile.name.isEmpty {
                profile.name = identity.serverDeviceName ?? identity.instanceName
            }
            profile.deploymentType = identity.deploymentType
            profile.provider = identity.provider
        }
        profile.lastConnectedAt = Date()
        servers = ServerStore.upsert(profile, into: servers)
        currentServer = servers.first(where: { $0.id == profile.id || ($0.instanceID != nil && $0.instanceID == profile.instanceID) }) ?? profile
        if let currentServer { ServerStore.selectedID = currentServer.id }
        ServerStore.save(servers)
    }

    private func requireClient() throws -> APIClient {
        guard let client else { throw APIClientError.invalidResponse }
        return client
    }

    private func message(for error: Error) -> String {
        if let localized = error as? LocalizedError, let message = localized.errorDescription { return message }
        return error.localizedDescription
    }

    private static var platformKey: String {
#if os(macOS)
        "macos"
#else
        "ios"
#endif
    }

    private static var defaultDeviceName: String {
#if os(macOS)
        Host.current().localizedName ?? "Mac"
#else
        UIDevice.current.name
#endif
    }

    private static var defaultDeviceAvatar: String {
#if os(macOS)
        "💻"
#else
        "📱"
#endif
    }
}
