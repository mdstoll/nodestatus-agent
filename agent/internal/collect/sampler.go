package collect

import (
	"sync"
	"time"
)

// Sampler draait de 1 Hz-loop en houdt een ringbuffer met historie bij.
// De loop stopt zichzelf zodra er geen abonnees meer zijn, zodat idle-CPU
// daadwerkelijk nul is.
type Sampler struct {
	mu       sync.RWMutex
	ring     []Sample
	head     int
	filled   int
	interval time.Duration

	prevCPU   cpuTimes
	prevCores []cpuTimes
	prevNet   map[string]netCounters
	prevDisk  map[string]diskCounters
	prevTime  time.Time
	primed    bool

	gpu   *gpuCache
	power *powerCache

	subsMu  sync.Mutex
	subs    map[chan Sample]struct{}
	stop    chan struct{}
	running bool
	linger  *time.Timer
}

// lingerFor bepaalt hoe lang de sampler doorloopt nadat de laatste client weg
// is. Zonder deze marge is de ringbuffer bij elke nieuwe verbinding leeg en
// heeft `backfill` geen nut — dan staat de grafiek in de app het eerste minuut
// leeg. Vijf minuten kost ~0,3% van één core; een echt idle server valt daarna
// alsnog terug naar nul.
const lingerFor = 5 * time.Minute

func NewSampler(historySize int, hz int, gpuEnabled bool) *Sampler {
	if hz < 1 {
		hz = 1
	}
	return &Sampler{
		ring:     make([]Sample, historySize),
		interval: time.Second / time.Duration(hz),
		subs:     map[chan Sample]struct{}{},
		gpu:      newGPUCache(gpuEnabled),
		power:    newPowerCache(),
	}
}

func (s *Sampler) Subscribe() chan Sample {
	ch := make(chan Sample, 8)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	n := len(s.subs)
	s.subsMu.Unlock()
	if n == 1 {
		s.start()
	}
	return ch
}

func (s *Sampler) cancelLinger() {
	if s.linger != nil {
		s.linger.Stop()
		s.linger = nil
	}
}

func (s *Sampler) Unsubscribe(ch chan Sample) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	n := len(s.subs)
	s.subsMu.Unlock()
	close(ch)
	if n == 0 {
		s.subsMu.Lock()
		s.cancelLinger()
		s.linger = time.AfterFunc(lingerFor, func() {
			s.subsMu.Lock()
			idle := len(s.subs) == 0
			s.subsMu.Unlock()
			if idle {
				s.halt()
			}
		})
		s.subsMu.Unlock()
	}
}

func (s *Sampler) start() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	s.cancelLinger()
	if s.running {
		return
	}
	s.running = true
	s.stop = make(chan struct{})
	go s.loop(s.stop)
}

func (s *Sampler) halt() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if !s.running {
		return
	}
	close(s.stop)
	s.running = false
}

func (s *Sampler) loop(stop chan struct{}) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			smp, ok := s.Tick()
			if !ok {
				continue
			}
			s.subsMu.Lock()
			for ch := range s.subs {
				select {
				case ch <- smp:
				default: // trage client: sample overslaan, nooit blokkeren
				}
			}
			s.subsMu.Unlock()
		}
	}
}

