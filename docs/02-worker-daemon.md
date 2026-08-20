# 02 — Worker daemon (`serverinfo-agent`)

## 2.1 Taalkeuze

| Optie | RSS | Deps op target | Bouwstap | Oordeel |
|-------|-----|----------------|----------|---------|
| **Go** | ~8 MB | **geen** (static, CGO uit) | cross-compile op Mac | ✅ **Gekozen** |
| Python 3 (stdlib-only) | ~25 MB | python3 (staat er meestal) | geen | Goede fallback, maar trager en je bent afhankelijk van de systeem-Python |
| Rust | ~5 MB | geen | cross-compile (lastiger dan Go) | Marginaal beter dan Go, veel meer bouwcomplexiteit |
| Node.js | ~45 MB | node runtime installeren | geen | Te zwaar, extra dependency op elke server |
| C | ~2 MB | libc | per-distro bouwen | Te veel handwerk voor JSON/TLS/HTTP |

**Go wint** omdat het het enige is dat tegelijk lightweight *en* dependency-vrij *en*
makkelijk te bouwen is. Eén `GOOS=linux GOARCH=amd64 go build` op je Mac levert een bestand
dat op Debian 11, Ubuntu 24.04 en een Raspberry Pi (arm64) draait zonder dat je daar ooit
een compiler of interpreter installeert. Dat is precies wat "clean werken" betekent voor
een agent die op willekeurige machines terechtkomt.

Er zijn **geen externe Go-modules nodig** behalve één QR-encoder voor de pairing-output —
alles (HTTP-server, TLS, JSON, crypto/rand) zit in de stdlib.

## 2.2 Interne opbouw

```
main.go
 ├─ config.Load()          → /etc/serverinfo-agent/config.toml
 ├─ collect.NewSampler()   → 1 Hz ticker, ringbuffer[300]
 ├─ api.NewServer()
 │    ├─ middleware: auth (bearer, constant-time) → ratelimit → recover → json
 │    ├─ /v1/health        (geen auth, alleen {"ok":true,"version":...})
 │    ├─ /v1/system        static-cache 60 s
 │    ├─ /v1/metrics       laatste sample uit ringbuffer
 │    ├─ /v1/stream        SSE-broadcaster
 │    ├─ /v1/hardware/*    sensors, smart, gpu, disks, net-interfaces
 │    ├─ /v1/tools/*       logs, apt, locale, uptime, cpuinfo
 │    └─ /v1/jobs/*        job-runner voor speedtest/ping/dns/whois/traceroute
 └─ http.ServeTLS(:29500)
```

### Sampler
Eén goroutine. Houdt het vórige `/proc/stat`- en `/proc/net/dev`-sample vast om deltas te
berekenen. Schrijft in een ringbuffer die met een `sync.RWMutex` wordt gedeeld. Stopt
zichzelf (`ticker.Stop()`) zodra het aantal SSE-abonnees op 0 staat en start weer bij de
eerste abonnee — zo is idle-CPU écht nul.

### Job-runner
`speedtest`, `ping`, `traceroute`, `whois` en `dig` duren te lang voor een request/response.
Patroon:

```
POST /v1/jobs        {"type":"speedtest"}            → 202 {"job_id":"j_7f3a"}
GET  /v1/jobs/j_7f3a                                  → {"state":"running","progress":0.4}
GET  /v1/jobs/j_7f3a/stream  (SSE)                    → live regels + eindresultaat
```

Maximaal **2 gelijktijdige jobs**, elk met een harde timeout (speedtest 90 s, ping 60 s,
rest 20 s) en `exec.CommandContext` zodat een timeout het proces daadwerkelijk killt.
Jobs worden 5 minuten bewaard en dan opgeruimd. Nooit `sh -c` — altijd argv-array
(zie [07 — Security](07-security.md)).

## 2.3 Configuratie — `/etc/serverinfo-agent/config.toml`

```toml
# Gegenereerd door install.sh — chmod 600, owner serverinfo:serverinfo
bind          = "0.0.0.0:29500"   # of "127.0.0.1:29500" achter een tunnel,
                                  # of "100.64.0.5:29500" op je Tailscale-IP
token         = "kQ8s...bLp"      # 32 random bytes, base64url
tls_cert      = "/etc/serverinfo-agent/cert.pem"   # leeg = platte HTTP (alleen achter een proxy op loopback!)
tls_key       = "/etc/serverinfo-agent/key.pem"
trusted_proxy = []                # bv. ["127.0.0.1"] — dan wordt X-Forwarded-For gebruikt
                                  # voor rate limiting en allow_cidr. Alleen vertrouwen
                                  # als er écht een proxy voor staat.
display_name  = "web-01"          # wat de app als naam toont
allow_cidr    = []                # bv. ["192.168.1.0/24"] — leeg = alles toestaan
sample_hz     = 1
history_size  = 300               # 5 min à 1 Hz

enroll_window_minutes = 15
enroll_max_attempts   = 5
client_cert_days      = 365

[features]
smart      = true                 # vereist smartmontools + sudoers-regel
gpu        = true                 # nvidia-smi / amdgpu-sysfs / intel_gpu_top
speedtest  = true                 # vereist speedtest of librespeed-cli
apt        = true
logs       = true

[logs]
# Whitelist. Alles wat hier niet staat, is niet leesbaar via de API.
units = ["ssh", "nginx", "docker", "systemd-journald", "cron", "ufw"]
files = ["/var/log/syslog", "/var/log/auth.log", "/var/log/kern.log",
         "/var/log/nginx/access.log", "/var/log/nginx/error.log"]
max_lines = 500
```

