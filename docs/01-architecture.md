# 01 — Architecture

## 1.1 Components

```
┌───────────────────────────────────────────┐        ┌──────────────────────────────────┐
│ iOS app "Node Status"                     │        │ Linux machine                    │
│                                           │        │                                  │
│  ServerStore (JSON + Keychain)            │        │  nodestatus-agent (systemd)      │
│   ├─ Server A  ← selected                 │        │   ├─ HTTPS :29500, mutual TLS    │
│   └─ Server B                             │        │   ├─ sampler loop, 1 Hz          │
│                                           │        │   │    └─ ring buffer, 300 samples│
│  APIClient  ──── GET /v1/… ───────────────┼───────▶│   ├─ static cache (60 s)         │
│  StreamClient ── GET /v1/stream (SSE) ◀───┼────────┤   ├─ job runner (speedtest, ping)│
│                                           │        │   └─ control socket for the CLI  │
│  MetricsStore (@Observable)               │        │                                  │
│   └─ ring buffers for the charts          │        │  reads /proc /sys /run           │
└───────────────────────────────────────────┘        │  execs smartctl, intel_gpu_top, …│
                                                      └──────────────────────────────────┘
```

## 1.2 Three data speeds

The single most important design decision, because it determines whether the app can run
at 1 Hz without loading the server.

| Class | Examples | Refresh | Endpoint |
|-------|----------|---------|----------|
| **Static** | Model, chip, kernel, distro, total RAM, disk layout, boot time | once, cached 60 s | `GET /v1/system` |
| **Hot** | CPU%, per core, RAM, swap, load, network throughput, temperatures | every second, pushed | `GET /v1/stream` (SSE) |
| **Cold** | SMART, apt updates, journal, speedtest, whois, ping, processes | on request only | `GET/POST /v1/tools/*` |

Without that split the agent would run `smartctl` and `apt-get -s` every second, which
costs seconds of CPU and hammers the disk. The 1 Hz loop now reads a handful of files from
`/proc` and `/sys`: under a millisecond per sample.

## 1.3 The 1 Hz loop

One goroutine, running only while at least one SSE client is attached:

1. Read `/proc/stat`, `/proc/meminfo`, `/proc/net/dev`, `/proc/diskstats`, `/proc/loadavg`, hwmon.
2. Compute deltas against the previous sample — CPU percentages and network speeds *are* deltas.
3. Append to a ring buffer of 300 samples (5 minutes).
4. Broadcast the sample as one SSE event.

The loop keeps running for **five minutes after the last client leaves**. Stopping it
immediately sounds tidier, but then the ring buffer is empty on every reconnect and
`?backfill=60` returns nothing — the chart would spend its first minute filling up. Five
minutes of idle sampling costs almost nothing; a server nobody watches still falls back
to zero.

The GPU is refreshed on its own schedule (15 s) in a background goroutine, because
`intel_gpu_top` needs several seconds per measurement and must never hold up the loop.

## 1.4 Why SSE

| | 1 Hz polling | WebSocket | **SSE** |
|---|---|---|---|
| Connections | one request per second | one persistent | one persistent |
| Battery | poor | good | good |
| Auto-reconnect | build it yourself | build it yourself | in the protocol |
| Server complexity | low | medium (framing, ping/pong) | **low — it is just HTTP** |
| Bidirectional | n/a | yes | no, and not needed |

Commands (ping, speedtest) are separate POSTs, so a bidirectional channel buys nothing.
Polling stays in the app as a fallback for the case where a reverse proxy breaks SSE.

## 1.5 Reaching the server

Four connection profiles, chosen with `install.sh --mode`. All four use the same access
control: mutual TLS with the per-server CA from [10 — Device enrollment](10-device-enrollment.md).
The profiles differ only in *where* the agent listens.

### `lan` (default)

```toml
bind       = "0.0.0.0:29500"
allow_cidr = ["192.168.1.0/24"]   # the installer fills in your own subnet
```

Nothing open to the internet. The firewall rule is bound to your subnet, not the world.

### `vpn`

```toml
bind       = "100.64.0.5:29500"   # WireGuard or Tailscale address
allow_cidr = ["100.64.0.0/10"]
```

For machines behind a firewall you cannot open. The safest option: the port does not exist
on the LAN or WAN interface at all.

### `public`

The agent listens on a publicly reachable port. Safe because of mutual TLS: without a
paired client certificate the handshake fails and there is nothing to harvest. No public
certificate needed — the app validates against the CA it received when pairing, so there
is no ACME, no renewals and no port 80.

### `proxy`

Only when you need everything on 443 (restrictive guest networks). The agent binds
loopback and nginx terminates TLS *and* verifies the client certificate — in a **separate
server block**, never your existing vhost, or every browser visitor gets a certificate
picker. Config in [10 §10.7](10-device-enrollment.md).

### In the app

A server may hold two addresses (`lan_host` and `remote_host`), tried in parallel with a
300 ms head start for the LAN one. Use **hostnames, not IP literals** for remote hosts: on
the IPv6-only mobile networks Apple requires apps to support, a hostname resolves through
NAT64/DNS64 and a hardcoded IPv4 address does not.

## 1.6 Alternatives that were rejected

| Alternative | Why not |
|---|---|
| Prometheus node_exporter + Grafana | Heavy, text format to parse on a phone, no tools, no native UI |
| Netdata | Fine product, but ~150 MB RAM and its own web UI — the opposite of lightweight |
| SSH from the app | An SSH key on your phone, fragile parsing of `top`/`df`, and you hand the app a full shell |
| Agent pushes to a cloud | Needs a server you host and pay for, and puts all your server data with a third party |
| SNMP | Standard, but limited metrics, no SMART/GPU/logs |

## 1.7 Measured footprint

| Measurement | Target | Measured on the test machine |
|---|---|---|
| Binary | small | 7.3 MB (amd64), 6.8 MB (arm64) |
| RSS | < 25 MB | 20–22 MB |
| CPU, one client at 1 Hz | < 1% of a core | not measurable above noise |
| CPU idle | zero | zero, five minutes after the last client |
| Disk writes | none | none, except one certificate renewal per year |
| Startup | < 100 ms | < 100 ms |
