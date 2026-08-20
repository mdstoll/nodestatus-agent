// Package config laadt de agent-configuratie uit een TOML-subset bestand.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Features struct {
	SMART     bool
	GPU       bool
	Speedtest bool
	APT       bool
	Logs      bool
}

type Logs struct {
	Units    []string
	Files    []string
	MaxLines int
}

type Config struct {
	Path string

	Bind         string
	Mode         string // lan | vpn | public | proxy
	DisplayName  string
	TLSCert      string
	TLSKey       string
	CADir        string
	StateDir     string
	AllowCIDR    []string
	TrustedProxy []string

	SampleHz    int
	HistorySize int

	EnrollWindowMinutes int
	EnrollMaxAttempts   int
	ClientCertDays      int

	Features Features
	Logs     Logs
}

func Default() *Config {
	return &Config{
		Bind:                "0.0.0.0:29500",
		Mode:                "lan",
		TLSCert:             "/etc/serverinfo-agent/cert.pem",
		TLSKey:              "/etc/serverinfo-agent/key.pem",
		CADir:               "/etc/serverinfo-agent/ca",
		StateDir:            "/var/lib/serverinfo-agent",
		SampleHz:            1,
		HistorySize:         300,
		EnrollWindowMinutes: 15,
		EnrollMaxAttempts:   5,
		ClientCertDays:      365,
		Features:            Features{SMART: true, GPU: true, Speedtest: true, APT: true, Logs: true},
		Logs: Logs{
			Units:    []string{"ssh", "sshd", "nginx", "docker", "cron", "ufw", "systemd-journald", "systemd-resolved"},
			Files:    []string{"/var/log/syslog", "/var/log/auth.log", "/var/log/kern.log", "/var/log/messages", "/var/log/daemon.log"},
			MaxLines: 500,
		},
	}
}

// Load leest een TOML-subset: [sectie], key = "waarde", key = 12, key = true,
// key = ["a", "b"]. Genoeg voor onze configuratie en zonder externe dependency.
func Load(path string) (*Config, error) {
	c := Default()
	c.Path = path
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	section := ""
	sc := bufio.NewScanner(f)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s:%d: geen key = value", path, ln)
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		raw := strings.TrimSpace(line[eq+1:])
		if i := strings.Index(raw, " #"); i >= 0 { // inline comment
			raw = strings.TrimSpace(raw[:i])
		}
		if err := c.set(section, key, raw); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, ln, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	c.applyEnv()
	return c, c.Validate()
}

func unquote(s string) string { return strings.Trim(s, `"'`) }

func parseList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		return nil
	}
	s = strings.Trim(s, "[]")
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = unquote(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) set(section, key, raw string) error {
	val := unquote(raw)
	atoi := func() int { n, _ := strconv.Atoi(val); return n }
	switch section {
	case "":
		switch key {
		case "bind":
			c.Bind = val
		case "mode":
			c.Mode = val
		case "display_name":
			c.DisplayName = val
		case "tls_cert":
			c.TLSCert = val
		case "tls_key":
			c.TLSKey = val
		case "ca_dir":
			c.CADir = val
		case "state_dir":
			c.StateDir = val
		case "allow_cidr":
			c.AllowCIDR = parseList(raw)
		case "trusted_proxy":
			c.TrustedProxy = parseList(raw)
		case "sample_hz":
			c.SampleHz = atoi()
		case "history_size":
			c.HistorySize = atoi()
		case "enroll_window_minutes":
			c.EnrollWindowMinutes = atoi()
		case "enroll_max_attempts":
			c.EnrollMaxAttempts = atoi()
		case "client_cert_days":
			c.ClientCertDays = atoi()
		}
	case "features":
		b := val == "true"
		switch key {
		case "smart":
			c.Features.SMART = b
		case "gpu":
			c.Features.GPU = b
		case "speedtest":
			c.Features.Speedtest = b
		case "apt":
			c.Features.APT = b
		case "logs":
			c.Features.Logs = b
		}
	case "logs":
		switch key {
		case "units":
			c.Logs.Units = parseList(raw)
		case "files":
			c.Logs.Files = parseList(raw)
		case "max_lines":
			c.Logs.MaxLines = atoi()
		}
	}
	return nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("SERVERINFO_BIND"); v != "" {
		c.Bind = v
	}
	if v := os.Getenv("SERVERINFO_MODE"); v != "" {
		c.Mode = v
	}
	if v := os.Getenv("SERVERINFO_NAME"); v != "" {
		c.DisplayName = v
	}
}

func (c *Config) Validate() error {
	if c.SampleHz < 1 {
		c.SampleHz = 1
	}
	if c.HistorySize < 10 {
		c.HistorySize = 10
	}
	if c.HistorySize > 3600 {
		c.HistorySize = 3600
	}
	if c.ClientCertDays < 1 {
		c.ClientCertDays = 365
	}
	// Veiligheidsklem: platte HTTP mag alleen op loopback (profiel proxy).
	if c.TLSCert == "" && !isLoopbackBind(c.Bind) {
		return fmt.Errorf("tls_cert is leeg maar bind (%s) is niet loopback — weigeren", c.Bind)
	}
	return nil
}

func isLoopbackBind(bind string) bool {
	h := bind
	if i := strings.LastIndex(bind, ":"); i > 0 {
		h = bind[:i]
	}
	h = strings.Trim(h, "[]")
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

func (c *Config) TLSEnabled() bool { return c.TLSCert != "" }
