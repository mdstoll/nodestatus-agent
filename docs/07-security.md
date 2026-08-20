# 07 — Security

A monitoring agent is an attractive target: it runs on all your servers, knows every system
detail, and can execute commands. So this is part of the design, not an appendix.

## 7.1 Threat model

| Attacker | Can | Mitigation |
|---|---|---|
| Someone on the same LAN | Scan port 29500, send requests | **mTLS**: without a paired client certificate the handshake fails — no endpoint responds at all |
| Passive eavesdropper | Read traffic | TLS 1.3 |
| Active MITM | Impersonate the server | The server certificate is validated against the CA the app received when pairing |
| Internet scanner | Find the port | Bind to VPN or loopback; `allow_cidr`; fail2ban when public |
| Another app on the phone | Read credentials | iOS sandbox plus Keychain, `whenUnlockedThisDeviceOnly` |
| A lost phone | Everything the app can | Optional Face ID lock, and **revoke the device remotely** |
| Local non-root user on the server | Read the token | Config `chmod 600`, owned by the agent user |
| An attacker abusing the agent | Inject commands, read arbitrary files | Whitelists and argv arrays, §7.4 |

### What "read-only" means here

Read-only refers to **system state**, not to "never starts a process". A speed test, ping
and traceroute are processes by definition, and they are still read-only in the sense that
matters:

| | Changes the system? | Survives a reboot? |
|---|---|---|
| Reading `/proc`, `/sys` | no | n/a |
| `smartctl -A`, `intel_gpu_top`, `journalctl` | no | no |
| `apt-get -s dist-upgrade` (simulation) | no | no |
| `speedtest`, `ping`, `dig`, `whois`, `traceroute` | no | no |
| ~~restart a service, `apt upgrade`, reboot~~ | **yes** | **yes** — not in v1 |

No endpoint writes to the filesystem, installs anything, changes configuration, starts or
stops services, or leaves anything behind. An attacker holding a paired certificate can
**read and diagnose**, not **change or persist**. That is the difference that bounds the
impact of an incident.

The price is a larger attack surface than pure file reads, which is why §7.4 exists. And
one specific risk: a speed test burns 1–3 GB. Repeated calls could eat a data cap, so only
one runs at a time.

## 7.2 TLS

Every connection is **mutual TLS 1.3** with certificates from that server's own CA.

- ECDSA P-256, SHA-256. CA and server certificate 10 years; client certificates 1 year with
  automatic renewal.
- The server certificate is **397 days**, not longer. iOS flatly refuses anything above 398
  days for a TLS *server* certificate, even from a CA the device explicitly trusts:
  `Certificate exceeds maximum temporal validity period`. That rule does not apply to the CA
  itself, which is what matters — a paired device stays paired.
- Because of that limit the agent must be able to replace its own certificate, so
  `ReadWritePaths` includes `/etc/nodestatus-agent` and a `CertManager` serves the current
  certificate through `tls.Config.GetCertificate`. It checks every 12 hours and in practice
  writes once a year.
- `ClientAuth` is **dynamic**: `RequireAndVerifyClientCert` at rest,
  `VerifyClientCertIfGiven` only during a pairing window and for 15 seconds after a
  successful pairing (so a racing parallel connection does not turn a successful pairing
  into an error).
- The app validates the server against the CA it received at pairing
  (`SecTrustSetAnchorCertificates`), not against the system trust store and not via
  trust-on-first-use. Stricter than pinning and maintenance-free, since a renewed server
  certificate from the same CA stays valid.

## 7.3 Tokens

32 bytes from `crypto/rand`, base64url, unique per paired device, stored only as a SHA-256
hash on the server. Compared in constant time. A wrong token gets a fixed 250 ms delay and
a rate-limited `401` with no detail.

## 7.4 Running commands

Every place the agent starts an external program follows four rules:

1. **Never a shell.** `exec.CommandContext(ctx, "/usr/bin/ping", "-c", "10", target)` — an
   argv array. Shell injection is structurally impossible, not "hopefully escaped".
