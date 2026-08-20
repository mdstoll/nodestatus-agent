package collect

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// ---------- CPU ----------

type cpuTimes struct{ user, nice, system, idle, iowait, irq, softirq, steal uint64 }

func (c cpuTimes) total() uint64 {
	return c.user + c.nice + c.system + c.idle + c.iowait + c.irq + c.softirq + c.steal
}
func (c cpuTimes) busy() uint64 { return c.total() - c.idle - c.iowait }

func readCPUTimes() (cpuTimes, []cpuTimes) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, nil
	}
	defer f.Close()
	var all cpuTimes
	var cores []cpuTimes
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 8 || !strings.HasPrefix(fs[0], "cpu") {
			continue
		}
		n := func(i int) uint64 {
			if i >= len(fs) {
				return 0
			}
			v, _ := strconv.ParseUint(fs[i], 10, 64)
			return v
		}
		t := cpuTimes{n(1), n(2), n(3), n(4), n(5), n(6), n(7), n(8)}
		if fs[0] == "cpu" {
			all = t
		} else {
			cores = append(cores, t)
		}
	}
	return all, cores
}

func pct(cur, prev cpuTimes) (total, user, system, iowait, steal float64) {
	dt := float64(cur.total() - prev.total())
	if dt <= 0 {
		return 0, 0, 0, 0, 0
	}
	d := func(a, b uint64) float64 {
		if a < b {
			return 0
		}
		return float64(a-b) / dt * 100
	}
	return d(cur.busy(), prev.busy()), d(cur.user+cur.nice, prev.user+prev.nice),
		d(cur.system+cur.irq+cur.softirq, prev.system+prev.irq+prev.softirq),
		d(cur.iowait, prev.iowait), d(cur.steal, prev.steal)
}

func readLoad() ([3]float64, int, int) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return [3]float64{}, 0, 0
	}
	fs := strings.Fields(string(b))
	var l [3]float64
	for i := 0; i < 3 && i < len(fs); i++ {
		l[i], _ = strconv.ParseFloat(fs[i], 64)
	}
	var run, tot int
	if len(fs) > 3 {
		if p := strings.SplitN(fs[3], "/", 2); len(p) == 2 {
			run, _ = strconv.Atoi(p[0])
			tot, _ = strconv.Atoi(p[1])
		}
	}
	return l, run, tot
}

func readCPUFreq(n int) []int {
	out := make([]int, 0, n)
	any := false
	for i := 0; i < n; i++ {
		b, err := os.ReadFile("/sys/devices/system/cpu/cpu" + strconv.Itoa(i) + "/cpufreq/scaling_cur_freq")
		if err != nil {
			out = append(out, 0)
			continue
		}
		khz, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		out = append(out, khz/1000)
		any = true
	}
	if !any {
		return nil
	}
	return out
}

// ---------- geheugen ----------

func readMeminfo() map[string]uint64 {
	m := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), ":", 2)
		if len(p) != 2 {
			continue
		}
		fs := strings.Fields(p[1])
		if len(fs) == 0 {
			continue
		}
		v, _ := strconv.ParseUint(fs[0], 10, 64)
		m[p[0]] = v * 1024 // kB -> bytes
	}
	return m
}

func memSample() MemSample {
	m := readMeminfo()
	total := m["MemTotal"]
	avail, ok := m["MemAvailable"]
	if !ok {
		avail = m["MemFree"] + m["Cached"] + m["Buffers"]
	}
	used := uint64(0)
	if total > avail {
		used = total - avail
	}
	var p float64
	if total > 0 {
		p = float64(used) / float64(total) * 100
	}
	return MemSample{
		Total: total, Used: used, Available: avail,
		Cached: m["Cached"], Buffers: m["Buffers"],
		SwapTotal: m["SwapTotal"], SwapUsed: m["SwapTotal"] - m["SwapFree"],
		Percent: p,
	}
}

// ---------- opslag ----------

// pseudoFS zijn filesystems die geen echte opslag representeren.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true, "cgroup": true,
	"cgroup2": true, "overlay": true, "squashfs": true, "ramfs": true, "autofs": true,
	"debugfs": true, "tracefs": true, "devpts": true, "mqueue": true, "hugetlbfs": true,
	"pstore": true, "efivarfs": true, "bpf": true, "configfs": true, "fusectl": true,
	"nsfs": true, "securityfs": true, "binfmt_misc": true, "rpc_pipefs": true,
	"selinuxfs": true, "fuse.gvfsd-fuse": true, "fuse.portal": true, "iso9660": true,
}

