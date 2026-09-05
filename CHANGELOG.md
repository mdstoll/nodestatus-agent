# Changelog

All notable changes to Node Status are recorded here. This file, plus git tags on
GitHub, is the backup and version history for the project — you don't need to track
this yourself.

## [Unreleased]

## v0.2.14 — 2026-09-06

### Changed
- **The iperf3 test measures download first, then upload** — the same order as
  the internet speed test, which reports ping → download → upload. It used to
  run upload first, because that is iperf3's own default, so the two tests in
  the app built up the same two numbers in opposite orders. There is no
  technical reason for either order: they are two separate runs.

## v0.2.13 — 2026-09-06

### Changed
- **The agent speaks English everywhere now.** v0.2.5 claimed this was done,
  but around 30 strings were missed and still reached the app in Dutch:
  pairing errors ("koppelcode klopt niet"), job errors, the "feature is
  switched off" replies, the default device name, and the DNS result's server
  field. Code comments stay Dutch — those are for whoever edits the code, not
  for the app.
- Documentation caught up with the last five releases: the tools the app now
  offers, `extras install`, `doctor`, the job types and the cancel endpoint,
  the four architectures that are actually built, and the correct uninstall
  command. Two rate limits the API doc described (per-token stream limits, a
  30–60 s response cache) were never implemented and are gone from the table.

## v0.2.12 / App v0.2.8 — 2026-09-05

A full review of both codebases, so this release is fixes only.

### Fixed
- **A data race in the sampler could take the whole agent down.** `Tick()` runs
  in the sampler loop, but `Latest()` also calls it from an HTTP handler
  goroutine while the ring buffer is still empty. Both wrote to `prevNet` and
  `prevDisk`, and a concurrent map read+write is not a race Go lets you
  survive — the runtime aborts the process. One mutex around a measurement;
  proven with `-race` before and after.
- **The Geekbench Pro licence broke the very run it was meant to improve.** The
  agent passed `--username`/`--password`, which the Geekbench 6 CLI does not
  have: it only knows `--unlock EMAIL KEY`, a one-time activation with a
  licence *key*, not an account password. Anyone who filled the field in made
  their benchmark fail. Flags and the app's licence UI are gone; a proper
  `--unlock` flow can come back later as its own command.
- **iperf3 errors were unreadable.** stderr wasn't captured anywhere, so
  "unable to connect to server: Connection refused" reached the app as
  "exit status 1".
- **`POST /v1/jobs` answered without a `Content-Type`**: the header was set
  after `WriteHeader`, where it is silently ignored.
- **The rate-limit map grew without bound** — every client IP stayed in it
  forever, and a phone on mobile data arrives with a different IP every time.
- The Geekbench installer reported a changed archive layout as a confusing
  chmod error, because it ran `chmod` before checking the file was there.
- **The app leaked a URLSession per poll.** `APIClient` is the delegate of its
  own sessions, and a session holds its delegate until you invalidate it, so
  every 5 s poll per node added one that never went away. One client per node
  now, reused (which also saves a TLS handshake per poll) and invalidated when
  the address changes or the node is removed.
- **A card kept showing the last CPU and RAM of a node that had gone offline**,
  as if those numbers were current. Metrics already handled this properly
  (dimmed, with a banner); the card does now too.
- **Detail screens kept the previous node's data when you switched nodes** —
  and if loading failed for the new one, kept it indefinitely, presented as
  belonging to the node you were now looking at.
- A bare IPv6 address is bracketed in the base URL, so such a node is
  reachable at all.
- The stream no longer reconnects on `.inactive`: pulling down Control Centre
  is not a reason to drop it.
- Removed dead state (`lastRun`) and a comment describing a speedtest rate
  limit that does not exist; the Dutch copy in the app promised it too.

## v0.2.11 / App v0.2.6–0.2.7 — 2026-08-31

### Fixed
- **Stopping a benchmark did not stop it.** Geekbench forks a worker process
  per benchmark; killing only the launcher left the worker running with the
  stdout pipe still open, so the read side never saw EOF and the job stayed
  "running" forever. It runs in its own process group now and cancelling kills
  the group.
- **iperf3 showed no live throughput during a test.** Without a terminal its
  stdout is block-buffered, so every per-second line arrived in one burst when
  the process exited — the live figure sat at 0 for a whole direction
  regardless of the job timeout. `--forceflush` fixes it at the source.

