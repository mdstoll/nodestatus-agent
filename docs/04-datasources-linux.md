# 04 — Linux datasources

Waar élk getal in de app vandaan komt. Dit is bewust exhaustief, want dit is het deel waar
implementaties normaal gesproken op stukloopt (verkeerde eenheden, tellers die overlopen,
percentages die geen deltas zijn).

## 4.1 CPU

| Wat | Bron | Let op |
|-----|------|--------|
| Totaal % | `/proc/stat` regel `cpu` | **Delta tussen twee samples**, niet de absolute waarde. `busy = total - (idle + iowait)` |
| Per core % | `/proc/stat` regels `cpu0..N` | Idem, per regel |
| user/system/iowait/steal | zelfde regel, velden 1/3/5/8 | `steal` > 0 verraadt een overbelaste hypervisor — waardevol op VPS'en |
| Model, vendor, flags | `/proc/cpuinfo` | Op ARM staat er geen `model name`; val terug op `/sys/firmware/devicetree/base/model` |
| Fysieke cores vs threads | `/proc/cpuinfo` `core id` + `physical id`, of `/sys/devices/system/cpu/cpu*/topology/` | `nproc` geeft threads, niet cores |
| Huidige frequentie | `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` (kHz) | Bestaat niet in veel VM's → veld weglaten |
| Governor | `.../cpufreq/scaling_governor` | |
| Load average | `/proc/loadavg` | Deel door aantal threads voor een percentage-achtig gevoel |
| Procs running/total | `/proc/loadavg` veld 4 (`3/412`) | |
| Context switches | `/proc/stat` `ctxt` | Delta; leuk voor de CPU-tool |

**Valkuil:** `cpu_percent` bij de eerste sample. Bij de allereerste meting is er geen vorige
sample; de agent moet dan `null` sturen of één tick wachten voordat hij de eerste
SSE-sample verstuurt. Anders zie je bij het openen van de app altijd één frame met een
onzinnig getal (meestal het gemiddelde sinds boot).

## 4.2 Geheugen

`/proc/meminfo` (waarden in kB — ×1024 voor bytes).

```
used = MemTotal - MemAvailable
```

**Niet** `MemTotal - MemFree`: dan tel je page cache mee als "gebruikt" en zit je altijd
op 95%+. `MemAvailable` is de kernel-schatting van wat een nieuwe applicatie kan krijgen en
is sinds kernel 3.14 het juiste getal. De app toont `used / total` en het percentage
daarvan; `cached` en `buffers` tonen we apart in de detailweergave.

Swap: `SwapTotal` / `SwapFree`. DIMM-details (type, snelheid, slot) alleen via `dmidecode`
(root nodig) — optioneel, met capability-flag.

## 4.3 Opslag

| Wat | Bron |
|-----|------|
| Mountpoints | `/proc/mounts` — filter pseudo-fs: `tmpfs, devtmpfs, proc, sysfs, cgroup*, overlay, squashfs, ramfs, autofs, fuse.*snap*, debugfs, tracefs` |
| Gebruik per mount | `statfs()`-syscall: `total = f_blocks*f_bsize`, `free = f_bavail*f_bsize` |
| Device-layout | `lsblk -J -b -o NAME,SIZE,TYPE,MOUNTPOINT,FSTYPE,MODEL,ROTA` |
| Read/write throughput | `/proc/diskstats` velden 6 en 10 (sectoren, ×512) — **delta** |
| IOPS / util% | `/proc/diskstats` velden 4, 8, 13 (`io_ticks`) |
| RAID | `/proc/mdstat` |
| ZFS | `zpool list -Hp`, `zfs list -Hp` (alleen als `zpool` bestaat) |
| LVM | `lvs --reportformat json` |

**Meerdere schijven in de UI** (jouw vraag): zie [06 — Designsysteem §6.6](06-design-system.md).
Kort: één samengevatte balk met daaronder een uitklapbare lijst per mount, met per mount
een dunne balk. `/` staat altijd bovenaan; `total = som van alle non-pseudo mounts`.

