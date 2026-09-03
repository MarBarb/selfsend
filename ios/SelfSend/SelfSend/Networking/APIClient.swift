import Foundation
import UniformTypeIdentifiers

enum APIClientError: LocalizedError {
    case invalidResponse
    case server(Int, String)
    case invalidUploadLocation

    var errorDescription: String? {
        switch self {
        case .invalidResponse: "服务器返回了无法识别的响应"
        case .server(_, let message): message
        case .invalidUploadLocation: "服务器返回了无效的上传地址"
        }
    }
}

final class APIClient {
    let baseURL: URL
    private let session: URLSession
    private let decoder: JSONDecoder

    init(baseURL: URL) {
        self.baseURL = baseURL
        let configuration = URLSessionConfiguration.default
        configuration.httpCookieStorage = .shared
        configuration.httpShouldSetCookies = true
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 60 * 60
        self.session = URLSession(configuration: configuration)
        self.decoder = JSONDecoder()
        self.decoder.keyDecodingStrategy = .convertFromSnakeCase
    }

    func status() async throws -> InstanceStatus {
        try await request("api/status")
    }

    func setup(password: String) async throws {
        let _: OKResponse = try await request("api/setup", method: "POST", json: ["password": password])
    }

    func login(password: String) async throws {
        let _: OKResponse = try await request("api/login", method: "POST", json: ["password": password])
    }

    func logout() async throws {
        try await requestVoid("api/logout", method: "POST")
    }

    func registerDevice(id: String, name: String, avatar: String) async throws -> Device {
        try await request("api/devices/register", method: "POST", json: ["id": id, "name": name, "avatar": avatar])
    }

    func updateDevice(id: String, name: String, avatar: String) async throws -> Device {
        try await request("api/devices/\(path(id))", method: "PATCH", json: ["name": name, "avatar": avatar])
    }

    func conversations() async throws -> [Conversation] {
        let response: ConversationsResponse = try await request("api/conversations")
        return response.conversations
    }

    func timeline(conversationID: String, before: Int64? = nil) async throws -> TimelineResponse {
        var components = URLComponents(url: endpoint("api/items"), resolvingAgainstBaseURL: false)
        components?.queryItems = [
            URLQueryItem(name: "conversation_id", value: conversationID),
            URLQueryItem(name: "limit", value: "50"),
        ]
        if let before { components?.queryItems?.append(URLQueryItem(name: "before", value: String(before))) }
        guard let url = components?.url else { throw APIClientError.invalidResponse }
        return try await request(url: url)
    }

    func sendText(conversationID: String, text: String) async throws -> TextItem {
        try await request("api/notes", method: "POST", json: ["conversation_id": conversationID, "text": text])
    }

    func deleteItem(id: String) async throws {
        try await requestVoid("api/items/\(path(id))", method: "DELETE")
    }

    func serverDetails() async throws -> ServerDetails {
        try await request("api/server")
    }

    func upload(fileURL: URL, fileName: String, mimeType: String?, conversationID: String) async throws {
        let attributes = try FileManager.default.attributesOfItem(atPath: fileURL.path)
        let fileSize = (attributes[.size] as? NSNumber)?.int64Value ?? 0
        let modified = (attributes[.modificationDate] as? Date).map { Int64($0.timeIntervalSince1970 * 1000) } ?? 0
        let metadata = [
            "filename \(metadataValue(fileName))",
            "filetype \(metadataValue(mimeType ?? inferredMIMEType(fileURL: fileURL)))",
            "lastmodified \(metadataValue(String(modified)))",
            "conversation \(metadataValue(conversationID))",
        ].joined(separator: ",")

        var create = URLRequest(url: endpoint("api/uploads"))
        create.httpMethod = "POST"
        create.setValue("1.0.0", forHTTPHeaderField: "Tus-Resumable")
        create.setValue(String(fileSize), forHTTPHeaderField: "Upload-Length")
        create.setValue(metadata, forHTTPHeaderField: "Upload-Metadata")
        let (_, createResponse) = try await perform(create)
        guard let location = createResponse.value(forHTTPHeaderField: "Location"), location.hasPrefix("/api/uploads/") else {
            throw APIClientError.invalidUploadLocation
        }
        guard fileSize > 0 else { return }

        let uploadURL = URL(string: location, relativeTo: baseURL)!.absoluteURL
        let handle = try FileHandle(forReadingFrom: fileURL)
        defer { try? handle.close() }
        var offset: Int64 = 0
        let chunkSize = 4 * 1024 * 1024
        while offset < fileSize {
            try handle.seek(toOffset: UInt64(offset))
            let data = try handle.read(upToCount: min(chunkSize, Int(fileSize - offset))) ?? Data()
            if data.isEmpty { throw APIClientError.invalidResponse }
            var patch = URLRequest(url: uploadURL)
            patch.httpMethod = "PATCH"
            patch.httpBody = data
            patch.setValue("1.0.0", forHTTPHeaderField: "Tus-Resumable")
            patch.setValue("application/offset+octet-stream", forHTTPHeaderField: "Content-Type")
            patch.setValue(String(offset), forHTTPHeaderField: "Upload-Offset")
            let (_, response) = try await perform(patch)
            offset = Int64(response.value(forHTTPHeaderField: "Upload-Offset") ?? "") ?? (offset + Int64(data.count))
        }
    }

    func download(item: FileItem) async throws -> URL {
        let request = URLRequest(url: endpoint("api/files/\(path(item.id))"))
        let (temporaryURL, response) = try await session.download(for: request)
        try validate(response: response, data: nil)
        let directory = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("SelfSendDownloads", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let target = directory.appendingPathComponent("\(UUID().uuidString)-\(safeFileName(item.fileName))")
        try? FileManager.default.removeItem(at: target)
        try FileManager.default.moveItem(at: temporaryURL, to: target)
        return target
    }

    private func request<T: Decodable>(_ relativePath: String, method: String = "GET", json: [String: Any]? = nil) async throws -> T {
        try await request(url: endpoint(relativePath), method: method, json: json)
    }

    private func request<T: Decodable>(url: URL, method: String = "GET", json: [String: Any]? = nil) async throws -> T {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let json {
            request.httpBody = try JSONSerialization.data(withJSONObject: json)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, _) = try await perform(request)
        return try decoder.decode(T.self, from: data)
    }

    private func requestVoid(_ relativePath: String, method: String) async throws {
        var request = URLRequest(url: endpoint(relativePath))
        request.httpMethod = method
        _ = try await perform(request)
    }

    private func perform(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw APIClientError.invalidResponse }
        try validate(response: http, data: data)
        return (data, http)
    }

    private func validate(response: URLResponse, data: Data?) throws {
        guard let http = response as? HTTPURLResponse else { throw APIClientError.invalidResponse }
        guard (200..<300).contains(http.statusCode) else {
            let message = data.flatMap { try? decoder.decode(ErrorResponse.self, from: $0).error } ?? "请求失败（\(http.statusCode)）"
            throw APIClientError.server(http.statusCode, message)
        }
    }

    private func endpoint(_ relativePath: String) -> URL {
        baseURL.appending(path: relativePath)
    }

    private func path(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? value
    }

    private func metadataValue(_ value: String) -> String {
        Data(value.utf8).base64EncodedString()
    }

    private func inferredMIMEType(fileURL: URL) -> String {
        UTType(filenameExtension: fileURL.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
    }

    private func safeFileName(_ value: String) -> String {
        value.replacingOccurrences(of: "/", with: "-").replacingOccurrences(of: ":", with: "-")
    }
}
