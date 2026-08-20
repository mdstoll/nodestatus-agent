# 03 — API-contract

Basis-URL: `https://<host>:29500`, of achter een reverse proxy `https://<host>/<prefix>`
(bijv. `https://a.mest.dev/_si`).
Content-Type: `application/json; charset=utf-8`. Alle tijden zijn Unix-epoch seconden (float).
Alle byte-waarden zijn **bytes** (integers) — formatteren doet de app, niet de server.

## 3.0 Authenticatie

**Laag 1 — client-certificaat (mTLS).** Elke verbinding vereist een client-certificaat dat
door de CA van díe server is uitgegeven en waarvan de fingerprint in de allowlist staat.
Zonder geldig certificaat faalt de TLS-handshake; er komt geen HTTP-verzoek aan.
De enige uitzondering is `POST /v1/enroll` tijdens een open enrollment-venster.
Zie [10 — Device enrollment](10-device-enrollment.md).

**Laag 2 — bearer token.** Het token mag via **twee** headers, en de agent accepteert ze allebei:

```
Authorization: Bearer <token>
X-Server-Info-Token: <token>
```

De tweede bestaat omdat een reverse proxy de `Authorization`-header al kan claimen —
`a.mest.dev` draait bijvoorbeeld HTTP Basic Auth (`WWW-Authenticate: Basic realm="Administrator"`).
Met de custom header kunnen beide lagen naast elkaar bestaan zonder elkaar te overschrijven.
Staan ze allebei in het verzoek, dan wint `X-Server-Info-Token`.

Beide lagen gelden op **alle** endpoints, inclusief `/v1/health` — die is dus niet
publiek. Enige uitzondering: verzoeken vanaf `127.0.0.1`, zodat `install.sh` zijn zelftest
kan doen. Fouten geven een lege `401`/`403` met een vaste vertraging van 250 ms en zonder
enig onderscheid tussen "onbekend apparaat" en "verkeerd token".

### Enrollment- en apparaat-endpoints

| Endpoint | Auth | Doel |
|----------|------|------|
| `POST /v1/enroll` | eenmalige koppelcode | CSR ondertekenen, geeft client-cert + CA + token terug |
| `GET /v1/devices` | mTLS + token | Lijst gekoppelde apparaten |
| `POST /v1/devices/me/renew` | mTLS + token | Certificaat verlengen (vanaf 30 dagen voor verval) |
| `DELETE /v1/devices/{id}` | mTLS + token | Apparaat intrekken |

```
POST /v1/enroll
{ "code": "K7QM3XR9", "csr_pem": "-----BEGIN CERTIFICATE REQUEST-----…",
  "device_name": "iPhone van Merlin" }
→ 200 { "device_id": "d_a41f", "client_cert_pem": "…", "ca_cert_pem": "…",
        "api_token": "kQ8s…bLp", "expires_at": 1787238767 }
```

Fouten zijn altijd:
```json
{ "error": { "code": "unauthorized|not_found|unavailable|invalid_argument|timeout|internal",
             "message": "menselijke uitleg" } }
```

---

## 3.1 `GET /v1/health`

```json
{ "ok": true, "version": "0.1.0", "uptime_s": 12843 }
```
Gebruikt door de app om een server als *online* te markeren en door `install.sh` als zelftest.
**Niet publiek** — vereist mTLS + token, behalve vanaf loopback (zie §3.0).

---

## 3.2 `GET /v1/system` — statische info (blok bovenaan Metrics)

