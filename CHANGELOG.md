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
