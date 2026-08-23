import SwiftUI

/// Design tokens. Everything about colour, spacing or type lives here, so a
/// change in one place carries through the whole app.
///
/// Colours are dynamic (`UIColor { trait in ... }`), so they follow whatever
/// appearance is active — including the app's own Dark/Light/System setting,
/// which is applied at the root via `.preferredColorScheme`.
enum Theme {

    // MARK: - Colours

    enum C {
        static let base          = dyn(light: 0xF2F2F7, dark: 0x000000)
        static let card          = dyn(light: 0xFFFFFF, dark: 0x1C1C1E)
        static let cardElevated  = dyn(light: 0xF2F2F7, dark: 0x2C2C2E)
        static let hairline      = dynOpacity(light: (0x000000, 0.08), dark: (0xFFFFFF, 0.08))

        static let text          = dyn(light: 0x000000, dark: 0xFFFFFF)
        static let textSecondary = dynOpacity(light: (0x3C3C43, 0.60), dark: (0xEBEBF5, 0.62))
        static let textTertiary  = dynOpacity(light: (0x3C3C43, 0.30), dark: (0xEBEBF5, 0.32))

        static let accent        = Color(hex: 0x0A84FF)
        static let ok            = dyn(light: 0x248A3D, dark: 0x30D158)
        static let warn          = dyn(light: 0xC77400, dark: 0xFF9F0A)
        static let crit          = dyn(light: 0xD70015, dark: 0xFF375F)

        static let track         = dynOpacity(light: (0x000000, 0.08), dark: (0xFFFFFF, 0.10))

        // Icon tiles — these stay identical in both appearances; they carry
        // their own contrast via a 20% fill regardless of background.
        static let blue    = Color(hex: 0x0A84FF)
        static let cyan    = Color(hex: 0x32D0FF)
        static let magenta = Color(hex: 0xFF2D9B)
        static let red     = Color(hex: 0xFF453A)
        static let green   = Color(hex: 0x30D158)
        static let mint    = Color(hex: 0x66E39A)
        static let purple  = Color(hex: 0xBF5AF2)
        static let indigo  = Color(hex: 0x5E5CE6)
        static let orange  = Color(hex: 0xFF9F0A)
        static let teal    = Color(hex: 0x40C8E0)
        static let gray    = Color(hex: 0x8E8E93)
    }

    // MARK: - Gradients
    //
    // The gradient runs across the full width of the bar, not the filled
    // portion — otherwise the colour would shift with the fill and 22% would
    // look different from 80%. Gradients are identical in both appearances.

    enum G {
        static let cpu     = LinearGradient(colors: [C.blue, C.cyan], startPoint: .leading, endPoint: .trailing)
        static let ram     = LinearGradient(colors: [C.blue, C.cyan], startPoint: .leading, endPoint: .trailing)
        static let storage = LinearGradient(colors: [C.magenta, C.red], startPoint: .leading, endPoint: .trailing)
        static let load    = LinearGradient(colors: [C.green, C.mint], startPoint: .leading, endPoint: .trailing)

        static func status(_ s: Status) -> LinearGradient {
            switch s {
            case .ok:   LinearGradient(colors: [C.green, C.mint], startPoint: .leading, endPoint: .trailing)
            case .warn: LinearGradient(colors: [C.orange, Color(hex: 0xFFD60A)], startPoint: .leading, endPoint: .trailing)
            case .crit: LinearGradient(colors: [C.crit, Color(hex: 0xFF6482)], startPoint: .leading, endPoint: .trailing)
            }
        }
    }

    // MARK: - Metrics

    enum M {
        static let cardRadius: CGFloat = 20
        static let tileRadius: CGFloat = 10
        static let cardPadding: CGFloat = 16
        static let cardGap: CGFloat = 12
        static let sectionGap: CGFloat = 24
        static let screenMargin: CGFloat = 16
        static let barHeight: CGFloat = 8
        static let iconTile: CGFloat = 32
        /// Shared height for the CPU/RAM/Storage/Load tiles on Metrics, so all
        /// four are the same size regardless of how many lines their content
        /// happens to need.
        static let metricTileHeight: CGFloat = 150
    }

    /// A colour that is one fixed value in light mode and another in dark —
    /// resolved by the system (or our own override) at draw time.
    private static func dyn(light: UInt32, dark: UInt32) -> Color {
        Color(uiColor: UIColor { trait in
            trait.userInterfaceStyle == .dark ? UIColor(Color(hex: dark)) : UIColor(Color(hex: light))
        })
    }

    private static func dynOpacity(light: (UInt32, Double), dark: (UInt32, Double)) -> Color {
        Color(uiColor: UIColor { trait in
            trait.userInterfaceStyle == .dark
                ? UIColor(Color(hex: dark.0).opacity(dark.1))
                : UIColor(Color(hex: light.0).opacity(light.1))
        })
    }
}

/// Dark / Light / System, chosen in Settings and applied at the app root.
enum AppearanceMode: String, CaseIterable, Sendable {
    case system, light, dark

    var label: String {
        switch self {
        case .system: "System"
        case .light: "Light"
        case .dark: "Dark"
        }
    }

    /// nil means "follow the system", which is exactly what
    /// `.preferredColorScheme(nil)` does.
    var colorScheme: ColorScheme? {
        switch self {
        case .system: nil
        case .light: .light
        case .dark: .dark
        }
    }
}

enum Status: String, Codable, Sendable {
    case ok, warn, crit

    var color: Color {
        switch self {
        case .ok: Theme.C.ok
        case .warn: Theme.C.warn
        case .crit: Theme.C.crit
        }
    }

    /// Colour is never the only carrier of information.
    var symbol: String {
        switch self {
        case .ok: "checkmark.circle.fill"
        case .warn: "exclamationmark.triangle.fill"
        case .crit: "xmark.octagon.fill"
        }
    }

    static func forPercent(_ p: Double) -> Status {
        switch p {
        case ..<70: .ok
        case ..<90: .warn
        default: .crit
        }
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            opacity: 1
        )
    }
}