### Changed
- The benchmark screen shows which phase is running (single-core or
  multi-core) instead of streaming raw CLI output into a terminal view.
- Benchmark moved out of Node Uptime into its own entry in the Tools list.
- The per-core usage bar fills up immediately: its block duration follows the
  configured history window instead of assuming a fixed five minutes, which is
  more than the app ever backfills.

## v0.2.10 — 2026-08-31

### Fixed
- iperf3's job timeout (30 s) was too tight for its own two 10-second
  directions plus connection setup, so the download half could be cut off.
  Raised to 60 s, and the live value resets at the upload→download handoff
  instead of briefly showing the previous direction's number.

## v0.2.9 / App v0.2.4–0.2.5 — 2026-08-31

### Added
- **`nodestatus-agent extras install [deps|iperf3|geekbench|all]`** — installs
  optional software without re-running install.sh, for a machine that was set
  up before a tool existed.
- **iperf3 as a second speed test**, next to the internet speed test: raw
  throughput to a machine of your own, in both directions, with the same live
  chart. Hidden unless iperf3 is installed on the node.
- **Geekbench CPU benchmark**, run from the app, with the result linked to the
  Geekbench Browser. The free anonymous flow needs no account. Downloads the
  build for the machine's own architecture; there is no 32-bit build.
- `POST /v1/jobs/{id}/cancel`, so a long job can be stopped from the app.

## v0.2.8 — 2026-08-30

### Fixed
- **install.sh installed none of the optional packages on a Raspberry Pi.**
  They were installed in one `apt-get install` call, and a single package apt
  cannot resolve for that architecture (`intel-gpu-tools` on ARM) fails the
  entire call, taking every other package with it. Installed one at a time
  now, and intel-gpu-tools is only requested on x86.
- **No GPU on a Raspberry Pi.** There was no detection for Broadcom/VideoCore
  at all, and once added, `vcgencmd` still failed for the unprivileged agent
  user: it needs the `video` group to open `/dev/vchiq`. The installer grants
  it, alongside the `disk` group it already granted for smartctl.

## v0.2.7 / App v0.2.3 — 2026-08-25

### Added
- **CPU power draw on Metrics**, right under Temperature — only on machines
  with Intel RAPL (the same source as the Sensors-page power reading), hidden
  everywhere else, same rule as GPU. Runs on its own 1 Hz background cache
  (`internal/collect/power.go`) rather than in the hot sampling loop, since
  `sudo cat` is a real subprocess call. Verified against raw `energy_uj`
  reads on the test NUC: agent and hardware agreed to within 0.03 W.
- **Offline servers sink to the bottom of the Server list**, online ones float
  back up — driven live by each card's own 5 s poll (`AppState.setOnline`),
  as a stable partition so a manual "Reorder" order survives within each
  group.

