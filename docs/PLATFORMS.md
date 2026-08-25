# Supported platforms

## What ships today

`make release` builds four Linux binaries. Between them they cover every machine
this agent can run on:

| Build   | Covers                                                                     |
|---------|----------------------------------------------------------------------------|
| `amd64` | 64-bit Debian/Ubuntu and any other x86_64 distro                            |
| `386`   | 32-bit Debian/Ubuntu on x86 (i386/i686), including older hardware           |
| `arm64` | Raspberry Pi 3/4/5 and Zero 2 W on a 64-bit OS                              |
| `arm`   | Every other Pi: Zero/Zero W and Pi 1 (armv6) through Pi 2/3/4/5 on a 32-bit OS |

The `arm` build is compiled with `GOARM=6`, which runs on armv6 *and* armv7. One
binary covers both; the only cost is not using armv7-only FPU instructions, which
this workload never used anyway. `install.sh` maps `armv6l`, `armv7l`, `armv8l`
and `arm` onto it.

There is deliberately no separate build per distribution. The agent is a single
static binary with no libc dependency, so Debian, Ubuntu, Raspberry Pi OS and the
rest are the same target — only the CPU architecture matters. What *does* differ
per machine is which optional tools are installed, and that is handled at runtime
rather than at build time (see below).

## Optional modules are probed, not assumed

Whether a module works is decided on the machine itself, at startup, by actually
running it once — not by checking whether a binary exists.

That distinction matters. On a Raspberry Pi, `smartctl` is often installed and
still unusable: the Pi boots from `/dev/mmcblk0`, the sudoers rule did not cover
that device, and every call came back "permission denied". The app happily showed
a SMART tool that could never work. The same applied to a `speedtest` binary built
for a different architecture — present, and broken on first use.

`nodestatus-agent doctor` runs those same probes and prints the result:

```bash
sudo -u nodestatus nodestatus-agent doctor
```

`install.sh` runs it at the end of an install, so you see what this machine can
and cannot do before you ever open the app. Modules that fail the probe are left
out of the capability list, and the app hides them entirely rather than offering
a tool that returns an error.

## macOS

Not supported, and not a matter of adding a build target.

`GOOS=darwin go build` succeeds, which is misleading: the binary compiles and then
reports nothing. Every metric this agent collects comes from `/proc` and `/sys` —
around 40 read sites across the collectors — and macOS has no procfs at all. CPU
times, memory, disks, network counters, sensors and GPU would each need a second
implementation against `sysctl`, `host_statistics64`, `statfs` and IOKit, most of
which need cgo and therefore give up the static-binary property the whole install
story rests on. `install.sh` is also built around systemd, where macOS uses
launchd.

So a macOS port is a real port: a second platform backend plus its own service
integration and its own testing, not a flag. It is not started. `install.sh`
refuses to run on a non-Linux kernel with an explanation rather than installing
something that would sit there reporting zeros.

## Windows

Same shape of problem as macOS — `GOOS=windows go build` succeeds and reports
nothing — but a wider spread of difficulty per collector rather than one flat
wall, because Windows does have documented APIs for most of what `/proc` and
`/sys` give for free on Linux:

| Collector | Windows equivalent | Difficulty |
|---|---|---|
| CPU, RAM, disks, network | `GetSystemTimes`, `GlobalMemoryStatusEx`, `GetDiskFreeSpaceEx`, `GetIfTable2` (or the `windows/svc` + WMI route `gopsutil` already wraps) | Low — well-trodden, other Go tools do this routinely |
| SMART | `smartctl` — smartmontools ships a native Windows build, same `-j` JSON output the agent already parses | Low, possibly closer to a config change than new code |
| Nvidia GPU | `nvidia-smi.exe` ships with the Windows driver, same CLI | Low |
| Intel GPU | `intel_gpu_top` is Linux-only; no equivalent CLI | Not practical without new tooling |
| Sensors / temperature | No sysfs-style interface. Real readings need a kernel driver (what LibreHardwareMonitor bundles) or WMI's unreliable ACPI thermal zone, which many boards don't populate at all | High — the actual long pole |
| journald / logs | Windows Event Log (`wevtutil`, or the Win32 Event Log API) | Medium — different model, needs its own parser |
| apt updates | No equivalent; Windows Update is a COM API (`WUA`), not a CLI | Medium, and arguably out of scope |
| systemd unit + sudoers | Windows Service (`golang.org/x/sys/windows/svc`) running as `LocalSystem` | Medium — different privilege story, see below |

