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

That installs the optional extras too — SMART, WHOIS, DNS, QR codes, sensors and GPU
metrics. Skip them with `--no-modules`:

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash -s -- --no-modules
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

### Update the agent

```bash
sudo nodestatus-agent update
```

Checks the latest GitHub release, downloads the matching architecture's tarball, verifies
it against `SHA256SUMS`, swaps the binary in place and restarts the service. A no-op if
you're already current.

Re-running the installer works just as well and comes to the same result — it's
idempotent, so your config, pairing and certificates are left alone:

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash
```

Remove it again — completely:

```bash
sudo /usr/local/bin/uninstall.sh --purge --remove-extras
```

---

## Pair it with the app

The agent never pushes itself onto a device — pairing always starts from the server.

1. Run `sudo nodestatus-agent enroll --new` (or just install — it opens a pairing window on
   its own). You get a one-time code and a QR code, valid for 15 minutes.
2. In the app, tap **Pair a server** and either scan the QR code or enter the host, port,
   pairing code and CA fingerprint by hand.
3. The app generates its own key pair on-device and sends only the public half; the agent
   signs it and hands back a client certificate. The private key never leaves the phone.

From then on that device authenticates with mutual TLS on every connection — no password,
nothing to type again. Pair a second phone the same way; revoke either one any time with
`sudo nodestatus-agent devices revoke <id>`, which cuts it off immediately, including a
connection that is already open. Full flow in [docs/07-security.md](docs/07-security.md).

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
and [pairing / TLS](docs/07-security.md).

---

## Build it yourself

```bash
cd agent && make release          # tarballs for amd64, 386, arm and arm64
```

Requires Go 1.24+. See [INSTALL.md](INSTALL.md) for the full build, deploy and test
workflow.

The iOS client ("Node Status") is closed-source and lives in a private companion repo —
this repository is the agent only.

---

## Security in one paragraph

Every server is its own certificate authority. At install time it generates a CA and a
server certificate; when you pair, the app generates a P-256 key pair in the Keychain,
sends only the public half, and receives a client certificate signed by that CA. From then
on the agent requires that certificate for every connection, checks its fingerprint against
an allowlist, and requires a bearer token on top. Revoking a device takes effect
immediately, including on connections that are already open. Details in
[docs/07-security.md](docs/07-security.md).
