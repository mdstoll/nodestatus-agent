import Foundation
import SwiftUI
import Observation

/// Language choice. `system` follows the device language (falling back to
/// English for anything we don't have a translation for); `dutch` is only
/// offered as an explicit option when the device itself is set to Dutch —
/// otherwise it's a choice nobody who sees it would want.
enum AppLanguage: String, CaseIterable, Sendable {
    case system = "system"
    case english = "en"
    case dutch = "nl"

    var label: String {
        switch self {
        case .system: "System"
        case .english: "English"
        case .dutch: "Nederlands"
        }
    }

    /// Only the languages worth showing in the picker on this device.
    static var offered: [AppLanguage] {
        Localizer.systemIsDutch ? [.system, .english, .dutch] : [.system, .english]
    }
}

@Observable
@MainActor
final class Localizer {
    static let shared = Localizer()

    /// True if the device's own language is Dutch. Not actor-isolated: it's
    /// a pure computation with no shared state, and AppLanguage.offered
    /// (used from a synchronous enum context) needs to read it too.
    nonisolated static let systemIsDutch: Bool = {
        Locale.preferredLanguages.first?.hasPrefix("nl") ?? false
    }()

    /// What's actually stored in Settings — may be `.system`.
    var language: AppLanguage {
        didSet { UserDefaults.standard.set(language.rawValue, forKey: "appLanguage") }
    }

    /// What T(_:_:) actually uses: `.system` resolves to Dutch only when the
    /// device is Dutch, English otherwise. Never resolves to Dutch on a
    /// device that isn't Dutch, even if that was somehow stored previously.
    var effective: AppLanguage {
        switch language {
        case .system: Self.systemIsDutch ? .dutch : .english
        case .dutch: Self.systemIsDutch ? .dutch : .english
        case .english: .english
        }
    }

    private init() {
        let stored = UserDefaults.standard.string(forKey: "appLanguage")
        language = stored.flatMap(AppLanguage.init(rawValue:)) ?? .system
    }
}

/// Translates one string. Both languages sit side by side at the call site
/// rather than in separate string-table files — reads better and works
/// naturally with string interpolation.
///
/// Reads `Localizer.shared.effective`, so SwiftUI redraws automatically the
/// moment the language (or the resolved system language) changes.
@MainActor
func T(_ english: String, _ dutch: String) -> String {
    Localizer.shared.effective == .dutch ? dutch : english
}