### Changed
- `install.sh` now installs the optional extras (smartmontools, whois,
  dnsutils, qrencode, lm-sensors, intel-gpu-tools) **by default**; skip them
  with `--no-modules` (`--with-extras` still works, it's just redundant now).
  The Pair-a-server page spells this out and shows the opt-out flag.
- Metrics tiles now have a soft shadow and a thin frosted-glass layer
  (`Card(elevated: true)`) instead of a flat panel — deliberately left alone
  everywhere else in the app (Settings, Tools, the Server list).
- The live network chart (the Metrics widget and Tools → Network, which
  already shared the same component) has a thicker line and a vertical
  gradient fill instead of a flat semi-transparent one; the same treatment was
  applied to the throughput chart during a speed test.
- Around 20 places where English and Dutch ran into each other were brought in
  line: the tab names, every detail-screen title, and loose strings on the
  Metrics and Tools pages that ignored the language setting altogether
  ("Settings", "Storage", "Device Status", "Sensors", …).

### Fixed
- **Tapping a server card did nothing** ever since the Server tab became a
  `List` (for drag-to-reorder): `.onTapGesture` apparently loses to List's own
  touch handling (swipe actions, built-in cell selection). Back to a `Button`
  — possible again now that dragging no longer goes through a long-press on
  the card itself, but through the separate "Reorder" button.
- **Reordering servers did not actually work.** Two earlier attempts both
  failed for the same kind of reason and neither was verified at the time:
  `.draggable`/`.dropDestination` was beaten to the long-press by the card's
  own `.contextMenu`, and `List.onMove` only drags in edit mode on iPhone, so
  a long-press outside it does nothing. Reordering now sits behind a
  "Reorder" button. Edit mode was avoided originally because of the row of
  red delete circles; `.deleteDisabled(true)` removes those, leaving only the
  drag handles. Verified end-to-end, including that the new order survives an
  app restart.
- Edit and Delete moved from the long-press context menu to swipe actions, so
  nothing competes for the long-press and Delete is no longer one stray tap
  away. ("Refresh now" is gone — the cards already poll every 5 seconds.)
- **RAPL power draw was overstated on the Sensors page** (Package-0 read
  ~13–14 W there against ~10 W on the Metrics widget for the same machine at
  the same time). `raplChip()` assumed its read-sleep-read window was exactly
  200 ms; in reality each read goes through `RunSudo`, which first tries a
  plain `cat` (always denied for this root-only file) and only then
  `sudo -n cat` — that overhead was never in the 0.2 s divisor. Now measured
  against the actual elapsed wall-clock time, like the widget already did;
  both agree.
- **Charts occasionally tore into a jagged, self-crossing shape**, most
  visible in the network chart's gradient fill, especially after a brief
  reconnect. A reconnect re-sends up to `historyWindow` seconds of backfill
  without clearing the buffer first (by design, so the chart never blanks on
  a hiccup) — but appended blindly, that backfill overlapped what was already
  buffered and broke the strict time ordering every chart assumes. Samples
  are now only appended if they move time forward.

### Repo
- **Split the iOS app into its own private repo**
  (`github.com/mdstoll/nodestatus-ios`), history intact. This repo goes back
  to being just the agent — public, as it always was.

## v0.2.5 / App v0.2.2 — 2026-08-24

### Added
- **Modules are probed, not assumed.** The agent now runs each optional module
  once at startup and only reports it as a capability if it actually works.
  Checking "is the binary installed?" was not enough: on a Raspberry Pi
  `smartctl` is present but unusable (see below), and a `speedtest` binary
  built for another architecture only fails on first use. The app hides
  whatever the server does not report, so a tool that cannot work is no longer
  offered at all.
- `nodestatus-agent doctor` runs those same probes and prints, per module,
  whether it works — and if not, why and how to fix it. `install.sh` runs it at
  the end of an install (as the agent's own user, so it sees the same
  permissions the agent will), so you learn what this machine can do before
  opening the app rather than by hitting an error later.
- 32-bit x86 builds (`386`), covering 32-bit Debian/Ubuntu. Together with
  `amd64`, `arm64` and `arm` (GOARM=6) this covers every Raspberry Pi model —
  Zero/Zero W and Pi 1 through Pi 5, on 32-bit or 64-bit. `install.sh` maps
  `i386`/`i686` and `armv8l` onto the right build.
- Settings → Update agent has a "Check now" button; `GET /v1/update?refresh=1`
  asks GitHub immediately instead of waiting out the six-hour cache. The agent
  still rate-limits how often that actually leaves the machine.
- `docs/PLATFORMS.md`: what each build covers, how module probing works, and
  why macOS is a port rather than a build flag.

### Fixed
- **SMART on a Raspberry Pi always failed with "permission denied".** The
  generated sudoers rule covered `/dev/sd*`, `/dev/nvme*` and `/dev/vd*`, but a
  Pi boots from `/dev/mmcblk0`, which the agent asks about and sudo then
  refuses. Added `mmcblk` and `hd*`, and the probe now distinguishes "sudo
  won't allow it" (fixable, and it says how) from "this disk has no SMART"
  (not an error — the module is simply hidden).
- **The agent spoke Dutch to an English app.** Around 40 user-facing strings —
  including the "geen rechten", "niet geïnstalleerd" and speedtest errors that
  surfaced in the app — were Dutch regardless of the app's language. They are
  English now; the app translates its own UI.
- **Pairing produced an address unreachable from mobile data.** The QR encoded
  the server's raw IPv4 address. Mobile networks are commonly IPv6-only with
  NAT64/DNS64, which synthesises IPv6 only for names resolved through DNS,
  never for a literal IPv4 address — so pairing worked on Wi-Fi and silently
  could not work on 4G/5G. When the machine has a real public FQDN, that name
  is now advertised instead. Loopback answers are ignored, because Debian's
  default `/etc/hosts` maps the FQDN to `127.0.1.1` and Go reads that file
  before DNS. Covered by a test.
- The GPU capability check called the GPU cache's async accessor, which by
  design returns an empty list on its first call — so every machine reported
  "no GPU" at startup and the app hid the GPU section. It collects
  synchronously now. Verified end-to-end against a VAAPI encode on the test
  NUC: 28.7% load, Render/3D 28.7%, Video 24.8%, 1.69 W, matching
  `intel_gpu_top`.
- Reverted the previous release's `NoClientCert`: while a pairing window was
  open the server stopped asking for a client certificate at all, so
  already-paired devices got "no client certificate" on every request. It asks
  again without requiring one, which is what the app-side fix needed anyway.

### Changed
- The Metrics tiles no longer have a fixed height. Two tiles side by side take
  the height of the taller one and their content stays top-aligned, so a tile
  that grows a line takes its neighbour with it instead of clipping.
- Settings → Uninstall no longer puts `--remove-extras` in the copyable
  command. It removed packages that are useful outside this agent; the flag is
  documented in the footnote for anyone who wants it.
- `enroll --new` prints the full CA fingerprint in groups of eight instead of
  abbreviating it, so manual pairing is possible when scanning the QR is not.
- Dutch label "Opslag- en geheugeneenheden" shortened to "Data-eenheden": it
  was the one settings row that wrapped onto a second line.
- `install.sh` refuses to run on a non-Linux kernel with an explanation,
  instead of installing an agent that would report nothing.

### Resolved — the public-IP pairing failure
- The "known issue" listed under App v0.2.1 is fixed, and the remaining cause
  was neither a race nor the Simulator. `NSAllowsArbitraryLoads` was being
  **ignored outright**: on iOS 10+ its value is discarded whenever
  `NSAllowsLocalNetworking`, `NSAllowsArbitraryLoadsInWebContent` or
  `NSAllowsArbitraryLoadsForMedia` is also present. Both keys were set, so the
  local-networking exception covered the LAN server (which always worked) and
  ATS silently kept enforcing system trust for everything else — rejecting the
  self-signed certificate with -9802 before the app's own pinning delegate was
  ever consulted. Removing `NSAllowsLocalNetworking` (arbitrary loads already
  covers local networking) fixed it; pairing to a.mest.dev now succeeds and
  streams. Confirmed against the OS log rather than inferred: the failing
  connection showed `old_ats_enforced set true` while the working one did not.
- Also corrected while here: declining a client-certificate challenge used
  `.cancelAuthenticationChallenge`, which aborts the whole request. The correct
  disposition is `.useCredential` with a nil credential, which continues the
  handshake without offering a certificate.

## v0.2.2 — 2026-08-24

### Changed
- The TLS listener no longer requests an optional client certificate while
  no device is paired yet or a pairing window is open (`tls.NoClientCert`
  instead of `tls.VerifyClientCertIfGiven`) — nothing needs to identify
  itself at that point, and asking anyway was one factor in the pairing
  issue investigated for App v0.2.1.
- Also deployed to a.mest.dev during this investigation, replacing the
  0.1.0 build that had been running there since initial install — see the
  "Debugged the a.mest.dev pairing failure" note in v0.2.0/v0.1.1: those
  fixes were committed at the time but the running binary was never
  actually updated until now.

## App v0.2.1 — 2026-08-24

### Fixed (partially — completed in v0.2.5, see above)
- Pairing to a public-IP server could fail the TLS handshake outright:
  ATS's own system-trust pre-check (`errSSLXCertChainInvalid`, `-9802`) can
  reject a self-signed cert before the app's own pinning delegate gets a say.
  `NSAllowsLocalNetworking` in Info.plist only covers private/local addresses,
  not a VPS's public IP.
- Separately, when the agent optionally requested a client certificate (only
  happens during an open pairing window or before any device is paired), iOS
  sometimes auto-offered whatever client identity was already in the Keychain
  from a different, already-paired server — which the agent correctly
  rejected (wrong issuing CA). The client-certificate challenge is now
  declined explicitly instead of relying on default handling.
- The enrollment request now retries automatically on transport-level
  failures (secure-connection/timeout/connection-lost), each attempt with a
  fresh `URLSession` — mitigates, but does not fully resolve, the race
  described above.

## App v0.2.0 — 2026-08-24

### Added
- Servers on the Server tab can be reordered by holding and dragging —
  no Edit Mode, no delete-circle UI, just a natural long-press-then-drag.
- A real app icon, replacing the default placeholder.
- Tools page reordered to System → Hardware → Network; the Hardware
  category itself reordered to Hardware overview → CPU Information →
  Network interfaces → Storage & SMART → Sensors → GPU.
- Sensors on the Metrics page is now collapsed by default, expanding only
  on interaction, so a first glance at Metrics stays uncluttered.
- CPU, RAM, Storage and Load tiles on Metrics are now a uniform size.
- About: credits "Merlin Stoll" as creator and a closing line — "Built
  with ❤️ and with the help of AI in the Netherlands".

## v0.1.1 — 2026-08-23

### Fixed
- **GPU metrics were frozen or wrong under real load.** Verified end-to-end with an
  actual `ffmpeg` VAAPI conversion on the test machine. Three separate bugs:
  - The GPU reading was refreshed by launching a fresh `intel_gpu_top` process every
    15 seconds and reading it for a few seconds. During a conversion this meant the
    value visibly froze for most of that window. Replaced with a single long-running
    `intel_gpu_top` stream that the agent parses continuously and shuts down 45s after
    the last request — one process instead of one every 15 seconds.
  - A fallback rule read "GPU didn't sleep (rc6=0), so it must be busy" and reported
    100% load with all per-engine values at zero — most visible right as a job started
    or stopped. Removed; utilisation now comes only from what the engines report.
  - The sudoers rule for `intel_gpu_top` pinned `-s 600`, but a later change in the
    code used `-s 700`. sudoers matches arguments exactly, so this silently failed
    with "a password is required" and the GPU stayed at its last cached value. The
    agent now prints its own sudoers rules (`nodestatus-agent sudoers`) from the same
    constant the code executes, and `install.sh` writes them from that output — the
    two can no longer drift apart.
- Confirmed live in the app: GPU load tracks encode start/stop (0% idle → ~25–30%
  during encode → 0% after), with power draw and engine breakdown moving accordingly.

## v0.2.0 — 2026-08-23

### Added
- `nodestatus-agent update` — downloads the latest release for the running
  architecture, verifies it against SHA256SUMS, replaces the binary, and
  restarts the service. Manual only: the app can see that an update exists
  via the new `GET /v1/update`, but can never trigger one — running code on
  your server is not something a phone should be able to ask for.
- Release builds now include `arm` (GOARM=6), covering a Raspberry Pi
  Zero/1 through a Pi 3/4 running the 32-bit OS.
- Real CPU power draw via Intel RAPL (`/sys/class/powercap`), surfaced as a
  sensor. Works on essentially every Intel CPU since Sandy Bridge, including
  small boards with no PSU-monitoring hwmon chip at all — the test NUC is
  exactly that case. Verified reading ~12.6 W package power at idle.
- App: Light/Dark/System appearance, chosen in Settings.
- App: Language setting gained a "System" option, alongside the existing
  English default and Dutch (only offered when the device itself is Dutch).
- App: Units are now dropdowns with an inline explanation of what each one
  affects and where, instead of unlabelled switches.
- App: "Revoke all other devices" in Settings.
- App: Update-agent and uninstall-agent sections in Settings, styled like
  the pairing screen's copyable command boxes.
- App: CPU, RAM and Load tiles on Metrics are now tappable, matching Storage;
  RAM opens a `btop`-style breakdown (used/cached/buffers/free) with history,
  Load opens a load-average history chart. Storage's detail screen now links
  through to Storage & SMART.
- App: the Hardware overview page is now System/Processor/Memory only —
  Storage, Network, Sensors and GPU moved to their own reachable places, so
  the page a first-time visitor lands on doesn't try to be everything at
  once. GPU appears under Tools → Hardware only when a GPU is actually
  present.

### Fixed
- **`install.sh` could lock every real client out on a public VPS.** Mode
  `lan` restricts access to the primary interface's subnet — sensible on a
  home network, but on a VPS that "subnet" is the datacenter's public
  allocation (a a.mest.dev install was scoped to a /18 that no phone would
  ever be inside). The installer now checks whether the detected address is
  actually private before restricting to it.
- Self-update's version comparison used string inequality, not semver. The
  very first test run "updated" a newer dev build down to an older published
  release. Fixed before it shipped to anyone.
- The generated sudoers rule for reading RAPL power needed the colon in
  `intel-rapl:*` escaped for sudoers' own parser, not just for Go's string
  literal — two different escaping rules that both needed to be right.

### Changed
- Repository renamed to `github.com/mdstoll/nodestatus-agent`.
- The installed uninstaller is now `/usr/local/bin/nodestatus-uninstall.sh`.