```json
{
  "hostname": "web-01",
  "display_name": "Web Server",
  "os": { "name": "Ubuntu", "version": "24.04.1 LTS", "id": "ubuntu",
          "kernel": "6.8.0-45-generic", "arch": "x86_64",
          "virtualization": "kvm", "container": null },
  "model": { "vendor": "Dell Inc.", "product": "PowerEdge R640",
             "serial_present": true, "board": "0W23F8" },
  "cpu":  { "model": "Intel Xeon Silver 4210R", "vendor": "GenuineIntel",
            "cores_physical": 10, "threads": 20, "sockets": 1,
            "base_mhz": 2400, "max_mhz": 3200, "flags_notable": ["avx2","aes","vmx"],
            "cache_l3_bytes": 14417920 },
  "memory": { "total_bytes": 68719476736, "swap_total_bytes": 8589934592,
              "modules": [{ "size_bytes": 34359738368, "type": "DDR4",
                            "speed_mts": 2933, "locator": "DIMM_A1" }] },
  "storage_total_bytes": 2000398934016,
  "boot_time": 1755548280,
  "uptime_s": 154487,
  "capabilities": ["smart","gpu.nvidia","speedtest.ookla","whois","dig","apt","journal"],
  "generated_at": 1755702767.42
}
```

> `model.vendor/product` komen uit `/sys/class/dmi/id/`. In een VM staat daar bijvoorbeeld
> `QEMU / Standard PC` — de app toont dan `virtualization` erbij, zodat je ziet dat het een
> VM is. In een container zijn DMI-velden leeg en toont de app `container: docker`.

---

## 3.3 `GET /v1/metrics` — één momentopname

Identiek aan één SSE-sample. Bedoeld voor de fallback-polling en voor de serverlijst
(daar wil je goedkoop een CPU-percentage per server tonen zonder een stream te openen).

```json
{
  "t": 1755702767.42,
  "cpu": { "total": 22.4, "user": 15.2, "system": 7.1, "iowait": 0.1, "steal": 0.0,
           "cores": [21.2, 18.0, 30.4, 12.1],
           "freq_mhz": [3100, 2400, 2400, 2400],
           "load": [0.84, 0.61, 0.55], "procs_running": 3, "procs_total": 412 },
  "memory": { "total": 68719476736, "used": 61407494144, "available": 7311982592,
              "cached": 12884901888, "buffers": 536870912,
              "swap_total": 8589934592, "swap_used": 0, "percent": 89.4 },
  "storage": [
    { "mount": "/", "device": "/dev/nvme0n1p2", "fstype": "ext4",
      "total": 494384795648, "used": 315952988160, "percent": 63.9,
      "read_bps": 122880, "write_bps": 4915200 },
    { "mount": "/data", "device": "/dev/sdb1", "fstype": "xfs",
      "total": 1506014138368, "used": 402653184000, "percent": 26.7,
      "read_bps": 0, "write_bps": 0 }
  ],
  "network": {
    "rx_bps": 9420, "tx_bps": 5230,
    "rx_total": 3758096384, "tx_total": 1932735283,
    "interfaces": [
      { "name": "eno1", "up": true, "speed_mbps": 1000,
        "rx_bps": 9420, "tx_bps": 5230, "rx_total": 3758096384, "tx_total": 1932735283 }
    ]
  },
  "temps": [ { "key": "cpu_package", "label": "CPU Package", "celsius": 42.0,
               "high": 80.0, "critical": 100.0 } ],
  "gpu": [ { "index": 0, "util_percent": 14, "mem_used": 1073741824,
             "mem_total": 25769803776, "temp_c": 38, "power_w": 62 } ]
}
```

**Ontwerpnoot — `rx_total`/`tx_total`:** dit zijn de tellers sinds boot uit `/proc/net/dev`.
Ze lopen over bij 2^64 en resetten bij reboot; de app moet daar tegen kunnen (bij een
lagere waarde dan de vorige: reset detecteren, delta = 0). Voor de "Total Usage"-weergave
uit je screenshot is dit precies wat je wilt: verbruik sinds boot.

---

## 3.4 `GET /v1/stream` — SSE, het hart van de app

```
GET /v1/stream?backfill=60&sections=cpu,memory,storage,network,temps,gpu
Accept: text/event-stream
```

Response:
```
retry: 2000

event: sample
id: 1755702767
data: {"t":1755702767.42,"cpu":{...},"memory":{...}, ... }

event: sample
id: 1755702768
data: {...}

: keep-alive
```

