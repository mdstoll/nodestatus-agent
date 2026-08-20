package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------- uptime ----------

type Boot struct {
	Index int    `json:"index"`
	Start string `json:"start"`
	Stop  string `json:"stop,omitempty"`
}

type UptimeInfo struct {
	UptimeS   float64 `json:"uptime_s"`
	BootTime  int64   `json:"boot_time"`
	IdleS     float64 `json:"idle_s"`
	BusyRatio float64 `json:"busy_ratio"`
	CPUTime   struct {
		User   float64 `json:"user"`
		System float64 `json:"system"`
		IOWait float64 `json:"iowait"`
		Idle   float64 `json:"idle"`
	} `json:"cpu_time"`
	Boots []Boot `json:"boots"`
}

func Uptime(ctx context.Context, uptimeS float64, bootTime int64, idleS float64, cores int) UptimeInfo {
	u := UptimeInfo{UptimeS: uptimeS, BootTime: bootTime, IdleS: idleS, Boots: []Boot{}}
	if uptimeS > 0 && cores > 0 {
		busy := 1 - idleS/(uptimeS*float64(cores))
		if busy < 0 {
			busy = 0
		}
		u.BusyRatio = busy
	}
	// Verdeling van CPU-tijd sinds boot uit /proc/stat.
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 6 || f[0] != "cpu" {
				continue
			}
			n := func(i int) float64 { v, _ := strconv.ParseFloat(f[i], 64); return v }
			user, nice, sys, idle, iow := n(1), n(2), n(3), n(4), n(5)
			tot := user + nice + sys + idle + iow
			if tot > 0 {
				u.CPUTime.User = (user + nice) / tot * 100
				u.CPUTime.System = sys / tot * 100
				u.CPUTime.IOWait = iow / tot * 100
				u.CPUTime.Idle = idle / tot * 100
			}
			break
		}
	}
	if b, err := Run(ctx, "journalctl", "--list-boots", "--no-pager", "-o", "short-iso"); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		for _, line := range lines {
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			idx, err := strconv.Atoi(f[0])
			if err != nil {
				continue
			}
			bt := Boot{Index: idx}
			if len(f) >= 4 {
				bt.Start = f[2] + " " + f[3]
			}
			u.Boots = append(u.Boots, bt)
		}
	}
	return u
}

// ---------- locale & regio ----------

type LocaleInfo struct {
	LocaleIdentifier string   `json:"locale_identifier"`
	Language         string   `json:"language"`
	Region           string   `json:"region"`
	PreferredLangs   []string `json:"preferred_languages"`
	KeyboardLayout   string   `json:"keyboard_layout,omitempty"`
	TimeZone         string   `json:"time_zone"`
	UTCOffset        string   `json:"utc_offset"`
	LocalTime        string   `json:"local_time"`
	NTPSynchronized  bool     `json:"ntp_synchronized"`
	RTCInLocalTZ     bool     `json:"rtc_in_local_tz"`
	Calendar         string   `json:"calendar"`
	FirstDayOfWeek   string   `json:"first_day_of_week"`
	HourCycle        string   `json:"hour_cycle"`
}

func Locale(ctx context.Context) LocaleInfo {
	l := LocaleInfo{Calendar: "Gregorian", FirstDayOfWeek: "Monday", HourCycle: "24-hour", PreferredLangs: []string{}}
	kv := map[string]string{}
	if b, err := Run(ctx, "localectl", "status"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok {
				kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	if v, ok := kv["System Locale"]; ok {
		l.LocaleIdentifier = strings.TrimPrefix(v, "LANG=")
	}
	if l.LocaleIdentifier == "" {
		l.LocaleIdentifier = firstNonEmpty(readTrim("/etc/default/locale"), os.Getenv("LANG"), "C")
		l.LocaleIdentifier = strings.TrimPrefix(strings.Split(l.LocaleIdentifier, "\n")[0], "LANG=")
	}
	l.LocaleIdentifier = strings.Trim(l.LocaleIdentifier, `"`)
	base := strings.SplitN(l.LocaleIdentifier, ".", 2)[0]
	if lang, reg, ok := strings.Cut(base, "_"); ok {
		l.Language, l.Region = lang, reg
	} else {
		l.Language = base
	}
	l.PreferredLangs = append(l.PreferredLangs, base)
	l.KeyboardLayout = firstNonEmpty(kv["X11 Layout"], kv["VC Keymap"])

	if b, err := Run(ctx, "timedatectl", "show"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "Timezone":
				l.TimeZone = v
			case "NTPSynchronized":
				l.NTPSynchronized = v == "yes"
			case "LocalRTC":
				l.RTCInLocalTZ = v == "yes"
			}
		}
	}
	if l.TimeZone == "" {
		l.TimeZone = firstNonEmpty(readTrim("/etc/timezone"), "UTC")
	}
	now := time.Now()
	_, off := now.Zone()
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	l.UTCOffset = "UTC" + sign + strconv.Itoa(off/3600)
	if m := (off % 3600) / 60; m != 0 {
		l.UTCOffset += ":" + strconv.Itoa(m)
	}
	l.LocalTime = now.Format("2006-01-02 15:04:05")
	return l
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ---------- apt updates ----------

type Package struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
	Security  bool   `json:"security"`
}

