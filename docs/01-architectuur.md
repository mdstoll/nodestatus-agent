# 01 — Architectuur

## 1.1 Componenten

```
┌───────────────────────────────────────────┐        ┌──────────────────────────────────┐
│ iOS-app "Server Info"                     │        │ Linux-machine                    │
│                                           │        │                                  │
│  ServerStore (SwiftData)                  │        │  serverinfo-agent (systemd)      │
│   ├─ Server A  ← selected                 │        │   ├─ HTTP-server :29500 (TLS)    │
│   ├─ Server B                             │        │   ├─ Sampler-loop  1 Hz          │
│   └─ Server C                             │        │   │    └─ ringbuffer 300 samples │
│                                           │        │   ├─ Static-cache  (60 s TTL)    │
│  APIClient  ──── GET /v1/... ─────────────┼───────▶│   ├─ Job-runner (speedtest/ping) │
│  StreamClient ── GET /v1/stream (SSE) ◀───┼────────┤   └─ SSE-broadcaster             │
│                                           │        │                                  │
│  MetricsViewModel (@Observable)           │        │  leest: /proc /sys /run          │
│   └─ ringbuffers voor charts              │        │  exec:  smartctl nvidia-smi ...  │
└───────────────────────────────────────────┘        └──────────────────────────────────┘
```

## 1.2 Dataflow, drie snelheden

De agent deelt data in drie klassen in. Dit is de belangrijkste architectuurkeuze, want het
bepaalt of de app soepel op 1 Hz kan draaien zonder de server te belasten.

| Klasse | Voorbeelden | Verversing | Endpoint |
|--------|-------------|-----------|----------|
| **Statisch** | Model, chip, kernel, distro, totale RAM, disk-layout, boot-tijd | 1× bij start, cache 60 s | `GET /v1/system` |
| **Hot** | CPU%, per-core, RAM, swap, load, net-throughput, temps, GPU-load | elke seconde, gepusht | `GET /v1/stream` (SSE) |
| **Koud / on-demand** | SMART, apt-updates, journal-tail, speedtest, whois, ping | alleen op verzoek | `GET/POST /v1/tools/*` |

**Waarom dit onderscheid?** Als de app elke seconde alles zou opvragen, zou de agent elke
seconde `smartctl` en `apt-get -s` moeten draaien — dat kost seconden CPU en spamt de disk.
Nu is de 1 Hz-loop puur het lezen van een handvol bestanden uit `/proc` en `/sys`:
< 1 ms CPU per sample.

## 1.3 De 1 Hz-loop in detail

De agent draait één ticker van 1 s die *altijd* loopt zodra er ≥1 SSE-client is
(en anders slaapt, om idle-CPU op nul te houden):

1. Lees `/proc/stat`, `/proc/meminfo`, `/proc/net/dev`, `/proc/loadavg`, hwmon-inputs.
2. Bereken deltas t.o.v. het vorige sample (CPU% en netwerksnelheid zijn *per definitie* deltas).
3. Schrijf het sample in een ringbuffer van 300 stuks (= 5 minuten historie).
4. Broadcast het sample als één SSE-event naar alle verbonden clients.

De ringbuffer is er zodat de app bij (her)verbinden meteen een gevulde grafiek kan tonen
in plaats van 60 seconden een lege chart: `GET /v1/stream?backfill=60` stuurt eerst 60
historische samples en gaat daarna live verder.

## 1.4 Waarom SSE en niet polling of WebSocket

| | 1 Hz polling | WebSocket | **SSE** |
|---|---|---|---|
| Verbindingen | 1 nieuwe request/s | 1 persistent | 1 persistent |
| TLS-handshakes | veel (tenzij keep-alive) | 1 | 1 |
| Batterij iPhone | slecht | goed | goed |
| Auto-reconnect | zelf bouwen | zelf bouwen | ingebouwd in protocol (`Last-Event-ID`) |
| Server-complexiteit | laag | middel (framing, ping/pong) | **laag** (het is gewoon HTTP) |
| Bidirectioneel | n.v.t. | ja | nee — niet nodig |
| iOS-support | trivia | `URLSessionWebSocketTask` | `URLSession.bytes(for:)` + regels parsen |

We hebben geen bidirectionele stream nodig: commando's (ping, speedtest) zijn losse POSTs.
SSE is daarmee de simpelste optie die aan alle eisen voldoet. Polling blijft als
fallback in de app zitten voor het geval een reverse proxy SSE breekt.

## 1.5 Netwerktopologie — hoe bereikt de telefoon de server?

In de praktijk het lastigste punt, dus de agent kent vier **connectieprofielen**.
`install.sh --mode <profiel>` zet de config in één keer goed.

> **Alle profielen gebruiken hetzelfde toegangsmechanisme:** mutual TLS met de per-server
> CA uit [10 — Device enrollment](10-device-enrollment.md). Alleen gekoppelde apparaten
> komen door de handshake. Het verschil tussen de profielen zit puur in *waar* de agent
> luistert, niet in hoe streng hij is.

### Profiel A — `lan` (standaard)

Voor alles thuis en op kantoor. De basis, en waar de meeste servers in vallen.

