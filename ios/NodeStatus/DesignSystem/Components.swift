import SwiftUI

// MARK: - Kaart

/// De basiskaart uit de screenshots: donker vlak, 20 pt radius, haarlijn.
struct Card<Content: View>: View {
    var padding: CGFloat = Theme.M.cardPadding
    @ViewBuilder var content: Content

    var body: some View {
        content
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: Theme.M.cardRadius, style: .continuous)
                    .fill(Theme.C.card)
            )
            .overlay(
                RoundedRectangle(cornerRadius: Theme.M.cardRadius, style: .continuous)
                    .strokeBorder(Theme.C.hairline, lineWidth: 0.5)
            )
    }
}

/// Gekleurde icoontegel, 32×32 — het herhalende element in alle screenshots.
struct IconTile: View {
    let symbol: String
    let color: Color
    var size: CGFloat = Theme.M.iconTile

    var body: some View {
        RoundedRectangle(cornerRadius: Theme.M.tileRadius, style: .continuous)
            .fill(color.opacity(0.20))
            .frame(width: size, height: size)
            .overlay(
                Image(systemName: symbol)
                    .font(.system(size: size * 0.5, weight: .semibold))
                    .foregroundStyle(color)
            )
    }
}

// MARK: - Voortgangsbalk

/// De gradientbalk. De gradient loopt over de volle breedte en niet over het
/// gevulde deel, zodat 22% dezelfde kleurovergang heeft als 80%.
struct GaugeBar: View {
    let fraction: Double
    let gradient: LinearGradient
    var height: CGFloat = Theme.M.barHeight

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.C.track)
                Capsule()
                    .fill(gradient)
                    .mask(alignment: .leading) {
                        // Minimaal 6 pt: 0,3% moet een stipje zijn, geen niets.
                        Rectangle()
                            .frame(width: max(fraction > 0 ? 6 : 0,
                                              geo.size.width * min(max(fraction, 0), 1)))
                    }
            }
        }
        .frame(height: height)
        .animation(.easeOut(duration: 0.35), value: fraction)
    }
}

/// Gesegmenteerde balk voor meerdere volumes: één balk, wel zichtbare verdeling.
struct SegmentedGaugeBar: View {
    struct Segment: Identifiable {
        let id: String
        let fraction: Double
        let tint: Double  // 0…1 positie in het verloop
    }
    let segments: [Segment]
    var height: CGFloat = Theme.M.barHeight

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.C.track)
                HStack(spacing: 1) {
                    ForEach(segments) { s in
                        Rectangle()
                            .fill(color(for: s.tint))
                            .frame(width: max(0, geo.size.width * min(max(s.fraction, 0), 1)))
                    }
                    Spacer(minLength: 0)
                }
                .clipShape(Capsule())
            }
        }
        .frame(height: height)
    }

    private func color(for t: Double) -> Color {
        // Interpolatie tussen magenta en rood, hetzelfde verloop als één volume.
        let from = (r: 1.0, g: 0.176, b: 0.608)
        let to = (r: 1.0, g: 0.271, b: 0.227)
        return Color(.sRGB,
                     red: from.r + (to.r - from.r) * t,
                     green: from.g + (to.g - from.g) * t,
                     blue: from.b + (to.b - from.b) * t)
    }
}

// MARK: - Metrische tegel

/// De 2×2-tegels van het Metrics-scherm: icoon + titel, balk, waarde rechts.
struct MetricTile: View {
    let title: String
    let symbol: String
    let tint: Color
    let gradient: LinearGradient
    let fraction: Double
    let value: String
    var caption: String?
    /// Shows a chevron to signal that tapping this tile goes somewhere.
    /// Purely visual — the NavigationLink wrapping happens at the call site.
    var isLink: Bool = false

    var body: some View {
        Card(padding: 14) {
            // maxHeight .infinity met .top: de rij in de LazyVGrid is zo hoog
            // als de hoogste tegel, en elke tegel rekt daarnaar uit in plaats
            // van een vaste hoogte te forceren. Wordt één tegel langer (een
            // extra regel, een langere waarde), dan groeit zijn buurman mee en
            // blijft de inhoud bovenaan staan.
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 10) {
                    IconTile(symbol: symbol, color: tint)
                    Text(title)
                        .font(.headline)
                        .foregroundStyle(Theme.C.text)
                    if isLink {
                        Spacer(minLength: 0)
                        Image(systemName: "chevron.right")
                            .font(.caption2.weight(.semibold))
                            .foregroundStyle(Theme.C.textTertiary)
                    }
                }
                VStack(alignment: .leading, spacing: 6) {
                    GaugeBar(fraction: fraction, gradient: gradient)
                    // Caption en waarde blijven op één regel, net als in de
                    // referentie. De caption krimpt mee; de waarde niet.
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        if let caption {
                            Text(caption)
                                .font(.footnote)
                                .monospacedDigit()
                                .lineLimit(1)
                                .minimumScaleFactor(0.55)
                                .foregroundStyle(Theme.C.textSecondary)
                        }
                        Spacer(minLength: 0)
                        Text(value)
                            .font(.system(.title3, design: .default, weight: .bold))
                            .monospacedDigit()
                            .lineLimit(1)
                            .fixedSize()
                            .contentTransition(.numericText())
                            .foregroundStyle(Theme.C.text)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
        .frame(minHeight: Theme.M.metricTileHeight)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(title), \(value)")
    }
}

// MARK: - Kleine bouwstenen

struct SectionTitle: View {
    let text: String
    var trailing: AnyView?

