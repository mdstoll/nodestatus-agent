# 02 — The agent

## 2.1 Why Go

| Option | RSS | Deps on target | Verdict |
|---|---|---|---|
| **Go** | ~20 MB | **none** (static, CGO off) | ✅ chosen |
| Python 3 (stdlib only) | ~25 MB | python3 | Workable, but slower and tied to the system Python |
| Rust | ~5 MB | none | Marginally better, considerably more build complexity |
| Node.js | ~45 MB | node runtime | Too heavy, an extra dependency on every server |

Go is the only option that is lightweight *and* dependency-free *and* easy to build. One
`GOOS=linux go build` on a Mac produces a file that runs on Debian 11, Ubuntu 24.04 and a
Raspberry Pi without ever installing a compiler there.

There are **no external Go modules**. HTTP, TLS, JSON and crypto all come from the standard
library.

## 2.2 Layout

```
cmd/nodestatus-agent/   daemon + CLI (run, enroll, devices, bootstrap, version)
cmd/validate/           test client that mimics the app, including negative tests
internal/collect/       sampler, /proc and /sys readers, GPU
internal/api/           router, mTLS auth, SSE, enrollment
internal/tools/         SMART, sensors, logs, apt, jobs (speedtest, ping, dns, whois)
internal/pki/           CA, server certificate, client certificates
internal/store/         devices.json, pairing window
internal/control/       unix socket so the CLI can talk to the running daemon
packaging/              install.sh, uninstall.sh, systemd unit
```

## 2.3 The CLI

Running the bare command prints help and **does not start the daemon**. That is a
deliberate safety measure: a second daemon started by accident takes over the control
socket from the running instance and then dies on the occupied port, after which
`enroll --new` reports "connection refused" and nothing works. The control socket is now
also opened *after* the TCP listener succeeds, so a failed start cannot clobber a healthy one.

```
nodestatus-agent                     help
nodestatus-agent --version
nodestatus-agent run --config <path> start in the foreground (systemd does this)
nodestatus-agent enroll --new        open a pairing window, print code + QR
nodestatus-agent enroll --cancel     close it
nodestatus-agent devices list
nodestatus-agent devices revoke <id>
nodestatus-agent bootstrap           create CA + server certificate (installer does this)
```

When the CLI cannot reach the daemon it says what to do rather than printing a raw dial
error — it distinguishes "not running" from "running but the socket was hijacked".

## 2.4 Configuration — `/etc/nodestatus-agent/config.toml`

```toml
bind          = "0.0.0.0:29500"
mode          = "lan"
display_name  = "web-01"
tls_cert      = "/etc/nodestatus-agent/cert.pem"   # empty = plain HTTP, loopback only
tls_key       = "/etc/nodestatus-agent/key.pem"
ca_dir        = "/etc/nodestatus-agent/ca"
state_dir     = "/var/lib/nodestatus-agent"
allow_cidr    = ["192.168.1.0/24"]
trusted_proxy = []          # only set when a proxy really is in front

sample_hz             = 1
history_size          = 300
enroll_window_minutes = 15
enroll_max_attempts   = 5
client_cert_days      = 365

[features]
smart = true; gpu = true; speedtest = true; apt = true; logs = true

[logs]
units = ["ssh", "nginx", "docker", "cron", "ufw", …]
files = ["/var/log/syslog", "/var/log/auth.log", …]
max_lines = 500
```

Everything is also settable through environment variables (`NODESTATUS_BIND`, …) so the
agent works in a container too.

## 2.5 systemd hardening

```ini
NoNewPrivileges=no            # see below
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service perf_event_open
ReadWritePaths=/var/lib/nodestatus-agent /etc/nodestatus-agent
SupplementaryGroups=systemd-journal disk
MemoryMax=128M
TasksMax=64
```

Three of these deserve an explanation, because they are all trade-offs rather than defaults:

**`NoNewPrivileges=no`.** NVMe SMART needs `CAP_SYS_ADMIN` through `NVME_IOCTL_ADMIN_CMD`.
The two ways to get it were granting the agent that capability outright — near-root for the
whole process, permanently — or letting it call exactly one pinned command through sudo.
The second is much narrower even though the audit flag looks worse.
`systemd-analyze security` reports 5.3 (MEDIUM) because of it.

**`perf_event_open` in the syscall filter.** `intel_gpu_top` needs it to read GPU
utilisation. Without it the child is killed with SIGSYS, which surfaces as the delightfully
unhelpful "bad system call". The agent gains nothing from the syscall without CAP_PERFMON,
which only arrives via the sudoers rule for that one command.

**`/etc` writable.** Only so the agent can renew its own server certificate before it
expires after 397 days (see [07 §7.2](07-security.md)). In practice that is one write per year.

## 2.6 Installing and removing

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash
```

The installer is idempotent and does: preflight (root, systemd, architecture, free port) →
system user → binary → config for the chosen profile → optional packages (on by default,
`--no-modules` skips them) → sudoers rules
for `smartctl` and `intel_gpu_top` → CA and server certificate → systemd unit → firewall
rule → self-test against `/v1/health` → pairing window with a QR code. If the agent does
not come up it prints the last 20 journal lines instead of a bare failure.

Removing it:

```bash
sudo /usr/local/bin/uninstall.sh                          # keep config and pairings
sudo /usr/local/bin/uninstall.sh --purge --remove-extras  # leave nothing behind
```

The uninstaller always prints what it did **not** remove, so nothing lingers silently.

## 2.7 Optional tools

The agent never fails because a tool is missing. At startup it detects what is present and
publishes that as `capabilities` in `/v1/system`; the app hides UI for missing
capabilities rather than offering dead buttons.

| Feature | Binary | Without it |
|---|---|---|
| SMART | `smartctl` | Disks show without health data |
| Temperatures | — | Always works, read straight from `/sys/class/hwmon` |
| Intel GPU load | `intel_gpu_top` | Falls back to an estimate from the clock frequency |
| NVIDIA GPU | `nvidia-smi` | GPU section hidden |
| Speed test | `speedtest` or `librespeed-cli` | Button disabled with an explanation |
| WHOIS / DNS / traceroute | `whois`, `dig`, `traceroute` | Those tools return 501 |
| QR code | `qrencode` | Pairing URL printed as text instead |

## 2.8 Releasing

```bash
make release   # dist/nodestatus-agent_linux_{amd64,arm64}.tar.gz + SHA256SUMS
```

Each tarball holds the binary, `install.sh`, `uninstall.sh` and the systemd unit. The same
`install.sh` also works standalone: piped from curl it detects that no binary sits next to
it, downloads the matching release asset and verifies it against `SHA256SUMS` before
unpacking anything.
