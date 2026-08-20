// Package api bevat de HTTP-laag: mTLS, enrollment, metrics en tools.
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"serverinfo/internal/collect"
	"serverinfo/internal/config"
	"serverinfo/internal/pki"
	"serverinfo/internal/store"
	"serverinfo/internal/tools"
)

type Server struct {
	cfg     *config.Config
	ca      *pki.CA
	store   *store.Store
	sampler *collect.Sampler
	jobs    *tools.Runner
	version string
	caps    []string
	log     *slog.Logger

	allowNets   []netip.Prefix
	trustedPrxy []netip.Prefix

	rlMu sync.Mutex
	rl   map[string]*bucket
}

func New(cfg *config.Config, ca *pki.CA, st *store.Store, sm *collect.Sampler, version string, caps []string, log *slog.Logger) *Server {
	s := &Server{
		cfg: cfg, ca: ca, store: st, sampler: sm,
		jobs: tools.NewRunner(), version: version, caps: caps, log: log,
		rl: map[string]*bucket{},
	}
	s.allowNets = parsePrefixes(cfg.AllowCIDR)
	s.trustedPrxy = parsePrefixes(cfg.TrustedProxy)
	return s
}

func parsePrefixes(list []string) []netip.Prefix {
	var out []netip.Prefix
	for _, c := range list {
		if p, err := netip.ParsePrefix(c); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// TLSConfig levert een dynamische configuratie: normaal is een client-certificaat
// verplicht, alleen tijdens een open enrollment-venster is het optioneel.
// Het servercertificaat komt uit de CertManager, zodat een vernieuwing meteen
// actief is zonder herstart.
func (s *Server) TLSConfig(certs *pki.CertManager) *tls.Config {
	base := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: certs.GetCertificate,
		ClientCAs:      s.ca.Pool(),
		NextProtos:     []string{"http/1.1"},
	}
	base.GetConfigForClient = func(hi *tls.ClientHelloInfo) (*tls.Config, error) {
		c := base.Clone()
		c.GetConfigForClient = nil
		if s.store.EnrollmentOpen() || s.store.Count() == 0 {
			c.ClientAuth = tls.VerifyClientCertIfGiven
		} else {
			c.ClientAuth = tls.RequireAndVerifyClientCert
		}
		return c, nil
	}
	return base
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Enrollment is het enige endpoint dat zonder client-certificaat mag,
	// en alleen tijdens een open venster.
	mux.HandleFunc("POST /v1/enroll", s.handleEnroll)

	auth := func(h http.HandlerFunc) http.Handler { return s.authenticated(h) }

	mux.Handle("GET /v1/health", auth(s.handleHealth))
	mux.Handle("GET /v1/system", auth(s.handleSystem))
	mux.Handle("GET /v1/metrics", auth(s.handleMetrics))
	mux.Handle("GET /v1/stream", auth(s.handleStream))

	mux.Handle("GET /v1/hardware/sensors", auth(s.handleSensors))
	mux.Handle("GET /v1/hardware/smart", auth(s.handleSMART))
	mux.Handle("GET /v1/hardware/gpu", auth(s.handleGPU))
	mux.Handle("GET /v1/hardware/disks", auth(s.handleDisks))
	mux.Handle("GET /v1/hardware/network", auth(s.handleNetwork))

	mux.Handle("GET /v1/tools/cpuinfo", auth(s.handleCPUInfo))
	mux.Handle("GET /v1/tools/uptime", auth(s.handleUptime))
	mux.Handle("GET /v1/tools/locale", auth(s.handleLocale))
	mux.Handle("GET /v1/tools/updates", auth(s.handleUpdates))
	mux.Handle("GET /v1/tools/processes", auth(s.handleProcesses))
	mux.Handle("GET /v1/tools/logs/sources", auth(s.handleLogSources))
	mux.Handle("GET /v1/tools/logs", auth(s.handleLogs))

	mux.Handle("POST /v1/jobs", auth(s.handleJobCreate))
	mux.Handle("GET /v1/jobs/{id}", auth(s.handleJobGet))

	mux.Handle("GET /v1/devices", auth(s.handleDevices))
	mux.Handle("DELETE /v1/devices/{id}", auth(s.handleDeviceRevoke))
	mux.Handle("POST /v1/devices/me/renew", auth(s.handleRenew))

	return s.baseMiddleware(mux)
}

// baseMiddleware zet security-headers, vangt panics en houdt de agent stil:
// geen Server-header, geen software-informatie voor onbevoegden.
func (s *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic in handler", "path", r.URL.Path, "err", rec)
				writeErr(w, http.StatusInternalServerError, "internal", "interne fout")
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		h.Del("Server")

		if len(s.allowNets) > 0 && !s.allowedIP(r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, _ := netip.ParseAddr(host)
	// X-Forwarded-For alleen vertrouwen als de directe peer een trusted proxy is.
	if len(s.trustedPrxy) > 0 && prefixesContain(s.trustedPrxy, addr) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if a, err := netip.ParseAddr(first); err == nil {
				return a
			}
		}
	}
	return addr
}

func (s *Server) allowedIP(r *http.Request) bool {
	addr := s.clientIP(r)
	if !addr.IsValid() || addr.IsLoopback() {
		return true
	}
	return prefixesContain(s.allowNets, addr)
}

func prefixesContain(ps []netip.Prefix, a netip.Addr) bool {
	if !a.IsValid() {
		return false
	}
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ---------- authenticatie ----------

type ctxKey string

const deviceKey ctxKey = "device"

// authenticated controleert laag 1 (client-certificaat + allowlist) en
// laag 2 (bearer token). Fouten zijn uniform en zonder detail.
func (s *Server) authenticated(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Uitzondering: /v1/health vanaf loopback, zodat install.sh zijn
		// zelftest kan doen voordat er een apparaat gekoppeld is.
		if r.URL.Path == "/v1/health" && s.clientIP(r).IsLoopback() {
			h(w, r)
			return
		}
		if !s.rateOK(r) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			s.authFail(r, "geen client-certificaat")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fp := pki.CertFingerprint(r.TLS.PeerCertificates[0])
		dev, ok := s.store.Lookup(fp)
		if !ok {
			s.authFail(r, "onbekend of ingetrokken apparaat")
			time.Sleep(250 * time.Millisecond)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		token := r.Header.Get("X-Server-Info-Token")
		if token == "" {
			if v, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
				token = strings.TrimSpace(v)
			}
		}
		if token == "" || !s.store.VerifyToken(dev, token) {
			s.authFail(r, "ongeldig token")
			time.Sleep(250 * time.Millisecond)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		s.store.Touch(fp)
		h(w, r.WithContext(context.WithValue(r.Context(), deviceKey, dev)))
	})
}

// authFail logt in een vast formaat zodat fail2ban erop kan filteren.
func (s *Server) authFail(r *http.Request, reason string) {
	s.log.Warn("auth failure from "+s.clientIP(r).String(), "reason", reason, "path", r.URL.Path)
}

// ---------- rate limiting ----------

type bucket struct {
	tokens float64
	last   time.Time
}

func (s *Server) rateOK(r *http.Request) bool {
	key := s.clientIP(r).String()
	const rate, burst = 10.0, 30.0
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	b, ok := s.rl[key]
	now := time.Now()
	if !ok {
		s.rl[key] = &bucket{tokens: burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": kind, "message": msg},
	})
}