**Valkuil:** btrfs- en ZFS-subvolumes verschijnen meerdere keren in `/proc/mounts` met
dezelfde onderliggende ruimte. Dedupliceer op `(device, fsid)` anders tel je dubbel.

## 4.4 Netwerk

| Wat | Bron |
|-----|------|
| Bytes/packets per interface | `/proc/net/dev` — **delta** voor snelheid, absoluut voor "Total Usage" |
| Interface up/down | `/sys/class/net/<if>/operstate` |
| Linksnelheid | `/sys/class/net/<if>/speed` (Mbps; `-1` bij virtual/wifi) |
| MAC / MTU | `/sys/class/net/<if>/address`, `/mtu` |
| IP-adressen | netlink (Go: `net.Interfaces()`) |
| Default gateway | `/proc/net/route` |
| DNS-servers | `/etc/resolv.conf` + `resolvectl status` bij systemd-resolved |
| Verbindingen | `/proc/net/tcp{,6}` — optioneel, voor een "open poorten"-tool |

**Filteren:** `lo`, `docker*`, `veth*`, `br-*`, `virbr*` uitsluiten van het totaal
(anders telt loopback-verkeer mee en zie je rare pieken), maar wél tonen in de
interface-lijst. De agent stuurt daarom `network.rx_bps` (gefilterd totaal) én
`network.interfaces[]` (alles).

**Teller-overflow:** op 32-bit systemen lopen `/proc/net/dev`-tellers over bij 4 GiB.
Detecteer `nieuw < oud` → behandel als reset, delta 0 voor die tick.

## 4.5 Sensoren

| Type | Bron | Opmerking |
|------|------|-----------|
| Temperatuur | `/sys/class/hwmon/hwmon*/temp*_input` (milli-°C) | Label uit `temp*_label`, chipnaam uit `hwmon*/name` |
| Drempels | `temp*_max`, `temp*_crit` | Gebruik deze voor `status: ok/warn/crit` — nooit hardcoden |
| Fans | `fan*_input` (RPM) | |
| Voltage | `in*_input` (mV) | |
| Vermogen | `power*_input` (µW) | Vooral op servers met PSU-monitoring |
| Thermal zones | `/sys/class/thermal/thermal_zone*/temp` + `type` | Fallback als hwmon leeg is (veel ARM/SBC) |
| Raspberry Pi | `vcgencmd measure_temp` | Alleen als het bestaat |
| Batterij (laptops) | `/sys/class/power_supply/BAT*/` | Zeldzaam op servers, maar gratis meegenomen |
| SMART-temp | `smartctl -j -A` | Per schijf; hoort visueel bij de sensors |

`lm-sensors` is **niet nodig** om te meten — alleen om mooiere labels te krijgen na
`sensors-detect`. De agent leest hwmon rechtstreeks.

**Deduplicatie:** `k10temp`, `coretemp` en `acpitz` rapporteren vaak dezelfde CPU-temp.
Kies één "primaire" temperatuur voor het Metrics-scherm (voorkeursvolgorde:
`coretemp/k10temp Package` → `cpu_thermal` → `acpitz` → hoogste hwmon-waarde) en toon
de rest alleen in de Sensors-tool.

## 4.6 GPU

| Vendor | Bron |
|--------|------|
| NVIDIA | `nvidia-smi --query-gpu=index,name,utilization.gpu,utilization.memory,memory.used,memory.total,temperature.gpu,fan.speed,power.draw,power.limit,clocks.sm,clocks.mem,driver_version --format=csv,noheader,nounits` |
| NVIDIA processen | `nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv,noheader,nounits` |
| AMD | `/sys/class/drm/card*/device/gpu_busy_percent`, `mem_info_vram_used/total`, hwmon voor temp/power/fan |
| Intel | `intel_gpu_top -J -s 1000` (root of `CAP_PERFMON`) — of `/sys/class/drm/card*/gt_cur_freq_mhz` als lichte variant |
| Generiek | `/sys/class/drm/card*/device/{vendor,device}` → PCI-ID's voor naamherkenning |

