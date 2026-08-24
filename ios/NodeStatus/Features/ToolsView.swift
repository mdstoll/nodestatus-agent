import SwiftUI

/// Tabblad Tools: gegroepeerde lijst, hetzelfde patroon als de referentie-app.
struct ToolsView: View {
    @Environment(AppState.self) private var app

    var body: some View {
        NavigationStack {
            Group {
                if app.selected == nil {
                    EmptyStateView(symbol: "wrench.and.screwdriver",
                                   title: T("No server selected", "Geen server geselecteerd"),
                                   message: T("Pick a server first, in the Server tab.", "Kies eerst een server in het tabblad Server."))
                } else {
                    list
                }
            }
            .screenBackground()
            .navigationTitle("Tools")
        }
    }

    private var list: some View {
        List {
            Section {
                HStack(spacing: 10) {
                    StatusDot(status: app.connection == .live ? .ok : .warn,
                              pulsing: app.connection == .live)
                    Text(app.selected?.name ?? "")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Theme.C.text)
                    Spacer()
                    Text(app.selected?.displayHost ?? "")
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.C.textTertiary)
                }
                .listRowBackground(Theme.C.card)
            } footer: {
                Text(T("Every tool runs on the selected server.", "Alle tools draaien op de geselecteerde server."))
            }

            Section(T("System", "Systeem")) {
                if has("journal") {
                    ToolRow(title: "Log Analyzer", symbol: "doc.text.magnifyingglass",
                            tint: Theme.C.blue) { LogAnalyzerView() }
                }
                if has("apt") {
                    ToolRow(title: "System & Updates", symbol: "arrow.down.circle.fill",
                            tint: Theme.C.orange) { UpdatesView() }
                }
                ToolRow(title: T("Processes", "Processen"), symbol: "list.bullet.rectangle",
                        tint: Theme.C.purple) { ProcessesView() }
                ToolRow(title: "Locale & Region", symbol: "globe.europe.africa.fill",
                        tint: Theme.C.blue) { LocaleView() }
                ToolRow(title: "Device Uptime", symbol: "clock.fill", tint: Theme.C.gray) { UptimeView() }
            }

            Section(T("Hardware", "Hardware")) {
                ToolRow(title: T("Hardware overview", "Hardware-overzicht"), symbol: "server.rack",
                        tint: Theme.C.indigo) { HardwareView() }
                ToolRow(title: "CPU Information", symbol: "cpu.fill", tint: Theme.C.magenta) { CPUInfoView() }
                ToolRow(title: T("Network interfaces", "Netwerkinterfaces"), symbol: "cable.connector",
                        tint: Theme.C.green) { NetworkDetailView() }
                if has("disks") || has("smart") {
                    ToolRow(title: T("Storage & SMART", "Opslag & SMART"), symbol: "internaldrive.fill",
                            tint: Theme.C.magenta) { SmartDetailView() }
                }
                if has("sensors") {
                    ToolRow(title: T("Sensors", "Sensoren"), symbol: "sensor.fill", tint: Theme.C.teal) { SensorsDetailView() }
                }
                if has("gpu") {
                    ToolRow(title: "GPU", symbol: "cpu.fill", tint: Theme.C.purple) { GPUDetailView() }
                }
            }

            Section(T("Network", "Netwerk")) {
                if has("speedtest") {
                    ToolRow(title: "Network Speed", symbol: "speedometer", tint: Theme.C.cyan,
                            subtitle: speedtestSubtitle) { SpeedtestView() }
                }
                if has("ping") {
                    ToolRow(title: "Ping", symbol: "dot.radiowaves.left.and.right",
                            tint: Theme.C.blue) { PingView() }
                }
                if has("dns") {
                    ToolRow(title: "DNS Query", symbol: "globe", tint: Theme.C.blue) { DNSView() }
                }
                if has("traceroute") {
                    ToolRow(title: "Traceroute", symbol: "arrow.triangle.turn.up.right.diamond.fill",
                            tint: Theme.C.teal) { TracerouteView() }
                }
                if has("whois") {
                    ToolRow(title: "WHOIS", symbol: "magnifyingglass.circle.fill",
                            tint: Theme.C.orange) { WhoisView() }
                }
            }
        }
        .listStyle(.insetGrouped)
        .screenBackground()
        .accessoryInset()
    }

    /// De agent meldt alleen capabilities die op díe machine ook echt werken —
    /// hij test ze bij het starten in plaats van alleen te kijken of het binary
    /// bestaat. Een tool tonen die gegarandeerd "niet geïnstalleerd" of "geen
    /// rechten" teruggeeft is erger dan hem weglaten, dus verbergen we hem.
    ///
    /// Zolang /v1/system nog niet binnen is tonen we alles: dat voorkomt dat de
    /// lijst zichtbaar staat te verspringen bij het openen van het tabblad.
    private func has(_ capability: String) -> Bool {
        guard let sys = app.system else { return true }
        return sys.has(capability)
    }

    private var speedtestSubtitle: String? {
        guard let sys = app.system else { return nil }
        return sys.hasSpeedtest ? nil : T("requires speedtest on the server", "vereist speedtest op de server")
    }
}