- `backfill=N` — stuurt eerst N historische samples uit de ringbuffer (max 300), zodat de
  netwerkgrafiek meteen gevuld is. Deze dragen `event: backfill` zodat de app ze anders
  kan animeren (geen "instroom van rechts", maar in één keer neerzetten).
- `sections=` — laat de app zeggen wat hij nodig heeft. Op de serverlijst volstaat
  `cpu,memory`; dat scheelt bandbreedte en batterij.
- Elke 15 s een `:` comment als keep-alive, zodat proxies de verbinding niet sluiten.
- Bij herverbinden stuurt de client `Last-Event-ID`; de agent hervat vanaf dat punt als het
  nog in de ringbuffer zit.

---

## 3.5 Hardware

### `GET /v1/hardware/sensors`
```json
{ "chips": [
    { "name": "coretemp-isa-0000", "label": "Intel Core Temperature",
      "sensors": [
        { "key":"temp1_input", "label":"Package id 0", "type":"temperature",
          "value":42.0, "unit":"°C", "high":80.0, "critical":100.0, "status":"ok" },
        { "key":"fan1_input", "label":"Fan 1", "type":"fan",
          "value":1240, "unit":"RPM", "status":"ok" },
        { "key":"in0_input", "label":"Vcore", "type":"voltage",
          "value":1.02, "unit":"V", "status":"ok" }
      ] } ],
  "available": 12, "unavailable": 2 }
```
`status` is `ok | warn | crit | unavailable` — de app hoeft geen drempels te kennen.
De teller `available`/`unavailable` voedt precies de "✅ Available 30 ❌ Not Available 2"-
badge uit je screenshot.

### `GET /v1/hardware/smart`
```json
{ "disks": [
  { "device":"/dev/nvme0n1", "model":"Samsung SSD 980 PRO 1TB", "serial_present":true,
    "size_bytes":1000204886016, "rotation_rpm":0, "protocol":"NVMe",
    "health":"PASSED", "temp_c":41, "power_on_hours":8123, "power_cycles":142,
    "percentage_used":3, "data_units_written":38294011,
    "attributes":[{"id":5,"name":"Reallocated_Sector_Ct","value":100,"raw":0,"status":"ok"}] } ] }
```
Kost ~200 ms per schijf → **nooit in de 1 Hz-loop**, 60 s cache.

### `GET /v1/hardware/gpu`
```json
{ "gpus": [
  { "index":0, "vendor":"NVIDIA", "name":"RTX A4000", "driver":"550.90.07",
    "util_percent":14, "mem_util_percent":4,
    "mem_used":1073741824, "mem_total":17179869184,
    "temp_c":38, "fan_percent":30, "power_w":62, "power_limit_w":140,
    "clock_graphics_mhz":1215, "clock_mem_mhz":6500,
    "processes":[{"pid":2841,"name":"ollama","mem_used":900000000}] } ] }
```
Leeg array (`{"gpus":[]}`) als er geen GPU is — de app verbergt de sectie dan.

### `GET /v1/hardware/disks` en `GET /v1/hardware/network`
Statische layout: block devices, partities, RAID/LVM/ZFS-detectie; NIC's met MAC, MTU,
IPv4/IPv6, gateway, DNS-servers.

---

## 3.6 Tools

| Endpoint | Methode | Levert |
|----------|---------|--------|
| `/v1/tools/cpuinfo` | GET | Volledige CPU-details, per-core freq/temp/governor, C-states |
| `/v1/tools/uptime` | GET | uptime, boot_time, load-historie, `last -x reboot`-lijst, idle-tijd uit `/proc/uptime` |
| `/v1/tools/locale` | GET | Locale, taal, timezone (+UTC-offset), NTP-sync-status, keyboard, currency |
| `/v1/tools/updates` | GET | apt: aantal upgradable, waarvan security, lijst pakketten, `reboot-required`, unattended-upgrades status |
| `/v1/tools/logs/sources` | GET | Welke journal-units en logbestanden beschikbaar zijn (uit whitelist, gefilterd op wat bestaat) |
| `/v1/tools/logs` | GET | `?source=unit:ssh&lines=200&since=1h&priority=warning&q=failed` |
| `/v1/tools/logs/stream` | GET (SSE) | Live tail (`journalctl -f` / `tail -F`), max 1 tegelijk |
| `/v1/tools/processes` | GET | Volledige proceslijst, sorteerbaar op CPU/RAM. Alleen als tool-scherm, niet in de 1 Hz-stream |