`nvidia-smi` kost ~80 ms → **niet elke seconde**. Draai hem op 2 Hz→1 Hz in een eigen
goroutine met eigen cache en voeg de laatst bekende waarde toe aan het sample.

## 4.7 Systeem & updates

| Wat | Bron |
|-----|------|
| Distro | `/etc/os-release` |
| Kernel | `uname` syscall |
| Hardwaremodel | `/sys/class/dmi/id/{sys_vendor,product_name,board_name}` |
| Virtualisatie | `systemd-detect-virt` (of zelf: `/sys/hypervisor`, DMI-strings, `/proc/1/cgroup`) |
| Boot-tijd | `/proc/stat` regel `btime` (epoch) — betrouwbaarder dan `uptime` aftrekken |
| Uptime + idle | `/proc/uptime` (twee velden: uptime, som idle over alle cores) |
| Reboot-historie | `last -x reboot` / `journalctl --list-boots` |
| Upgradable pakketten | `apt-get -s -o Debug::NoLocking=1 dist-upgrade` **of** `/usr/lib/update-notifier/apt-check --human-readable` |
| Security-updates | tel pakketten met `-security` in de origin (`apt-get -s ... \| grep`) — netter: `apt list --upgradable` + `apt-cache policy` |
| Reboot nodig | bestaan van `/var/run/reboot-required` + `/var/run/reboot-required.pkgs` |
| Unattended upgrades | `/etc/apt/apt.conf.d/20auto-upgrades` |

**Belangrijk:** de agent draait **nooit** `apt-get update` (dat verandert systeemstatus en
vereist netwerk + lock). Hij leest alleen de bestaande cache en meldt wanneer die voor het
laatst is bijgewerkt (mtime van `/var/lib/apt/periodic/update-success-stamp`). Als de cache
oud is, toont de app "cache is 6 dagen oud" — informatief, geen actie.

## 4.8 Locale & regio

| Wat | Bron |
|-----|------|
| Locale, taal | `localectl status` of `/etc/default/locale` + env |
| Timezone + offset | `timedatectl show` / `/etc/timezone` |
| NTP-sync | `timedatectl show -p NTPSynchronized` |
| Keyboard layout | `localectl status` (`X11 Layout`, `VC Keymap`) |
| Systeemtijd vs UTC | `timedatectl` (`RTC in local TZ`) |

## 4.9 Logs

| Bron | Commando |
|------|----------|
| systemd-journal | `journalctl -u <unit> -n <N> -o json --no-pager` (+ `--since`, `-p`, `-g` voor grep) |
| Live tail | `journalctl -u <unit> -f -o json` |
| Platte bestanden | `tail -n <N> <pad>` / `tail -F` |
| Beschikbare units | `systemctl list-units --type=service --state=running -o json` ∩ whitelist |

`-o json` geeft gestructureerde velden (`__REALTIME_TIMESTAMP`, `PRIORITY`, `_PID`,
`_SYSTEMD_UNIT`, `MESSAGE`) — veel robuuster dan tekst parsen, en de app kan op priority
kleuren en filteren.

De agent hoort bij de groep `systemd-journal` (zie de systemd-unit) en kan daarmee het
volledige journal lezen zonder root.

## 4.10 Speedtest

| Optie | Commando | Voordeel |
|-------|----------|----------|
| Ookla CLI (aanbevolen) | `speedtest -f json --accept-license --accept-gdpr` | Accuraat, servers overal, geeft `result_url` |
| librespeed-cli | `librespeed-cli --json` | Geen licentie-acceptatie, open source, single binary |
| `speedtest-cli` (python) | `speedtest-cli --json` | Deprecated, minder accuraat |

Ookla vereist eenmalig licentie-acceptatie; `install.sh --with-extras` doet dat met de
flags erbij, zodat de agent nooit op een interactieve prompt blijft hangen.

**Waarschuwing die de app moet tonen:** een speedtest verbruikt makkelijk 1–3 GB data.
Op een VPS met datalimiet is dat relevant. De app vraagt daarom om bevestiging voor de
eerste run per server en onthoudt die keuze.