// MARK: - CPU

struct CPUInfoView: View {
    @Environment(AppState.self) private var app
    @State private var showHistory = true

    var body: some View {
        DetailScroll {
            if let s = app.latest {
                Card {
                    VStack(alignment: .leading, spacing: 12) {
                        Text(T("CPU Usage", "CPU-gebruik")).font(.headline).foregroundStyle(Theme.C.text)
                        CPUHistoryChart(samples: app.history.elements)
                        HStack(spacing: 16) {
                            legend(T("Total", "Totaal"), Theme.C.blue, s.cpu.total)
                            legend("User", Theme.C.green, s.cpu.user)
                            legend("System", Theme.C.orange, s.cpu.system)
                        }
                    }
                }

                Card {
                    VStack(alignment: .leading, spacing: 10) {
                        HStack {
                            Text(T("Per-Core Usage", "Gebruik per core")).font(.headline).foregroundStyle(Theme.C.text)
                            Spacer()
                            Toggle(T("History", "Historie"), isOn: $showHistory)
                                .labelsHidden()
                        }
                        ForEach(Array(s.cpu.cores.enumerated()), id: \.offset) { i, v in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text("Core \(i)")
                                        .font(.caption.monospaced())
                                        .foregroundStyle(Theme.C.textSecondary)
                                    if let f = s.cpu.freqMhz, i < f.count, f[i] > 0 {
                                        Text("\(f[i]) MHz")
                                            .font(.caption2.monospacedDigit())
                                            .foregroundStyle(Theme.C.textTertiary)
                                    }
                                    Spacer()
                                    Text(Fmt.percent(v))
                                        .font(.caption.monospacedDigit().weight(.medium))
                                        .foregroundStyle(Theme.C.text)
                                }
                                GaugeBar(fraction: v / 100, gradient: Theme.G.cpu, height: 6)
                                if showHistory {
                                    Sparkline(values: coreHistory(i), color: Theme.C.blue,
                                              filled: false, height: 22)
                                }
                            }
                            .padding(.vertical, 3)
                        }
                    }
                }

                InfoCard(title: T("Load", "Belasting"), symbol: "gauge.with.dots.needle.50percent", tint: Theme.C.green) {
                    InfoRow(label: T("Load average", "Load average"), value: s.cpu.loadLine)
                    InfoRow(label: T("Processes", "Processen"), value: T("\(s.cpu.procsRunning) running / \(s.cpu.procsTotal) total", "\(s.cpu.procsRunning) actief / \(s.cpu.procsTotal) totaal"))
                    InfoRow(label: "I/O wait", value: Fmt.percent(s.cpu.iowait))
                    InfoRow(label: "Steal", value: Fmt.percent(s.cpu.steal))
                }
            }

            if let sys = app.system {
                InfoCard(title: T("Specifications", "Specificaties"), symbol: "cpu.fill", tint: Theme.C.magenta) {
                    InfoRow(label: T("Model", "Model"), value: sys.cpu.model)
                    InfoRow(label: T("Cores / threads", "Cores / threads"), value: "\(sys.cpu.coresPhysical) / \(sys.cpu.threads)")
                    if let m = sys.cpu.maxMhz, m > 0 { InfoRow(label: T("Max frequency", "Max frequentie"), value: "\(m) MHz") }
                    if let g = sys.cpu.governor { InfoRow(label: T("Governor", "Governor"), value: g) }
                }
            }
        }
        .navigationTitle("CPU")
    }

    private func legend(_ label: String, _ color: Color, _ value: Double) -> some View {
        HStack(spacing: 5) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(label).font(.caption).foregroundStyle(Theme.C.textSecondary)
            Text(Fmt.percent(value)).font(.caption.monospacedDigit().weight(.semibold))
                .foregroundStyle(Theme.C.text)
        }
    }

    private func coreHistory(_ i: Int) -> [Double] {
        let v = app.history.elements.compactMap { $0.cpu.cores.indices.contains(i) ? $0.cpu.cores[i] : nil }
        return v.isEmpty ? [0] : v
    }
}

