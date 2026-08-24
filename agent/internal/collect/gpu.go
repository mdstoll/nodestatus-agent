package collect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	// Intel komt uit een doorlopende stream en kost per uitlezing niets, dus
	// hier mag het snel. Alleen nvidia-smi is een echte exec (~80 ms) en die
	// wordt op de achtergrond ververst.
	g := &gpuCache{interval: 1 * time.Second}
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
		sample, topErr := intelMon.sample()
		if sample != nil {
			g.UtilPercent = sample.util
			g.PowerW = sample.powerGPU
			g.Engines = sample.engines
			if sample.freq > 0 {
				g.ClockMHz = int(sample.freq)
			}
		} else {
			// Nog geen meting (het proces warmt op) of hij lukt niet. De
			// klokfrequentie uit sysfs is dan de beste indicatie: de GPU
			// klokt terug naar nul zodra hij idle is.
			g.Note = "estimated from the clock frequency"
			if topErr != "" {
				g.Note = "estimated from the clock frequency — " + topErr
			}
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

// intelMonitor houdt één doorlopend intel_gpu_top-proces aan zolang er naar de
// GPU gekeken wordt. De vorige aanpak — elke 15 seconden een nieuw proces van
// vier seconden starten — was zowel te traag (de waarde stond een kwartier
// stil tijdens een conversie) als te duur (er draaide vrijwel permanent een
// sudo-proces). Eén stream levert elke ~700 ms een verse meting voor de prijs
// van één proces.
type intelMonitor struct {
	mu       sync.Mutex
	latest   *intelSample
	latestAt time.Time
	lastUse  time.Time
	running  bool
	lastErr  string
}

// idleStop bepaalt hoe lang het proces blijft draaien nadat er niemand meer
// naar kijkt. Kort genoeg om een idle server met rust te laten, lang genoeg
// om niet te herstarten tussen twee schermen door.
const gpuIdleStop = 45 * time.Second

// intelTopInterval is het meetinterval in milliseconden. Deze waarde staat
// ook letterlijk in de sudoers-regel: die pint de argumenten, dus een andere
// waarde hier betekent "sudo: a password is required" en een GPU die op nul
// blijft staan. Daarom genereert de agent die regel zelf — zie SudoersRules.
const intelTopInterval = "600"

var intelMon = &intelMonitor{}

// SudoersRules geeft de regels die de agent nodig heeft, met exact de
// argumenten die hij ook echt gebruikt. install.sh schrijft ze hieruit weg,
// zodat code en sudoers niet uit elkaar kunnen lopen.
func SudoersRules(user string) []string {
	var out []string
	for _, p := range []string{"/usr/sbin/smartctl", "/usr/bin/smartctl"} {
		if _, err := exec.LookPath(p); err == nil {
			// Deze patronen moeten dekken wat tools.blockDevices() teruggeeft
			// (alles in /sys/block behalve loop/ram/zram/dm-/md/sr). Ontbreekt
			// er een, dan vraagt de agent smartctl op een schijf die sudo niet
			// toestaat en krijgt de gebruiker "permission denied" in plaats van een
			// nette "not supported" — precies wat er op een Raspberry Pi
			// gebeurde, die van /dev/mmcblk0 boot.
			out = append(out, user+" ALL=(root) NOPASSWD: "+
				p+" -j -A -H -i /dev/sd[a-z], "+
				p+" -j -A -H -i /dev/nvme[0-9]n[0-9], "+
				p+" -j -A -H -i /dev/vd[a-z], "+
				p+" -j -A -H -i /dev/mmcblk[0-9], "+
				p+" -j -A -H -i /dev/hd[a-z]")
			break
		}
	}
	if p, ok := lookIntelTop(); ok {
		out = append(out, user+" ALL=(root) NOPASSWD: "+p+" -J -s "+intelTopInterval)
	}
	// RAPL: energy_uj is root-only (a 2020 kernel mitigation against a power-
	// side-channel attack) while the rest of the powercap tree stays
	// world-readable. One wildcarded cat rule covers however many domains
	// this CPU exposes (package, psys, per-socket, ...) without needing to
	// know the exact count or names in advance.
	if matches, _ := filepath.Glob("/sys/class/powercap/intel-rapl:[0-9]*/energy_uj"); len(matches) > 0 {
		if p, err := exec.LookPath("/usr/bin/cat"); err == nil {
			out = append(out, user+" ALL=(root) NOPASSWD: "+p+" /sys/class/powercap/intel-rapl\\:*/energy_uj")
		}
	}
	return out
}

// sample geeft de laatste meting, of nil zolang het proces nog opwarmt.
func (m *intelMonitor) sample() (*intelSample, string) {
	m.mu.Lock()
	m.lastUse = time.Now()
	if !m.running {
		m.running = true
		go m.run()
	}
	s, at, err := m.latest, m.latestAt, m.lastErr
	m.mu.Unlock()

	if s == nil || time.Since(at) > 5*time.Second {
		if err != "" {
			return nil, err
		}
		return nil, ""
	}
	return s, ""
}

func (m *intelMonitor) fail(msg string) {
	m.mu.Lock()
	m.lastErr = msg
	m.latest = nil
	m.running = false
	m.mu.Unlock()
}

func (m *intelMonitor) run() {
	path, ok := lookIntelTop()
	if !ok {
		m.fail("intel_gpu_top is not installed")
		return
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		m.fail("sudo is missing")
		return
	}
	cmd := exec.Command(sudo, "-n", path, "-J", "-s", intelTopInterval)
	cmd.Env = []string{"LC_ALL=C", "PATH="}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.fail(err.Error())
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		m.fail(err.Error())
		return
	}

	// Stoppen zodra er een tijd niemand meer kijkt.
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				m.mu.Lock()
				idle := time.Since(m.lastUse) > gpuIdleStop
				m.mu.Unlock()
				if idle {
					// SIGTERM, niet SIGKILL: sudo draait het commando in een
					// pty en moet die buffer nog kunnen legen.
					_ = cmd.Process.Signal(syscall.SIGTERM)
					return
				}
			}
		}
	}()

	streamJSONObjects(stdout, func(raw []byte) {
		if s := parseIntelBlock(raw); s != nil {
			m.mu.Lock()
			m.latest, m.latestAt, m.lastErr = s, time.Now(), ""
			m.mu.Unlock()
		}
	})
	close(done)
	_ = cmd.Wait()

	m.mu.Lock()
	m.running = false
	if m.latest == nil && m.lastErr == "" {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = "no usable output"
		}
		m.lastErr = msg
	}
	m.mu.Unlock()
}

