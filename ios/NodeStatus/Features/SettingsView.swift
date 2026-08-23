import SwiftUI

/// Settings tab: global defaults you can override per server, plus the
/// server-management actions (pairing devices, updating, uninstalling).
struct SettingsView: View {
    @Environment(AppState.self) private var app
    @State private var devices: [DevicesResult.Device] = []
    @State private var deviceError: String?
    @State private var confirmingRevokeAll = false
    @State private var updateCheck: UpdateCheckResult?

    var body: some View {
        NavigationStack {
            Form {
                appearanceSection
                languageSection
                unitsSection
                realTimeSection
                privacySection
                if app.selected != nil { devicesSection }
                if app.selected != nil { updateSection }
                if app.selected != nil { uninstallSection }
                aboutSection
            }
            .screenBackground()
            .accessoryInset()
            .navigationTitle("Settings")
            .task(id: app.selectedID) {
                await loadDevices()
                await loadUpdateCheck()
            }
        }
    }

    // MARK: - Appearance

    private var appearanceSection: some View {
        Section {
            Picker("Appearance", selection: Binding(
                get: { app.prefs.appearance },
                set: { app.prefs.appearance = $0; app.prefs.save() })) {
                ForEach(AppearanceMode.allCases, id: \.self) { m in
                    Text(m.label).tag(m)
                }
            }
            .pickerStyle(.segmented)
        } header: {
            Text("Appearance")
        }
    }

    // MARK: - Language

    private var languageSection: some View {
        Section {
            Picker(T("Language", "Taal"), selection: Binding(
                get: { Localizer.shared.language },
                set: { Localizer.shared.language = $0 })) {
                ForEach(AppLanguage.offered, id: \.self) { l in
                    Text(l.label).tag(l)
                }
            }
            .pickerStyle(.menu)
        } header: {
            Text(T("Language", "Taal"))
        } footer: {
            Text(T("System follows your device's language setting.",
                   "Systeem volgt de taalinstelling van je toestel."))
        }
    }

    // MARK: - Units

    private var unitsSection: some View {
        Section {
            HStack {
                Picker(T("Temperature", "Temperatuur"), selection: Binding(
                    get: { app.prefs.fahrenheit },
                    set: { app.prefs.fahrenheit = $0; app.prefs.save() })) {
                    Text("Celsius (°C)").tag(false)
                    Text("Fahrenheit (°F)").tag(true)
                }
                .pickerStyle(.menu)
                InfoButton(text: T(
                    "Used for every temperature reading: the Metrics temperature card, the Sensors tool, and CPU Information.",
                    "Geldt voor elke temperatuurwaarde: de temperatuurkaart op Metrics, de Sensors-tool en CPU Information."))
            }
            HStack {
                Picker(T("Data sizes", "Opslag- en geheugeneenheden"), selection: Binding(
                    get: { app.prefs.binaryUnits },
                    set: { app.prefs.binaryUnits = $0; app.prefs.save() })) {
                    Text("Decimal (GB, MB)").tag(false)
                    Text("Binary (GiB, MiB)").tag(true)
                }
                .pickerStyle(.menu)
                InfoButton(text: T(
                    "Decimal (1 GB = 1000 MB) matches what drive and cloud vendors print on the box. Binary (1 GiB = 1024 MiB) matches what the Linux kernel itself counts in. Affects RAM, Storage and total network usage everywhere they're shown.",
                    "Decimaal (1 GB = 1000 MB) is wat fabrikanten op de doos zetten. Binair (1 GiB = 1024 MiB) is wat de Linux-kernel zelf telt. Geldt overal waar RAM, Storage en totaal netwerkverbruik getoond worden."))
            }
            HStack {
                Picker(T("Network speed", "Netwerksnelheid"), selection: Binding(
                    get: { app.prefs.bitsPerSecond },
                    set: { app.prefs.bitsPerSecond = $0; app.prefs.save() })) {
                    Text("Bytes/s (MB/s)").tag(false)
                    Text("Bits/s (Mbps)").tag(true)
                }
                .pickerStyle(.menu)
                InfoButton(text: T(
                    "Bytes/s is how file transfers are usually measured. Bits/s (Mbps) is how internet plans and the Network Speed test are usually advertised. Affects the live network chart on Metrics and the speed test result.",
                    "Bytes/s is hoe bestandsoverdrachten meestal gemeten worden. Bits/s (Mbps) is hoe internetabonnementen en de speedtest meestal geadverteerd worden. Geldt voor de live netwerkgrafiek op Metrics en het speedtest-resultaat."))
            }
        } header: {
            Text(T("Units", "Eenheden"))
        }
    }

