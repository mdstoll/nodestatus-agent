package tools

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------- SMART ----------

type SmartDisk struct {
	Device       string `json:"device"`
	Model        string `json:"model"`
	SizeBytes    uint64 `json:"size_bytes"`
	Protocol     string `json:"protocol"`
	RotationRPM  int    `json:"rotation_rpm"`
	Health       string `json:"health"`
	TempC        int    `json:"temp_c,omitempty"`
	PowerOnHours int    `json:"power_on_hours,omitempty"`
	PowerCycles  int    `json:"power_cycles,omitempty"`
	PercentUsed  int    `json:"percentage_used,omitempty"`
	Error        string `json:"error,omitempty"`
}

func SMART(ctx context.Context) []SmartDisk {
	out := []SmartDisk{}
	for _, dev := range blockDevices() {
		d := SmartDisk{Device: dev}
		b, err := RunSudo(ctx, "smartctl", "-j", "-A", "-H", "-i", dev)
		if err != nil {
			d.Error = err.Error()
			out = append(out, d)
			continue
		}
		var r struct {
			ModelName string `json:"model_name"`
			UserCap   struct {
				Bytes uint64 `json:"bytes"`
			} `json:"user_capacity"`
			RotationRate int `json:"rotation_rate"`
			Device       struct {
				Protocol string `json:"protocol"`
			} `json:"device"`
			SmartStatus struct {
				Passed bool `json:"passed"`
			} `json:"smart_status"`
			Temperature struct {
				Current int `json:"current"`
			} `json:"temperature"`
			PowerOnTime struct {
				Hours int `json:"hours"`
			} `json:"power_on_time"`
			PowerCycleCount int `json:"power_cycle_count"`
			NVMeHealth      struct {
				PercentageUsed int `json:"percentage_used"`
				PowerCycles    int `json:"power_cycles"`
				PowerOnHours   int `json:"power_on_hours"`
				Temperature    int `json:"temperature"`
			} `json:"nvme_smart_health_information_log"`
		}
		if json.Unmarshal(b, &r) != nil {
			d.Error = "smartctl-output onleesbaar"
			out = append(out, d)
			continue
		}
		var msgs struct {
			Smartctl struct {
				ExitStatus int `json:"exit_status"`
				Messages   []struct {
					String   string `json:"string"`
					Severity string `json:"severity"`
				} `json:"messages"`
			} `json:"smartctl"`
		}
		_ = json.Unmarshal(b, &msgs)
		for _, m := range msgs.Smartctl.Messages {
			if m.Severity == "error" {
				d.Error = m.String
			}
		}
		if d.Error != "" && r.ModelName == "" {
			out = append(out, d)
			continue
		}
		d.Model = r.ModelName
		d.SizeBytes = r.UserCap.Bytes
		d.RotationRPM = r.RotationRate
		d.Protocol = r.Device.Protocol
		d.Health = "UNKNOWN"
		if r.SmartStatus.Passed {
			d.Health = "PASSED"
		} else if b != nil && strings.Contains(string(b), `"passed":false`) {
			d.Health = "FAILED"
		}
		d.TempC = r.Temperature.Current
		d.PowerOnHours = r.PowerOnTime.Hours
		d.PowerCycles = r.PowerCycleCount
		if r.NVMeHealth.PowerOnHours > 0 {
			d.PowerOnHours = r.NVMeHealth.PowerOnHours
			d.PowerCycles = r.NVMeHealth.PowerCycles
			d.PercentUsed = r.NVMeHealth.PercentageUsed
			if d.TempC == 0 {
				d.TempC = r.NVMeHealth.Temperature
			}
		}
		out = append(out, d)
	}
	return out
}

// blockDevices vindt fysieke schijven via /sys/block, zonder loop/ram/zram.
func blockDevices() []string {
	var out []string
	entries, _ := os.ReadDir("/sys/block")
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") ||
			strings.HasPrefix(n, "zram") || strings.HasPrefix(n, "dm-") ||
			strings.HasPrefix(n, "md") || strings.HasPrefix(n, "sr") {
			continue
		}
		out = append(out, "/dev/"+n)
	}
	return out
}