// streamJSONObjects knipt complete { … }-blokken uit een lopende stream en
// geeft ze één voor één door. intel_gpu_top schrijft een array die nooit wordt
// afgesloten, met komma's tussen de blokken; een json.Decoder loopt daarop vast.
func streamJSONObjects(r io.Reader, emit func([]byte)) {
	br := bufio.NewReaderSize(r, 64*1024)
	var buf []byte
	depth := 0
	inString, escaped := false, false
	for {
		c, err := br.ReadByte()
		if err != nil {
			return
		}
		if depth > 0 {
			buf = append(buf, c)
		}
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
				buf = append(buf[:0], c)
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				emit(buf)
				buf = buf[:0]
			}
			if depth < 0 {
				depth = 0
			}
		}
	}
}

func parseIntelBlock(raw []byte) *intelSample {
	var blk struct {
		Period struct {
			Duration float64 `json:"duration"`
		} `json:"period"`
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
	if json.Unmarshal(raw, &blk) != nil || blk.Engines == nil {
		return nil
	}
	// Het eerste blok heeft een meetperiode van rond de 150 ms en staat altijd
	// op nul, terwijl rc6 wél 0 is. Zonder deze grens rapporteert de GPU vlak
	// na het starten van de monitor 100% belasting die er niet is.
	if blk.Period.Duration < 300 {
		return nil
	}
	s := &intelSample{freq: blk.Frequency.Actual, powerGPU: blk.Power.GPU}
	for name, e := range blk.Engines {
		s.engines = append(s.engines, GPUEngine{Name: name, Busy: e.Busy})
		if e.Busy > s.util {
			s.util = e.Busy
		}
	}
	sort.Slice(s.engines, func(i, j int) bool { return s.engines[i].Name < s.engines[j].Name })
	// Bewust géén rc6-fallback meer. "De GPU sliep niet, dus hij zal wel druk
	// zijn" leverde bij het starten en stoppen van een taak een volle 100%
	// op terwijl elke motor nul rapporteerde. De belasting is wat de motoren
	// zeggen; is dat nul, dan is het nul.
	return s
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

// DetectGPUs geeft de GPU's die deze machine heeft, ongeacht fabrikant
// (NVIDIA via nvidia-smi, AMD via sysfs, Intel via i915). Bedoeld voor de
// eenmalige capability-check bij het starten: zonder GPU meldt de agent de
// capability niet en verbergt de app het hele GPU-onderdeel.
func DetectGPUs() []GPU {
	// Bewust collect() en niet get(): get() geeft per ontwerp meteen de cache
	// terug en ververst op de achtergrond, dus de eerste aanroep levert altijd
	// een lege lijst op. Bij het starten is dat precies één keer — en dan zou
	// elke machine "geen GPU" melden en de app het GPU-onderdeel verbergen.
	return newGPUCache(true).collect()
}
