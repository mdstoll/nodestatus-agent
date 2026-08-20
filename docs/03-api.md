# 03 — API contract

Base URL: `https://<host>:29500`, or `https://<host>/<prefix>` behind a reverse proxy.
Times are Unix epoch seconds (float). Byte values are **bytes** — formatting is the app's
job, not the server's. Arrays are always arrays, never `null`.

Errors are always:

```json
{ "error": { "code": "unauthorized|not_found|unavailable|invalid_argument|timeout|internal",
             "message": "human readable" } }
```

## 3.0 Authentication

**Layer 1 — client certificate (mTLS).** Every connection needs a client certificate
issued by that server's CA whose fingerprint is on the allowlist. Without one the TLS
handshake fails and no HTTP request is ever seen. The only exception is `POST /v1/enroll`
during an open pairing window.

**Layer 2 — bearer token**, in either header:

```
Authorization: Bearer <token>
X-Node-Status-Token: <token>
```

The second exists because a reverse proxy may already claim `Authorization` (HTTP Basic
Auth, for instance). Both layers apply to **all** endpoints including `/v1/health`; the
only exception is a request from `127.0.0.1`, so the installer can run its self-test.
Failures return an empty `401`/`403` after a fixed 250 ms delay, with no distinction
between "unknown device" and "wrong token".

### Pairing

```
POST /v1/enroll
{ "code": "K7QM3XR9", "public_key_b64": "<X9.63 P-256 point>", "device_name": "iPhone" }
→ 200 { "device_id": "d_a41f", "client_cert_pem": "…", "ca_cert_pem": "…",
        "api_token": "…", "expires_at": 1818800761,
        "hostname": "web-01", "display_name": "web-01" }
```

| Endpoint | Purpose |
|---|---|
| `GET /v1/devices` | List paired devices |
| `POST /v1/devices/me/renew` | Renew this device's certificate (from 30 days before expiry) |
| `DELETE /v1/devices/{id}` | Revoke a device, effective immediately |

## 3.1 `GET /v1/health`

```json
{ "ok": true, "version": "0.1.0", "uptime_s": 12843, "devices": 2 }
```

## 3.2 `GET /v1/system` — static, 60 s cache

```json
{
  "hostname": "DebianG3",
  "display_name": "DebianG3",
  "os": { "name": "Debian GNU/Linux", "version": "13 (trixie)", "id": "debian",
          "kernel": "6.12.101+deb13-amd64", "arch": "amd64", "virtualization": null },
  "model": { "vendor": "GMKtec", "product": "NucBox G3", "board": "…" },
  "cpu": { "model": "Intel(R) N100", "vendor": "GenuineIntel",
           "cores_physical": 4, "threads": 4, "sockets": 1, "max_mhz": 3400,
           "cache_l3_bytes": 6291456, "flags_notable": ["avx","avx2","aes"],
           "governor": "powersave" },
  "memory_total": 33395376128,
  "swap_total": 16697688064,
  "storage_total_bytes": 1999353413632,
  "boot_time": 1787241028,
  "uptime_s": 25400.2,
  "capabilities": ["metrics","stream","sensors","processes","smart","speedtest.ookla",
                   "whois","dns","ping","traceroute","disks","journal","apt"],
  "agent_version": "0.1.0"
}
```

## 3.3 `GET /v1/metrics` and `GET /v1/stream`

Both carry the same sample. `/v1/metrics` returns one; `/v1/stream` pushes one per second
as Server-Sent Events.

```json
{
  "t": 1787264751.3,
  "cpu": { "total": 12.4, "user": 9.1, "system": 3.0, "iowait": 0.3, "steal": 0,
           "cores": [14.2, 11.0, 12.8, 11.6], "freq_mhz": [1300, 700, 700, 1200],
           "load": [0.61, 0.47, 0.33], "procs_running": 1, "procs_total": 8720 },
  "memory": { "total": 33395376128, "used": 4600000000, "available": 28795376128,
              "cached": 10307921920, "buffers": 3461120,
              "swap_total": 16697688064, "swap_used": 0, "percent": 13.8 },
  "storage": [
    { "mount": "/", "device": "/dev/mapper/DebianG3--vg-root", "fstype": "btrfs",
      "total": 1999353413632, "used": 660981133312, "percent": 33.1,
      "remote": false, "read_bps": 0, "write_bps": 122880 },
    { "mount": "/mnt/dv2ssd", "device": "//192.168.1.111/…", "fstype": "cifs",
      "total": 3155254775808, "used": 689761607680, "percent": 21.9, "remote": true }
  ],
  "network": {
    "rx_bps": 9420, "tx_bps": 5230, "rx_total": 3758096384, "tx_total": 1932735283,
    "interfaces": [
      { "name": "eth0", "up": true, "speed_mbps": 2500, "virtual": false,
        "rx_bps": 9420, "tx_bps": 5230, "rx_total": 3758096384, "tx_total": 1932735283 },
      { "name": "docker0", "up": true, "virtual": true, "rx_bps": 0, "tx_bps": 0 }
    ]
  },
  "temps": [ { "key": "coretemp/temp1", "label": "Package id 0", "chip": "coretemp",
               "celsius": 57, "high": 105, "critical": 105, "status": "ok", "primary": true } ],
  "gpu": [ { "index": 0, "vendor": "Intel", "name": "Intel UHD Graphics (Alder Lake-N)",
             "util_percent": 35.1, "shared_memory": true, "power_w": 2.25,
             "clock_mhz": 700, "clock_max_mhz": 750,
             "engines": [ { "name": "Render/3D", "busy": 35.1 },
                          { "name": "Video", "busy": 32.3 } ] } ]
}
```

