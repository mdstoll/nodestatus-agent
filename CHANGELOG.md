# Changelog

All notable changes to Node Status are recorded here. This file, plus git tags on
GitHub, is the backup and version history for the project — you don't need to track
this yourself.

## [Unreleased]

### Fixed
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
