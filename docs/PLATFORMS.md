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

## Synology DSM / ESXi

Both were investigated separately and are not install targets. DSM 7 turns out to
have real systemd and a working `smartctl`, but no `useradd` (it uses `synouser`)
and a `smartctl` too old for the JSON output the agent parses. ESXi has no Go
runtime at all and would need a signed VIB. Neither is blocked on the build
matrix; both need their own install path.
