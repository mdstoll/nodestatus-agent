// Package tools voert strikt afgebakende diagnostische commando's uit.
//
// Vier regels gelden overal in dit pakket:
//  1. nooit een shell — altijd exec met een argv-array
//  2. elk argument wordt gevalideerd tegen een strikt patroon
//  3. absolute paden, opgezocht bij het starten van de agent
//  4. altijd een context met timeout en een cap op de output
package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

const maxOutput = 1 << 20 // 1 MB

var (
	binMu sync.RWMutex
	bins  = map[string]string{}
	home  = "/tmp"
)

// SetHome bepaalt de HOME van child-processen. Niet optioneel: de Ookla
// speedtest-CLI leest getenv("HOME") en bouwt daar zonder null-check een
// std::string van, dus zonder HOME crasht hij met
//
//	terminate called after throwing an instance of 'std::logic_error'
//	  what():  basic_string::_M_construct null not valid
//
// Hij bewaart er ook de licentie-acceptatie, dus het moet een pad zijn waar
// de agent mag schrijven (de state dir).
func SetHome(dir string) {
	binMu.Lock()
	home = dir
	binMu.Unlock()
}

// childEnv is de omgeving voor elk extern commando: minimaal, met een lege
// PATH zodat een child niets kan opzoeken, maar mét een geldige HOME.
func childEnv() []string {
	binMu.RLock()
	defer binMu.RUnlock()
	return []string{"LC_ALL=C", "PATH=", "HOME=" + home}
}

// Discover zoekt de externe tools één keer op bij het starten.
func Discover() map[string]string {
	names := []string{
		"smartctl", "nvidia-smi", "speedtest", "librespeed-cli", "whois", "dig",
		"ping", "traceroute", "lsblk", "journalctl", "systemd-detect-virt",
		"localectl", "timedatectl", "systemctl", "apt", "apt-get", "apt-cache", "last", "qrencode", "cat",
	}
	found := map[string]string{}
	for _, n := range names {
		for _, dir := range []string{"/usr/bin/", "/bin/", "/usr/sbin/", "/sbin/", "/usr/local/bin/", "/usr/local/sbin/"} {
			p := dir + n
			if fi, err := exec.LookPath(p); err == nil {
				found[n] = fi
				break
			}
		}
	}
	binMu.Lock()
	bins = found
	binMu.Unlock()
	return found
}

func Bin(name string) (string, bool) {
	binMu.RLock()
	defer binMu.RUnlock()
	p, ok := bins[name]
	return p, ok
}

func Has(name string) bool { _, ok := Bin(name); return ok }

// Run voert een commando uit zonder shell en met een harde outputlimiet.
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, ok := Bin(name)
	if !ok {
		return nil, fmt.Errorf("%s is niet geïnstalleerd op deze server", name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = childEnv()
	var out, errb bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, n: maxOutput}
	cmd.Stderr = &limitedWriter{w: &errb, n: 64 << 10}
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		// Output wordt meegegeven: ping en traceroute leveren bruikbare
		// resultaten met een niet-nul exitcode.
		return out.Bytes(), fmt.Errorf("%s: %s", name, firstLine(msg))
	}
	return out.Bytes(), nil
}

// RunSudo draait via sudo -n; alleen voor commando's die in sudoers staan.
func RunSudo(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, ok := Bin(name)
	if !ok {
		return nil, fmt.Errorf("%s is niet geïnstalleerd op deze server", name)
	}
	if out, err := Run(ctx, name, args...); err == nil {
		return out, nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("%s vereist root en sudo ontbreekt", name)
	}
	cmd := exec.CommandContext(ctx, sudo, append([]string{"-n", path}, args...)...)
	cmd.Env = childEnv()
	var out bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, n: maxOutput}
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("%s: geen rechten (ontbreekt de sudoers-regel?)", name)
	}
	return out.Bytes(), nil
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil // stil weggooien, niet falen
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	l.n -= len(p)
	return l.w.Write(p)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---------- validatie ----------

var hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)

// ValidTarget accepteert alleen een geldig IP-adres of een geldige hostname.
// Dit is de enige plek waar gebruikersinvoer een commando in gaat.
func ValidTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 253 {
		return "", fmt.Errorf("ongeldig doeladres")
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
			return "", fmt.Errorf("dit adres is niet toegestaan")
		}
		return addr.String(), nil
	}
	if !hostnameRe.MatchString(s) {
		return "", fmt.Errorf("ongeldige hostname")
	}
	return s, nil
}

// ClampInt begrenst een numeriek argument tot een veilig bereik.
func ClampInt(v, min, max, def int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

var dnsRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "MX": true, "TXT": true, "NS": true,
	"CNAME": true, "SOA": true, "SRV": true, "PTR": true, "CAA": true,
}

func ValidRecordType(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "A", nil
	}
	if !dnsRecordTypes[s] {
		return "", fmt.Errorf("onbekend recordtype")
	}
	return s, nil
}
