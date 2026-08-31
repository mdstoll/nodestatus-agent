package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"nodestatus/internal/collect"
	"nodestatus/internal/pki"
	"nodestatus/internal/store"
	"nodestatus/internal/tools"
)

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	// ?refresh=1 kijkt meteen bij GitHub in plaats van het gecachte antwoord
	// van maximaal zes uur oud terug te geven. De app gebruikt dit voor de
	// knop "nu controleren"; de checker begrenst zelf hoe vaak dat echt het
	// netwerk op gaat.
	if r.URL.Query().Get("refresh") == "1" {
		writeJSON(w, s.updates.Refresh())
		return
	}
	writeJSON(w, s.updates.Result())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok": true, "version": s.version,
		"uptime_s": int64(collect.UptimeSeconds()),
		"devices":  s.store.Count(),
	})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, collect.System(s.cfg.DisplayName, s.version, s.caps))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	smp, ok := s.sampler.Latest()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "no sample available yet")
		return
	}
	writeJSON(w, smp)
}

// ---------- enrollment ----------

type enrollRequest struct {
	Code       string `json:"code"`
	PublicKey  string `json:"public_key_b64"` // X9.63 uncompressed P-256 punt
	DeviceName string `json:"device_name"`
}

type enrollResponse struct {
	DeviceID      string `json:"device_id"`
	ClientCertPEM string `json:"client_cert_pem"`
	CACertPEM     string `json:"ca_cert_pem"`
	APIToken      string `json:"api_token"`
	ExpiresAt     int64  `json:"expires_at"`
	Hostname      string `json:"hostname"`
	DisplayName   string `json:"display_name"`
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.store.EnrollmentOpen() {
		s.authFail(r, "pairing window closed")
		time.Sleep(250 * time.Millisecond)
		writeErr(w, http.StatusForbidden, "unavailable", "no pairing window is open")
		return
	}
	var req enrollRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", "onleesbaar verzoek")
		return
	}
	if !s.store.CheckEnrollCode(req.Code) {
		s.authFail(r, "wrong pairing code")
		time.Sleep(500 * time.Millisecond)
		writeErr(w, http.StatusForbidden, "unauthorized", "koppelcode klopt niet")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", "publieke sleutel is geen geldige base64")
		return
	}
	pub, err := pki.ParseP256PublicKey(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	name := req.DeviceName
	if name == "" {
		name = "Onbekend apparaat"
	}
	certPEM, fp, notAfter, err := s.ca.IssueClient(pub, name, s.cfg.ClientCertDays)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "certificaat uitgeven mislukt")
		return
	}
	dev, token, err := s.store.Add(name, fp, notAfter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "apparaat opslaan mislukt")
		return
	}
	s.store.CloseEnrollment()
	s.log.Info("device paired", "device", dev.Name, "id", dev.ID)

	si := collect.System(s.cfg.DisplayName, s.version, s.caps)
	writeJSON(w, enrollResponse{
		DeviceID: dev.ID, ClientCertPEM: string(certPEM), CACertPEM: string(s.ca.CertPEM),
		APIToken: token, ExpiresAt: notAfter.Unix(),
		Hostname: si.Hostname, DisplayName: si.DisplayName,
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	me, _ := r.Context().Value(deviceKey).(*store.Device)
	list := s.store.List()
	out := make([]map[string]any, 0, len(list))
	for _, d := range list {
		out = append(out, map[string]any{
			"id": d.ID, "name": d.Name,
			"enrolled_at": d.EnrolledAt.Unix(), "last_seen": d.LastSeen.Unix(),
			"expires_at": d.ExpiresAt.Unix(),
			"is_current": me != nil && me.ID == d.ID,
		})
	}
	writeJSON(w, map[string]any{"devices": out})
}

func (s *Server) handleDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name, err := s.store.Revoke(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	s.log.Info("device revoked", "device", name)
	writeJSON(w, map[string]any{"revoked": name})
}

func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request) {
	me, _ := r.Context().Value(deviceKey).(*store.Device)
	if me == nil {
		writeErr(w, http.StatusForbidden, "unauthorized", "")
		return
	}
	var req struct {
		PublicKey string `json:"public_key_b64"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", "onleesbaar verzoek")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", "ongeldige sleutel")
		return
	}
	pub, err := pki.ParseP256PublicKey(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	certPEM, fp, notAfter, err := s.ca.IssueClient(pub, me.Name, s.cfg.ClientCertDays)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "vernieuwen mislukt")
		return
	}
	if _, err := s.store.Replace(me.Fingerprint, fp, notAfter); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"client_cert_pem": string(certPEM), "ca_cert_pem": string(s.ca.CertPEM),
		"expires_at": notAfter.Unix(),
	})
}

// ---------- hardware ----------

func (s *Server) handleSensors(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, tools.Sensors())
}

func (s *Server) handleSMART(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Features.SMART || !tools.Has("smartctl") {
		writeJSON(w, map[string]any{"disks": []any{}, "unavailable": "smartmontools is not installed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	writeJSON(w, map[string]any{"disks": tools.SMART(ctx)})
}

func (s *Server) handleGPU(w http.ResponseWriter, r *http.Request) {
	smp, _ := s.sampler.Latest()
	gpus := smp.GPU
	if gpus == nil {
		gpus = []collect.GPU{}
	}
	writeJSON(w, map[string]any{"gpus": gpus})
}

func (s *Server) handleDisks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, map[string]any{"devices": tools.Disks(ctx)})
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, tools.Network())
}

// ---------- tools ----------

func (s *Server) handleCPUInfo(w http.ResponseWriter, r *http.Request) {
	smp, _ := s.sampler.Latest()
	writeJSON(w, map[string]any{
		"static":  collect.CPUStatic(),
		"current": smp.CPU,
		"history": cpuHistory(s.sampler.History(120)),
	})
}

func cpuHistory(h []collect.Sample) []map[string]float64 {
	out := make([]map[string]float64, 0, len(h))
	for _, s := range h {
		out = append(out, map[string]float64{
			"t": s.T, "total": s.CPU.Total, "user": s.CPU.User, "system": s.CPU.System,
		})
	}
	return out
}

func (s *Server) handleUptime(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, tools.Uptime(ctx, collect.UptimeSeconds(), collect.BootTime(), collect.IdleSeconds(), runtime.NumCPU()))
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, tools.Locale(ctx))
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Features.APT {
		writeErr(w, http.StatusNotImplemented, "unavailable", "apt-integratie staat uit")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	writeJSON(w, tools.Updates(ctx))
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	smp, _ := s.sampler.Latest()
	writeJSON(w, tools.Processes(smp.Memory.Total))
}

func (s *Server) handleLogSources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, map[string]any{"sources": tools.LogSources(ctx, s.cfg.Logs.Units, s.cfg.Logs.Files)})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Features.Logs {
		writeErr(w, http.StatusNotImplemented, "unavailable", "logs staan uit")
		return
	}
	q := r.URL.Query()
	lines, _ := strconv.Atoi(q.Get("lines"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := tools.Logs(ctx, q.Get("source"), lines, q.Get("since"), q.Get("priority"), q.Get("q"),
		s.cfg.Logs.Units, s.cfg.Logs.Files)
	if err != nil {
		writeErr(w, http.StatusForbidden, "invalid_argument", err.Error())
		return
	}
	writeJSON(w, map[string]any{"source": q.Get("source"), "lines": res})
}

// ---------- jobs ----------

func (s *Server) handleJobCreate(w http.ResponseWriter, r *http.Request) {
	var req tools.JobRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_argument", "onleesbaar verzoek")
		return
	}
	if req.Type == "speedtest" && !s.cfg.Features.Speedtest {
		writeErr(w, http.StatusNotImplemented, "unavailable", "speedtest staat uit")
		return
	}
	if req.Type == "iperf3" && !tools.Has("iperf3") {
		writeErr(w, http.StatusNotImplemented, "unavailable", "iperf3 is not installed")
		return
	}
	if req.Type == "geekbench" && !tools.GeekbenchInstalled() {
		writeErr(w, http.StatusNotImplemented, "unavailable", "Geekbench is not installed")
		return
	}
	job, err := s.jobs.Submit(req)
	if err != nil {
		writeErr(w, http.StatusTooManyRequests, "unavailable", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, job)
}

func (s *Server) handleJobGet(w http.ResponseWriter, r *http.Request) {
	job, ok := s.jobs.Get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "taak niet gevonden")
		return
	}
	writeJSON(w, job)
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	if !s.jobs.Cancel(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "not_found", "no running job with that id")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var _ = fmt.Sprintf
