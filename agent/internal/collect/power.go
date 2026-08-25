package collect

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nodestatus/internal/tools"
)

// powerCache houdt het CPU-vermogen (Intel RAPL) apart bij, met hetzelfde
// patroon als gpuCache: sudo cat is een echte subprocess-aanroep en hoort
// niet in de 1 Hz-hoofdlus. Ververst zelf op de achtergrond zolang er iets
// naar kijkt; de aanroeper krijgt altijd meteen de laatst bekende waarde.
//
// Bewust een eigen, kleine lezer in plaats van tools.raplChip() hergebruiken:
// die bouwt een SensorChip met één regel per RAPL-domein (package, psys, …)
// voor de Sensors-pagina, terwijl dit widget maar één representatief getal
// nodig heeft. Twee dingen anders combineren was meer risico voor de
// al-werkende Sensors-pagina dan een paar regels dubbel lezen hier.
type powerCache struct {
	mu         sync.RWMutex
	watts      float64
	ok         bool
	last       time.Time
	refreshing bool

	// domain is het pad van het gekozen RAPL-domein, één keer bepaald bij de
	// eerste zoekactie. unsupported staat op true als die zoektocht niets
	// opleverde: RAPL-steun verandert niet terwijl de agent draait, dus dan
	// hoeft er niet elke seconde opnieuw over /sys/class/powercap gezocht.
	domain      string
	unsupported bool
	prevE       uint64
	prevT       time.Time
	primed      bool
}

func newPowerCache() *powerCache { return &powerCache{} }

// get geeft meteen de laatst bekende waarde terug en ververst op de
// achtergrond als die te oud is — nooit blokkerend voor de aanroeper.
func (p *powerCache) get() (watts float64, ok bool) {
	p.mu.RLock()
	stale := time.Since(p.last) >= time.Second
	watts, ok = p.watts, p.ok
	busy := p.refreshing
	p.mu.RUnlock()

	p.mu.RLock()
	unsupported := p.unsupported
	p.mu.RUnlock()

	if stale && !busy && !unsupported {
		p.mu.Lock()
		if !p.refreshing {
			p.refreshing = true
			go p.refresh()
		}
		p.mu.Unlock()
	}
	return watts, ok
}

func (p *powerCache) refresh() {
	w, ok := p.sample()
	p.mu.Lock()
	p.watts, p.ok, p.last, p.refreshing = w, ok, time.Now(), false
	p.mu.Unlock()
}

// sample leest energy_uj één keer en rekent het vermogen uit als
// verschil-per-seconde tegen de vórige ronde — geen ingebouwde sleep nodig
// zoals raplChip() op de Sensors-pagina die wél gebruikt (dat is één losse
// aanroep zonder eerdere meting; hier draait de cache elke seconde door, dus
// de vórige ronde ís de eerdere meting).
func (p *powerCache) sample() (float64, bool) {
	if p.domain == "" {
		d := findRAPLDomain()
		if d == "" {
			p.mu.Lock()
			p.unsupported = true
			p.mu.Unlock()
			return 0, false
		}
		p.domain = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := tools.RunSudo(ctx, "cat", filepath.Join(p.domain, "energy_uj"))
	if err != nil {
		return 0, false
	}
	e, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, false
	}
	now := time.Now()
	if !p.primed {
		p.prevE, p.prevT, p.primed = e, now, true
		return 0, false // eerste meting geeft nog geen snelheid
	}
	dt := now.Sub(p.prevT).Seconds()
	delta := e - p.prevE // wrap-around (uint64) rondt vanzelf goed door
	p.prevE, p.prevT = e, now
	if dt <= 0 {
		return 0, false
	}
	return float64(delta) / 1e6 / dt, true
}

// findRAPLDomain kiest één top-level RAPL-domein: bij voorkeur "package",
// anders het eerste gevonden domein. Sub-domeinen (core/uncore, "x:y:z")
// tellen energie al mee in hun package en worden overgeslagen — zelfde regel
// als raplChip() in internal/tools/hardware.go.
func findRAPLDomain() string {
	domains, _ := filepath.Glob("/sys/class/powercap/intel-rapl:[0-9]*")
	sort.Strings(domains)
	fallback := ""
	for _, d := range domains {
		if strings.Count(filepath.Base(d), ":") > 1 {
			continue
		}
		name := readTrim(filepath.Join(d, "name"))
		if name == "" {
			continue
		}
		if fallback == "" {
			fallback = d
		}
		if strings.Contains(name, "package") {
			return d
		}
	}
	return fallback
}