### `GET /v1/tools/updates`
```json
{ "upgradable": 14, "security": 3, "reboot_required": true,
  "reboot_required_pkgs": ["linux-image-6.8.0-45-generic"],
  "unattended_upgrades": { "installed": true, "enabled": true },
  "last_apt_update": 1755690000,
  "packages":[{ "name":"openssl", "current":"3.0.13-0ubuntu3.1",
                "candidate":"3.0.13-0ubuntu3.4", "security":true }] }
```

### `GET /v1/tools/logs`
```json
{ "source":"unit:ssh", "lines":[
    { "t":1755702701.2, "priority":6, "level":"info", "unit":"ssh",
      "pid":1284, "message":"Accepted publickey for merlin from 192.168.1.10 port 51234" } ],
  "truncated": false }
```
Priority volgt syslog (0=emerg … 7=debug) zodat de app kan kleuren.

---

## 3.7 Jobs (langlopend)

```
POST /v1/jobs
{ "type": "speedtest" }
{ "type": "ping",       "target": "1.1.1.1", "count": 10, "timeout_ms": 1000 }
{ "type": "dns",        "target": "example.com", "record": "A", "server": "8.8.8.8" }
{ "type": "whois",      "target": "example.com" }
{ "type": "traceroute", "target": "1.1.1.1", "max_hops": 20 }
→ 202 { "job_id": "j_7f3a2c", "type": "speedtest", "state": "queued" }
```

```
GET /v1/jobs/j_7f3a2c
{ "job_id":"j_7f3a2c", "type":"speedtest", "state":"running",
  "progress":0.42, "phase":"download", "started_at":1755702767.4 }
```

Eindresultaat speedtest:
```json
{ "state":"done", "result": {
    "download_bps": 943000000, "upload_bps": 512000000,
    "ping_ms": 4.2, "jitter_ms": 0.8, "packet_loss": 0.0,
    "server": { "name":"Amsterdam", "sponsor":"KPN", "id":"1234", "distance_km":12.4 },
    "isp":"Ziggo", "external_ip_masked":"84.28.x.x",
    "result_url":"https://www.speedtest.net/result/c/…" } }
```

Eindresultaat ping:
```json
{ "state":"done", "result": {
    "target":"1.1.1.1", "resolved_ip":"1.1.1.1",
    "sent":10, "received":10, "loss_percent":0.0,
    "min_ms":3.8, "avg_ms":4.2, "max_ms":5.9, "mdev_ms":0.6,
    "rtts_ms":[4.1,4.0,3.8,…] } }
```
De `rtts_ms`-array laat de app een mooie sparkline per ping tekenen in plaats van
alleen een tekstblok — dat is precies het verschil met een terminal.

`GET /v1/jobs/{id}/stream` (SSE) geeft per regel output mee tijdens het draaien, zodat
traceroute hop-voor-hop in beeld verschijnt.

---

## 3.8 Rate limits en fair use

| Groep | Limiet |
|-------|--------|
| `/v1/metrics`, `/v1/system` | 10 req/s per token |
| `/v1/stream` | max 4 gelijktijdige streams per token |
| `/v1/hardware/smart`, `/v1/tools/updates` | 1 per 30 s (daarna uit cache) |
| `POST /v1/jobs` | max 2 gelijktijdig, 20 per 5 min |
| `POST /v1/jobs` type `speedtest` | 1 per 5 min (verbruikt 1–3 GB) |

Bij overschrijding: `429` met `Retry-After`.