// MARK: - Processen

struct ProcessesView: View {
    @State private var sort = "cpu"
    @State private var query = ""

    var body: some View {
        AsyncLoad(path: "v1/tools/processes", refreshInterval: 5) { (r: ProcessesResult) in
            let list = filtered(r.processes)
            DetailScroll {
                summaryCard(r)
                Picker(T("Sort", "Sorteer"), selection: $sort) {
                    Text("CPU").tag("cpu")
                    Text(T("Memory", "Geheugen")).tag("mem")
                    Text(T("Name", "Naam")).tag("name")
                }
                .pickerStyle(.segmented)

                ForEach(list.prefix(60)) { p in
                    Card(padding: 12) {
                        VStack(alignment: .leading, spacing: 7) {
                            HStack {
                                Text(p.name)
                                    .font(.subheadline.weight(.medium))
                                    .foregroundStyle(Theme.C.text)
                                    .lineLimit(1)
                                Spacer()
                                Text("\(p.pid)")
                                    .font(.caption2.monospacedDigit())
                                    .foregroundStyle(Theme.C.textTertiary)
                            }
                            HStack(spacing: 12) {
                                Text(p.user).font(.caption).foregroundStyle(Theme.C.textTertiary)
                                Spacer()
                                Text("CPU \(Fmt.percent(p.cpuPercent))")
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(Theme.C.blue)
                                Text("RAM \(Fmt.bytes(p.rss))")
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(Theme.C.green)
                            }
                            GaugeBar(fraction: (sort == "mem" ? p.memPercent : p.cpuPercent) / 100,
                                     gradient: sort == "mem" ? Theme.G.load : Theme.G.cpu, height: 4)
                        }
                    }
                }
            }
            .searchable(text: $query, prompt: T("Search processes", "Zoek proces"))
        }
        .navigationTitle(T("Processes", "Processen"))
    }

    /// Een proceslijst zonder samenvatting verbergt precies het soort probleem
    /// dat je wilt zien: op de testmachine stonden 8194 zombies van één
    /// programma dat zijn children niet opruimt.
    @ViewBuilder
    private func summaryCard(_ r: ProcessesResult) -> some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 16) {
                    stat(T("Total", "Totaal"), r.summary.total, Theme.C.text)
                    stat(T("Running", "Actief"), r.summary.running, Theme.C.ok)
                    stat(T("Sleeping", "Slapend"), r.summary.sleeping, Theme.C.textSecondary)
                    stat(T("Zombie", "Zombie"), r.summary.zombie,
                         r.summary.zombie > 50 ? Theme.C.crit : Theme.C.textSecondary)
                    Spacer(minLength: 0)
                }
                if r.summary.zombie > 50 {
                    Divider().overlay(Theme.C.hairline)
                    Label(T("\(r.summary.zombie) zombie processes — a program is not reaping its children",
                            "\(r.summary.zombie) zombie-processen — een programma ruimt zijn children niet op"),
                          systemImage: "exclamationmark.triangle.fill")
                        .font(.caption)
                        .foregroundStyle(Theme.C.warn)
                    ForEach(r.zombieParents ?? []) { p in
                        HStack {
                            Text(p.name.isEmpty ? "pid \(p.pid)" : p.name)
                                .font(.caption.monospaced())
                                .foregroundStyle(Theme.C.text)
                            Text("pid \(p.pid)")
                                .font(.caption2)
                                .foregroundStyle(Theme.C.textTertiary)
                            Spacer()
                            Text("\(p.count)")
                                .font(.caption.monospacedDigit().weight(.semibold))
                                .foregroundStyle(Theme.C.crit)
                        }
                    }
                }
            }
        }
    }

    private func stat(_ label: String, _ value: Int, _ color: Color) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text("\(value)")
                .font(.headline.monospacedDigit())
                .foregroundStyle(color)
            Text(label).font(.caption2).foregroundStyle(Theme.C.textTertiary)
        }
    }

    private func filtered(_ list: [ProcessesResult.Proc]) -> [ProcessesResult.Proc] {
        var out = list
        if !query.isEmpty {
            out = out.filter { $0.name.localizedCaseInsensitiveContains(query) }
        }
        switch sort {
        case "mem":  out.sort { $0.rss > $1.rss }
        case "name": out.sort { $0.name.lowercased() < $1.name.lowercased() }
        default:     out.sort { $0.cpuPercent > $1.cpuPercent }
        }
        return out
    }
}

