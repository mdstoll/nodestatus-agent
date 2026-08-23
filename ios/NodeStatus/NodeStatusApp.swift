import SwiftUI

@main
struct NodeStatusApp: App {
    @State private var app = AppState()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .preferredColorScheme(app.prefs.appearance.colorScheme)
                .tint(Theme.C.accent)
                .onChange(of: scenePhase) { _, phase in
                    // De stream stopt in de achtergrond: iOS zou hem toch
                    // killen, en netjes sluiten voorkomt een bevroren UI bij
                    // terugkeren.
                    switch phase {
                    case .active: app.restartStream()
                    case .background, .inactive: app.stopStream()
                    @unknown default: break
                    }
                }
        }
    }
}

enum AppTab: Hashable {
    case server, metrics, tools, settings
}

struct RootView: View {
    @Environment(AppState.self) private var app
    @State private var tab: AppTab = .metrics

    #if DEBUG
    /// Testvoorziening: hiermee kan een build automatisch koppelen en op een
    /// bepaald tabblad starten, zodat schermen zonder handmatig tikken te
    /// verifiëren zijn. Staat achter #if DEBUG en zit dus niet in een release.
    private func applyLaunchArguments() {
        let d = UserDefaults.standard
        if let raw = d.string(forKey: "SITab") {
            switch raw {
            case "server":   tab = .server
            case "tools":    tab = .tools
            case "settings": tab = .settings
            default:         tab = .metrics
            }
        }
        if let raw = d.string(forKey: "SIPairURL"), let url = URL(string: raw),
           let info = PairingInfo(url: url), app.servers.isEmpty {
            tab = .server
            app.pendingPairing = info
        }
    }
    #endif

    var body: some View {
        @Bindable var app = app
        TabView(selection: $tab) {
            Tab(T("Server", "Server"), systemImage: "server.rack", value: AppTab.server) {
                ServerListView()
            }
            Tab("Metrics", systemImage: "chart.bar.xaxis", value: AppTab.metrics) {
                MetricsView()
            }
            Tab("Tools", systemImage: "wrench.and.screwdriver", value: AppTab.tools) {
                ToolsView()
            }
            Tab("Settings", systemImage: "gearshape", value: AppTab.settings) {
                SettingsView()
            }
        }
        .tabBarMinimizeBehavior(.onScrollDown)
        .modifier(BottomAccessory(enabled: app.selected != nil))
        .onAppear {
            #if DEBUG
            applyLaunchArguments()
            #endif
            app.restartStream()
        }
        .onOpenURL { url in
            // nodestatus://enroll?… — vanuit de QR-scanner van het systeem of
            // door de koppel-link aan te tikken. Zelfde flow als scannen.
            guard let info = PairingInfo(url: url) else { return }
            tab = .server
            app.pendingPairing = info
        }
    }
}

/// De accessoire-strook alleen tonen als er iets te tonen valt: een lege
/// glazen capsule onder een leeg scherm ziet er kapot uit.
private struct BottomAccessory: ViewModifier {
    let enabled: Bool
    func body(content: Content) -> some View {
        if enabled {
            content.tabViewBottomAccessory { SelectedServerAccessory() }
        } else {
            content
        }
    }
}

/// De strook boven de tabbar: welke server je bekijkt en hoe die het doet,
/// ongeacht in welk tabblad je zit.
struct SelectedServerAccessory: View {
    @Environment(AppState.self) private var app

    var body: some View {
        HStack(spacing: 10) {
            StatusDot(status: app.connection == .live ? .ok : .warn,
                      pulsing: app.connection == .live)
            Text(app.selected?.name ?? "—")
                .font(.subheadline.weight(.medium))
                .lineLimit(1)
            Spacer(minLength: 6)
            if let s = app.latest {
                Label(Fmt.percent(s.cpu.total), systemImage: "cpu.fill")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(Theme.C.textSecondary)
                Label(Fmt.percent(s.memory.percent), systemImage: "memorychip.fill")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(Theme.C.textSecondary)
            } else {
                Text(app.connection.label)
                    .font(.caption)
                    .lineLimit(1)
                    .foregroundStyle(Theme.C.textTertiary)
            }
        }
        .padding(.horizontal, 14)
        .labelStyle(.titleAndIcon)
    }
}
