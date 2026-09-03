import XCTest
@testable import SelfSend

final class ServerStoreTests: XCTestCase {
    func testLocalHTTPAddressIsAccepted() throws {
        let url = try ServerStore.normalizedURL("http://192.168.1.20:8080/path", deploymentType: .local)
        XCTAssertEqual(url.absoluteString, "http://192.168.1.20:8080")
    }

    func testPublicHTTPAddressIsRejected() {
        XCTAssertThrowsError(try ServerStore.normalizedURL("http://send.example.com", deploymentType: .cloud))
    }

    func testHTTPSAddressIsAcceptedForCloud() throws {
        let url = try ServerStore.normalizedURL("https://send.example.com/", deploymentType: .cloud)
        XCTAssertEqual(url.absoluteString, "https://send.example.com")
    }

    func testUpsertDeduplicatesByInstanceID() throws {
        let first = ServerProfile(instanceID: "instance", name: "旧地址", baseURL: URL(string: "http://192.168.1.2:8080")!, deploymentType: .local)
        let moved = ServerProfile(instanceID: "instance", name: "新地址", baseURL: URL(string: "https://send.example.com")!, deploymentType: .cloud)
        let result = ServerStore.upsert(moved, into: [first])
        XCTAssertEqual(result.count, 1)
        XCTAssertEqual(result[0].id, first.id)
        XCTAssertEqual(result[0].baseURL, moved.baseURL)
    }
}
