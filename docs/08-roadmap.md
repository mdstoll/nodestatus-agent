# 08 — Roadmap

Zeven fases. Elke fase levert iets dat **werkt en zichtbaar is** — geen fase waarin je
twee weken bouwt zonder iets te zien. De schattingen gaan uit van geconcentreerd werk.

---

## Fase 0 — Fundament (½ dag)

- [ ] `brew install go` (het enige dat nog ontbreekt op je Mac)
- [ ] Repo-structuur uit [README §4](../README.md) aanmaken, `git init`, private GitHub-repo
- [ ] Go-module + Xcode-project (SwiftUI App, iOS 26, bundle-id, teamsigning)
- [ ] Één test-Linux-machine kiezen om op te ontwikkelen (VM of bestaande server)

**Klaar wanneer:** `make build` levert een Linux-binary en de lege app start op je iPhone.

---

## Fase 1 — Agent: skelet + hot metrics (1–2 dagen)

- [ ] Config laden, **per-server CA + servercert** genereren
- [ ] mTLS met dynamische `ClientAuth`, enrollment-venster, `POST /v1/enroll`
- [ ] `devices list/revoke`, allowlist op fingerprint, `devices.json`
- [ ] HTTPS-server op 29500 met auth-middleware
- [ ] `/v1/health`, `/v1/system`, `/v1/metrics`
- [ ] Sampler-loop met ringbuffer: CPU (+per core), memory, storage, network, load
- [ ] `/v1/stream` (SSE) met `backfill` en keep-alive
- [ ] `install.sh` (profielen `lan`/`vpn`/`proxy`/`public`) + `uninstall.sh` + systemd-unit
- [ ] Token ook via `X-Server-Info-Token`; `trusted_proxy` voor X-Forwarded-For

**Klaar wanneer:** `curl -k -H "Authorization: Bearer …" https://host:29500/v1/stream`
laat elke seconde een JSON-regel binnenlopen, en `uninstall.sh --purge` laat de machine
achter alsof er niets is gebeurd.

---

## Fase 2 — App: verbinden en zien (2–3 dagen)

- [ ] Designsysteem-tokens (kleuren, typografie, `MetricCard`, `GaugeBar`)
- [ ] `TabView` (Liquid Glass) met vier tabs: Server / Metrics / Tools / Settings
- [ ] Server-tab: lijst, toevoegen-formulier, test-verbinding, selectie
- [ ] Keychain: keypair, CSR, client-identiteit; CA als trust anchor
- [ ] Koppelscherm: instructiecommando → QR scannen → enrollment
- [ ] `APIClient` + `StreamClient` (SSE-parser)
- [ ] Metrics-tab: identiteitskaart + de vier tegels, live op 1 Hz

**Klaar wanneer:** je een server toevoegt en de CPU-balk vloeiend meebeweegt.
Dit is het moment waarop het project "echt" voelt.

---

## Fase 3 — Netwerk, sensors, GPU (2–3 dagen)

- [ ] Agent: hwmon/thermal-uitlezing, GPU-collector, `/v1/hardware/*`
- [ ] App: netwerksectie met Swift Charts (twee lijnen, rechts-naar-links, sticky max)
- [ ] App: temperatuur-sectie met sparkline
- [ ] App: sensors-grid met available/unavailable-badges
- [ ] App: GPU-sectie, verborgen zonder capability

**Klaar wanneer:** het Metrics-scherm compleet is en visueel overeenkomt met je screenshots.

---

## Fase 4 — Hardware-detailschermen + SMART (1–2 dagen)

- [ ] Agent: `lsblk`, `smartctl -j`, DMI, NIC-details, 60 s cache
- [ ] `install.sh --with-extras` met sudoers-regel voor smartctl
- [ ] App: `HardwareView` + subschermen, bereikbaar vanuit Metrics (identiteitskaart,
      GPU/Sensors-knoppen) én vanuit de Hardware-sectie in Tools
- [ ] Meerdere-volumes-weergave (gesegmenteerde balk + uitklaplijst)

