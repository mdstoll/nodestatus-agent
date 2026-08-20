package collect

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OSInfo struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	ID             string `json:"id"`
	Kernel         string `json:"kernel"`
	Arch           string `json:"arch"`
	Virtualization string `json:"virtualization,omitempty"`
}

type ModelInfo struct {
	Vendor  string `json:"vendor"`
	Product string `json:"product"`
	Board   string `json:"board,omitempty"`
}

type CPUInfo struct {
	Model         string   `json:"model"`
	Vendor        string   `json:"vendor"`
	CoresPhysical int      `json:"cores_physical"`
	Threads       int      `json:"threads"`
	Sockets       int      `json:"sockets"`
	MaxMHz        int      `json:"max_mhz,omitempty"`
	CacheL3Bytes  uint64   `json:"cache_l3_bytes,omitempty"`
	FlagsNotable  []string `json:"flags_notable,omitempty"`
	Governor      string   `json:"governor,omitempty"`
}

type SystemInfo struct {
	Hostname          string    `json:"hostname"`
	DisplayName       string    `json:"display_name"`
	OS                OSInfo    `json:"os"`
	Model             ModelInfo `json:"model"`
	CPU               CPUInfo   `json:"cpu"`
	MemoryTotal       uint64    `json:"memory_total"`
	SwapTotal         uint64    `json:"swap_total"`
	StorageTotalBytes uint64    `json:"storage_total_bytes"`
	BootTime          int64     `json:"boot_time"`
	UptimeS           float64   `json:"uptime_s"`
	Capabilities      []string  `json:"capabilities"`
	AgentVersion      string    `json:"agent_version"`
	GeneratedAt       float64   `json:"generated_at"`
}

var (
	staticMu   sync.Mutex
	staticVal  *SystemInfo
	staticTime time.Time
)

// System levert de statische informatie met een cache van 60 seconden.
func System(displayName, version string, caps []string) *SystemInfo {
	staticMu.Lock()
	defer staticMu.Unlock()
	if staticVal != nil && time.Since(staticTime) < time.Minute {
		s := *staticVal
		s.UptimeS = uptimeSeconds()
		s.GeneratedAt = float64(time.Now().Unix())
		return &s
	}
	host, _ := os.Hostname()
	if displayName == "" {
		displayName = host
	}
	m := readMeminfo()
	var storTotal uint64
	for _, s := range storageSamples(nil) {
		if !s.Remote {
			storTotal += s.Total
		}
	}
	si := &SystemInfo{
		Hostname:          host,
		DisplayName:       displayName,
		OS:                osInfo(),
		Model:             modelInfo(),
		CPU:               cpuInfo(),
		MemoryTotal:       m["MemTotal"],
		SwapTotal:         m["SwapTotal"],
		StorageTotalBytes: storTotal,
		BootTime:          bootTime(),
		UptimeS:           uptimeSeconds(),
		Capabilities:      caps,
		AgentVersion:      version,
		GeneratedAt:       float64(time.Now().Unix()),
	}
	staticVal, staticTime = si, time.Now()
	s := *si
	return &s
}

func osInfo() OSInfo {
	o := OSInfo{Arch: runtime.GOARCH}
	if f, err := os.Open("/etc/os-release"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), "=")
			if !ok {
				continue
			}
			v = strings.Trim(v, `"`)
			switch k {
			case "NAME":
				o.Name = v
			case "VERSION":
				o.Version = v
			case "VERSION_ID":
				if o.Version == "" {
					o.Version = v
				}
			case "ID":
				o.ID = v
			}
		}
	}
	o.Kernel = readTrim("/proc/sys/kernel/osrelease")
	if b, err := exec.Command("/usr/bin/systemd-detect-virt").Output(); err == nil {
		if v := strings.TrimSpace(string(b)); v != "" && v != "none" {
			o.Virtualization = v
		}
	}
	return o
}

func modelInfo() ModelInfo {
	return ModelInfo{
		Vendor:  readTrim("/sys/class/dmi/id/sys_vendor"),
		Product: readTrim("/sys/class/dmi/id/product_name"),
		Board:   readTrim("/sys/class/dmi/id/board_name"),
	}
}

var notableFlags = []string{"avx", "avx2", "avx512f", "aes", "sha_ni", "vmx", "svm", "sse4_2", "rdrand"}

func cpuInfo() CPUInfo {
	c := CPUInfo{Threads: runtime.NumCPU(), Sockets: 1}
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return c
	}
	defer f.Close()
	cores := map[string]bool{}
	sockets := map[string]bool{}
	var physID, coreID string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "model name", "Model":
			if c.Model == "" {
				c.Model = v
			}
		case "vendor_id":
			if c.Vendor == "" {
				c.Vendor = v
			}
		case "physical id":
			physID = v
			sockets[v] = true
		case "core id":
			coreID = v
			cores[physID+"/"+coreID] = true
		case "flags", "Features":
			if len(c.FlagsNotable) == 0 {
				have := map[string]bool{}
				for _, fl := range strings.Fields(v) {
					have[fl] = true
				}
				for _, want := range notableFlags {
					if have[want] {
						c.FlagsNotable = append(c.FlagsNotable, want)
					}
				}
			}
		}
	}
	if len(cores) > 0 {
		c.CoresPhysical = len(cores)
	} else {
		c.CoresPhysical = c.Threads
	}
	if len(sockets) > 0 {
		c.Sockets = len(sockets)
	}
	if c.Model == "" {
		c.Model = readTrim("/sys/firmware/devicetree/base/model")
	}
	if v := readTrim("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"); v != "" {
		khz, _ := strconv.Atoi(v)
		c.MaxMHz = khz / 1000
	}
	c.Governor = readTrim("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor")
	if v := readTrim("/sys/devices/system/cpu/cpu0/cache/index3/size"); v != "" {
		c.CacheL3Bytes = parseSize(v)
	}
	return c
}

func parseSize(s string) uint64 {
	s = strings.TrimSpace(s)
	mult := uint64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1024, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1024*1024, strings.TrimSuffix(s, "M")
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v * mult
}

// bootTime leest btime uit /proc/stat: betrouwbaarder dan uptime aftrekken.
func bootTime() int64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n
		}
	}
	return 0
}

func uptimeSeconds() float64 {
	fs := strings.Fields(readTrim("/proc/uptime"))
	if len(fs) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fs[0], 64)
	return v
}

// IdleSeconds is de som van idle-tijd over alle cores (tweede veld /proc/uptime).
func IdleSeconds() float64 {
	fs := strings.Fields(readTrim("/proc/uptime"))
	if len(fs) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(fs[1], 64)
	return v
}

func BootTime() int64        { return bootTime() }
func UptimeSeconds() float64 { return uptimeSeconds() }
func CPUStatic() CPUInfo     { return cpuInfo() }
