import SwiftUI

@main
struct SelfSendMacApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            MacRootView()
                .environmentObject(model)
                .tint(SelfSendTheme.green)
                .frame(minWidth: 760, minHeight: 520)
        }
        .defaultSize(width: 1040, height: 720)
        .commands {
            SidebarCommands()
        }

        Settings {
            MacSettingsView()
                .environmentObject(model)
                .tint(SelfSendTheme.green)
        }
    }
}

enum SelfSendTheme {
    static let green = Color(red: 0.03, green: 0.76, blue: 0.38)
    static let bubble = Color(red: 0.58, green: 0.93, blue: 0.41)
}