type UpdatesInfo struct {
	Upgradable         int       `json:"upgradable"`
	Security           int       `json:"security"`
	RebootRequired     bool      `json:"reboot_required"`
	RebootRequiredPkgs []string  `json:"reboot_required_pkgs"`
	UnattendedEnabled  bool      `json:"unattended_upgrades"`
	LastAptUpdate      int64     `json:"last_apt_update,omitempty"`
	Packages           []Package `json:"packages"`
	Error              string    `json:"error,omitempty"`
}

var upgradableRe = regexp.MustCompile(`^([^/\s]+)/(\S+)\s+(\S+)\s+\S+\s+\[upgradable from:\s*([^\]]+)\]`)

// Updates leest alleen de bestaande apt-cache. De agent draait nooit zelf
// `apt-get update`: dat verandert systeemtoestand en vereist een lock.
func Updates(ctx context.Context) UpdatesInfo {
	u := UpdatesInfo{RebootRequiredPkgs: []string{}, Packages: []Package{}}
	if b, err := os.ReadFile("/var/run/reboot-required.pkgs"); err == nil {
		u.RebootRequired = true
		for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if l != "" {
				u.RebootRequiredPkgs = append(u.RebootRequiredPkgs, l)
			}
		}
	} else if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		u.RebootRequired = true
	}
	if b, err := os.ReadFile("/etc/apt/apt.conf.d/20auto-upgrades"); err == nil {
		u.UnattendedEnabled = strings.Contains(string(b), `Unattended-Upgrade "1"`)
	}
	for _, p := range []string{"/var/lib/apt/periodic/update-success-stamp", "/var/lib/apt/lists"} {
		if fi, err := os.Stat(p); err == nil {
			u.LastAptUpdate = fi.ModTime().Unix()
			break
		}
	}
	b, err := Run(ctx, "apt", "list", "--upgradable")
	if err != nil {
		if b, err = Run(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade"); err != nil {
			u.Error = "apt niet beschikbaar"
			return u
		}
		return parseAptSimulate(string(b), u)
	}
	for _, line := range strings.Split(string(b), "\n") {
		m := upgradableRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		sec := strings.Contains(m[2], "-security") || strings.Contains(m[2], "Security")
		u.Packages = append(u.Packages, Package{Name: m[1], Current: m[4], Candidate: m[3], Security: sec})
		u.Upgradable++
		if sec {
			u.Security++
		}
	}
	sort.Slice(u.Packages, func(i, j int) bool {
		if u.Packages[i].Security != u.Packages[j].Security {
			return u.Packages[i].Security
		}
		return u.Packages[i].Name < u.Packages[j].Name
	})
	return u
}

func parseAptSimulate(out string, u UpdatesInfo) UpdatesInfo {
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "Inst" {
			u.Packages = append(u.Packages, Package{Name: f[1]})
			u.Upgradable++
		}
	}
	return u
}

// ---------- logs ----------

type LogSource struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"` // unit | file
	Available bool   `json:"available"`
}

type LogLine struct {
	T        float64 `json:"t"`
	Priority int     `json:"priority"`
	Unit     string  `json:"unit,omitempty"`
	PID      int     `json:"pid,omitempty"`
	Message  string  `json:"message"`
}

func LogSources(ctx context.Context, units, files []string) []LogSource {
	out := []LogSource{}
	// Alleen units tonen die daadwerkelijk op deze machine bestaan. Anders
	// staat de lijst vol met nginx en docker op een server waar ze niet
	// draaien, en dat maakt de lijst waardeloos.
	existing := installedUnits(ctx)
	for _, u := range units {
		out = append(out, LogSource{
			ID: "unit:" + u, Label: u, Kind: "unit",
			Available: existing[u] || existing[u+".service"],
		})
	}
	for _, f := range files {
		_, err := os.Stat(f)
		out = append(out, LogSource{ID: "file:" + f, Label: f, Kind: "file", Available: err == nil})
	}
	return out
}

// installedUnits geeft de systemd-units die op deze machine bestaan.
func installedUnits(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	b, err := Run(ctx, "systemctl", "list-units", "--type=service", "--all",
		"--no-legend", "--no-pager", "--plain")
	if err != nil {
		// systemctl niet gevonden: liever alles tonen dan niets.
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		name := f[0]
		out[name] = true
		out[strings.TrimSuffix(name, ".service")] = true
	}
	return out
}