Two things the client must handle:

- **`rx_total` / `tx_total`** are counters since boot. They wrap and reset on reboot; a
  lower value than the previous one means a reset, not negative traffic.
- **`remote: true`** marks network mounts (cifs, nfs, fuse.*). They are shown but excluded
  from the storage total — on the test machine that is 45 TB of mounted cloud drives that
  would otherwise be reported as local disk.
- **`virtual: true`** marks docker bridges, veth pairs and tunnels. Their traffic also
  crosses the physical interface, so counting them would double the totals.

### Stream parameters

```
GET /v1/stream?backfill=60&sections=cpu,memory
```

`backfill` replays up to 300 buffered samples as `event: backfill` before going live, so
the chart is populated immediately. Keep-alive comments every 15 s stop proxies from
closing the connection.

## 3.4 Hardware

| Endpoint | Returns |
|---|---|
| `GET /v1/hardware/sensors` | Every hwmon chip: temperatures, fans, voltages, power, with thresholds and status |
| `GET /v1/hardware/smart` | Per disk: model, capacity, health, temperature, power-on hours, wear |
| `GET /v1/hardware/gpu` | NVIDIA (nvidia-smi), AMD (sysfs) or Intel (sysfs + intel_gpu_top) |
| `GET /v1/hardware/disks` | Block devices and partitions from `lsblk` |
| `GET /v1/hardware/network` | NICs with MAC, MTU, speed, addresses, plus gateway and DNS |

## 3.5 Tools

| Endpoint | Returns |
|---|---|
| `GET /v1/tools/cpuinfo` | Static CPU detail, current load, 120 samples of history |
| `GET /v1/tools/uptime` | Uptime, boot time, CPU time split, recent boots |
| `GET /v1/tools/locale` | Locale, language, timezone, NTP sync, keyboard, calendar |
| `GET /v1/tools/updates` | apt: upgradable count, security count, package list, reboot-required |
| `GET /v1/tools/processes` | Summary (total/running/sleeping/zombie), top processes, zombie parents |
| `GET /v1/tools/logs/sources` | Available journal views, systemd units and log files |
| `GET /v1/tools/logs` | `?source=journal:kernel&lines=200&since=1h&priority=warning&q=…` |

Log sources come in three kinds: `journal:all`, `journal:kernel`, `journal:errors`,
`journal:boot`; `unit:<name>` for systemd units; and `file:<path>` for plain files. Units
and files must appear **verbatim** in the whitelist — no prefix matching, no path joining
with user input.

The agent never runs `apt-get update`: that changes system state and needs a lock. It reads
the existing cache and reports how old it is.

## 3.6 Jobs

Long-running work returns a job id and is polled.

```
POST /v1/jobs
{ "type": "speedtest" }
{ "type": "ping",       "target": "1.1.1.1", "count": 10 }
{ "type": "dns",        "target": "example.com", "record": "A", "server": "8.8.8.8" }
{ "type": "whois",      "target": "example.com" }
{ "type": "traceroute", "target": "1.1.1.1", "max_hops": 20 }
→ 202 { "job_id": "j_7f3a2c", "type": "speedtest", "state": "queued" }
```

```
GET /v1/jobs/j_7f3a2c
{ "state": "running", "phase": "download", "progress": 0.42,
  "live_bps": 682100000, "ping_ms": 15.4,
  "samples": [ { "t": …, "bps": 640000000, "phase": "download" }, … ] }
```

`live_bps` and `samples` exist so the app can show throughput as it happens rather than a
spinner. The agent reads the Ookla CLI's `jsonl` stream with
`--progress-update-interval=200` and updates the job five times a second.

Only one speedtest runs at a time — two would share the line and produce nonsense — but
there is no artificial cooldown beyond that.

## 3.7 Rate limits

| Group | Limit |
|---|---|
| Normal endpoints | 10 req/s per client, burst 30 |
| `/v1/stream` | 4 concurrent per token |
| `smart`, `updates` | cached 30–60 s |
| `POST /v1/jobs` | 2 concurrent, one speedtest at a time |

Exceeding them returns `429` with `Retry-After`.
