import Foundation

enum DeploymentType: String, Codable, CaseIterable, Identifiable {
    case local
    case cloud
    case nas

    var id: String { rawValue }

    var title: String {
        switch self {
        case .local: "本地服务器"
        case .cloud: "云端服务器"
        case .nas: "NAS 服务器"
        }
    }

    var shortTitle: String {
        switch self {
        case .local: "本地"
        case .cloud: "云端"
        case .nas: "NAS"
        }
    }

    var symbol: String {
        switch self {
        case .local: "desktopcomputer"
        case .cloud: "cloud"
        case .nas: "externaldrive.connected.to.line.below"
        }
    }
}

struct ServerProfile: Codable, Identifiable, Hashable {
    var id: UUID
    var instanceID: String?
    var name: String
    var baseURL: URL
    var deploymentType: DeploymentType
    var provider: String?
    var lastConnectedAt: Date?

    init(
        id: UUID = UUID(),
        instanceID: String? = nil,
        name: String,
        baseURL: URL,
        deploymentType: DeploymentType,
        provider: String? = nil,
        lastConnectedAt: Date? = nil
    ) {
        self.id = id
        self.instanceID = instanceID
        self.name = name
        self.baseURL = baseURL
        self.deploymentType = deploymentType
        self.provider = provider
        self.lastConnectedAt = lastConnectedAt
    }
}

struct ServerIdentity: Codable, Hashable {
    let instanceID: String
    let instanceName: String
    let canonicalURL: String
    let state: String
    let successorURL: String?
    let serverDeviceID: String?
    let serverDeviceName: String?
    let deploymentType: DeploymentType
    let provider: String?
    let migrationEpoch: Int64
}

struct InstanceStatus: Codable {
    let setupRequired: Bool
    let authenticated: Bool
    let maxUploadSize: Int64
    let version: String
    let itemCount: Int64?
    let totalBytes: Int64?
    let device: Device?
    let server: ServerIdentity?
}

struct Device: Codable, Identifiable, Hashable {
    let id: String
    var name: String
    var avatar: String
    let createdAt: Int64
    let lastSeenAt: Int64
    let isServer: Bool
}

struct Conversation: Codable, Identifiable, Hashable {
    let id: String
    let conversationID: String
    let kind: String
    let name: String
    let avatar: String
    let createdAt: Int64
    let lastSeenAt: Int64?
    let lastMessageAt: Int64
    let lastKind: String?
    let lastPreview: String?
    let memberCount: Int?
    let isServer: Bool

    var preview: String {
        guard lastMessageAt > 0 else {
            return kind == "group" ? "\(memberCount ?? 0) 台设备" : "现在可以给这台设备发消息"
        }
        return lastKind == "file" ? "[文件] \(lastPreview ?? "")" : (lastPreview ?? "")
    }
}

struct TextItem: Codable, Identifiable, Hashable {
    let kind: String
    let id: String
    let text: String
    let createdAt: Int64
    let senderDeviceID: String
    let senderName: String
    let senderAvatar: String
}

struct FileItem: Codable, Identifiable, Hashable {
    let kind: String
    let id: String
    let fileName: String
    let mimeType: String
    let size: Int64
    let sha256: String
    let createdAt: Int64
    let lastModified: Int64?
    let senderDeviceID: String
    let senderName: String
    let senderAvatar: String
}

enum TimelineItem: Decodable, Identifiable, Hashable {
    case text(TextItem)
    case file(FileItem)

    var id: String {
        switch self {
        case .text(let item): item.id
        case .file(let item): item.id
        }
    }

    var createdAt: Int64 {
        switch self {
        case .text(let item): item.createdAt
        case .file(let item): item.createdAt
        }
    }

    var senderDeviceID: String {
        switch self {
        case .text(let item): item.senderDeviceID
        case .file(let item): item.senderDeviceID
        }
    }

    private enum CodingKeys: String, CodingKey { case kind }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        switch try container.decode(String.self, forKey: .kind) {
        case "text": self = .text(try TextItem(from: decoder))
        case "file": self = .file(try FileItem(from: decoder))
        default:
            throw DecodingError.dataCorruptedError(forKey: .kind, in: container, debugDescription: "Unknown timeline item")
        }
    }
}

struct TimelineResponse: Decodable {
    let items: [TimelineItem]
    let nextCursor: Int64
}

struct ConversationsResponse: Decodable {
    let conversations: [Conversation]
}

struct OKResponse: Decodable { let ok: Bool }

struct ErrorResponse: Decodable { let error: String? }

struct ServerDetails: Decodable {
    let server: ServerIdentity
    let itemCount: Int64
    let totalBytes: Int64
    let pendingUploads: Int64
    let version: String
}