    // MARK: - Real-time

    private var realTimeSection: some View {
        Section {
            Picker(T("History window", "Historievenster"), selection: bind(\.historyWindow)) {
                Text("30 s").tag(30)
                Text("60 s").tag(60)
                Text("120 s").tag(120)
                Text("300 s").tag(300)
            }
        } header: {
            Text("Real-time")
        } footer: {
            Text(T("How much history the charts show. The agent keeps at most 5 minutes in memory and writes nothing to disk.", "Bepaalt hoeveel geschiedenis de grafieken tonen. De agent bewaart maximaal 5 minuten in geheugen en schrijft niets naar schijf."))
        }
    }

    // MARK: - Privacy

    private var privacySection: some View {
        Section {
            Toggle(T("Mask sensitive data", "Gevoelige gegevens maskeren"), isOn: bind(\.maskSensitive))
            Toggle(T("Warn before speedtest", "Waarschuwen vóór speedtest"), isOn: bind(\.warnBeforeSpeedtest))
        } header: {
            Text(T("Privacy & data", "Privacy & data"))
        } footer: {
            Text(T("A speedtest uses 1–3 GB on the server. On a VPS with a data cap that matters.", "Een speedtest verbruikt 1–3 GB op de server. Op een VPS met datalimiet is dat relevant."))
        }
    }

    // MARK: - Paired devices