// remoteFS zijn netwerkmounts: wel tonen, niet meetellen in het opslagtotaal.
func isRemoteFS(t string) bool {
	switch {
	case t == "cifs", t == "smbfs", t == "nfs", t == "nfs4", t == "afs", t == "ceph", t == "glusterfs", t == "sshfs":
		return true
	case strings.HasPrefix(t, "fuse.") && t != "fuse.gvfsd-fuse" && t != "fuse.portal":
		return true // fuse.rclone, fuse.sshfs, ...
	}
	return false
}

type mountEntry struct{ dev, mount, fstype string }

func readMounts() []mountEntry {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []mountEntry
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 3 {
			continue
		}
		dev, mnt, typ := fs[0], unescapeMount(fs[1]), fs[2]
		if pseudoFS[typ] {
			continue
		}
		// Dedupliceer bind-mounts en btrfs-subvolumes op (device, mountpoint).
		k := dev + "\x00" + mnt
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, mountEntry{dev, mnt, typ})
	}
	return out
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

func storageSamples(io map[string]diskIO) []StorageSample {
	out := []StorageSample{}
	seenDev := map[string]bool{}
	for _, m := range readMounts() {
		var st syscall.Statfs_t
		if err := syscall.Statfs(m.mount, &st); err != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		if total == 0 {
			continue
		}
		avail := st.Bavail * uint64(st.Bsize)
		used := total - st.Bfree*uint64(st.Bsize)
		remote := isRemoteFS(m.fstype)
		// Eén regel per fysiek device: btrfs mount / en /home vanaf dezelfde
		// pool en zou anders dubbel tellen.
		if !remote {
			if seenDev[m.dev] {
				continue
			}
			seenDev[m.dev] = true
		}
		var p float64
		if used+avail > 0 {
			p = float64(used) / float64(used+avail) * 100
		}
		s := StorageSample{
			Mount: m.mount, Device: m.dev, FSType: m.fstype,
			Total: total, Used: used, Percent: p, Remote: remote,
		}
		if d, ok := io[baseDevice(m.dev)]; ok {
			s.ReadBps, s.WriteBps = d.readBps, d.writeBps
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Remote != out[j].Remote {
			return !out[i].Remote
		}
		if out[i].Mount == "/" {
			return true
		}
		if out[j].Mount == "/" {
			return false
		}
		return out[i].Mount < out[j].Mount
	})
	return out
}

// baseDevice maakt van /dev/mapper/vg-root of /dev/nvme0n1p2 de kernelnaam
// zoals die in /proc/diskstats staat.
func baseDevice(dev string) string {
	if !strings.HasPrefix(dev, "/dev/") {
		return ""
	}
	name := strings.TrimPrefix(dev, "/dev/")
	if strings.HasPrefix(name, "mapper/") {
		if link, err := os.Readlink(dev); err == nil {
			name = filepath.Base(link)
		}
	}
	return name
}

type diskIO struct{ readBps, writeBps uint64 }
type diskCounters struct{ read, write uint64 }

func readDiskstats() map[string]diskCounters {
	out := map[string]diskCounters{}
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 10 {
			continue
		}
		r, _ := strconv.ParseUint(fs[5], 10, 64)
		w, _ := strconv.ParseUint(fs[9], 10, 64)
		out[fs[2]] = diskCounters{r * 512, w * 512}
	}
	return out
}

// ---------- netwerk ----------

type netCounters struct{ rx, tx uint64 }

func readNetDev() map[string]netCounters {
	out := map[string]netCounters{}
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		fs := strings.Fields(line[i+1:])
		if len(fs) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fs[0], 10, 64)
		tx, _ := strconv.ParseUint(fs[8], 10, 64)
		out[name] = netCounters{rx, tx}
	}
	return out
}

