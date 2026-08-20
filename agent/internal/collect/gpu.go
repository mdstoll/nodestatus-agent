package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// gpuCache houdt GPU-data apart bij: nvidia-smi kost ~80 ms en mag daarom niet
// in de 1 Hz-loop staan.
type gpuCache struct {
	mu         sync.RWMutex
	gpus       []GPU
	last       time.Time
	nvidia     string
	interval   time.Duration
	refreshing bool
}

func newGPUCache(enabled bool) *gpuCache {
	// 15 seconden: intel_gpu_top heeft vier seconden nodig per meting, dus
	// vaker pollen betekent dat er vrijwel permanent een sudo-proces draait.
	// Dat kostte op de testmachine bijna twee minuten CPU per uur.
	g := &gpuCache{interval: 15 * time.Second}
	if enabled {
		g.nvidia, _ = exec.LookPath("nvidia-smi")
	}
	return g
}

// get blokkeert nooit. GPU-informatie ophalen kost tot enkele seconden
// (intel_gpu_top moet een meetperiode afwachten); dat mag de 1 Hz-loop niet
// ophouden. Bij verouderde data wordt op de achtergrond ververst en krijgt de
// aanroeper alvast de vorige waarde.
func (g *gpuCache) get() []GPU {
	g.mu.RLock()
	stale := time.Since(g.last) >= g.interval
	out := g.gpus
	busy := g.refreshing
	g.mu.RUnlock()

	if stale && !busy {
		g.mu.Lock()
		if !g.refreshing {
			g.refreshing = true
			go func() {
				gpus := g.collect()
				g.mu.Lock()
				g.gpus, g.last, g.refreshing = gpus, time.Now(), false
				g.mu.Unlock()
			}()
		}
		g.mu.Unlock()
	}
	return out
}

func (g *gpuCache) collect() []GPU {
	if g.nvidia != "" {
		if out := g.nvidiaGPUs(); len(out) > 0 {
			return out
		}
	}
	if out := amdGPUs(); len(out) > 0 {
		return out
	}
	return intelGPUs()
}

func (g *gpuCache) nvidiaGPUs() []GPU {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.nvidia,
		"--query-gpu=index,name,driver_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,fan.speed,clocks.sm",
		"--format=csv,noheader,nounits")
	b, err := cmd.Output()
	if err != nil {
		return nil
	}
	var out []GPU
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 10 {
			continue
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
		idx, _ := strconv.Atoi(f[0])
		out = append(out, GPU{
			Index: idx, Vendor: "NVIDIA", Name: f[1], Driver: f[2],
			UtilPercent: num(f[3]),
			MemUsed:     uint64(num(f[4])) * 1024 * 1024,
			MemTotal:    uint64(num(f[5])) * 1024 * 1024,
			TempC:       num(f[6]), PowerW: num(f[7]),
			FanPercent: num(f[8]), ClockMHz: int(num(f[9])),
		})
	}
	return out
}

// intelGPUs leest een geïntegreerde Intel-GPU. De frequenties komen uit
// sysfs en werken altijd; belasting per motor en vermogen vereisen
// intel_gpu_top met CAP_PERFMON, dus die gaan via sudo (zie sudoers).
func intelGPUs() []GPU {
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]")
	for i, card := range cards {
		maxFreq := readTrim(filepath.Join(card, "gt_max_freq_mhz"))
		if maxFreq == "" {
			continue // geen i915/xe
		}
		g := GPU{
			Index: i, Vendor: "Intel", Name: intelName(card),
			SharedMem: true,
		}
		g.ClockMaxMHz, _ = strconv.Atoi(maxFreq)
		if v := readTrim(filepath.Join(card, "gt_act_freq_mhz")); v != "" {
			g.ClockMHz, _ = strconv.Atoi(v)
		}
		sample, topErr := intelTop()
		if s := sample; s != nil {
			g.UtilPercent = s.util
			g.PowerW = s.powerGPU
			g.Engines = s.engines
			if s.freq > 0 {
				g.ClockMHz = int(s.freq)
			}
		} else {
			g.Note = "geschat uit de klokfrequentie"
			if topErr != nil {
				g.Note = "geschat uit de klokfrequentie — " + topErr.Error()
			}
			// Zonder PMU is de klokfrequentie de beste indicatie: de GPU
			// klokt terug naar 0 zodra hij idle is (rc6).
			if g.ClockMaxMHz > 0 {
				g.UtilPercent = float64(g.ClockMHz) / float64(g.ClockMaxMHz) * 100
			}
		}
		// Gedeeld geheugen: een iGPU heeft geen eigen VRAM.
		return []GPU{g}
	}
	return nil
}

// intelName leidt een leesbare naam af uit de PCI-device-id. Het label uit
// sysfs is bewust de laatste keus: dat is de DMI-slotnaam ("Onboard - Video")
// en zegt niets over welke GPU het is.
func intelName(card string) string {
	switch readTrim(filepath.Join(card, "device", "device")) {
	case "0x46d0", "0x46d1", "0x46d2", "0x46d3", "0x46d4":
		return "Intel UHD Graphics (Alder Lake-N)"
	case "0x4680", "0x4682", "0x4688", "0x468a", "0x4690", "0x4692", "0x4693":
		return "Intel UHD Graphics (Alder Lake-S)"
	case "0x9a49", "0x9a40", "0x9a59", "0x9a60", "0x9a68", "0x9a70", "0x9a78":
		return "Intel Iris Xe Graphics"
	case "0xa780", "0xa781", "0xa782", "0xa783", "0xa788", "0xa789":
		return "Intel UHD Graphics (Raptor Lake)"
	case "0x7d55", "0x7dd5", "0x7d45":
		return "Intel Arc Graphics (Meteor Lake)"
	}
	if n := readTrim(filepath.Join(card, "device", "label")); n != "" {
		return "Intel Graphics · " + n
	}
	return "Intel Graphics"
}

