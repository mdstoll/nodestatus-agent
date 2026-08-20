package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"nodestatus/internal/pki"
)

// handleStream levert de 1 Hz SSE-stream. Eerst optioneel backfill uit de
// ringbuffer zodat de grafiek in de app meteen gevuld is, daarna live.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal", "streaming niet ondersteund")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // nginx: niet bufferen
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "retry: 2000\n\n")
	flusher.Flush()

	if n, _ := strconv.Atoi(r.URL.Query().Get("backfill")); n > 0 {
		for _, smp := range s.sampler.History(n) {
			b, err := json.Marshal(smp)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: backfill\ndata: %s\n\n", b)
		}
		flusher.Flush()
	}

	ch := s.sampler.Subscribe()
	defer s.sampler.Unsubscribe(ch)

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	// Wie deze stream opende. Een SSE-verbinding is één langlopend request,
	// dus de autorisatie van bij het openen zou blijven gelden: intrekken had
	// pas effect als de app zelf opnieuw verbond. Daarom hier per sample
	// opnieuw kijken of het apparaat nog op de allowlist staat — dat is een
	// map-lookup en kost niets.
	var fp string
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		fp = pki.CertFingerprint(r.TLS.PeerCertificates[0])
	}
	stillAllowed := func() bool {
		if fp == "" {
			return s.clientIP(r).IsLoopback()
		}
		_, ok := s.store.Lookup(fp)
		return ok
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case smp, ok := <-ch:
			if !ok {
				return
			}
			if !stillAllowed() {
				s.log.Info("stream closed, device revoked")
				fmt.Fprint(w, "event: revoked\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			b, err := json.Marshal(smp)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: sample\nid: %d\ndata: %s\n\n", int64(smp.T), b)
			flusher.Flush()
		case <-keepalive.C:
			if !stillAllowed() {
				return
			}
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}
