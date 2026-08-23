# Node Status

Real-time monitoring of Linux servers from your iPhone.

Two parts: a **worker daemon** on the Linux machine and an **iOS app** as the client.
Only devices you have explicitly paired get through the TLS handshake — an unpaired
client cannot complete a connection, let alone read anything.

```
iPhone (SwiftUI)  ──mutual TLS 1.3──▶  nodestatus-agent  ──▶  /proc, /sys, hwmon,
   1 Hz SSE stream                      (Go, systemd)          smartctl, intel_gpu_top,
                                        :29500                 journalctl, apt, speedtest
```

---

## Install the agent

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash
```

Add `--with-extras` for SMART, WHOIS, DNS, QR codes and GPU metrics:

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash -s -- --with-extras
```

The installer prints a pairing code and a QR code. Scan it in the app and you are done.
Debian and Ubuntu on amd64 or arm64; systemd required, nothing else.

Manage it with:

```bash
nodestatus-agent            # help
nodestatus-agent --version
sudo nodestatus-agent enroll --new
sudo nodestatus-agent devices list
sudo nodestatus-agent devices revoke <id>
systemctl status nodestatus-agent
```

Remove it again — completely:

```bash
sudo /usr/local/bin/uninstall.sh --purge --remove-extras
```

---

## What it shows

**Metrics** — CPU, RAM, storage and load as live gauges; a network chart with upload and
download; temperature with history; every sensor the machine exposes.

**Tools** — speed test with live throughput, ping, DNS, traceroute, WHOIS, per-core CPU
detail, SMART health, network interfaces, a log analyzer over journald and plain log files,
apt updates, locale, uptime, and a process list that flags zombie processes.

**Settings** — units, history window, privacy, paired devices, and a language switch
(English by default; Dutch appears when your device language is Dutch).

---

## Design

| Decision | Choice | Why |
|---|---|---|
| Language | Go, no external dependencies | One static binary, ~7 MB, ~20 MB RSS, no runtime to install |
| Port | 29500/tcp | Unassigned at IANA, below Linux' ephemeral range, above 1024 |
| Transport | Mutual TLS 1.3 with a per-server CA | Unpaired clients fail the handshake; nothing is disclosed |
| Live data | Server-Sent Events at 1 Hz | One connection, built-in reconnect, kind to the battery |
| Pairing | One-time code, valid 15 minutes | The private key never leaves the phone |
| Scope | Read-only on system state | A stolen certificate leaks information, never control |

Full analysis in [`docs/`](docs/). Start with [the architecture](docs/01-architecture.md)
and [device enrollment](docs/10-device-enrollment.md).

---

## Build it yourself

```bash
cd agent && make release          # tarballs for amd64 and arm64
```

```bash
cd ios && xcodegen generate && open NodeStatus.xcodeproj
```

Requires Go 1.24+, Xcode 26 and [xcodegen](https://github.com/yonaskolb/XcodeGen).
See [INSTALL.md](INSTALL.md) for the full build, deploy and test workflow.

---

## Security in one paragraph

Every server is its own certificate authority. At install time it generates a CA and a
server certificate; when you pair, the app generates a P-256 key pair in the Keychain,
sends only the public half, and receives a client certificate signed by that CA. From then
on the agent requires that certificate for every connection, checks its fingerprint against
an allowlist, and requires a bearer token on top. Revoking a device takes effect
immediately, including on connections that are already open. Details in
[docs/07-security.md](docs/07-security.md).