Alle waarden zijn ook via environment variables te zetten (`SERVERINFO_BIND`, …) zodat
de agent ook in een container bruikbaar is.

## 2.4 systemd-unit — `/etc/systemd/system/serverinfo-agent.service`

```ini
[Unit]
Description=Server Info agent
Documentation=https://github.com/<jij>/server-info
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=serverinfo
Group=serverinfo
ExecStart=/usr/local/bin/serverinfo-agent --config /etc/serverinfo-agent/config.toml
Restart=on-failure
RestartSec=3
WatchdogSec=30

# --- hardening ---
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=no            # nee: SMART heeft /dev/sd* nodig
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallFilter=@system-service
SystemCallArchitectures=native
CapabilityBoundingSet=CAP_NET_RAW      # alleen als je ICMP-ping in-process wilt
ReadWritePaths=/var/lib/serverinfo-agent
SupplementaryGroups=systemd-journal    # journalctl mag lezen zonder root
MemoryMax=128M
TasksMax=64

[Install]
WantedBy=multi-user.target
```

`Type=notify` + `WatchdogSec` betekent dat systemd de agent herstart als de sampler-loop
zou vastlopen — je merkt nooit dat er iets misging.

## 2.5 `install.sh`

Eén script, idempotent, veilig om twee keer te draaien.

```bash
sudo ./install.sh                          # profiel lan (standaard), detecteert je subnet
sudo ./install.sh --mode vpn --vpn-ip 100.64.0.5
sudo ./install.sh --mode proxy             # bind op 127.0.0.1, print de nginx-snippet
sudo ./install.sh --mode public --tls-cert /etc/letsencrypt/live/a.mest.dev/fullchain.pem \
                                --tls-key  /etc/letsencrypt/live/a.mest.dev/privkey.pem
sudo ./install.sh --with-extras            # + smartmontools, lm-sensors, whois, dnsutils, speedtest
sudo ./install.sh --port 29500 --name "web-01"
```