**Klaar wanneer:** je van elke schijf de health en temperatuur ziet.

---

## Fase 5 — Tools (3–4 dagen)

- [ ] Agent: job-runner + `/v1/jobs`, `/v1/jobs/{id}/stream`
- [ ] Speedtest, ping, DNS, whois, traceroute — met alle validatie uit [07 §7.4](07-security.md)
- [ ] `/v1/tools/updates`, `/locale`, `/uptime`, `/cpuinfo`
- [ ] `/v1/tools/logs*` met whitelist en live tail
- [ ] App: Tools-lijst + alle detailschermen
- [ ] App: Log Analyzer met filters, kleuren en live tail

**Klaar wanneer:** je vanaf de bank een speedtest op je server draait en de nginx-errorlog
live ziet meelopen.

---

## Fase 6 — Polish en robuustheid (2–3 dagen)

- [ ] Settings-tab: weergave, eenheden, real-time, privacy, data, over
- [ ] Alle lege/fout/offline-staten uit [05 §5.12](05-ios-app.md)
- [ ] Reconnect-backoff, achtergrond/voorgrond, netwerkwissel LAN↔4G
- [ ] Certificaatvernieuwing (30 dagen voor verval) end-to-end testen
- [ ] Apparaat intrekken vanuit de app + vanaf de CLI
- [ ] Secure Enclave als hardening onderzoeken (risico-item, zie [10 §10.9](10-device-enrollment.md))
- [ ] Bottom accessory met geselecteerde server + live CPU/RAM
- [ ] Dynamic Type, VoiceOver, Reduce Motion
- [ ] Haptics, numerieke transitions, `.monospacedDigit()` overal
- [ ] Test op een tweede architectuur (arm64 / Raspberry Pi) en een tweede distro
- [ ] Profiel `proxy` end-to-end testen op `a.mest.dev`: nginx-location, SSE zonder
      buffering, basic auth naast het token, publiek certificaat zonder pinning

**Klaar wanneer:** je de app een week gebruikt zonder ergens tegenaan te lopen.

---

## Fase 7 — Uitrol (½ dag)

- [ ] `make release` → tarballs amd64 + arm64 met SHA256SUMS
- [ ] `location /si/` op `a.mest.dev` (zonder basic auth) + rsync-target in de Makefile
- [ ] One-liner installer end-to-end testen op een verse machine
- [ ] Optioneel `.deb` via `nfpm`
- [ ] Korte README voor de agent (installeren, config, uninstall)
- [ ] App-signing regelen (gratis Apple ID = 7 dagen; Developer Program = 1 jaar)

---

## Totaal

**Ongeveer 13–19 dagen** geconcentreerd werk voor v1.0.
De eerste "wow" (Fase 2) ligt op **3–5 dagen**.

## Volgorde-advies

Bouw agent en app **in tandem per feature**, niet eerst de hele agent en dan de hele app.
Elke endpoint die je toevoegt, bouw je diezelfde dag in de app in. Zo merk je meteen dat
een veld ontbreekt of een eenheid onhandig is, in plaats van na twee weken al je JSON te
moeten herzien.

## Ideeën voor ná v1.0

| Idee | Waarde |
|------|--------|
| Push-notificaties bij drempels (disk > 90%, temp kritiek, server offline) | Hoog — verandert de app van "kijken" in "gewaarschuwd worden". Vereist wel een klein pushrelay of een agent die zelf APNs aanroept. |
| Widgets + Live Activities | Hoog — CPU/RAM van je hoofdserver op je lockscreen |
| Docker/Podman-container-overzicht | Hoog als je containers draait |
| Historie langer dan 5 min (SQLite in de agent, 7 dagen op 1 min) | Middel — maakt trendgrafieken mogelijk |
| Acties: service herstarten, `apt upgrade` | Middel, hoge securityimpact — zie [07 §7.8](07-security.md) |
| macOS-versie van de app (zelfde SwiftUI-code) | Laag/gratis |
| Apple Watch-complicatie | Leuk |
