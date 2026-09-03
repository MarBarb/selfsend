import SwiftUI

struct MainView: View {
    var body: some View {
        TabView {
            ConversationListView()
                .tabItem { Label("消息", systemImage: "message") }
            MeView()
                .tabItem { Label("我", systemImage: "person") }
        }
    }
}