// Tick neemt één sample. De eerste tick levert geen bruikbaar resultaat op
// (percentages zijn deltas), vandaar de bool.
func (s *Sampler) Tick() (Sample, bool) {
	now := time.Now()
	cpuAll, cpuCores := readCPUTimes()
	net := readNetDev()
	disk := readDiskstats()

	if !s.primed {
		s.prevCPU, s.prevCores, s.prevNet, s.prevDisk, s.prevTime = cpuAll, cpuCores, net, disk, now
		s.primed = true
		return Sample{}, false
	}
	elapsed := now.Sub(s.prevTime).Seconds()
	if elapsed <= 0 {
		return Sample{}, false
	}

	// Altijd lege slices, nooit nil: een nil-slice serialiseert naar JSON-null
	// en dat is geen geldige array voor de client.
	smp := Sample{
		CPU:     CPUSample{Cores: []float64{}},
		Storage: []StorageSample{},
		Network: NetSample{Interfaces: []NetIface{}},
		Temps:   []Temp{},
		GPU:     []GPU{},
	}
	smp.T = float64(now.UnixNano()) / 1e9

	tot, usr, sys, iow, stl := pct(cpuAll, s.prevCPU)
	smp.CPU = CPUSample{Total: round1(tot), User: round1(usr), System: round1(sys), IOWait: round1(iow), Steal: round1(stl)}
	for i := range cpuCores {
		if i < len(s.prevCores) {
			c, _, _, _, _ := pct(cpuCores[i], s.prevCores[i])
			smp.CPU.Cores = append(smp.CPU.Cores, round1(c))
		}
	}
	smp.CPU.FreqMHz = readCPUFreq(len(cpuCores))
	smp.CPU.Load, smp.CPU.ProcsRunning, smp.CPU.ProcsTotal = readLoad()

	smp.Memory = memSample()

	// disk-IO deltas
	dio := map[string]diskIO{}
	for name, cur := range disk {
		if prev, ok := s.prevDisk[name]; ok && cur.read >= prev.read && cur.write >= prev.write {
			dio[name] = diskIO{
				readBps:  uint64(float64(cur.read-prev.read) / elapsed),
				writeBps: uint64(float64(cur.write-prev.write) / elapsed),
			}
		}
	}
	smp.Storage = storageSamples(dio)

	// netwerk deltas
	var rxT, txT, rxB, txB uint64
	for name, cur := range net {
		virt := isVirtualIface(name)
		var rbps, tbps uint64
		if prev, ok := s.prevNet[name]; ok {
			if cur.rx >= prev.rx {
				rbps = uint64(float64(cur.rx-prev.rx) / elapsed)
			}
			if cur.tx >= prev.tx {
				tbps = uint64(float64(cur.tx-prev.tx) / elapsed)
			}
		}
		if !virt {
			rxT += cur.rx
			txT += cur.tx
			rxB += rbps
			txB += tbps
		}
		smp.Network.Interfaces = append(smp.Network.Interfaces, NetIface{
			Name: name, Up: ifaceUp(name), SpeedMbps: ifaceSpeed(name), Virtual: virt,
			RxBps: rbps, TxBps: tbps, RxTotal: cur.rx, TxTotal: cur.tx,
		})
	}
	sortIfaces(smp.Network.Interfaces)
	smp.Network.RxBps, smp.Network.TxBps = rxB, txB
	smp.Network.RxTotal, smp.Network.TxTotal = rxT, txT

	if t := readTemps(); t != nil {
		smp.Temps = t
	}
	if g := s.gpu.get(); g != nil {
		smp.GPU = g
	}
	if w, ok := s.power.get(); ok {
		smp.PowerW = &w
	}

	s.prevCPU, s.prevCores, s.prevNet, s.prevDisk, s.prevTime = cpuAll, cpuCores, net, disk, now
	s.push(smp)
	return smp, true
}

func (s *Sampler) push(smp Sample) {
	s.mu.Lock()
	s.ring[s.head] = smp
	s.head = (s.head + 1) % len(s.ring)
	if s.filled < len(s.ring) {
		s.filled++
	}
	s.mu.Unlock()
}

// Latest geeft het meest recente sample; neemt er zo nodig zelf één.
func (s *Sampler) Latest() (Sample, bool) {
	s.mu.RLock()
	if s.filled > 0 {
		idx := (s.head - 1 + len(s.ring)) % len(s.ring)
		smp := s.ring[idx]
		s.mu.RUnlock()
		return smp, true
	}
	s.mu.RUnlock()
	// Nog geen historie: prime en meet direct.
	s.Tick()
	time.Sleep(250 * time.Millisecond)
	return s.Tick()
}

// History geeft de laatste n samples, oudste eerst.
func (s *Sampler) History(n int) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n > s.filled {
		n = s.filled
	}
	out := make([]Sample, 0, n)
	for i := n; i > 0; i-- {
		idx := (s.head - i + len(s.ring)*2) % len(s.ring)
		out = append(out, s.ring[idx])
	}
	return out
}

func round1(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 100 {
		f = 100
	}
	return float64(int(f*10+0.5)) / 10
}

func sortIfaces(ifs []NetIface) {
	for i := 1; i < len(ifs); i++ {
		for j := i; j > 0; j-- {
			a, b := ifs[j-1], ifs[j]
			swap := (a.Virtual && !b.Virtual) || (a.Virtual == b.Virtual && a.Name > b.Name)
			if !swap {
				break
			}
			ifs[j-1], ifs[j] = b, a
		}
	}
}