// isVirtualIface bepaalt of een interface meetelt in het totaal. Docker-bridges,
// veth-paren en VPN-tunnels tellen niet mee: hun verkeer loopt óók over de
// fysieke interface en zou dus dubbel geteld worden.
func isVirtualIface(name string) bool {
	switch {
	case name == "lo":
		return true
	case strings.HasPrefix(name, "veth"), strings.HasPrefix(name, "docker"),
		strings.HasPrefix(name, "br-"), strings.HasPrefix(name, "virbr"),
		strings.HasPrefix(name, "tun"), strings.HasPrefix(name, "tap"),
		strings.HasPrefix(name, "wg"), strings.HasPrefix(name, "vmnet"),
		strings.HasPrefix(name, "cni"), strings.HasPrefix(name, "flannel"),
		strings.HasPrefix(name, "kube"), strings.HasPrefix(name, "zt"):
		return true
	}
	return false
}

func ifaceUp(name string) bool {
	b, err := os.ReadFile("/sys/class/net/" + name + "/operstate")
	return err == nil && strings.TrimSpace(string(b)) == "up"
}

func ifaceSpeed(name string) int {
	b, err := os.ReadFile("/sys/class/net/" + name + "/speed")
	if err != nil {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// ---------- temperaturen ----------

func readTemps() []Temp {
	out := []Temp{}
	dirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, d := range dirs {
		chip := readTrim(filepath.Join(d, "name"))
		inputs, _ := filepath.Glob(filepath.Join(d, "temp*_input"))
		sort.Strings(inputs)
		for _, in := range inputs {
			mv := readFloat(in)
			if mv == 0 {
				continue
			}
			base := strings.TrimSuffix(in, "_input")
			label := readTrim(base + "_label")
			key := filepath.Base(base)
			if label == "" {
				label = chip + " " + key
			}
			t := Temp{
				Key:      chip + "/" + key,
				Label:    label,
				Chip:     chip,
				Celsius:  mv / 1000,
				High:     sanitizeThreshold(readFloat(base+"_max") / 1000),
				Critical: sanitizeThreshold(readFloat(base+"_crit") / 1000),
			}
			t.Status = tempStatus(t)
			out = append(out, t)
		}
	}
	if len(out) == 0 { // fallback voor ARM/SBC zonder hwmon
		zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
		for _, z := range zones {
			mv := readFloat(filepath.Join(z, "temp"))
			if mv == 0 {
				continue
			}
			typ := readTrim(filepath.Join(z, "type"))
			t := Temp{Key: "thermal/" + filepath.Base(z), Label: typ, Chip: "thermal", Celsius: mv / 1000}
			t.Status = tempStatus(t)
			out = append(out, t)
		}
	}
	markPrimaryTemp(out)
	return out
}

// sanitizeThreshold gooit onzinwaarden weg. Sommige NVMe-schijven rapporteren
// temp*_max als 65261850 (= 65261 °C) wanneer er geen drempel is ingesteld;
// die klakkeloos overnemen betekent dat een schijf van 86 °C als "ok" geldt.
func sanitizeThreshold(c float64) float64 {
	if c <= 0 || c > 150 {
		return 0
	}
	return c
}

func tempStatus(t Temp) string {
	switch {
	case t.Critical > 0 && t.Celsius >= t.Critical:
		return "crit"
	case t.High > 0 && t.Celsius >= t.High:
		return "warn"
	// Ruim vóór de kritieke grens waarschuwen: bij een kritieke waarde van
	// 100 °C is 85 °C al reden om te kijken, niet pas 99 °C.
	case t.Critical > 0 && t.Celsius >= t.Critical*0.85:
		return "warn"
	case t.High > 0 && t.Celsius >= t.High*0.9:
		return "warn"
	case t.Critical == 0 && t.High == 0 && t.Celsius >= 80:
		return "warn"
	}
	return "ok"
}

// markPrimaryTemp kiest één temperatuur voor het hoofdscherm. coretemp/k10temp
// Package gaat voor, daarna cpu_thermal, daarna acpitz, anders de hoogste.
func markPrimaryTemp(ts []Temp) {
	best, score := -1, -1
	for i, t := range ts {
		s := 0
		switch {
		case (t.Chip == "coretemp" || t.Chip == "k10temp") && strings.Contains(strings.ToLower(t.Label), "package"):
			s = 100
		case t.Chip == "coretemp" || t.Chip == "k10temp":
			s = 80
		case strings.Contains(t.Chip, "cpu_thermal"):
			s = 60
		case t.Chip == "acpitz":
			s = 40
		default:
			s = 10
		}
		if s > score {
			best, score = i, s
		}
	}
	if best >= 0 {
		ts[best].Primary = true
	}
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readFloat(p string) float64 {
	s := readTrim(p)
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
