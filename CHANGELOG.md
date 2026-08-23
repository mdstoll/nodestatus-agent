# Changelog

All notable changes to Node Status are recorded here. This file, plus git tags on
GitHub, is the backup and version history for the project — you don't need to track
this yourself.

## [Unreleased]

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