type intelSample struct {
	util     float64
	freq     float64
	powerGPU float64
	engines  []GPUEngine
}

// intelTop draait intel_gpu_top kort en pakt het laatste volledige blok.
// Het eerste blok heeft een korte meetperiode en staat altijd op nul.
func intelTop() (*intelSample, error) {
	path, ok := lookIntelTop()
	if !ok {
		return nil, fmt.Errorf("intel_gpu_top niet geïnstalleerd")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("sudo ontbreekt")
	}
	cmd := exec.CommandContext(ctx, sudo, "-n", path, "-J", "-s", "600")
	cmd.Env = []string{"LC_ALL=C", "PATH="}
	// intel_gpu_top stopt uit zichzelf niet, dus we kappen hem af. Bewust met
	// SIGTERM en niet met het standaard SIGKILL: sudo draait het commando in
	// een pty en moet de kans krijgen die buffer te legen, anders komt er geen
	// byte terug.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	s := parseIntelTop(stdout.Bytes())
	if s == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = fmt.Sprintf("geen bruikbare uitvoer (%d bytes, %v)", stdout.Len(), runErr)
		}
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return s, nil
}

var intelTopPath struct {
	once sync.Once
	path string
	ok   bool
}

func lookIntelTop() (string, bool) {
	intelTopPath.once.Do(func() {
		for _, p := range []string{"/usr/bin/intel_gpu_top", "/usr/local/bin/intel_gpu_top"} {
			if _, err := exec.LookPath(p); err == nil {
				intelTopPath.path, intelTopPath.ok = p, true
				return
			}
		}
	})
	return intelTopPath.path, intelTopPath.ok
}

// parseIntelTop pakt het laatste volledige blok uit de uitvoer.
// intel_gpu_top schrijft een array die nooit wordt afgesloten en waarvan de
// blokken door komma's worden gescheiden; een json.Decoder loopt daarop vast.
// Daarom knippen we de blokken zelf op haakjesniveau uit.
func parseIntelTop(out []byte) *intelSample {
	blocks := jsonObjects(string(out))
	var last *intelSample
	for _, raw := range blocks {
		var blk struct {
			Frequency struct {
				Actual float64 `json:"actual"`
			} `json:"frequency"`
			RC6 struct {
				Value float64 `json:"value"`
			} `json:"rc6"`
			Power struct {
				GPU float64 `json:"GPU"`
			} `json:"power"`
			Engines map[string]struct {
				Busy float64 `json:"busy"`
			} `json:"engines"`
		}
		if json.Unmarshal([]byte(raw), &blk) != nil {
			continue
		}
		sample := &intelSample{freq: blk.Frequency.Actual, powerGPU: blk.Power.GPU}
		for name, e := range blk.Engines {
			sample.engines = append(sample.engines, GPUEngine{Name: name, Busy: e.Busy})
			if e.Busy > sample.util {
				sample.util = e.Busy
			}
		}
		sort.Slice(sample.engines, func(i, j int) bool {
			return sample.engines[i].Name < sample.engines[j].Name
		})
		// rc6 is het percentage van de tijd dat de GPU sliep; 100 - rc6 is
		// een goede totaalindicatie als geen enkele motor druk is.
		if idle := blk.RC6.Value; idle > 0 && sample.util == 0 {
			sample.util = 100 - idle
			if sample.util < 0 {
				sample.util = 0
			}
		}
		last = sample
	}
	return last
}

// jsonObjects knipt alle complete { … }-blokken uit een tekst, met respect
// voor haakjes binnen strings en escapes.
func jsonObjects(s string) []string {
	var out []string
	depth, start := 0, -1
	inString, escaped := false, false
	for i, c := range s {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				out = append(out, s[start:i+1])
				start = -1
			}
		}
	}
	return out
}

// amdGPUs leest de AMD-sysfs. Werkt zonder extra tooling.
func amdGPUs() []GPU {
	var out []GPU
	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]/device")
	for i, dev := range cards {
		busy := readTrim(filepath.Join(dev, "gpu_busy_percent"))
		if busy == "" {
			continue
		}
		u, _ := strconv.ParseFloat(busy, 64)
		used, _ := strconv.ParseUint(readTrim(filepath.Join(dev, "mem_info_vram_used")), 10, 64)
		total, _ := strconv.ParseUint(readTrim(filepath.Join(dev, "mem_info_vram_total")), 10, 64)
		g := GPU{Index: i, Vendor: "AMD", Name: "AMD GPU", UtilPercent: u, MemUsed: used, MemTotal: total}
		if hw, _ := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*")); len(hw) > 0 {
			g.TempC = readFloat(filepath.Join(hw[0], "temp1_input")) / 1000
			g.PowerW = readFloat(filepath.Join(hw[0], "power1_average")) / 1e6
		}
		out = append(out, g)
	}
	return out
}
