# 04 — Linux data sources

Where every number comes from, and the traps that come with it. This is the part
implementations usually get wrong: wrong units, counters that wrap, percentages that are
not deltas.

## 4.1 CPU

| What | Source | Watch out |
|---|---|---|
| Total % | `/proc/stat` line `cpu` | **Delta between two samples**, never the absolute value. `busy = total − (idle + iowait)` |
| Per core | `/proc/stat` lines `cpu0..N` | Same, per line |
| user/system/iowait/steal | fields 1/3/5/8 | `steal > 0` reveals an oversubscribed hypervisor — valuable on a VPS |
| Model, flags | `/proc/cpuinfo` | ARM has no `model name`; fall back to `/sys/firmware/devicetree/base/model` |
| Cores vs threads | `physical id` + `core id` | `nproc` gives threads, not cores |
| Frequency | `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` (kHz) | Absent in many VMs — omit the field rather than reporting zero |
| Load | `/proc/loadavg` | Divide by thread count for something percentage-like |

**The first sample.** There is no previous sample to diff against, so the agent primes once
and only emits from the second tick. Skipping that gives every app launch one frame of
nonsense — usually the average since boot.

## 4.2 Memory

`/proc/meminfo`, values in kB.

```
used = MemTotal − MemAvailable
```

**Not** `MemTotal − MemFree`: that counts page cache as used and pins every machine at 95%.
`MemAvailable` is the kernel's own estimate of what a new process could get, and is the
correct number since kernel 3.14.

## 4.3 Storage

| What | Source |
|---|---|
| Mount points | `/proc/mounts`, filtering pseudo filesystems |
| Usage | `statfs()`: `total = f_blocks × f_bsize`, `free = f_bavail × f_bsize` |
| Layout | `lsblk -J -b` |
| Throughput | `/proc/diskstats` fields 6 and 10 (sectors × 512) — **delta** |

Three filters matter, and the test machine exercised all three:

- **Pseudo filesystems** (tmpfs, proc, sysfs, cgroup, overlay, squashfs, …) are skipped.
- **Network mounts** (cifs, nfs, sshfs, any `fuse.*`) are marked `remote` and excluded from
  the total. The test machine has nine rclone and CIFS mounts totalling 45 TB of cloud
  storage; counting them as local disk would make the storage gauge meaningless.
- **Deduplication by device.** btrfs subvolumes and bind mounts appear several times in
  `/proc/mounts` sharing the same underlying space. Without dedup you count them twice.

## 4.4 Network

| What | Source |
|---|---|
| Bytes per interface | `/proc/net/dev` — delta for speed, absolute for "total usage" |
| Up/down | `/sys/class/net/<if>/operstate` |
| Link speed | `/sys/class/net/<if>/speed` (`-1` on virtual and wifi) |
| Addresses | netlink via `net.Interfaces()` |
| Default gateway | `/proc/net/route` |
| DNS | `/etc/resolv.conf` |

`lo`, `veth*`, `docker*`, `br-*`, `virbr*`, `tun*`, `tap*`, `wg*` and container networks are
excluded from the totals but still listed — you want to see that they exist. On the test
machine that is eight veth pairs, a docker bridge and a VPN tunnel whose traffic all also
crosses `eth0`; counting them would double or triple the totals.

**Counter wrap.** On 32-bit systems the counters wrap at 4 GiB. A new value lower than the
previous one means a reset: treat that tick as zero rather than emitting a huge spike.

## 4.5 Sensors

| Type | Source |
|---|---|
| Temperature | `/sys/class/hwmon/hwmon*/temp*_input` (milli-°C), label from `temp*_label` |
| Thresholds | `temp*_max`, `temp*_crit` |
| Fans | `fan*_input` (RPM) |
| Voltage | `in*_input` (mV) |
| Power | `power*_input` (µW) |
| Fallback | `/sys/class/thermal/thermal_zone*` for ARM boards without hwmon |