    init(_ text: String) { self.text = text; self.trailing = nil }
    init<T: View>(_ text: String, @ViewBuilder trailing: () -> T) {
        self.text = text
        self.trailing = AnyView(trailing())
    }

    var body: some View {
        HStack {
            Text(text)
                .font(.title3.bold())
                .foregroundStyle(Theme.C.text)
            Spacer()
            trailing
        }
    }
}

/// Label/waarde-paar zoals in het identiteitsblok en het Locale-scherm.
struct KeyValue: View {
    let key: String
    let value: String
    var mono = false

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(key)
                .font(.subheadline)
                .foregroundStyle(Theme.C.text)
            Text(value)
                .font(mono ? .system(.subheadline, design: .monospaced) : .subheadline)
                .foregroundStyle(Theme.C.textSecondary)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct StatusDot: View {
    let status: Status
    var pulsing = false
    @State private var on = false

    var body: some View {
        Circle()
            .fill(status.color)
            .frame(width: 8, height: 8)
            .opacity(pulsing && on ? 0.35 : 1)
            .animation(pulsing ? .easeInOut(duration: 1).repeatForever(autoreverses: true) : nil, value: on)
            .onAppear { on = pulsing }
    }
}

/// Rij in de Tools-lijst: gekleurde tegel, titel, chevron.
struct ToolRow<Destination: View>: View {
    let title: String
    let symbol: String
    let tint: Color
    var subtitle: String?
    @ViewBuilder var destination: Destination

    var body: some View {
        NavigationLink {
            destination
        } label: {
            HStack(spacing: 12) {
                IconTile(symbol: symbol, color: tint)
                VStack(alignment: .leading, spacing: 1) {
                    Text(title).foregroundStyle(Theme.C.text)
                    if let subtitle {
                        Text(subtitle).font(.caption).foregroundStyle(Theme.C.textTertiary)
                    }
                }
            }
        }
        .listRowBackground(Theme.C.card)
    }
}

/// Lege staat met uitleg — elk scherm heeft er een.
struct EmptyStateView: View {
    let symbol: String
    let title: String
    let message: String
    var actionTitle: String?
    var action: (() -> Void)?

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: symbol)
                .font(.system(size: 48, weight: .light))
                .foregroundStyle(Theme.C.textTertiary)
            Text(title)
                .font(.title3.bold())
                .foregroundStyle(Theme.C.text)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(Theme.C.textSecondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 32)
            if let actionTitle, let action {
                Button(actionTitle, action: action)
                    .buttonStyle(.borderedProminent)
                    .padding(.top, 4)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.vertical, 60)
    }
}

/// Achtergrond + standaardmarges voor elk scherm.
struct ScreenBackground: ViewModifier {
    func body(content: Content) -> some View {
        content
            .background(Theme.C.base.ignoresSafeArea())
            .scrollContentBackground(.hidden)
    }
}

extension View {
    func screenBackground() -> some View { modifier(ScreenBackground()) }

    /// Maakt onderaan ruimte vrij voor de zwevende accessoire-strook boven de
    /// tabbar. Zonder dit valt de laatste rij van een lijst eronder en is hij
    /// niet aan te tikken. safeAreaInset werkt hier niet: een List binnen een
    /// Tab rekent die niet mee. contentMargins wél.
    func accessoryInset(_ height: CGFloat = 80) -> some View {
        contentMargins(.bottom, height, for: .scrollContent)
    }
}

/// A copyable shell command in a card, used everywhere the user needs to run
/// something on the server: pairing, updating, uninstalling.
struct CommandBox: View {
    let command: String
    @State private var copied = false

    var body: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                Text(command)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Theme.C.text)
                    .textSelection(.enabled)
                Button {
                    UIPasteboard.general.string = command
                    UINotificationFeedbackGenerator().notificationOccurred(.success)
                    copied = true
                    Task {
                        try? await Task.sleep(for: .seconds(1.6))
                        copied = false
                    }
                } label: {
                    Label(copied ? T("Copied", "Gekopieerd") : T("Copy command", "Kopieer commando"),
                          systemImage: copied ? "checkmark" : "doc.on.doc")
                        .font(.footnote)
                        .foregroundStyle(copied ? Theme.C.ok : Theme.C.accent)
                        .contentTransition(.symbolEffect)
                }
                .animation(.easeOut(duration: 0.2), value: copied)
            }
        }
    }
}

/// Small (i) button that reveals an explanation in a popover — used where a
/// setting's effect isn't obvious from its label alone.
struct InfoButton: View {
    let text: String
    @State private var showing = false

    var body: some View {
        Button {
            showing = true
        } label: {
            Image(systemName: "info.circle")
                .foregroundStyle(Theme.C.textTertiary)
                .font(.footnote)
        }
        .buttonStyle(.plain)
        .popover(isPresented: $showing) {
            Text(text)
                .font(.footnote)
                .foregroundStyle(Theme.C.text)
                .padding()
                .frame(idealWidth: 280)
                .presentationCompactAdaptation(.popover)
        }
    }
}
