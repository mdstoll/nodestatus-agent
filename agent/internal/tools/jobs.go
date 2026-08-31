package tools

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type JobState string

const (
	JobQueued  JobState = "queued"
	JobRunning JobState = "running"
	JobDone    JobState = "done"
	JobFailed  JobState = "failed"
)

// LiveSample is één meetpunt tijdens een lopende taak, zodat de app de
// doorvoer kan laten meelopen in plaats van 30 seconden een spinner te tonen.
type LiveSample struct {
	T     float64 `json:"t"`
	Bps   float64 `json:"bps"`
	Phase string  `json:"phase"`
}

type Job struct {
	ID        string       `json:"job_id"`
	Type      string       `json:"type"`
	State     JobState     `json:"state"`
	Phase     string       `json:"phase,omitempty"`
	Progress  float64      `json:"progress"`
	LiveBps   float64      `json:"live_bps,omitempty"`
	PingMs    float64      `json:"ping_ms,omitempty"`
	Samples   []LiveSample `json:"samples,omitempty"`
	StartedAt float64      `json:"started_at"`
	EndedAt   float64      `json:"ended_at,omitempty"`
	Result    any          `json:"result,omitempty"`
	Error     string       `json:"error,omitempty"`
	Cancelled bool         `json:"cancelled,omitempty"`

	cancel context.CancelFunc
}

type JobRequest struct {
	Type    string `json:"type"`
	Target  string `json:"target"`
	Port    int    `json:"port"`
	Count   int    `json:"count"`
	Record  string `json:"record"`
	Server  string `json:"server"`
	MaxHops int    `json:"max_hops"`
	// Geekbench Pro credentials, forwarded straight to --username/--password.
	// Optional: the free, anonymous flow (no credentials) already uploads a
	// result and returns a shareable link.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type Runner struct {
	mu      sync.Mutex
	jobs    map[string]*Job
	running int
	lastRun map[string]time.Time
	maxPar  int
}

func NewRunner() *Runner {
	r := &Runner{jobs: map[string]*Job{}, lastRun: map[string]time.Time{}, maxPar: 2}
	go r.gc()
	return r
}

func (r *Runner) gc() {
	for range time.Tick(time.Minute) {
		r.mu.Lock()
		for id, j := range r.jobs {
			if j.EndedAt > 0 && time.Since(time.Unix(int64(j.EndedAt), 0)) > 5*time.Minute {
				delete(r.jobs, id)
			}
		}
		r.mu.Unlock()
	}
}

func (r *Runner) Get(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return nil, false
	}
	c := *j
	return &c, true
}

