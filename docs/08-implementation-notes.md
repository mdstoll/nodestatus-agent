# 08 — Implementation notes

Where the built version departs from the original design, and what only became visible by
running it against a real machine.

## 8.1 Public key instead of a CSR

The design had the app produce a PKCS#10 CSR. Building one in Swift means hand-rolling
ASN.1/DER — roughly 150 lines of error-prone byte assembly for no security gain, since the
private key stays in the Keychain either way and the agent decides every certificate field
regardless. The app sends the raw X9.63 public point and the agent builds the certificate
around it.

## 8.2 Server certificate: 397 days

iOS refuses anything longer for a TLS server certificate, even from a trusted CA:

```
Certificate 0 "DebianG3" has errors: Certificate exceeds maximum temporal validity period
```

The CA stays at 10 years — which is what keeps paired devices paired — and the agent renews
its own server certificate 30 days before expiry.

## 8.3 `NoNewPrivileges=no`

NVMe SMART needs `CAP_SYS_ADMIN` via `NVME_IOCTL_ADMIN_CMD`; without escalation `smartctl`
returns `Read NVMe SMART/Health Information failed: NVME_IOCTL_ADMIN_CMD: Permission denied`.
Granting the agent that capability permanently is broader than letting it run one pinned
command through sudo, so the narrower option won even though it looks worse in an audit.

## 8.4 `perf_event_open` in the syscall filter

`intel_gpu_top` needs it, and it is not part of `@system-service`. Without it the child is
killed with SIGSYS — which surfaces as `signal: bad system call` and explains nothing.

## 8.5 SIGTERM, not SIGKILL

Go's `exec.CommandContext` kills with SIGKILL on timeout. `intel_gpu_top` runs under sudo
with `use_pty`, and sudo then never flushes the pty buffer: zero bytes came back, even
though the same command worked perfectly from a shell (where `timeout` sends SIGTERM).
`cmd.Cancel` now sends SIGTERM with a `WaitDelay` behind it.

## 8.6 The sampler lingers for five minutes

Stopping it the moment the last client leaves means the ring buffer is empty on every
reconnect, so `?backfill=60` returns nothing and the chart spends its first minute filling
up — exactly what backfill was meant to prevent.

## 8.7 GPU polling must not block, and must be rare

The GPU refresh originally ran inline in the 1 Hz loop with a 3-second interval and a
4-second measurement, so a sudo process was running essentially permanently: nearly two
minutes of CPU per hour. It now refreshes in the background at 15-second intervals and
never holds up a sample.

## 8.8 Network mounts and virtual interfaces

Not anticipated in the design, immediately visible on the test machine: nine rclone and
CIFS mounts totalling 45 TB of cloud storage, and eight veth pairs plus a docker bridge and
a VPN tunnel. Counting either would have made storage and network numbers meaningless. Both
are shown but excluded from totals.

## 8.9 Sentinel sensor thresholds

The NVMe drive reports `temp2_max = 65261850` millidegrees — 65,261 °C — meaning "no
threshold". Taken literally, a disk at 85.8 °C read as healthy. Thresholds outside a
plausible range are discarded now, and the warning threshold moved from 90% to 85% of
critical: at a critical of 100 °C, 85 °C is already worth a look, not 99 °C.

Related: the status rule existed in two places, so the same sensor showed orange on Metrics
and green in the sensors detail. It now lives in one function.

## 8.10 Two iOS-specific traps

**`SecIdentityCreateWithCertificate` does not exist on iOS** — it is macOS only. On iOS the
Keychain forms an identity itself once certificate and private key are both present; you
find it by enumerating identities and comparing certificate DER.

**The stream's TLS challenge arrives at task level.** `URLSession.data(for:)` uses
`urlSession(_:didReceive:)`, but `URLSession.bytes(for:)` uses
`urlSession(_:task:didReceive:)`. With only the first implemented every endpoint worked and
only the live stream was rejected — a confusing symptom.

## 8.11 Parallel connections during pairing

URLSession opens several connections and races them. The first completed the pairing, which
closed the window; the second then failed the handshake and the whole task reported "the
network connection was lost" — while the device had in fact been paired successfully. The
pairing session is now limited to one connection per host, and the agent keeps accepting
unauthenticated connections for 15 seconds after a successful pairing.

## 8.12 Empty arrays, not null

Go serialises a nil slice as `null`. A server without a GPU sent `"gpu": null`, which made
the Swift decoder drop the entire sample — the screen stayed empty while nothing was
actually wrong. Fixed on both sides: the agent initialises every slice, and the app decodes
`null` as an empty array so it does not break against an older agent.

## 8.13 The bare command must not start the daemon

Running `sudo nodestatus-agent` with no arguments used to start a second daemon. It took
over the control socket from the running instance and then died on the occupied port, after
which `enroll --new` reported `connection refused` and nothing worked. The bare command now
prints help, and the control socket is only opened after the TCP listener succeeds.

## 8.14 Chart rescaling reads as flicker

The y-scale was recomputed from the peak every second, and the peak was taken over the full
300-sample buffer rather than the visible 60. A spike from four minutes ago inflated the
scale while being invisible, and 600 marks were drawn where 120 were shown. The scale is
now held in state with hysteresis, and computed over the visible window only.

## 8.15 Test affordances

Two launch arguments exist **only in DEBUG builds**: `-SIPairURL` pairs automatically and
`-SITab` opens a given tab. The simulator has no camera and `simctl` cannot tap, so without
them the pairing flow could not be exercised automatically.

## 8.16 Measured, not estimated

The design aimed for under 15 MB RSS. Measured: **20–22 MB**. The difference is the Go
runtime plus the process snapshot, which briefly builds a map over every PID — and on the
test machine that meant 8,700 of them. Still an order of magnitude below Netdata, but the
original figure was optimistic and is corrected here.