// MARK: - Updates

struct UpdatesView: View {
    var body: some View {
        AsyncLoad(path: "v1/tools/updates", refreshInterval: 120) { (r: UpdatesInfo) in
            DetailScroll {
                if r.rebootRequired {
                    Card {
                        HStack(spacing: 10) {
                            Image(systemName: "arrow.clockwise.circle.fill")
                                .font(.title2).foregroundStyle(Theme.C.warn)
                            VStack(alignment: .leading, spacing: 2) {
                                Text(T("Restart required", "Herstart vereist")).font(.headline).foregroundStyle(Theme.C.text)
                                if !r.rebootRequiredPkgs.isEmpty {
                                    Text(r.rebootRequiredPkgs.joined(separator: ", "))
                                        .font(.caption).foregroundStyle(Theme.C.textSecondary)
                                        .lineLimit(2)
                                }
                            }
                        }
                    }
                }

                HStack(spacing: Theme.M.cardGap) {
                    statCard("\(r.upgradable)", T("Available", "Beschikbaar"), Theme.C.blue)
                    statCard("\(r.security)", T("Security", "Security"), r.security > 0 ? Theme.C.crit : Theme.C.ok)
                }

                InfoCard(title: T("Status", "Status"), symbol: "gear.badge", tint: Theme.C.orange) {
                    InfoRow(label: T("Unattended upgrades", "Unattended upgrades"),
                            value: r.unattendedUpgrades ? T("on", "aan") : T("off", "uit"),
                            tint: r.unattendedUpgrades ? Theme.C.ok : Theme.C.warn)
                    if let t = r.lastAptUpdate, t > 0 {
                        InfoRow(label: T("Cache updated", "Cache bijgewerkt"), value: Fmt.ago(Double(t)))
                    }
                }

                if !r.packages.isEmpty {
                    Text(T("Packages", "Pakketten")).font(.headline).foregroundStyle(Theme.C.text).padding(.top, 6)
                    ForEach(r.packages) { p in
                        Card(padding: 12) {
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(p.name).font(.subheadline.weight(.medium))
                                        .foregroundStyle(Theme.C.text)
                                    if p.security {
                                        Text("security")
                                            .font(.caption2.weight(.bold))
                                            .padding(.horizontal, 6).padding(.vertical, 2)
                                            .background(Capsule().fill(Theme.C.crit.opacity(0.2)))
                                            .foregroundStyle(Theme.C.crit)
                                    }
                                    Spacer()
                                }
                                if !p.current.isEmpty {
                                    HStack(spacing: 6) {
                                        Text(p.current).foregroundStyle(Theme.C.textTertiary)
                                        Image(systemName: "arrow.right").font(.caption2)
                                            .foregroundStyle(Theme.C.textTertiary)
                                        Text(p.candidate).foregroundStyle(Theme.C.ok)
                                    }
                                    .font(.caption.monospaced())
                                }
                            }
                        }
                    }
                }

                Text(T("Read-only: the app installs nothing. See the analysis, chapter 7.", "Alleen-lezen: de app installeert niets. Zie de analyse, hoofdstuk 7."))
                    .font(.caption2)
                    .foregroundStyle(Theme.C.textTertiary)
                    .padding(.top, 8)
            }
        }
        .navigationTitle("System & Updates")
    }

    private func statCard(_ value: String, _ label: String, _ color: Color) -> some View {
        Card {
            VStack(alignment: .leading, spacing: 4) {
                Text(value).font(.system(size: 34, weight: .bold, design: .rounded))
                    .foregroundStyle(color)
                Text(label).font(.caption).foregroundStyle(Theme.C.textSecondary)
            }
        }
    }
}

// MARK: - Locale