De vier profielen staan uitgewerkt in [01 §1.5](01-architectuur.md#15-netwerktopologie--hoe-bereikt-de-telefoon-de-server).
`--mode proxy` schrijft geen nginx-config (dat blijft jouw beheer), maar print wel de
kant-en-klare `location`-blok inclusief de SSE-instellingen die je moet plakken, en
controleert daarna met een lokale `curl` of het werkt.

Stappen:

1. **Preflight** — root-check, detecteer `dpkg`/`apt`, detecteer arch (`x86_64`→amd64,
   `aarch64`→arm64), check dat systemd draait, check dat poort 29500 vrij is (`ss -lnt`).
2. **Gebruiker** — `useradd --system --no-create-home --shell /usr/sbin/nologin serverinfo`.
3. **Binary** — kopieer naar `/usr/local/bin/serverinfo-agent`, `chmod 755`.
   (Of, als je later GitHub Releases gebruikt: `curl -fsSL .../install.sh | sudo bash`
   downloadt de juiste tarball, met SHA256-verificatie.)
4. **PKI** — genereer de per-server **CA** (ECDSA P-256, 10 jaar) in
   `/etc/serverinfo-agent/ca/`, en een servercertificaat getekend door die CA met SAN's
   voor álle lokale IP's + hostname. `chmod 600`, owner `serverinfo`.
   Zie [10 — Device enrollment](10-device-enrollment.md).
5. **Extra's** (`--with-extras`) — `apt-get install -y smartmontools lm-sensors whois
   dnsutils iputils-ping`. Speedtest: Ookla-repo toevoegen óf `librespeed-cli` binary
   plaatsen (zie 2.7). Draai `sensors-detect --auto` als lm-sensors nieuw is.
6. **sudoers** — `/etc/sudoers.d/serverinfo-agent` met precies drie regels
   (smartctl, nvidia-smi indien nodig, intel_gpu_top), `NOPASSWD`, volledig gespecificeerde
   paden en argumenten. `visudo -c` als check. Alleen als `--with-extras`.
7. **systemd** — unit plaatsen, `daemon-reload`, `enable --now`, wachten tot `active`.
8. **Firewall** — als `ufw` actief is: vraag/`--yes` → `ufw allow 29500/tcp comment 'Server Info'`.
9. **Koppelen** — opent een enrollment-venster van 15 minuten en print:
   ```
   ✔ serverinfo-agent draait op web-01

     Host        192.168.1.50:29500
     Fingerprint SHA256:9f2a…c4b1
     Koppelcode  K7QM-3XR9   (geldig tot 16:47)

     Scan deze QR in de Server Info app:
     ███▀▀▀███  … (ASCII-QR van serverinfo://enroll?…)
   ```
   Er staat bewust **geen token** in de output: dat wordt pas bij enrollment aangemaakt,
   per apparaat.
10. **Zelftest** — `curl -sk https://127.0.0.1:29500/v1/health` moet `{"ok":true}` geven,
    anders exit 1 met de laatste 20 journal-regels erbij.

## 2.6 `uninstall.sh`

Het spiegelbeeld, en het moet écht alles opruimen:

```bash
sudo ./uninstall.sh            # verwijdert agent, laat extra pakketten staan
sudo ./uninstall.sh --purge    # + config, certs, /var/lib, gebruiker, ufw-regel
sudo ./uninstall.sh --purge --remove-extras   # + apt-get remove smartmontools etc.
```

1. `systemctl disable --now serverinfo-agent` (negeer "not found").
2. Verwijder unit, `daemon-reload`, `reset-failed`.
3. Verwijder `/usr/local/bin/serverinfo-agent`.
4. `--purge`: `/etc/serverinfo-agent` (inclusief de CA en alle gekoppelde apparaten),
   `/var/lib/serverinfo-agent`,
   `/etc/sudoers.d/serverinfo-agent`, `userdel serverinfo`, `ufw delete allow 29500/tcp`.
5. Print aan het eind wat er *niet* is verwijderd, zodat er nooit iets stilletjes achterblijft.

Het script draait ook prima als de installatie half mislukt is — elke stap is
`|| true` met een expliciete melding.

## 2.7 Optionele externe tools

| Feature | Binary | Pakket | Zonder dit pakket |
|---------|--------|--------|-------------------|
| SMART | `smartctl` | `smartmontools` | Sensors-tab toont schijven zonder health |
| Temperaturen | *(geen)* | — (`lm-sensors` alleen voor labels) | Werkt via `/sys/class/hwmon` |
| Speedtest | `speedtest` | Ookla-repo | Fallback `librespeed-cli`, anders knop disabled |
| WHOIS | `whois` | `whois` | Endpoint geeft 501 |
| DNS | `dig` | `dnsutils` | Fallback: in-process DNS-query in Go (aanbevolen: gewoon altijd in-process) |
| Ping | `ping` | `iputils-ping` | Fallback: in-process ICMP met `CAP_NET_RAW` |
| NVIDIA GPU | `nvidia-smi` | driver | GPU-sectie verborgen |

**Ontwerpregel:** de agent crasht of faalt nooit door een ontbrekende tool. Bij start
detecteert hij wat aanwezig is en publiceert dat in `GET /v1/system` als
`"capabilities": ["smart","gpu.nvidia","speedtest.ookla","whois"]`. De **app verbergt
UI-elementen waarvoor de capability ontbreekt** — geen dode knoppen.

## 2.8 Bouwen en distribueren

```makefile
VERSION := 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/amd64/serverinfo-agent ./cmd/serverinfo-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/arm64/serverinfo-agent ./cmd/serverinfo-agent
	# tarball per arch met binary + install.sh + uninstall.sh + service-file
```

**Distributie** (besloten, zie [09 OQ-9](09-open-vragen.md)): broncode in een private
GitHub-repo, en de installer + tarballs geserveerd vanaf `a.mest.dev`, waar al nginx met een
geldig certificaat draait. Eén `location /si/` zonder basic auth met daarin `install.sh`,
de twee tarballs en `SHA256SUMS`. Uitrollen op een nieuwe machine wordt dan:

```bash
curl -fsSL https://a.mest.dev/si | sudo bash
```

Het script detecteert de architectuur, haalt de juiste tarball op, **verifieert de SHA256
voordat het iets uitpakt**, en draait daarna de stappen uit §2.5. `make release` bouwt beide
architecturen, genereert `SHA256SUMS` en rsynct het geheel naar die map — één commando per
release.

Tijdens de ontwikkeling werkt de handmatige route ook gewoon:

```bash
scp serverinfo-agent_0.1.0_linux_amd64.tar.gz user@host:/tmp/
ssh user@host 'cd /tmp && tar xzf serverinfo-agent_*.tar.gz && sudo ./install.sh --with-extras'
```

Later eventueel een `.deb` (met `nfpm`) zodat `apt remove serverinfo-agent` óók werkt —
maar de shell-scripts zijn stap 1 en werken op elke distro met systemd, niet alleen Debian.
