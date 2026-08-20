package collect

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gpuCache houdt GPU-data apart bij: nvidia-smi kost ~80 ms en mag daarom niet
// in de 1 Hz-loop staan.
type gpuCache struct {
	mu       sync.RWMutex
	gpus     []GPU
	last     time.Time
	nvidia   string
	interval time.Duration
}

func newGPUCache(enabled bool) *gpuCache {
	g := &gpuCache{interval: 2 * time.Second}
	if enabled {
		g.nvidia, _ = exec.LookPath("nvidia-smi")
	}
	return g
}

func (g *gpuCache) get() []GPU {
	g.mu.RLock()
	fresh := time.Since(g.last) < g.interval
	out := g.gpus
	g.mu.RUnlock()
	if fresh {
		return out
	}
	gpus := g.collect()
	g.mu.Lock()
	g.gpus, g.last = gpus, time.Now()
	g.mu.Unlock()
	return gpus
}

func (g *gpuCache) collect() []GPU {
	if g.nvidia != "" {
		if out := g.nvidiaGPUs(); len(out) > 0 {
			return out
		}
	}
	return amdGPUs()
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