struct LocaleView: View {
    var body: some View {
        AsyncLoad(path: "v1/tools/locale") { (r: LocaleInfo) in
            DetailScroll {
                InfoCard(title: "Locale & Region Info", symbol: "globe.europe.africa.fill", tint: Theme.C.blue) {
                    InfoRow(label: "Locale Identifier", value: r.localeIdentifier, symbol: "globe")
                    InfoRow(label: "Region", value: r.region.isEmpty ? "—" : r.region, symbol: "map")
                    InfoRow(label: "Language", value: r.language, symbol: "character.bubble")
                    InfoRow(label: "Preferred Languages",
                            value: r.preferredLanguages.joined(separator: " → "), symbol: "text.bubble")
                    if let k = r.keyboardLayout, !k.isEmpty {
                        InfoRow(label: "Keyboard", value: k, symbol: "keyboard")
                    }
                    InfoRow(label: "Time Zone", value: "\(r.timeZone) (\(r.utcOffset))", symbol: "clock")
                    InfoRow(label: "Local Time", value: r.localTime, symbol: "calendar.badge.clock")
                    InfoRow(label: "Calendar", value: r.calendar, symbol: "calendar")
                    InfoRow(label: "First Day of Week", value: r.firstDayOfWeek, symbol: "arrow.left.arrow.right")
                    InfoRow(label: "Hour Cycle", value: r.hourCycle, symbol: "clock.badge")
                    InfoRow(label: "NTP Synchronized", value: r.ntpSynchronized ? "ja" : "nee",
                            symbol: "network", tint: r.ntpSynchronized ? Theme.C.ok : Theme.C.warn)
                    InfoRow(label: "RTC in local TZ", value: r.rtcInLocalTz ? "ja" : "nee", symbol: "gearshape")
                }
            }
        }
        .navigationTitle("Locale & Region")
    }
}

// MARK: - Uptime

struct UptimeView: View {
    @Environment(AppState.self) private var app

    var body: some View {
        AsyncLoad(path: "v1/tools/uptime", refreshInterval: 30) { (r: UptimeInfo) in
            DetailScroll {
                Card {
                    VStack(spacing: 14) {
                        // Op een server is een wake/sleep-donut zinloos; de
                        // verdeling van CPU-tijd sinds boot is dat wel.
                        DonutChart(slices: [
                            .init(label: "User", value: r.cpuTime.user, color: Theme.C.blue),
                            .init(label: "System", value: r.cpuTime.system, color: Theme.C.orange),
                            .init(label: "I/O wait", value: max(r.cpuTime.iowait, 0.1), color: Theme.C.magenta),
                            .init(label: "Idle", value: r.cpuTime.idle, color: Theme.C.cardElevated),
                        ])
                        .frame(maxWidth: .infinity)

                        VStack(spacing: 8) {
                            legendRow("User", Theme.C.blue, r.cpuTime.user)
                            legendRow("System", Theme.C.orange, r.cpuTime.system)
                            legendRow("I/O wait", Theme.C.magenta, r.cpuTime.iowait)
                            legendRow("Idle", Theme.C.cardElevated, r.cpuTime.idle)
                        }
                    }
                }

                InfoCard(title: "Uptime", symbol: "clock.fill", tint: Theme.C.gray) {
                    InfoRow(label: "Device Uptime", value: Fmt.uptime(r.uptimeS))
                    InfoRow(label: "Boot Date", value: Fmt.date(r.bootTime))
                    InfoRow(label: T("Busy since boot", "Bezetting sinds boot"), value: Fmt.percent(r.busyRatio * 100))
                }

                if !r.boots.isEmpty {
                    Card {
                        VStack(alignment: .leading, spacing: 8) {
                            Text(T("Recent boots", "Laatste boots")).font(.headline).foregroundStyle(Theme.C.text)
                            ForEach(r.boots) { b in
                                HStack {
                                    Text("#\(b.index)")
                                        .font(.caption.monospacedDigit())
                                        .foregroundStyle(Theme.C.textTertiary)
                                        .frame(width: 34, alignment: .leading)
                                    Text(b.start).font(.caption.monospaced())
                                        .foregroundStyle(Theme.C.textSecondary)
                                    Spacer()
                                    if b.index == 0 {
                                        Text(T("current", "huidig")).font(.caption2)
                                            .foregroundStyle(Theme.C.ok)
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle("Device Uptime")
    }

    private func legendRow(_ label: String, _ color: Color, _ value: Double) -> some View {
        HStack(spacing: 8) {
            RoundedRectangle(cornerRadius: 2).fill(color).frame(width: 10, height: 10)
            Text(label).font(.footnote).foregroundStyle(Theme.C.textSecondary)
            Spacer()
            Text(Fmt.percent(value)).font(.footnote.monospacedDigit())
                .foregroundStyle(Theme.C.text)
        }
    }
}