// Submit start een job. Speedtest heeft een eigen limiet van 1 per 5 minuten
// omdat elke run 1-3 GB verkeer kost.
func (r *Runner) Submit(req JobRequest) (*Job, error) {
	r.mu.Lock()
	if r.running >= r.maxPar {
		r.mu.Unlock()
		return nil, fmt.Errorf("er lopen al %d taken; probeer het zo opnieuw", r.maxPar)
	}
	// Twee speedtests tegelijk leveren onzin op (ze delen de lijn), dus één
	// tegelijk. Verder geen kunstmatige wachttijd: de Ookla-CLI kent zelf
	// geen limiet, dus de app hoeft er ook geen te verzinnen.
	// Geekbench and iperf3 saturate a shared resource (all cores, or the
	// link) the same way a speedtest does, so the same "only one at a time"
	// rule applies to each of them individually.
	if req.Type == "speedtest" || req.Type == "geekbench" || req.Type == "iperf3" {
		for _, j := range r.jobs {
			if j.Type == req.Type && (j.State == JobRunning || j.State == JobQueued) {
				r.mu.Unlock()
				return nil, fmt.Errorf("a %s job is already running", req.Type)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout(req.Type))
	j := &Job{
		ID: "j_" + randID(), Type: req.Type, State: JobQueued,
		StartedAt: float64(time.Now().Unix()), cancel: cancel,
	}
	r.jobs[j.ID] = j
	r.running++
	r.lastRun[req.Type] = time.Now()
	r.mu.Unlock()

	go func() {
		defer cancel()
		r.update(j.ID, func(x *Job) { x.State = JobRunning })
		res, err := r.execute(ctx, req, j.ID)
		r.mu.Lock()
		r.running--
		if x, ok := r.jobs[j.ID]; ok {
			x.EndedAt = float64(time.Now().Unix())
			if err != nil {
				x.State, x.Error = JobFailed, err.Error()
				if ctx.Err() == context.Canceled {
					x.Cancelled, x.Error = true, "stopped"
				}
			} else {
				x.State, x.Result, x.Progress = JobDone, res, 1
			}
		}
		r.mu.Unlock()
	}()
	return j, nil
}

func (r *Runner) update(id string, f func(*Job)) {
	r.mu.Lock()
	if j, ok := r.jobs[id]; ok {
		f(j)
	}
	r.mu.Unlock()
}

// Cancel stopt een lopende taak. De job zelf zet, via zijn eigen ctx.Err(),
// State op "failed" met Cancelled: true — er is geen apart "cancelled"
// JobState nodig, de app kan op het veld filteren.
func (r *Runner) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok || (j.State != JobRunning && j.State != JobQueued) {
		return false
	}
	j.cancel()
	return true
}

func jobTimeout(t string) time.Duration {
	switch t {
	case "speedtest":
		return 120 * time.Second
	case "ping":
		return 60 * time.Second
	case "traceroute":
		return 60 * time.Second
	case "geekbench":
		// Het volledige CPU-pakket (single + multi-core) duurt op trage
		// hardware (een Pi) ruim over een minuut; 10 minuten is ruim
		// genoeg zonder een vastgelopen run voor altijd te laten draaien.
		return 10 * time.Minute
	case "iperf3":
		// Twee richtingen van 10 s elk (iperf3Direction's eigen "duration"),
		// plus verbindingsopzet per richting — 30 s was te krap en liet de
		// download-helft soms wegvallen doordat de context halverwege afliep.
		return 60 * time.Second
	}
	return 20 * time.Second
}

func (r *Runner) execute(ctx context.Context, req JobRequest, id string) (any, error) {
	switch req.Type {
	case "speedtest":
		return r.speedtest(ctx, id)
	case "ping":
		return pingJob(ctx, req)
	case "dns":
		return dnsJob(ctx, req)
	case "whois":
		return whoisJob(ctx, req)
	case "traceroute":
		return tracerouteJob(ctx, req)
	case "geekbench":
		return r.geekbenchJob(ctx, id, req)
	case "iperf3":
		return r.iperf3Job(ctx, id, req)
	}
	return nil, fmt.Errorf("onbekend taaktype %q", req.Type)
}

// ---------- speedtest ----------

type SpeedtestResult struct {
	DownloadBps float64 `json:"download_bps"`
	UploadBps   float64 `json:"upload_bps"`
	PingMs      float64 `json:"ping_ms"`
	JitterMs    float64 `json:"jitter_ms"`
	PacketLoss  float64 `json:"packet_loss"`
	ServerName  string  `json:"server_name"`
	ServerCity  string  `json:"server_city,omitempty"`
	ISP         string  `json:"isp,omitempty"`
	ExternalIP  string  `json:"external_ip_masked,omitempty"`
	ResultURL   string  `json:"result_url,omitempty"`
	Engine      string  `json:"engine"`
}

func (r *Runner) speedtest(ctx context.Context, id string) (any, error) {
	if Has("speedtest") {
		return r.speedtestOokla(ctx, id)
	}
	if Has("librespeed-cli") {
		return r.speedtestLibrespeed(ctx)
	}
	return nil, fmt.Errorf("no speedtest tool installed (install 'speedtest' or 'librespeed-cli')")
}

// speedtestOokla leest de jsonl-stream van de Ookla-CLI en werkt de job
// live bij, zodat de app de doorvoer ziet oplopen tijdens de test in plaats
// van 30 seconden naar een spinner te kijken.
func (r *Runner) speedtestOokla(ctx context.Context, id string) (any, error) {
	path, _ := Bin("speedtest")
	cmd := exec.CommandContext(ctx, path,
		"-f", "jsonl", "--progress-update-interval=200",
		"--accept-license", "--accept-gdpr")
	cmd.Env = childEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var final SpeedtestResult
	var haveResult bool
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for sc.Scan() {
		var e struct {
			Type string `json:"type"`
			Ping struct {
				Latency  float64 `json:"latency"`
				Jitter   float64 `json:"jitter"`
				Progress float64 `json:"progress"`
			} `json:"ping"`
			Download struct {
				Bandwidth float64 `json:"bandwidth"`
				Progress  float64 `json:"progress"`
			} `json:"download"`
			Upload struct {
				Bandwidth float64 `json:"bandwidth"`
				Progress  float64 `json:"progress"`
			} `json:"upload"`
			PacketLoss float64 `json:"packetLoss"`
			ISP        string  `json:"isp"`
			Server     struct {
				Name     string `json:"name"`
				Location string `json:"location"`
			} `json:"server"`
			Interface struct {
				ExternalIP string `json:"externalIp"`
			} `json:"interface"`
			Result struct {
				URL string `json:"url"`
			} `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "testStart":
			final.ServerName = e.Server.Name
			final.ServerCity = e.Server.Location
			final.ISP = e.ISP
			final.ExternalIP = maskIP(e.Interface.ExternalIP)
			r.progress(id, "ping", 0.02, 0, 0)
		case "ping":
			final.PingMs = e.Ping.Latency
			final.JitterMs = e.Ping.Jitter
			// Ping is ~10% van de totale test.
			r.progress(id, "ping", e.Ping.Progress*0.1, 0, e.Ping.Latency)
		case "download":
			final.DownloadBps = e.Download.Bandwidth * 8
			r.progress(id, "download", 0.1+e.Download.Progress*0.5, e.Download.Bandwidth*8, final.PingMs)
		case "upload":
			final.UploadBps = e.Upload.Bandwidth * 8
			r.progress(id, "upload", 0.6+e.Upload.Progress*0.4, e.Upload.Bandwidth*8, final.PingMs)
		case "result":
			final.PacketLoss = e.PacketLoss
			final.ResultURL = e.Result.URL
			final.Engine = "ookla"
			haveResult = true
		}
	}
	if err := cmd.Wait(); err != nil && !haveResult {
		return nil, fmt.Errorf("speedtest: %v", err)
	}
	if !haveResult {
		return nil, fmt.Errorf("the speedtest returned no result")
	}
	final.Engine = "ookla"
	return final, nil
}

// progress werkt de lopende job bij en bewaart een meetpunt voor de grafiek.
func (r *Runner) progress(id, phase string, p, bps, ping float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return
	}
	j.Phase = phase
	j.Progress = p
	j.LiveBps = bps
	if ping > 0 {
		j.PingMs = ping
	}
	if bps > 0 {
		j.Samples = append(j.Samples, LiveSample{
			T: float64(time.Now().UnixNano()) / 1e9, Bps: bps, Phase: phase,
		})
		// Ruim begrensd: een test duurt ~30 s bij 5 updates per seconde.
		if len(j.Samples) > 400 {
			j.Samples = j.Samples[len(j.Samples)-400:]
		}
	}
}

func (r *Runner) speedtestLibrespeed(ctx context.Context) (any, error) {
	b, err := Run(ctx, "librespeed-cli", "--json")
	if err != nil {
		return nil, err
	}
	var arr []struct {
		Ping     float64 `json:"ping"`
		Jitter   float64 `json:"jitter"`
		Download float64 `json:"download"`
		Upload   float64 `json:"upload"`
		Server   struct {
			Name string `json:"name"`
		} `json:"server"`
	}
	if err := json.Unmarshal(b, &arr); err != nil || len(arr) == 0 {
		return nil, fmt.Errorf("librespeed-output onleesbaar")
	}
	o := arr[0]
	return SpeedtestResult{
		DownloadBps: o.Download * 1e6, UploadBps: o.Upload * 1e6,
		PingMs: o.Ping, JitterMs: o.Jitter, ServerName: o.Server.Name, Engine: "librespeed",
	}, nil
}

func maskIP(ip string) string {
	p := strings.Split(ip, ".")
	if len(p) == 4 {
		return p[0] + "." + p[1] + ".x.x"
	}
	return ""
}

// ---------- ping ----------

type PingResult struct {
	Target     string    `json:"target"`
	ResolvedIP string    `json:"resolved_ip,omitempty"`
	Sent       int       `json:"sent"`
	Received   int       `json:"received"`
	LossPct    float64   `json:"loss_percent"`
	MinMs      float64   `json:"min_ms"`
	AvgMs      float64   `json:"avg_ms"`
	MaxMs      float64   `json:"max_ms"`
	MdevMs     float64   `json:"mdev_ms"`
	RTTs       []float64 `json:"rtts_ms"`
}

var (
	pingLineRe = regexp.MustCompile(`time=([\d.]+)\s*ms`)
	pingIPRe   = regexp.MustCompile(`PING\s+\S+\s+\(([\d.a-fA-F:]+)\)`)
	pingStatRe = regexp.MustCompile(`(\d+) packets transmitted, (\d+) received`)
	pingRTTRe  = regexp.MustCompile(`= ([\d.]+)/([\d.]+)/([\d.]+)/([\d.]+) ms`)
)

func pingJob(ctx context.Context, req JobRequest) (any, error) {
	target, err := ValidTarget(req.Target)
	if err != nil {
		return nil, err
	}
	count := ClampInt(req.Count, 1, 20, 10)
	b, err := Run(ctx, "ping", "-c", strconv.Itoa(count), "-W", "2", "-i", "0.3", target)
	if err != nil && len(b) == 0 {
		return nil, err
	}
	out := string(b)
	res := PingResult{Target: target, Sent: count, RTTs: []float64{}}
	if m := pingIPRe.FindStringSubmatch(out); m != nil {
		res.ResolvedIP = m[1]
	}
	for _, m := range pingLineRe.FindAllStringSubmatch(out, -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		res.RTTs = append(res.RTTs, v)
	}
	if m := pingStatRe.FindStringSubmatch(out); m != nil {
		res.Sent, _ = strconv.Atoi(m[1])
		res.Received, _ = strconv.Atoi(m[2])
	}
	if res.Sent > 0 {
		res.LossPct = float64(res.Sent-res.Received) / float64(res.Sent) * 100
	}
	if m := pingRTTRe.FindStringSubmatch(out); m != nil {
		res.MinMs, _ = strconv.ParseFloat(m[1], 64)
		res.AvgMs, _ = strconv.ParseFloat(m[2], 64)
		res.MaxMs, _ = strconv.ParseFloat(m[3], 64)
		res.MdevMs, _ = strconv.ParseFloat(m[4], 64)
	}
	if res.Received == 0 {
		return res, fmt.Errorf("no response from %s", target)
	}
	return res, nil
}

// ---------- dns ----------

type DNSAnswer struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	TTL   int    `json:"ttl"`
	Value string `json:"value"`
}

type DNSResult struct {
	Query   string      `json:"query"`
	Record  string      `json:"record"`
	Server  string      `json:"server"`
	Answers []DNSAnswer `json:"answers"`
	QueryMs float64     `json:"query_ms"`
}

func dnsJob(ctx context.Context, req JobRequest) (any, error) {
	target, err := ValidTarget(req.Target)
	if err != nil {
		return nil, err
	}
	rec, err := ValidRecordType(req.Record)
	if err != nil {
		return nil, err
	}
	args := []string{target, rec, "+noall", "+answer", "+stats", "+timeout=3", "+tries=1"}
	server := "systeem"
	if req.Server != "" {
		s, err := ValidTarget(req.Server)
		if err != nil {
			return nil, fmt.Errorf("invalid DNS server")
		}
		args = append([]string{"@" + s}, args...)
		server = s
	}
	b, err := Run(ctx, "dig", args...)
	if err != nil {
		return nil, err
	}
	res := DNSResult{Query: target, Record: rec, Server: server, Answers: []DNSAnswer{}}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			if strings.HasPrefix(line, ";; Query time:") {
				f := strings.Fields(line)
				if len(f) >= 4 {
					v, _ := strconv.ParseFloat(f[3], 64)
					res.QueryMs = v
				}
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		ttl, _ := strconv.Atoi(f[1])
		res.Answers = append(res.Answers, DNSAnswer{
			Name: strings.TrimSuffix(f[0], "."), Type: f[3], TTL: ttl,
			Value: strings.Join(f[4:], " "),
		})
	}
	return res, nil
}

// ---------- whois ----------

type WhoisResult struct {
	Query       string   `json:"query"`
	Registrar   string   `json:"registrar,omitempty"`
	Created     string   `json:"created,omitempty"`
	Expires     string   `json:"expires,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	NameServers []string `json:"name_servers"`
	Status      []string `json:"status"`
	Raw         string   `json:"raw"`
}

func whoisJob(ctx context.Context, req JobRequest) (any, error) {
	target, err := ValidTarget(req.Target)
	if err != nil {
		return nil, err
	}
	b, err := Run(ctx, "whois", "--", target)
	if err != nil {
		return nil, err
	}
	raw := string(b)
	if len(raw) > 60000 {
		raw = raw[:60000] + "\n… (afgekapt)"
	}
	res := WhoisResult{Query: target, Raw: raw, NameServers: []string{}, Status: []string{}}
	for _, line := range strings.Split(raw, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if v == "" {
			continue
		}
		switch k {
		case "registrar", "registrar name", "sponsoring registrar":
			if res.Registrar == "" {
				res.Registrar = v
			}
		case "creation date", "created", "registered on", "domain registration date":
			if res.Created == "" {
				res.Created = v
			}
		case "registry expiry date", "expiry date", "expires", "paid-till":
			if res.Expires == "" {
				res.Expires = v
			}
		case "updated date", "last modified", "changed":
			if res.Updated == "" {
				res.Updated = v
			}
		case "name server", "nserver", "nameserver":
			res.NameServers = append(res.NameServers, strings.Fields(v)[0])
		case "domain status", "status":
			res.Status = append(res.Status, v)
		}
	}
	return res, nil
}

// ---------- traceroute ----------

type Hop struct {
	Number int       `json:"number"`
	Host   string    `json:"host"`
	IP     string    `json:"ip,omitempty"`
	RTTs   []float64 `json:"rtts_ms"`
}

type TracerouteResult struct {
	Target string `json:"target"`
	Hops   []Hop  `json:"hops"`
}

func tracerouteJob(ctx context.Context, req JobRequest) (any, error) {
	target, err := ValidTarget(req.Target)
	if err != nil {
		return nil, err
	}
	hops := ClampInt(req.MaxHops, 1, 30, 20)
	b, err := Run(ctx, "traceroute", "-n", "-w", "2", "-q", "3", "-m", strconv.Itoa(hops), target)
	if err != nil && len(b) == 0 {
		return nil, err
	}
	res := TracerouteResult{Target: target, Hops: []Hop{}}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		h := Hop{Number: n, RTTs: []float64{}}
		if f[1] != "*" {
			h.Host, h.IP = f[1], f[1]
		} else {
			h.Host = "*"
		}
		for i := 2; i < len(f); i++ {
			if v, err := strconv.ParseFloat(f[i], 64); err == nil {
				h.RTTs = append(h.RTTs, v)
			}
		}
		res.Hops = append(res.Hops, h)
	}
	return res, nil
}

func randID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}