// ---------- block devices ----------

type BlockDevice struct {
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Size       uint64        `json:"size"`
	Type       string        `json:"type"`
	Model      string        `json:"model,omitempty"`
	Rotational bool          `json:"rotational"`
	FSType     string        `json:"fstype,omitempty"`
	Mount      string        `json:"mount,omitempty"`
	Children   []BlockDevice `json:"children,omitempty"`
}

func Disks(ctx context.Context) []BlockDevice {
	b, err := Run(ctx, "lsblk", "-J", "-b", "-o", "NAME,PATH,SIZE,TYPE,MOUNTPOINT,FSTYPE,MODEL,ROTA")
	if err != nil {
		return []BlockDevice{}
	}
	var r struct {
		BlockDevices []struct {
			Name     string          `json:"name"`
			Path     string          `json:"path"`
			Size     uint64          `json:"size"`
			Type     string          `json:"type"`
			Mount    *string         `json:"mountpoint"`
			FSType   *string         `json:"fstype"`
			Model    *string         `json:"model"`
			Rota     bool            `json:"rota"`
			Children json.RawMessage `json:"children"`
		} `json:"blockdevices"`
	}
	if json.Unmarshal(b, &r) != nil {
		return []BlockDevice{}
	}
	out := []BlockDevice{}
	for _, d := range r.BlockDevices {
		if d.Type == "loop" || strings.HasPrefix(d.Name, "zram") || strings.HasPrefix(d.Name, "ram") {
			continue
		}
		bd := BlockDevice{Name: d.Name, Path: d.Path, Size: d.Size, Type: d.Type, Rotational: d.Rota}
		if d.Model != nil {
			bd.Model = strings.TrimSpace(*d.Model)
		}
		if d.Mount != nil {
			bd.Mount = *d.Mount
		}
		if d.FSType != nil {
			bd.FSType = *d.FSType
		}
		bd.Children = parseChildren(d.Children)
		out = append(out, bd)
	}
	return out
}

func parseChildren(raw json.RawMessage) []BlockDevice {
	if len(raw) == 0 {
		return nil
	}
	var kids []struct {
		Name     string          `json:"name"`
		Path     string          `json:"path"`
		Size     uint64          `json:"size"`
		Type     string          `json:"type"`
		Mount    *string         `json:"mountpoint"`
		FSType   *string         `json:"fstype"`
		Children json.RawMessage `json:"children"`
	}
	if json.Unmarshal(raw, &kids) != nil {
		return nil
	}
	var out []BlockDevice
	for _, k := range kids {
		bd := BlockDevice{Name: k.Name, Path: k.Path, Size: k.Size, Type: k.Type}
		if k.Mount != nil {
			bd.Mount = *k.Mount
		}
		if k.FSType != nil {
			bd.FSType = *k.FSType
		}
		bd.Children = parseChildren(k.Children)
		out = append(out, bd)
	}
	return out
}

// ---------- netwerkinterfaces ----------

type NICInfo struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac"`
	MTU       int      `json:"mtu"`
	SpeedMbps int      `json:"speed_mbps,omitempty"`
	State     string   `json:"state"`
	Addresses []string `json:"addresses"`
	Virtual   bool     `json:"virtual"`
}

type NetworkInfo struct {
	Interfaces []NICInfo `json:"interfaces"`
	Gateway    string    `json:"gateway,omitempty"`
	DNS        []string  `json:"dns,omitempty"`
}

