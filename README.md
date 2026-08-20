# Server Info

Real-time monitoring van Linux-servers vanaf je iPhone.
Twee componenten: een **worker daemon** op de Linux-machine en een **iOS-app** als client.

**Status: gebouwd en werkend.** Geverifieerd tegen een echte server
(Debian 13, Intel N100) met de app in de iOS 26-simulator: mTLS-koppeling,
1 Hz SSE-stream en alle schermen met live data.

- **Aan de slag:** [INSTALLATIE.md](INSTALLATIE.md) — bouwen, uitrollen, koppelen, testen
- **Ontwerp:** de volledige analyse staat in [`docs/`](docs/)
- **Afwijkingen:** [docs/11-implementatienotities.md](docs/11-implementatienotities.md)

---

## 1. In één alinea

Op elke Debian/Ubuntu-machine draait `serverinfo-agent`: één statische binary (~6 MB, ~8 MB RSS)
die op **poort 29500/tcp** een kleine, met TLS + token beveiligde HTTP+JSON API aanbiedt.
Alleen apparaten die jij expliciet hebt gekoppeld komen door de TLS-handshake heen.
De iOS-app (SwiftUI, iOS 26) beheert een lijst servers, kiest er één als *selected server*
en toont daarvan Metrics en Tools. Real-time data (1 Hz) loopt over een
**SSE-stream**, niet over polling. Installatie en de-installatie zijn één script.

```
iPhone (SwiftUI)  ──TLS 1.3 + Bearer token──▶  serverinfo-agent  ──▶  /proc, /sys, hwmon,
   SSE 1 Hz stream                              (Go, systemd)          smartctl, nvidia-smi,
                                                :29500                 journalctl, apt, speedtest
```

---

## 2. Kernbeslissingen

| # | Onderwerp | Keuze | Waarom |
|---|-----------|-------|--------|
| D1 | Taal worker | **Go** (static, CGO_ENABLED=0) | Geen runtime-dependencies, 1 bestand, laag geheugen, cross-compile naar amd64/arm64 vanaf de Mac |
| D2 | Poort | **29500/tcp** | Aantoonbaar *unassigned* bij IANA, ligt buiten Linux' ephemeral range (32768–60999), >1024 dus geen root nodig |
| D3 | Transport | HTTP/1.1 + JSON over **mutual TLS 1.3**, certificaten van de per-server CA | Geen publieke CA, geen DNS-gedoe, geen ACME; de app valideert tegen de CA die hij bij koppeling ontving |
| D4 | Auth | **mTLS met per-server CA** + bearer token als tweede laag | Zonder gekoppeld client-certificaat komt een vreemde client niet eens door de TLS-handshake — geen banner, geen versie, geen foutmelding |
| D5 | Real-time | **Server-Sent Events** op `/v1/stream`, 1 Hz | Eén verbinding, auto-reconnect, werkt native met `URLSession.bytes`; goedkoper dan 1×/s polling |
| D6 | Zware taken | Job-pattern (`POST` → `job_id` → SSE/poll) | Speedtest/ping/traceroute duren 10–40 s; blokkeer nooit de metrics-loop |
| D7 | iOS minimum | **iOS 26** | De zwevende Liquid Glass tab bar uit je screenshots krijg je dan gratis van `TabView` |
| D8 | Persistentie iOS | SwiftData voor servers, **Keychain** voor tokens/fingerprints | Tokens horen niet in UserDefaults |
| D9 | Pairing | QR-code + eenmalige koppelcode, 15 min geldig | Één commando op de server, één scan in de app |
| D10 | Rechten | Eigen systeemgebruiker + systemd-hardening + smalle sudoers-whitelist | Niet als root draaien voor een monitoring-agent |
| D11 | Tabs | **Server / Metrics / Tools / Settings** | Vastgesteld door jou. Hardware-info wordt een detailscherm vanuit Metrics en Tools, geen eigen tab |
| D12 | Bereikbaarheid | Vier profielen: `lan` (default), `vpn`, `public`, `proxy` | Lokaal als basis; VPN achter firewalls; `a.mest.dev` direct met mTLS (zie D4) |

---

## 3. Documenten

| Document | Inhoud |
|----------|--------|
| [01 – Architectuur](docs/01-architectuur.md) | Componenten, dataflow, waarom deze opzet, alternatieven die afvielen |
| [02 – Worker daemon](docs/02-worker-daemon.md) | Taalkeuze, interne opbouw, systemd, install/uninstall-script, footprint |
| [03 – API-contract](docs/03-api-contract.md) | Alle endpoints met concrete JSON-payloads |
| [04 – Linux datasources](docs/04-datasources-linux.md) | Waar elk cijfer vandaan komt: /proc, /sys, hwmon, SMART, GPU, apt |
| [05 – iOS-app](docs/05-ios-app.md) | Schermen, navigatie, state-model, real-time pipeline, per-tab specificatie |
| [06 – Designsysteem](docs/06-design-system.md) | Kleuren, gradients, kaarten, iconenset, componenten uit je screenshots |
| [07 – Security](docs/07-security.md) | Threat model, TLS, command-injection, hardening, firewall |
| [08 – Roadmap](docs/08-roadmap.md) | 7 fases van skelet tot v1.0, met wat er per fase werkend is |
| [09 – Open vragen](docs/09-open-vragen.md) | Beslissingen die ik niet voor je kan nemen, met mijn advies erbij |
| [10 – Device enrollment](docs/10-device-enrollment.md) | mTLS, koppelflow, apparaten intrekken, informatie-lekken dichten |
| [11 – Implementatienotities](docs/11-implementatienotities.md) | Waar de gebouwde versie afwijkt van de analyse, en waarom |

---

## 4. Repo-indeling

```
Server Info/
├── agent/                     # Go worker
│   ├── cmd/serverinfo-agent/  # main.go
│   ├── internal/collect/      # cpu, mem, disk, net, sensors, gpu, smart
│   ├── internal/api/          # router, auth, sse, jobs
│   ├── internal/tools/        # speedtest, ping, dns, whois, logs, apt
│   ├── packaging/             # install.sh, uninstall.sh, serverinfo-agent.service
│   └── Makefile               # make release → tarballs amd64 + arm64
├── ios/
│   └── ServerInfo/            # Xcode-project (SwiftUI)
│       ├── Core/              # Models, APIClient, StreamClient, Keychain
│       ├── Features/          # Server, Metrics, Tools, Settings, Hardware (detail)
│       └── DesignSystem/      # Tokens, Cards, GaugeBar, Charts
└── docs/                      # deze analyse
```

## 5. Wat je nodig hebt

- **Mac:** Xcode 26.6 ✅ (aanwezig), Swift 6.3 ✅, Go 1.23+ ❌ → `brew install go`
- **Linux:** niets. Optionele extra's (`smartmontools`, `speedtest`, `lm-sensors`, `whois`, `dnsutils`) worden door `install.sh --with-extras` afgehandeld.
- **iPhone:** iOS 26.6 ✅ (iPhone 16 Pro). Een gratis Apple ID volstaat voor sideloaden; een betaald Developer-account (€99/jr) voorkomt dat je elke 7 dagen opnieuw moet signen.