    private var devicesSection: some View {
        Section {
            if let e = deviceError {
                Text(e).font(.caption).foregroundStyle(Theme.C.warn)
            }
            ForEach(devices) { d in
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Text(d.name).foregroundStyle(Theme.C.text)
                            if d.isCurrent {
                                Text(T("this device", "dit toestel"))
                                    .font(.caption2)
                                    .padding(.horizontal, 6).padding(.vertical, 2)
                                    .background(Capsule().fill(Theme.C.accent.opacity(0.2)))
                                    .foregroundStyle(Theme.C.accent)
                            }
                        }
                        Text(T("paired \(Fmt.shortDate(d.enrolledAt)) · expires \(Fmt.shortDate(d.expiresAt))", "gekoppeld \(Fmt.shortDate(d.enrolledAt)) · verloopt \(Fmt.shortDate(d.expiresAt))"))
                            .font(.caption2).foregroundStyle(Theme.C.textTertiary)
                    }
                    Spacer()
                }
                .listRowBackground(Theme.C.card)
                .swipeActions {
                    if !d.isCurrent {
                        Button(T("Revoke", "Intrekken"), role: .destructive) {
                            Task { await revoke(d) }
                        }
                    }
                }
            }
            if devices.contains(where: { !$0.isCurrent }) {
                Button(T("Revoke all other devices", "Trek alle andere apparaten in"), role: .destructive) {
                    confirmingRevokeAll = true
                }
                .listRowBackground(Theme.C.card)
                .confirmationDialog(
                    T("Revoke every other paired device?", "Alle andere gekoppelde apparaten intrekken?"),
                    isPresented: $confirmingRevokeAll, titleVisibility: .visible) {
                    Button(T("Revoke all", "Alles intrekken"), role: .destructive) {
                        Task { await revokeAll() }
                    }
                    Button(T("Cancel", "Annuleren"), role: .cancel) {}
                } message: {
                    Text(T("This device stays paired. Every other device will need to be paired again.",
                           "Dit toestel blijft gekoppeld. Elk ander apparaat moet opnieuw gekoppeld worden."))
                }
            }
        } header: {
            Text(T("Paired devices · \(app.selected?.name ?? "")", "Gekoppelde apparaten · \(app.selected?.name ?? "")"))
        } footer: {
            Text(T("Only these devices get through the TLS handshake. Revoking takes effect immediately, even on open connections.", "Alleen deze apparaten komen door de TLS-handshake. Intrekken werkt direct, ook op openstaande verbindingen."))
        }
    }

    // MARK: - Update agent

    private var updateSection: some View {
        Section {
            if let u = updateCheck {
                LabeledContent(T("Installed", "Geïnstalleerd"), value: u.current)
                if u.available, let latest = u.latest {
                    LabeledContent(T("Available", "Beschikbaar")) {
                        Text(latest).foregroundStyle(Theme.C.ok)
                    }
                    CommandBox(command: "sudo nodestatus-agent update")
                    if let url = u.releaseUrl, let link = URL(string: url) {
                        Link(T("What changed in \(latest)?", "Wat is er nieuw in \(latest)?"), destination: link)
                            .font(.footnote)
                    }
                } else {
                    Label(T("Up to date", "Up-to-date"), systemImage: "checkmark.circle.fill")
                        .foregroundStyle(Theme.C.ok)
                        .font(.footnote)
                }
            } else {
                ProgressView().frame(maxWidth: .infinity, alignment: .center)
            }
        } header: {
            Text(T("Update agent", "Agent bijwerken"))
        } footer: {
            Text(T("Checked from the server itself, at most every few hours. Nothing updates automatically — this only ever tells you a newer version exists.",
                   "Wordt door de server zelf gecontroleerd, hooguit elke paar uur. Er wordt nooit automatisch iets bijgewerkt — dit laat alleen zien dát er een nieuwere versie is."))
        }
    }

    // MARK: - Uninstall

    private var uninstallSection: some View {
        Section {
            CommandBox(command: "sudo nodestatus-uninstall.sh --purge --remove-extras")
        } header: {
            Text(T("Uninstall the agent", "Agent verwijderen"))
        } footer: {
            Text(T("Run on the server. Removes the binary, its configuration, the CA and every paired device. --remove-extras also removes smartmontools, whois, dnsutils and the other optional packages the installer added.",
                   "Draai dit op de server. Verwijdert de binary, de configuratie, de CA en elk gekoppeld apparaat. --remove-extras verwijdert ook smartmontools, whois, dnsutils en de andere optionele pakketten die de installer toevoegde."))
        }
    }

    // MARK: - About

    private var aboutSection: some View {
        Section {
            if let sys = app.system {
                LabeledContent(T("Agent", "Agent"), value: sys.agentVersion)
                LabeledContent(T("Server", "Server"), value: sys.hostname)
                LabeledContent("Capabilities", value: "\(sys.capabilities.count)")
            }
            LabeledContent("App", value: appVersion)
            LabeledContent(T("Creator", "Maker"), value: "Merlin Stoll")
        } header: {
            Text(T("About", "Over"))
        } footer: {
            VStack(alignment: .leading, spacing: 10) {
                Text(T("No analytics, no third-party crash reporting. The app talks only to your own servers.",
                       "Geen analytics, geen crash-reporting naar derden. De app praat uitsluitend met je eigen servers."))
                Text("Built with ❤️ and with the help of AI in the Netherlands")
                    .frame(maxWidth: .infinity, alignment: .center)
                    .padding(.top, 4)
            }
        }
    }

    private var appVersion: String {
        let v = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?"
        let b = Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "?"
        return "\(v) (\(b))"
    }

    private func bind<T>(_ key: ReferenceWritableKeyPath<Preferences, T>) -> Binding<T> {
        Binding(get: { app.prefs[keyPath: key] },
                set: { app.prefs[keyPath: key] = $0; app.prefs.save() })
    }

    private func loadDevices() async {
        guard app.selected != nil else { return }
        do {
            let api = try app.clientForSelected()
            devices = try await api.get("v1/devices", as: DevicesResult.self).devices
            deviceError = nil
        } catch {
            deviceError = error.localizedDescription
        }
    }

    private func loadUpdateCheck() async {
        guard app.selected != nil else { updateCheck = nil; return }
        do {
            let api = try app.clientForSelected()
            updateCheck = try await api.get("v1/update", as: UpdateCheckResult.self)
        } catch {
            updateCheck = nil
        }
    }

    private func revoke(_ d: DevicesResult.Device) async {
        do {
            let api = try app.clientForSelected()
            try await api.delete("v1/devices/\(d.id)")
            await loadDevices()
        } catch {
            deviceError = error.localizedDescription
        }
    }

    private func revokeAll() async {
        do {
            let api = try app.clientForSelected()
            for d in devices where !d.isCurrent {
                try await api.delete("v1/devices/\(d.id)")
            }
            await loadDevices()
        } catch {
            deviceError = error.localizedDescription
        }
    }
}
