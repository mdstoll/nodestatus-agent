package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
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

	for {
		select {
		case <-r.Context().Done():
			return
		case smp, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(smp)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: sample\nid: %d\ndata: %s\n\n", int64(smp.T), b)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}
