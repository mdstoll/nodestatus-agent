# Building, deploying and testing

Verified end to end against a real machine (Debian 13 trixie, Intel N100) with the iOS app.
This repo covers the agent only — the iOS client lives in a private companion repo, see
[README.md](README.md).

## 1. The agent

### Build

```bash
cd agent && make release
```

Produces `agent/dist/`:

| File | Size |
|---|---|
| `nodestatus-agent_linux_amd64.tar.gz` | ~3.0 MB |
| `nodestatus-agent_linux_arm64.tar.gz` | ~2.7 MB |
| `SHA256SUMS` | — |

Needs Go 1.24+ (`brew install go`) and nothing else.

### Install on a server

The short way, straight from GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/mdstoll/nodestatus-agent/main/install.sh | sudo bash
```

That installs the optional extras (SMART, WHOIS, DNS, QR, sensors, GPU) by default; add
`-s -- --no-modules` to skip them. Or from a tarball you built yourself:

```bash
scp agent/dist/nodestatus-agent_linux_amd64.tar.gz root@<server>:/tmp/
```

```bash
ssh root@<server> 'cd /tmp && mkdir -p ns && tar xzf nodestatus-agent_linux_amd64.tar.gz -C ns && cd ns && ./install.sh'
```

Connection profiles:

```bash
./install.sh --mode lan                     # own subnet only (default)
./install.sh --mode vpn --vpn-ip 100.64.0.5
./install.sh --mode public                  # publicly reachable, mTLS protects it
./install.sh --mode proxy                   # behind nginx on loopback
```

### Pair

```bash
sudo nodestatus-agent enroll --new
```

Scan the QR code in the app, or use **Enter manually** with the host, port, pairing code and
CA fingerprint.

### Manage

```bash
nodestatus-agent                            # help
nodestatus-agent --version
sudo nodestatus-agent devices list
sudo nodestatus-agent devices revoke d_a41f
systemctl status nodestatus-agent
journalctl -u nodestatus-agent -f
```

### Remove

```bash
sudo /usr/local/bin/uninstall.sh --purge --remove-extras
```

## 2. Testing

### API check from the outside

`agent/cmd/validate` does exactly what the app does — generate a key, pair, then walk every
endpoint over mTLS — including the negative tests.

```bash
sudo nodestatus-agent enroll --new    # on the server, note the code
```

```bash
cd agent && SI_HOST=<host>:29500 SI_CODE=XXXXXXXX go run ./cmd/validate
```

Expected output:

```
✔ enrollment  device=d_5c634d67 host=DebianG3
✔ without a client certificate: TLS handshake refused
✔ /v1/health … /v1/devices        all 200
✔ wrong token → 401
✔ /etc/shadow through the log tool → 403
✔ SSE stream: live samples at 1 Hz
```

Dump a single endpoint:

```bash
SI_HOST=<host>:29500 SI_CODE=XXXXXXXX SI_DUMP=/v1/hardware/gpu go run ./cmd/validate
```

## 3. Measured on the test machine

| | Measured |
|---|---|
| Binary | 7.3 MB (amd64), 6.8 MB (arm64) |
| RSS | 20–22 MB |
| CPU, one client at 1 Hz | not measurable above noise |
| CPU idle | zero, five minutes after the last client |
| Startup | < 100 ms |
| `systemd-analyze security` | 5.3 MEDIUM (see [docs/07-security.md](docs/07-security.md) §7.5) |
| Disk writes | none, apart from one certificate renewal per year |