// Logs haalt regels op. De bron moet exact in de whitelist staan — geen
// prefix-match, geen path-joins met gebruikersinvoer.
func Logs(ctx context.Context, source string, lines int, since, priority, query string, units, files []string) ([]LogLine, error) {
	out := []LogLine{}
	lines = ClampInt(lines, 1, 500, 200)
	if unit, ok := strings.CutPrefix(source, "unit:"); ok {
		if !inList(unit, units) {
			return nil, errNotAllowed
		}
		args := []string{"-u", unit, "-n", strconv.Itoa(lines), "-o", "json", "--no-pager"}
		if since != "" && validSince(since) {
			args = append(args, "--since", "-"+since)
		}
		if p := priorityNum(priority); p >= 0 {
			args = append(args, "-p", strconv.Itoa(p))
		}
		if query != "" && len(query) < 100 {
			args = append(args, "-g", regexp.QuoteMeta(query))
		}
		b, err := Run(ctx, "journalctl", args...)
		if err != nil {
			return out, nil
		}
		return parseJournal(b), nil
	}
	if path, ok := strings.CutPrefix(source, "file:"); ok {
		if !inList(path, files) {
			return nil, errNotAllowed
		}
		return tailFile(path, lines, query)
	}
	return nil, errNotAllowed
}

var errNotAllowed = &notAllowedError{}

type notAllowedError struct{}

func (e *notAllowedError) Error() string { return "deze logbron staat niet op de whitelist" }

func inList(v string, list []string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func validSince(s string) bool {
	return regexp.MustCompile(`^\d{1,3}[mhd]$`).MatchString(s)
}

func priorityNum(p string) int {
	switch strings.ToLower(p) {
	case "emerg":
		return 0
	case "alert":
		return 1
	case "crit":
		return 2
	case "err", "error":
		return 3
	case "warning", "warn":
		return 4
	case "notice":
		return 5
	case "info":
		return 6
	case "debug":
		return 7
	}
	return -1
}

func parseJournal(b []byte) []LogLine {
	out := []LogLine{}
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e map[string]any
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		l := LogLine{Priority: 6}
		if v, ok := e["__REALTIME_TIMESTAMP"].(string); ok {
			us, _ := strconv.ParseInt(v, 10, 64)
			l.T = float64(us) / 1e6
		}
		if v, ok := e["PRIORITY"].(string); ok {
			l.Priority, _ = strconv.Atoi(v)
		}
		if v, ok := e["_SYSTEMD_UNIT"].(string); ok {
			l.Unit = strings.TrimSuffix(v, ".service")
		}
		if v, ok := e["_PID"].(string); ok {
			l.PID, _ = strconv.Atoi(v)
		}
		switch m := e["MESSAGE"].(type) {
		case string:
			l.Message = m
		case []any:
			bs := make([]byte, 0, len(m))
			for _, x := range m {
				if f, ok := x.(float64); ok {
					bs = append(bs, byte(f))
				}
			}
			l.Message = string(bs)
		}
		out = append(out, l)
	}
	return out
}

func tailFile(path string, n int, query string) ([]LogLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		t := sc.Text()
		if query != "" && !strings.Contains(strings.ToLower(t), strings.ToLower(query)) {
			continue
		}
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, t)
	}
	out := make([]LogLine, 0, len(ring))
	for _, l := range ring {
		out = append(out, LogLine{Priority: 6, Message: l})
	}
	return out, nil
}

// ---------- processen ----------

type Process struct {
	PID     int     `json:"pid"`
	Name    string  `json:"name"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu_percent"`
	RSS     uint64  `json:"rss"`
	MemPct  float64 `json:"mem_percent"`
	State   string  `json:"state"`
	Threads int     `json:"threads"`
}

// Processes leest /proc rechtstreeks en berekent CPU% over een venster van
// 300 ms, zodat het een momentaan percentage is en niet het gemiddelde
// sinds het proces startte.
func Processes(memTotal uint64) []Process {
	first := procCPUSnapshot()
	time.Sleep(300 * time.Millisecond)
	second := procCPUSnapshot()
	hz := float64(100) // USER_HZ
	elapsed := 0.3

	out := []Process{}
	for pid, s2 := range second {
		s1, ok := first[pid]
		if !ok {
			continue
		}
		cpu := float64(s2.ticks-s1.ticks) / hz / elapsed * 100
		if cpu < 0 {
			cpu = 0
		}
		p := Process{PID: pid, Name: s2.name, User: s2.user, CPU: round2(cpu), RSS: s2.rss, State: s2.state, Threads: s2.threads}
		if memTotal > 0 {
			p.MemPct = round2(float64(s2.rss) / float64(memTotal) * 100)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].RSS > out[j].RSS
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

type procSnap struct {
	ticks   uint64
	name    string
	user    string
	rss     uint64
	state   string
	threads int
}

func procCPUSnapshot() map[int]procSnap {
	out := map[int]procSnap{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	pageSize := uint64(os.Getpagesize())
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if open < 0 || close < 0 || close < open {
			continue
		}
		name := s[open+1 : close]
		f := strings.Fields(s[close+2:])
		if len(f) < 22 {
			continue
		}
		n := func(i int) uint64 { v, _ := strconv.ParseUint(f[i], 10, 64); return v }
		snap := procSnap{
			ticks:   n(11) + n(12), // utime + stime (0-based na state)
			name:    name,
			state:   f[0],
			threads: int(n(17)),
			rss:     n(21) * pageSize,
		}
		if st, err := os.Stat("/proc/" + e.Name()); err == nil {
			snap.user = ownerName(st)
		}
		out[pid] = snap
	}
	return out
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
