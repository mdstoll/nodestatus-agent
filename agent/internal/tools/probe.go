package tools

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"
)

// Een capability wordt pas gemeld als de bijbehorende functie ook écht werkt.
//
// Alleen kijken of het binary bestaat is niet genoeg gebleken: op een
// Raspberry Pi staat smartctl geïnstalleerd, maar zonder passende sudoers-regel
// levert elke aanroep "permission denied" op. De app toonde dan een tool die
// gegarandeerd faalde. Hier wordt per module één goedkope, echte aanroep
// gedaan; wat niet werkt, wordt niet gemeld en dus in de app niet getoond.
type Capability struct {
	ID     string `json:"id"`               // "smart", "dns", …
	OK     bool   `json:"ok"`               // werkt het echt?
	Reason string `json:"reason,omitempty"` // zo niet: waarom
	Fix    string `json:"fix,omitempty"`    // en wat je eraan doet
}

const probeTimeout = 4 * time.Second

// ProbeAll test elke optionele module. Bedoeld om één keer bij het starten te
// draaien (en door `nodestatus-agent doctor`), niet per verzoek.
func ProbeAll(ctx context.Context) []Capability {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var caps []Capability
	add := func(c Capability) { caps = append(caps, c) }

	add(probeSMART(ctx))
	add(probeSpeedtest(ctx))

	// Deze hebben genoeg aan "bestaat het binary": ze draaien als de agent
	// zelf, zonder sudo, dus aanwezigheid is hier wél gelijk aan werken.
	add(simple("dns", "dig", "install dnsutils (Debian/Ubuntu) or bind-utils"))
	add(simple("whois", "whois", "install whois"))
	add(simple("ping", "ping", "install iputils-ping"))
	add(simple("traceroute", "traceroute", "install traceroute"))
	add(simple("disks", "lsblk", "install util-linux"))
	add(simple("journal", "journalctl", "systemd-journald is not present on this system"))

	sort.Slice(caps, func(i, j int) bool { return caps[i].ID < caps[j].ID })
	return caps
}

func simple(id, bin, fix string) Capability {
	if Has(bin) {
		return Capability{ID: id, OK: true}
	}
	return Capability{ID: id, Reason: bin + " is not installed", Fix: fix}
}

// probeSMART controleert niet alleen of smartctl bestaat, maar of we hem via
// sudo op een echte schijf mogen aanroepen. Een SD-kaart of eMMC ondersteunt
// vaak helemaal geen SMART; dat is geen fout maar wel een reden om de tool
// niet te tonen.
func probeSMART(ctx context.Context) Capability {
	if !Has("smartctl") {
		return Capability{ID: "smart", Reason: "smartctl is not installed", Fix: "install smartmontools"}
	}
	devs := blockDevices()
	if len(devs) == 0 {
		return Capability{ID: "smart", Reason: "no physical disks found"}
	}
	var lastErr string
	for _, dev := range devs {
		out, err := RunSudo(ctx, "smartctl", "-j", "-A", "-H", "-i", dev)
		if err == nil && len(out) > 0 {
			return Capability{ID: "smart", OK: true}
		}
		if err != nil {
			lastErr = err.Error()
			// smartctl geeft een niet-nul exitcode voor "device does not
			// support SMART" én voor "permission denied". Alleen dat laatste is
			// iets wat de gebruiker kan oplossen.
			if strings.Contains(lastErr, "permission denied") {
				return Capability{
					ID:     "smart",
					Reason: "sudo does not permit smartctl for " + dev,
					Fix:    "sudo nodestatus-agent sudoers --user nodestatus > /etc/sudoers.d/nodestatus-agent",
				}
			}
			continue
		}
		// Exitcode niet-nul maar wél JSON terug: smartctl meldt dan zelf de
		// status en dat is bruikbaar.
		if len(out) > 0 {
			return Capability{ID: "smart", OK: true}
		}
	}
	return Capability{ID: "smart", Reason: "no disk reports SMART data (" + lastErr + ")"}
}

// probeSpeedtest draait de CLI met --version. Het binary staat er op een Pi
// soms wel, maar dan als een build voor een andere architectuur of zonder
// geaccepteerde licentie; beide falen pas bij de eerste echte meting.
func probeSpeedtest(ctx context.Context) Capability {
	switch {
	case Has("speedtest"):
		if _, err := Run(ctx, "speedtest", "--version"); err != nil {
			return Capability{
				ID:     "speedtest",
				Reason: "speedtest does not start: " + firstLine(err.Error()),
				Fix:    "install the Ookla CLI for this architecture, or librespeed-cli",
			}
		}
		return Capability{ID: "speedtest", OK: true}
	case Has("librespeed-cli"):
		if _, err := Run(ctx, "librespeed-cli", "--version"); err != nil {
			return Capability{ID: "speedtest", Reason: "librespeed-cli does not start: " + firstLine(err.Error())}
		}
		return Capability{ID: "speedtest", OK: true}
	}
	return Capability{
		ID:     "speedtest",
		Reason: "no speedtest CLI installed",
		Fix:    "install librespeed-cli (works on any architecture) or the Ookla CLI",
	}
}

// HasSensors kijkt of er überhaupt temperatuurbronnen zijn. Een VPS heeft die
// vaak niet, en een lege sensorpagina is verwarrender dan geen sensorpagina.
func HasSensors() bool {
	for _, dir := range []string{"/sys/class/hwmon", "/sys/class/thermal"} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}