```toml
bind       = "0.0.0.0:29500"
allow_cidr = ["192.168.1.0/24"]      # install.sh vult je eigen subnet in
tls_cert   = "/etc/serverinfo-agent/cert.pem"   # getekend door de eigen CA
```

Niets open naar internet. Servercertificaat van de eigen CA, client-certificaat vereist.
Werkt out of the box en vereist geen enkele infrastructuur.

### Profiel B — `vpn`

Voor apparaten achter een firewall waar je geen poorten kunt of wilt openzetten.
De agent bindt uitsluitend op het VPN-adres, zodat hij op de LAN- en WAN-interface
domweg niet bestaat.

```toml
bind       = "100.64.0.5:29500"      # WireGuard- of Tailscale-adres
allow_cidr = ["100.64.0.0/10"]
```

Verder identiek aan profiel A. Dit is de veiligste variant en kost je in de app geen
enkele extra stap.

### Profiel C — `public` (aanbevolen voor `a.mest.dev`)

De agent luistert zelf op een publiek bereikbare poort. Dat is met mTLS veilig: zonder
gekoppeld client-certificaat faalt de handshake en is er niets te zien of te oogsten.

```toml
bind = "0.0.0.0:29500"
# certificaten van de eigen CA — geen Let's Encrypt, geen ACME, geen renewals
```

Geen publiek certificaat nodig, want de app valideert tegen de CA die hij bij koppeling
ontving. Zie [07 §7.6](07-security.md#76-netwerkblootstelling) voor de extra maatregelen
die bij publieke blootstelling horen (fail2ban, strengere auth-limits).

### Profiel D — `proxy` (alles op 443)

Alleen nodig als je op restrictieve netwerken zit die niets anders dan 443 doorlaten. De
agent bindt op loopback en nginx doet zowel TLS als de client-certificaatcontrole — in een
**apart server-blok**, niet je bestaande vhost (anders krijgen browserbezoekers een
certificaatkiezer). Volledige nginx-config in
[10 §10.7](10-device-enrollment.md#107-gevolgen-voor-de-vier-connectieprofielen).


### Wat dit betekent voor de app

| Profiel | Host in de app | Servercert wordt gevalideerd tegen |
|---------|----------------|-------------------------------------|
| A `lan` | `192.168.1.50:29500` | de CA van die server |
| B `vpn` | `100.64.0.5:29500` | de CA van die server |
| C `public` | `a.mest.dev:29500` | de CA van die server |
| D `proxy` | `a.mest.dev:8443` | nginx' certificaat (publieke CA) |

In alle gevallen stuurt de app zijn client-certificaat mee. Er is dus **één** codepad in de
app, niet vier — dat is precies waarom mTLS hier ook de simpelste oplossing is.

**Dual-host:** een server mag twee adressen hebben (`lan_host` + `remote_host`), die
parallel worden geprobeerd met 300 ms voorsprong voor het LAN-adres. Handig voor een
thuisserver die op je eigen wifi via het LAN-IP gaat en onderweg via VPN.

**Gebruik hostnames, geen IP-literals, voor remote hosts.** `a.mest.dev` heeft alleen een
A-record (194.163.173.139, geen AAAA). Op de IPv6-only mobiele netwerken die Apple
verplicht ondersteunt, werkt een hostname via NAT64/DNS64 wél en een hardgecodeerd
IPv4-adres níet. De app waarschuwt daarom bij het invoeren van een IPv4-literal als
remote host.

## 1.6 Alternatieven die zijn afgevallen

| Alternatief | Waarom niet |
|-------------|-------------|
| **Prometheus node_exporter + Grafana** | Zwaar, tekstformaat parsen op de telefoon, geen tools (ping/speedtest/logs), geen mooie native UI. Wel: als je ooit node_exporter al draait, kan de agent daar naast bestaan (andere poort). |
| **Netdata** | Prima product, maar 150+ MB RAM en een eigen webUI; het tegenovergestelde van "lightweight en to-the-point". |
| **SSH vanuit de app** | Geen daemon nodig, maar: SSH-key op je telefoon, shell-parsing van `top`/`df` is fragiel, 1 Hz over SSH is traag, en je geeft de app volledige shell-rechten. Slechte security-trade-off. |
| **Agent pusht naar een centrale cloud** | Vereist een server die jij moet hosten en betalen; introduceert een derde partij met al je serverdata. Overkill voor een persoonlijke app. |
| **SNMP** | Standaard, maar beperkte metrics, geen SMART/GPU/logs, en de MIB-wereld is niet leuk. |

## 1.7 Belasting op de server (target)

| Meting | Doel | Hoe |
|--------|------|-----|
| RSS in bedrijf | < 25 MB (gemeten: 22 MB) | Go, geen framework, ringbuffer met vaste grootte |
| CPU idle (geen client) | ~0 % | Sampler-loop stopt 5 min na de laatste SSE-client (zie [11 §11.4](11-implementatienotities.md)) |
| CPU met 1 client op 1 Hz | < 0,3 % van één core | Alleen `/proc`-reads, geen `exec` in de hot path |
| Disk-writes | 0 | Geen logging naar disk; alles naar journald op level warn+ |
| Opstarttijd | < 50 ms | Geen init-werk behalve config + cert laden |