2. **Validate every argument.** Hostnames against an RFC-1123 regex or `netip.ParseAddr`;
   counts clamped to a range; enums looked up in a fixed map. Loopback and link-local
   addresses are refused, which also blocks the cloud metadata endpoint.
3. **Absolute paths**, resolved at startup. The child's `PATH` is emptied so it cannot look
   anything up.
4. **Always a context with a timeout** and a 1 MB output cap.

For log files the path must appear **verbatim** in the whitelist. No prefix matching, no
`filepath.Join` with user input — otherwise `../../etc/shadow` is a matter of time.

One deliberate exception to the empty environment: `HOME` is set to the state directory.
The Ookla speedtest CLI reads `getenv("HOME")` without a null check and crashes with
`std::logic_error: basic_string::_M_construct null not valid` when it is missing.

## 7.5 Privileges

The agent runs as the system user `nodestatus` (nologin, no home), in the groups
`systemd-journal` (to read the journal without root) and `disk`. Two sudoers rules exist,
each pinning one command with fixed arguments:

```
nodestatus ALL=(root) NOPASSWD: /usr/sbin/smartctl -j -A -H -i /dev/nvme[0-9]n[0-9], …
nodestatus ALL=(root) NOPASSWD: /usr/bin/intel_gpu_top -J -s 600
```

Both exist because the alternative was granting the agent `CAP_SYS_ADMIN` and
`CAP_PERFMON` permanently, for the whole process. Two pinned commands is far narrower, even
though it forces `NoNewPrivileges=no` and costs points in `systemd-analyze security`
(5.3 MEDIUM). That trade-off is deliberate, not an oversight.

## 7.6 Exposure

Because mTLS applies to every profile, "what if someone finds the port" is largely
answered: without a paired certificate nothing gets through and there is nothing to
harvest. What remains is reachability and noise.

| Profile | Exposure | Additional requirements |
|---|---|---|
| `vpn` | none | none |
| `lan` | own subnet | `allow_cidr` plus a firewall rule scoped to that subnet |
| `public` | port 29500 on the internet | fail2ban, stricter auth limits |
| `proxy` | via nginx | `ssl_verify_client on` in a **separate** server block |

For the public profile: `allow_cidr` does not help, because a phone on mobile data has a
rotating address. The client certificate is the boundary, and it is a stronger one than an
IP filter. The agent logs rejected handshakes in a fixed format
(`auth failure from <ip>`) so a four-line fail2ban filter can act on it. The port number is
noise reduction, not security — assume it will be found, and that this is fine.

## 7.7 Information disclosure

- `/v1/health` is **not** public. It used to leak version and uptime to anyone; now it is
  behind the certificate, with one exception for `127.0.0.1` so the installer can self-test.
- No `Server:` header, no software name in error pages, no version outside the
  authenticated API.
- Uniform empty bodies on 401/403 — no way to tell "unknown device" from "wrong token".
- No mDNS/Bonjour advertising. Tempting for auto-discovery, but that is literally having
  your server announce itself.

## 7.8 On the phone

Key and certificate live in the Keychain with `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`
— no iCloud sync of server credentials, unreadable while the device is locked, and
unreachable to other apps through the iOS sandbox. Serial numbers and public IPs are masked
by default, so a screenshot does not give everything away. No analytics, no third-party
crash reporting, no traffic to anything except your own servers.

**Verify during hardening:** a key in the Secure Enclave would be stronger still, but
building a `SecIdentity` from a Secure Enclave key has historically been unreliable for
`URLSession` client authentication. The app uses a Keychain-resident key, which works; the
Secure Enclave is a candidate for a later hardening pass and is deliberately not promised
before it has been tested.

## 7.9 Deliberately not in v1

| Not included | Why | When |
|---|---|---|
| Write actions (restart, upgrade, reboot) | Turns an information leak into full control | Behind a separate `allow_actions` flag with per-action whitelist and Face ID |
| Multiple users or roles | One user, one token per device | Team use |
| Audit log | Overkill for a single user | Team use |
