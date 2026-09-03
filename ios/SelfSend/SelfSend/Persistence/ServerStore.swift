import Foundation

enum ServerAddressError: LocalizedError {
    case missingScheme
    case unsupportedScheme
    case publicHTTP
    case credentials

    var errorDescription: String? {
        switch self {
        case .missingScheme: "请输入包含 http:// 或 https:// 的完整地址"
        case .unsupportedScheme: "服务器地址只支持 HTTP 或 HTTPS"
        case .publicHTTP: "公网服务器必须使用 HTTPS"
        case .credentials: "服务器地址不能包含用户名或密码"
        }
    }
}

enum ServerStore {
#if os(macOS)
    private static let directoryKey = "selfsend.macos.servers.v1"
    private static let selectedKey = "selfsend.macos.selected-server.v1"
#else
    private static let directoryKey = "selfsend.ios.servers.v1"
    private static let selectedKey = "selfsend.ios.selected-server.v1"
#endif

    static func load() -> [ServerProfile] {
        guard let data = UserDefaults.standard.data(forKey: directoryKey),
              let servers = try? JSONDecoder().decode([ServerProfile].self, from: data) else { return [] }
        return servers
    }

    static func save(_ servers: [ServerProfile]) {
        guard let data = try? JSONEncoder().encode(servers) else { return }
        UserDefaults.standard.set(data, forKey: directoryKey)
    }

    static var selectedID: UUID? {
        get { UserDefaults.standard.string(forKey: selectedKey).flatMap(UUID.init(uuidString:)) }
        set { UserDefaults.standard.set(newValue?.uuidString, forKey: selectedKey) }
    }

    static func normalizedURL(_ value: String, deploymentType: DeploymentType) throws -> URL {
        guard value.contains("://"), var components = URLComponents(string: value.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            throw ServerAddressError.missingScheme
        }
        guard components.scheme == "http" || components.scheme == "https", components.host != nil else {
            throw ServerAddressError.unsupportedScheme
        }
        if components.user != nil || components.password != nil { throw ServerAddressError.credentials }
        if components.scheme == "http" && (deploymentType == .cloud || !isLocalHost(components.host ?? "")) {
            throw ServerAddressError.publicHTTP
        }
        components.path = ""
        components.query = nil
        components.fragment = nil
        guard let result = components.url else { throw ServerAddressError.unsupportedScheme }
        return result
    }

    static func upsert(_ profile: ServerProfile, into servers: [ServerProfile]) -> [ServerProfile] {
        var result = servers
        if let index = result.firstIndex(where: {
            ($0.instanceID != nil && $0.instanceID == profile.instanceID) || $0.baseURL == profile.baseURL || $0.id == profile.id
        }) {
            var merged = profile
            merged.id = result[index].id
            result[index] = merged
        } else {
            result.append(profile)
        }
        result.sort { ($0.lastConnectedAt ?? .distantPast) > ($1.lastConnectedAt ?? .distantPast) }
        return result
    }

    private static func isLocalHost(_ value: String) -> Bool {
        let host = value.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
        if host == "localhost" || host == "::1" || host.hasSuffix(".local") || host.hasSuffix(".home") || host.hasSuffix(".lan") { return true }
        if host.hasPrefix("fe80:") || host.hasPrefix("fc") || host.hasPrefix("fd") { return true }
        let parts = host.split(separator: ".").compactMap { Int($0) }
        guard parts.count == 4, parts.allSatisfy({ (0...255).contains($0) }) else { return false }
        return parts[0] == 10 || parts[0] == 127 ||
            (parts[0] == 169 && parts[1] == 254) ||
            (parts[0] == 172 && (16...31).contains(parts[1])) ||
            (parts[0] == 192 && parts[1] == 168)
    }
}