`lm-sensors` is **not** required to read anything — only to produce nicer labels after
`sensors-detect`.

**Sentinel thresholds.** The NVMe drive in the test machine reports
`temp2_max = 65261850` millidegrees, i.e. 65,261 °C, meaning "no threshold set". Taking
that at face value made a disk running at 85.8 °C read as healthy. Thresholds outside a
plausible range (≤ 0 or > 150 °C) are discarded, after which the fallback rule applies.

**One status rule.** Warn at 85% of critical, or at 90% of the high threshold, or above
80 °C when no thresholds exist at all. This lives in one place and is used by both
`/v1/metrics` and `/v1/hardware/sensors` — when it existed twice, the same 85 °C sensor
showed orange on one screen and green on the other.

**Deduplication.** `k10temp`, `coretemp` and `acpitz` often report the same CPU
temperature. One is marked `primary` for the main screen (preferring a `coretemp`/`k10temp`
package sensor); the rest appear only in the sensors detail.

## 4.6 GPU

| Vendor | Source |
|---|---|
| NVIDIA | `nvidia-smi --query-gpu=… --format=csv,noheader,nounits` |
| AMD | `/sys/class/drm/card*/device/gpu_busy_percent`, `mem_info_vram_*`, hwmon |
| Intel | `/sys/class/drm/card*/gt_*_freq_mhz` plus `intel_gpu_top -J` |

Intel integrated graphics need two sources. The sysfs clock frequencies always work
unprivileged and give a decent proxy for load. Real per-engine utilisation, power draw and
rc6 idle time come from `intel_gpu_top`, which needs `CAP_PERFMON` — so it runs through a
pinned sudoers rule, exactly like `smartctl`.

Three things bite here, and all three did:

1. `intel_gpu_top` never exits. It is killed on a timeout, and the **last complete JSON
   block** is used — the first block always reads zero because its measuring period is
   only ~150 ms.
2. It must be killed with **SIGTERM, not SIGKILL**. Go's `CommandContext` sends SIGKILL by
   default, and sudo (with `use_pty`) then never flushes the pty buffer, so not a single
   byte comes back.
3. The output is an array that is never closed, with commas between blocks. A
   `json.Decoder` chokes on it; the blocks are cut out by brace matching instead.

An integrated GPU shares system memory, so it reports `shared_memory: true` and no VRAM
gauge — showing one would misrepresent what is going on.

## 4.7 System and updates

| What | Source |
|---|---|
| Distro | `/etc/os-release` |
| Kernel | `uname` |
| Hardware model | `/sys/class/dmi/id/{sys_vendor,product_name,board_name}` |
| Virtualisation | `systemd-detect-virt` |
| Boot time | `/proc/stat` line `btime` — more reliable than subtracting uptime |
| Upgradable packages | `apt list --upgradable`, falling back to `apt-get -s upgrade` |
| Reboot required | `/var/run/reboot-required{,.pkgs}` |

## 4.8 Processes

`/proc/<pid>/stat` for everything, with CPU% measured over a 300 ms window so it is a
current percentage rather than the average since the process started.

**Zombies are counted but skipped.** The test machine had **8,194 zombie processes out of
8,477** — one program not reaping its children. Walking all of them twice for a CPU delta
they cannot have is pure waste, and the count itself is the more useful signal, so the app
surfaces the total plus which parent is responsible.

## 4.9 Logs

| Source | Command |
|---|---|
| Whole journal | `journalctl -n N -o json` |
| Kernel (dmesg) | `journalctl -k` |
| Errors only | `journalctl -p 3` |
| Current boot | `journalctl -b` |
| One unit | `journalctl -u <unit>` |
| Plain file | `tail -n N <path>` |

`-o json` gives structured fields (`__REALTIME_TIMESTAMP`, `PRIORITY`, `_PID`,
`_SYSTEMD_UNIT`, `MESSAGE`), which is far more robust than parsing text and lets the app
colour and filter by priority. The agent is a member of `systemd-journal`, so it reads the
journal without root.
