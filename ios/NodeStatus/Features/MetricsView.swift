import SwiftUI

/// Het hoofdscherm: Device Status, exact de opbouw uit de referentiescreenshots.
struct MetricsView: View {
    @Environment(AppState.self) private var app
    @State private var tick = Date()

    private let clock = Timer.publish(every: 1, on: .main, in: .common).autoconnect()

    var body: some View {
        NavigationStack {
            Group {
                if app.selected == nil {
                    EmptyStateView(
                        symbol: "server.rack",
                        title: T("No server selected", "Geen server geselecteerd"),
                        message: T("Pair a server first, in the Server tab.", "Koppel eerst een server in het tabblad Server.")
                    )
                } else {
                    content
                }
            }
            .screenBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) { EmptyView() }
            }
        }
        .onReceive(clock) { tick = $0 }
    }

    /// Data is verouderd zodra de stream weg is. Die dan onaangeroerd tonen
    /// suggereert dat hij live is; dimmen plus een banner met de leeftijd is
    /// eerlijker en scheelt verkeerde conclusies.
    private var isStale: Bool {
        if case .live = app.connection { return false }
        return app.latest != nil
    }

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.M.sectionGap) {
                header
                if isStale, let last = app.latest {
                    staleBanner(last)
                }
                if let sys = app.system {
                    identityCard(sys)
                }
                if let s = app.latest {
                    Group {
                        tiles(s)
                        networkSection(s)
                        if let t = s.primaryTemp { temperatureSection(t) }
                        quickLinks(s)
                        sensorsSection(s)
                    }
                    .opacity(isStale ? 0.45 : 1)
                    .animation(.easeInOut(duration: 0.3), value: isStale)
                } else {
                    loadingCard
                }
            }
            .padding(.horizontal, Theme.M.screenMargin)
            .padding(.bottom, 90)
        }
    }

    private func staleBanner(_ last: Sample) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "clock.badge.exclamationmark")
                .foregroundStyle(Theme.C.warn)
            VStack(alignment: .leading, spacing: 1) {
                Text(T("Showing the last known values", "Laatst bekende waarden"))
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Theme.C.text)
                Text(T("Last update \(Fmt.ago(last.t))", "Laatste update \(Fmt.ago(last.t))"))
                    .font(.caption)
                    .foregroundStyle(Theme.C.textSecondary)
            }
            Spacer()
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous)
            .fill(Theme.C.warn.opacity(0.12)))
    }

    // MARK: - Kop

    private var header: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(alignment: .firstTextBaseline) {
                Text("Device Status")
                    .font(.largeTitle.bold())
                    .foregroundStyle(Theme.C.text)
                Spacer()
                liveBadge
            }
            Text("Real-time monitoring · \(app.selected?.name ?? "")")
                .font(.subheadline)
                .foregroundStyle(Theme.C.textSecondary)
        }
        .padding(.top, 8)
    }

    private var liveBadge: some View {
        HStack(spacing: 5) {
            StatusDot(status: app.connection == .live ? .ok : .warn,
                      pulsing: app.connection == .live)
            Text(app.connection.label.uppercased())
                .font(.caption2.weight(.bold))
                .foregroundStyle(app.connection.color)
                .lineLimit(1)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Capsule().fill(app.connection.color.opacity(0.15)))
    }

    // MARK: - Identiteitskaart

    private func identityCard(_ sys: SystemInfo) -> some View {
        NavigationLink {
            HardwareView()
        } label: {
            Card {
                VStack(alignment: .leading, spacing: 18) {
                    HStack(spacing: 12) {
                        IconTile(symbol: "server.rack",
                                 color: app.selected?.accent ?? Theme.C.indigo, size: 46)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(sys.displayName)
                                .font(.title.bold())
                                .foregroundStyle(Theme.C.text)
                                .lineLimit(1)
                                .minimumScaleFactor(0.6)
                            Text("\(sys.osLine)  (\(sys.os.kernel))")
                                .font(.subheadline)
                                .foregroundStyle(Theme.C.textSecondary)
                                .lineLimit(1)
                                .minimumScaleFactor(0.7)
                        }
                        Spacer(minLength: 0)
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(Theme.C.textTertiary)
                    }

                    Grid(horizontalSpacing: 16, verticalSpacing: 14) {
                        GridRow {
                            KeyValue(key: T("Model", "Model"), value: sys.modelLine)
                            KeyValue(key: "Storage", value: Fmt.bytes(storageTotal(sys), binary: app.prefs.binaryUnits))
                        }
                        GridRow {
                            KeyValue(key: "Chip", value: sys.cpu.model)
                            KeyValue(key: "Memory", value: Fmt.bytes(sys.memoryTotal, binary: app.prefs.binaryUnits))
                        }
                        GridRow {
                            KeyValue(key: "Uptime", value: Fmt.uptime(liveUptime(sys)))
                            KeyValue(key: T("Reboot Date", "Reboot Date"), value: Fmt.date(sys.bootTime))
                        }
                    }
                }
            }
        }
        .buttonStyle(.plain)
    }

    /// Uptime telt door op de wandklok, dus zonder extra serververkeer. Zodra
    /// de verbinding weg is stopt hij: doortellen zou beweren dat de server
    /// nog draait, en dat weten we juist niet.
    private func liveUptime(_ sys: SystemInfo) -> Double {
        guard sys.bootTime > 0 else { return sys.uptimeS }
        let reference = isStale ? (app.latest?.t ?? tick.timeIntervalSince1970)
                                : tick.timeIntervalSince1970
        return reference - Double(sys.bootTime)
    }

    private func storageTotal(_ sys: SystemInfo) -> UInt64 {
        app.latest.map { $0.storageTotal } ?? sys.storageTotalBytes
    }

    // MARK: - Tegels

    private func tiles(_ s: Sample) -> some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: Theme.M.cardGap),
                            GridItem(.flexible(), spacing: Theme.M.cardGap)],
                  spacing: Theme.M.cardGap) {
            NavigationLink { CPUInfoView() } label: {
                MetricTile(title: "CPU", symbol: "cpu.fill", tint: Theme.C.blue,
                           gradient: Theme.G.cpu, fraction: s.cpu.total / 100,
                           value: Fmt.percent(s.cpu.total), isLink: true)
            }.buttonStyle(.plain)

            NavigationLink { RAMDetailView() } label: {
                MetricTile(title: "RAM", symbol: "memorychip.fill", tint: Theme.C.blue,
                           gradient: Theme.G.ram, fraction: s.memory.percent / 100,
                           value: Fmt.percent(s.memory.percent),
                           caption: "\(Fmt.bytes(s.memory.used, binary: app.prefs.binaryUnits)) / \(Fmt.bytes(s.memory.total, binary: app.prefs.binaryUnits))",
                           isLink: true)
            }.buttonStyle(.plain)

            NavigationLink { StorageDetailView(sample: s) } label: {
                storageTile(s)
            }.buttonStyle(.plain)

            NavigationLink { LoadDetailView() } label: {
                MetricTile(title: "Load", symbol: "gauge.with.dots.needle.50percent",
                           tint: Theme.C.green, gradient: Theme.G.load,
                           fraction: loadFraction(s), value: String(format: "%.2f", s.cpu.load.first ?? 0),
                           caption: s.cpu.loadShort, isLink: true)
            }.buttonStyle(.plain)
        }
    }

    /// Load wordt gedeeld door het aantal threads: 4.0 op 4 cores is 100%.
    private func loadFraction(_ s: Sample) -> Double {
        let threads = max(s.cpu.cores.count, 1)
        return min((s.cpu.load.first ?? 0) / Double(threads), 1)
    }

    private func storageTile(_ s: Sample) -> some View {
        let vols = s.localStorage
        return Card(padding: 14) {
            // Deze tegel is de langste (hij heeft er soms een regel "N volumes"
            // bij). Niet vastzetten op een hoogte: hij rekt uit tot de hoogte
            // van zijn rij, net als MetricTile, zodat Storage en Load altijd
            // even hoog zijn en de inhoud bovenaan blijft staan.
            VStack(alignment: .leading, spacing: 14) {
                HStack(spacing: 10) {
                    IconTile(symbol: "internaldrive.fill", color: Theme.C.magenta)
                    Text("Storage").font(.headline).foregroundStyle(Theme.C.text)
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(Theme.C.textTertiary)
                }
                VStack(alignment: .leading, spacing: 6) {
                    if vols.count > 1 {
                        SegmentedGaugeBar(segments: segments(vols, total: s.storageTotal))
                    } else {
                        GaugeBar(fraction: s.storagePercent / 100, gradient: Theme.G.storage)
                    }
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text("\(Fmt.bytes(s.storageUsed, binary: app.prefs.binaryUnits)) / \(Fmt.bytes(s.storageTotal, binary: app.prefs.binaryUnits))")
                            .font(.footnote).monospacedDigit()
                            .lineLimit(1).minimumScaleFactor(0.55)
                            .foregroundStyle(Theme.C.textSecondary)
                        Spacer(minLength: 0)
                        Text(Fmt.percent(s.storagePercent))
                            .font(.system(.title3, weight: .bold))
                            .monospacedDigit()
                            .lineLimit(1).fixedSize()
                            .contentTransition(.numericText())
                            .foregroundStyle(Theme.C.text)
                    }
                    if vols.count > 1 {
                        Text(T("\(vols.count) volumes", "\(vols.count) volumes"))
                            .font(.caption2)
                            .foregroundStyle(Theme.C.textTertiary)
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        }
        .frame(minHeight: Theme.M.metricTileHeight)
    }

    private func segments(_ vols: [Sample.Storage], total: UInt64) -> [SegmentedGaugeBar.Segment] {
        guard total > 0 else { return [] }
        let top = vols.sorted { $0.used > $1.used }.prefix(5)
        return top.enumerated().map { i, v in
            SegmentedGaugeBar.Segment(id: v.mount,
                                      fraction: Double(v.used) / Double(total),
                                      tint: top.count > 1 ? Double(i) / Double(top.count - 1) : 0)
        }
    }

    // MARK: - Netwerk

    private func networkSection(_ s: Sample) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink { NetworkDetailView() } label: {
                Card {
                    VStack(alignment: .leading, spacing: 14) {
                        HStack(spacing: 10) {
                            IconTile(symbol: "wifi", color: Theme.C.green)
                            VStack(alignment: .leading, spacing: 1) {
                                Text("Network").font(.headline).foregroundStyle(Theme.C.text)
                                if let i = s.network.primaryInterface {
                                    Text(i.name + (i.speedMbps.map { $0 > 0 ? " · \($0) Mb/s" : "" } ?? ""))
                                        .font(.caption).foregroundStyle(Theme.C.textTertiary)
                                }
                            }
                            Spacer(minLength: 0)
                            Image(systemName: "chevron.right")
                                .font(.caption2.weight(.semibold))
                                .foregroundStyle(Theme.C.textTertiary)
                        }

                        HStack {
                            Text(T("Total Usage", "Totaal verbruik"))
                                .font(.subheadline)
                                .foregroundStyle(Theme.C.textSecondary)
                            Spacer()
                            Label(Fmt.bytes(s.network.rxTotal), systemImage: "arrow.down.circle.fill")
                                .font(.subheadline.monospacedDigit())
                                .foregroundStyle(Theme.C.blue)
                            Label(Fmt.bytes(s.network.txTotal), systemImage: "arrow.up.circle.fill")
                                .font(.subheadline.monospacedDigit())
                                .foregroundStyle(Theme.C.green)
                        }

                        ZStack(alignment: .topTrailing) {
                            NetworkChart(samples: app.history.elements, window: app.prefs.historyWindow)
                            VStack(alignment: .trailing, spacing: 2) {
                                Text("\(Fmt.speed(s.network.txBps)) ↑")
                                    .foregroundStyle(Theme.C.green)
                                Text("\(Fmt.speed(s.network.rxBps)) ↓")
                                    .foregroundStyle(Theme.C.blue)
                            }
                            .font(.caption.monospacedDigit())
                            .contentTransition(.numericText())
                            .padding(.trailing, 2)
                        }
                    }
                }
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: - Temperatuur

    private func temperatureSection(_ t: Sample.Temp) -> some View {
        Card {
            VStack(alignment: .leading, spacing: 12) {
                HStack(spacing: 10) {
                    IconTile(symbol: "thermometer.medium", color: t.status.color)
                    VStack(alignment: .leading, spacing: 1) {
                        Text(T("Temperature", "Temperatuur")).font(.headline).foregroundStyle(Theme.C.text)
                        Text(t.label).font(.caption).foregroundStyle(Theme.C.textTertiary)
                    }
                    Spacer()
                    Text(Fmt.temp(t.celsius, fahrenheit: app.prefs.fahrenheit))
                        .font(.title2.bold())
                        .monospacedDigit()
                        .contentTransition(.numericText())
                        .foregroundStyle(t.status.color)
                }
                Sparkline(values: tempHistory(t.key), color: t.status.color, height: 44)
            }
        }
    }

    private func tempHistory(_ key: String) -> [Double] {
        let vals = app.history.elements.compactMap { s in
            s.temps.first { $0.key == key }?.celsius
        }
        return vals.isEmpty ? [0] : vals
    }

    // MARK: - Snelkoppelingen

    private func quickLinks(_ s: Sample) -> some View {
        Card(padding: 0) {
            HStack(spacing: 0) {
                if !s.gpu.isEmpty {
                    NavigationLink { GPUDetailView() } label: {
                        quickLabel("GPU", "cpu.fill", Theme.C.purple)
                    }.buttonStyle(.plain)
                    Divider().frame(height: 26).overlay(Theme.C.hairline)
                }
                NavigationLink { SensorsDetailView() } label: {
                    quickLabel("Sensors", "sensor.fill", Theme.C.teal)
                }.buttonStyle(.plain)
            }
            .frame(height: 54)
        }
    }

    private func quickLabel(_ title: String, _ symbol: String, _ tint: Color) -> some View {
        HStack(spacing: 7) {
            Image(systemName: symbol)
            Text(title)
        }
        .font(.body.weight(.medium))
        .foregroundStyle(tint)
        .frame(maxWidth: .infinity)
    }

    // MARK: - Sensoren

    @State private var sensorsExpanded = false

    private func sensorsSection(_ s: Sample) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Button {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                        sensorsExpanded.toggle()
                    }
                } label: {
                    HStack(spacing: 6) {
                        Text("Sensors").font(.title3.bold()).foregroundStyle(Theme.C.text)
                        Image(systemName: "chevron.down")
                            .font(.caption.weight(.bold))
                            .foregroundStyle(Theme.C.textSecondary)
                            .rotationEffect(.degrees(sensorsExpanded ? 0 : -90))
                    }
                }
                .buttonStyle(.plain)
                Spacer()
                badge(count: s.temps.filter { $0.status == .ok }.count,
                      symbol: "checkmark.circle.fill", color: Theme.C.ok, label: "Available")
                badge(count: s.temps.filter { $0.status != .ok }.count,
                      symbol: "xmark.circle.fill", color: Theme.C.crit, label: "Alert")
            }

            if sensorsExpanded {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 100), spacing: 12)], spacing: 16) {
                    ForEach(s.temps) { t in
                        sensorTile(t)
                    }
                }
            }
        }
    }

    private func badge(count: Int, symbol: String, color: Color, label: String) -> some View {
        HStack(spacing: 4) {
            Image(systemName: symbol).foregroundStyle(color)
            Text(T("\(label) \(count)", "\(label) \(count)")).foregroundStyle(Theme.C.textSecondary)
        }
        .font(.caption)
    }

    private func sensorTile(_ t: Sample.Temp) -> some View {
        VStack(spacing: 8) {
            ZStack(alignment: .bottomTrailing) {
                Circle()
                    .fill(t.status.color.opacity(0.15))
                    .frame(width: 62, height: 62)
                    .overlay(
                        Image(systemName: "thermometer.medium")
                            .font(.system(size: 24, weight: .medium))
                            .foregroundStyle(t.status.color)
                    )
                Image(systemName: t.status.symbol)
                    .font(.system(size: 17))
                    .foregroundStyle(t.status.color, Theme.C.base)
                    .background(Circle().fill(Theme.C.base).frame(width: 15, height: 15))
            }
            Text(t.label)
                .font(.caption)
                .foregroundStyle(Theme.C.text)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
            Text(Fmt.temp(t.celsius, fahrenheit: app.prefs.fahrenheit))
                .font(.caption2.monospacedDigit())
                .foregroundStyle(Theme.C.textSecondary)
        }
        .frame(maxWidth: .infinity)
    }

    private var loadingCard: some View {
        Card {
            VStack(spacing: 10) {
                if case .failed = app.connection {
                    Image(systemName: "wifi.exclamationmark")
                        .font(.system(size: 30))
                        .foregroundStyle(Theme.C.warn)
                } else {
                    ProgressView()
                }
                Text(app.connection.detail ?? app.connection.label)
                    .font(.footnote)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(Theme.C.textSecondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 26)
        }
    }
}