func Network() NetworkInfo {
	info := NetworkInfo{Interfaces: []NICInfo{}}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		n := NICInfo{
			Name: i.Name, MAC: i.HardwareAddr.String(), MTU: i.MTU,
			State: "down", Addresses: []string{},
			Virtual: isVirtualName(i.Name),
		}
		if i.Flags&net.FlagUp != 0 {
			n.State = "up"
		}
		if addrs, err := i.Addrs(); err == nil {
			for _, a := range addrs {
				n.Addresses = append(n.Addresses, a.String())
			}
		}
		if b, err := os.ReadFile("/sys/class/net/" + i.Name + "/speed"); err == nil {
			if v, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && v > 0 {
				n.SpeedMbps = v
			}
		}
		info.Interfaces = append(info.Interfaces, n)
	}
	info.Gateway = defaultGateway()
	info.DNS = resolvers()
	return info
}

func isVirtualName(n string) bool {
	for _, p := range []string{"veth", "docker", "br-", "virbr", "tun", "tap", "wg", "lo", "cni", "kube", "zt"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func defaultGateway() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil {
			continue
		}
		return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
	}
	return ""
}

func resolvers() []string {
	out := []string{}
	b, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "nameserver "); ok {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// ---------- sensoren ----------

type Sensor struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Type     string  `json:"type"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	High     float64 `json:"high,omitempty"`
	Critical float64 `json:"critical,omitempty"`
	Status   string  `json:"status"`
}

type SensorChip struct {
	Name    string   `json:"name"`
	Sensors []Sensor `json:"sensors"`
}

type SensorsResult struct {
	Chips       []SensorChip `json:"chips"`
	Available   int          `json:"available"`
	Unavailable int          `json:"unavailable"`
}

// Sensors leest alle hwmon-waarden: temperatuur, fans, spanning en vermogen.
func Sensors() SensorsResult {
	res := SensorsResult{Chips: []SensorChip{}}
	dirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, d := range dirs {
		chip := SensorChip{Name: readTrim(filepath.Join(d, "name")), Sensors: []Sensor{}}
		type spec struct {
			glob, typ, unit string
			div             float64
		}
		for _, sp := range []spec{
			{"temp*_input", "temperature", "°C", 1000},
			{"fan*_input", "fan", "RPM", 1},
			{"in*_input", "voltage", "V", 1000},
			{"power*_input", "power", "W", 1e6},
			{"curr*_input", "current", "A", 1000},
		} {
			files, _ := filepath.Glob(filepath.Join(d, sp.glob))
			for _, fp := range files {
				raw := readTrim(fp)
				if raw == "" {
					res.Unavailable++
					continue
				}
				v, err := strconv.ParseFloat(raw, 64)
				if err != nil {
					res.Unavailable++
					continue
				}
				base := strings.TrimSuffix(fp, "_input")
				label := readTrim(base + "_label")
				if label == "" {
					label = chip.Name + " " + filepath.Base(base)
				}
				s := Sensor{
					Key: chip.Name + "/" + filepath.Base(base), Label: label,
					Type: sp.typ, Value: v / sp.div, Unit: sp.unit,
					High:     sane(sp.typ, readFloatDiv(base+"_max", sp.div)),
					Critical: sane(sp.typ, readFloatDiv(base+"_crit", sp.div)),
					Status:   "ok",
				}
				if s.Critical > 0 && s.Value >= s.Critical {
					s.Status = "crit"
				} else if s.High > 0 && s.Value >= s.High {
					s.Status = "warn"
				}
				chip.Sensors = append(chip.Sensors, s)
				res.Available++
			}
		}
		if len(chip.Sensors) > 0 {
			res.Chips = append(res.Chips, chip)
		}
	}
	return res
}

// sane filtert sentinelwaarden uit hwmon: NVMe-schijven zetten temp*_max soms
// op 65261 °C als er geen drempel is.
func sane(typ string, v float64) float64 {
	if v <= 0 {
		return 0
	}
	switch typ {
	case "temperature":
		if v > 150 {
			return 0
		}
	case "fan":
		if v > 50000 {
			return 0
		}
	case "voltage":
		if v > 100 {
			return 0
		}
	}
	return v
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readFloatDiv(p string, div float64) float64 {
	s := readTrim(p)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v / div
}

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
