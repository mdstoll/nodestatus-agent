import Foundation
import SwiftUI
import Observation

@MainActor
enum ConnectionState: Equatable, Sendable {
    case idle
    case connecting
    case live
    case reconnecting
    case failed(String)

    /// Korte tekst voor de badge. Een volledige foutmelding hoort daar niet:
    /// die maakt de kop onleesbaar.
    var label: String {
        switch self {
        case .idle: "—"
        case .connecting: "verbinden"
        case .live: "LIVE"
        case .reconnecting: "herverbinden"
        case .failed: T("offline", "offline")
        }
    }

    /// Volledige uitleg, alleen voor de kaart en de foutmelding.
    var detail: String? {
        switch self {
        case .failed(let m): m
        case .connecting: T("Connecting to the server…", "Verbinding maken met de server…")
        case .reconnecting: T("The connection dropped; retrying…", "De verbinding is weggevallen; opnieuw proberen…")
        default: nil
        }
    }

    var color: Color {
        switch self {
        case .live: Theme.C.ok
        case .connecting, .reconnecting: Theme.C.warn
        case .failed: Theme.C.crit
        case .idle: Theme.C.gray
        }
    }
}

/// Vaste ringbuffer voor de charts. Nooit een groeiende array: dat is de
/// klassieke reden waarom monitoring-apps na een uur honderden MB gebruiken.
struct RingBuffer<Element>: Sendable where Element: Sendable {
    private var storage: [Element] = []
    let capacity: Int

    init(capacity: Int) { self.capacity = capacity }

    mutating func append(_ e: Element) {
        storage.append(e)
        if storage.count > capacity { storage.removeFirst(storage.count - capacity) }
    }
    mutating func removeAll() { storage.removeAll(keepingCapacity: true) }
    var elements: [Element] { storage }
    var last: Element? { storage.last }
    var count: Int { storage.count }
}

@Observable
@MainActor
final class AppState {
    var servers: [Server] = []
    var selectedID: UUID?
    var connection: ConnectionState = .idle
    var prefs = Preferences()

    /// Gezet door een nodestatus://enroll-deeplink; de Server-tab pakt hem op.
    var pendingPairing: PairingInfo?

    // Live data van de geselecteerde server
    var system: SystemInfo?
    var latest: Sample?
    var history = RingBuffer<Sample>(capacity: 300)

    private let store = ServerStore()
    private var streamTask: Task<Void, Never>?

    init() {
        servers = store.load()
        selectedID = servers.first?.id
        if let raw = UserDefaults.standard.string(forKey: "selectedServer"), let id = UUID(uuidString: raw),
           servers.contains(where: { $0.id == id }) {
            selectedID = id
        }
        prefs.load()
    }

    var selected: Server? {
        guard let selectedID else { return nil }
        return servers.first { $0.id == selectedID }
    }

    func select(_ server: Server) {
        guard selectedID != server.id else { return }
        selectedID = server.id
        UserDefaults.standard.set(server.id.uuidString, forKey: "selectedServer")
        resetLiveData()
        restartStream()
    }

    func add(_ server: Server) {
        servers.append(server)
        store.save(servers)
        select(server)
    }

    func update(_ server: Server) {
        guard let i = servers.firstIndex(where: { $0.id == server.id }) else { return }
        servers[i] = server
        store.save(servers)
    }

    func remove(_ server: Server) {
        IdentityStore.remove(serverID: server.id)
        servers.removeAll { $0.id == server.id }
        store.save(servers)
        if selectedID == server.id {
            selectedID = servers.first?.id
            resetLiveData()
            restartStream()
        }
    }

    private func resetLiveData() {
        system = nil
        latest = nil
        history.removeAll()
    }

    func client(for server: Server) throws -> APIClient {
        guard let url = server.baseURL else { throw APIClientError.badURL }
        return APIClient(baseURL: url, credentials: try IdentityStore.load(serverID: server.id))
    }

    func clientForSelected() throws -> APIClient {
        guard let s = selected else { throw IdentityError.missingCredentials }
        return try client(for: s)
    }

    // MARK: - Live stream

    func restartStream() {
        streamTask?.cancel()
        guard let server = selected else {
            connection = .idle
            return
        }
        streamTask = Task { await runStream(server) }
    }

    func stopStream() {
        streamTask?.cancel()
        streamTask = nil
        if case .live = connection { connection = .idle }
    }

    /// Herverbindt met oplopende backoff. De UI toont dat als een discrete
    /// pill, niet als een alert — een korte netwerkonderbreking is normaal.
    private func runStream(_ server: Server) async {
        var backoff: UInt64 = 1
        while !Task.isCancelled {
            connection = history.count > 0 ? .reconnecting : .connecting
            do {
                let api = try client(for: server)
                if system == nil {
                    system = try await api.get("v1/system", as: SystemInfo.self)
                }
                for try await event in api.stream(backfill: prefs.historyWindow) {
                    if Task.isCancelled { break }
                    connection = .live
                    backoff = 1
                    history.append(event.value)
                    latest = event.value
                }
                if Task.isCancelled { return }
            } catch is CancellationError {
                return
            } catch {
                if Task.isCancelled { return }
                connection = .failed(error.localizedDescription)
            }
            try? await Task.sleep(for: .seconds(Double(backoff)))
            backoff = min(backoff * 2, 10)
        }
    }
}

/// App-brede voorkeuren uit het Settings-tabblad.
@Observable
final class Preferences {
    var fahrenheit = false
    var binaryUnits = false
    var bitsPerSecond = false
    var maskSensitive = true
    var warnBeforeSpeedtest = true
    var historyWindow = 60
    var appearance: AppearanceMode = .dark

    func load() {
        let d = UserDefaults.standard
        fahrenheit = d.bool(forKey: "fahrenheit")
        binaryUnits = d.bool(forKey: "binaryUnits")
        bitsPerSecond = d.bool(forKey: "bitsPerSecond")
        maskSensitive = d.object(forKey: "maskSensitive") as? Bool ?? true
        warnBeforeSpeedtest = d.object(forKey: "warnBeforeSpeedtest") as? Bool ?? true
        historyWindow = d.object(forKey: "historyWindow") as? Int ?? 60
        // Existing installs had no appearance setting and were dark-only;
        // keep that look rather than switching everyone to System on update.
        appearance = AppearanceMode(rawValue: d.string(forKey: "appearance") ?? "dark") ?? .dark
    }

    func save() {
        let d = UserDefaults.standard
        d.set(fahrenheit, forKey: "fahrenheit")
        d.set(binaryUnits, forKey: "binaryUnits")
        d.set(bitsPerSecond, forKey: "bitsPerSecond")
        d.set(maskSensitive, forKey: "maskSensitive")
        d.set(warnBeforeSpeedtest, forKey: "warnBeforeSpeedtest")
        d.set(historyWindow, forKey: "historyWindow")
        d.set(appearance.rawValue, forKey: "appearance")
    }
}