Two things make this a different shape of effort than the Linux collectors, not
just more of the same:

- **The least-privilege story falls apart.** Linux gets scoped `sudo` rules for
  exactly the few commands that need root (§7.5 in
  [docs/07-security.md](07-security.md)); the rest of the agent runs as an
  unprivileged system user. Windows doesn't have an equivalent of "root for
  this one binary, nothing else" — a Windows Service either runs as
  `LocalSystem` (full privilege, for everything) or as a low-privilege account
  that can't read most of what SMART/sensors need. Matching the current
  security posture, not just the feature set, is its own design problem.
- **`install.sh` has no PowerShell sibling.** A working installer needs its own
  script (service account, cert generation, service registration, Windows
  Firewall rule via `netsh advfirewall`), plus a signing/execution-policy story
  so it isn't blocked by default like an unsigned `.ps1` normally is.

Overall: a real second platform backend, comparable in size to the Linux one —
CPU/RAM/disk/network and even SMART could realistically land in days, sensors
would take real investigation (possibly ending in "not supported on most
consumer boards"), and the installer + privilege model is its own project.
Worth doing if there's a machine to target; not something the current codebase
gets for free.

## Synology DSM / ESXi

Neither is an install target today. ESXi has no Go runtime at all and would
need a signed VIB — not investigated further, since it's a hypervisor rather
than a machine to monitor.

DSM 7 is more promising than macOS or Windows, because under the package
manager it actually *is* Linux, with a real kernel, `/proc`/`/sys`, and (as of
DSM 7) real systemd — the same collectors and the same service model apply in
principle. What's missing is Synology's own layer on top:

- **No `useradd`.** DSM manages users through its own `synouser` command; the
  installer's system-user step needs a DSM-specific branch rather than working
  unmodified.
- **`smartctl` is present but too old.** The agent parses `smartctl -j` (JSON
  output), which needs smartmontools ≥ 7.0. DSM has historically bundled an
  older version without it — SMART would need either a non-JSON parser
  fallback in the agent or a newer `smartctl` sideloaded some other way
  (e.g. from SynoCommunity), neither of which is a small addition.
- **No `apt`.** Package Center / `synopkg` replaces it, so the installer's
  "optional module" step (smartmontools, lm-sensors, intel-gpu-tools, …) has
  nothing to install against on DSM even where those tools would help.
- **DSM owns the firewall UI.** It's iptables underneath, but the
  point-and-click Control Panel is the supported way to open a port —
  `install.sh`'s automatic firewall rule doesn't have a DSM equivalent to call.
- **RAPL and `sudo` are unconfirmed.** Both are plausible on real Linux/systemd
  hardware, but whether DSM's kernel config exposes `/sys/class/powercap` and
  whether a usable `sudo` ships at all has not been tested on an actual unit.

**Specifically for the DS920+**: it's an Intel Celeron J4125 (Gemini Lake
Refresh) box, so the existing `amd64` static binary should simply *run* —
no cgo, no libc dependency, nothing architecture-specific to rebuild. The
Hot-path metrics that only touch `/proc` (CPU, RAM, network, load) should work
if the binary is running as a service at all, since those don't depend on any
of the DSM-specific gaps above. Storage is the one collector likely to need
its own look even once running: DSM builds volumes out of `mdraid` + LVM +
Btrfs under `/volume1`, so the existing storage collector — written against a
plain single-disk/partition layout — would probably need adjusting to label a
Synology volume sensibly instead of surfacing raw RAID member devices.

Net assessment: **not a `curl | bash` target today**, but also not a from-scratch
port like Windows. The realistic path is a dedicated `install.sh --mode
synology` (or a wholly separate `install-dsm.sh`) that branches on `synouser`,
skips the apt-based extras step, and either ships a bundled `smartctl` or
disables SMART, plus manual verification on real DS920+ hardware to settle the
RAPL/`sudo` and storage-layout questions above before calling it supported.
