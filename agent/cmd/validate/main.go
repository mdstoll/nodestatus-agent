// Command validate is een testclient die precies doet wat de iOS-app doet:
// een sleutelpaar maken, koppelen, en daarna met mutual TLS alle endpoints
// aflopen — inclusief de negatieve tests.
//
// Gebruik:
//
//	sudo nodestatus-agent enroll --new        # op de server, geeft een code
//	SI_HOST=192.168.1.102:29500 SI_CODE=XXXXXXXX go run ./cmd/validate
//	SI_HOST=... SI_CODE=... SI_DUMP=/v1/metrics go run ./cmd/validate
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	host = os.Getenv("SI_HOST")
	code = os.Getenv("SI_CODE")
)

type enrollResp struct {
	DeviceID      string `json:"device_id"`
	ClientCertPEM string `json:"client_cert_pem"`
	CACertPEM     string `json:"ca_cert_pem"`
	APIToken      string `json:"api_token"`
	ExpiresAt     int64  `json:"expires_at"`
	Hostname      string `json:"hostname"`
}

func main() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	pubRaw := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)

	// 1. Enrollment over TLS (server nog niet gevalideerd tegen CA — die krijgen we hier)
	body, _ := json.Marshal(map[string]string{
		"code":           code,
		"public_key_b64": base64.StdEncoding.EncodeToString(pubRaw),
		"device_name":    "Validatieclient",
	})
	insecure := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	resp, err := insecure.Post("https://"+host+"/v1/enroll", "application/json", bytes.NewReader(body))
	must(err)
	rb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Printf("ENROLL MISLUKT %d: %s\n", resp.StatusCode, rb)
		os.Exit(1)
	}
	var er enrollResp
	must(json.Unmarshal(rb, &er))
	fmt.Printf("✔ enrollment  device=%s host=%s cert geldig tot %s\n",
		er.DeviceID, er.Hostname, time.Unix(er.ExpiresAt, 0).Format("2006-01-02"))

	// 2. Bouw de mTLS-client: eigen cert + CA als enige trust anchor
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	clientCert, err := tls.X509KeyPair([]byte(er.ClientCertPEM), keyPEM)
	must(err)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(er.CACertPEM)) {
		fmt.Println("CA niet bruikbaar")
		os.Exit(1)
	}
	client := &http.Client{Timeout: 150 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      pool,
			MinVersion:   tls.VersionTLS12,
		},
	}}

	// 3. Bewijs dat een niet-gekoppelde client niets krijgt. Verse transport,
	//    anders wordt de keep-alive-verbinding van het enrollment hergebruikt
	//    en is de handshake al gedaan.
	fresh := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DisableKeepAlives: true,
	}}
	if r2, err := fresh.Get("https://" + host + "/v1/system"); err != nil {
		fmt.Printf("✔ zonder client-certificaat: TLS-handshake geweigerd (%s)\n", shorten(err.Error()))
	} else {
		fmt.Printf("✖ ZONDER CERT TOCH BINNEN: status %d\n", r2.StatusCode)
		r2.Body.Close()
	}

	get := func(path string) (int, []byte) {
		req, _ := http.NewRequest("GET", "https://"+host+path, nil)
		req.Header.Set("Authorization", "Bearer "+er.APIToken)
		rr, err := client.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		defer rr.Body.Close()
		b, _ := io.ReadAll(rr.Body)
		return rr.StatusCode, b
	}

	fmt.Println()
	for _, p := range []string{
		"/v1/health", "/v1/system", "/v1/metrics",
		"/v1/hardware/sensors", "/v1/hardware/smart", "/v1/hardware/gpu",
		"/v1/hardware/disks", "/v1/hardware/network",
		"/v1/tools/cpuinfo", "/v1/tools/uptime", "/v1/tools/locale",
		"/v1/tools/updates", "/v1/tools/processes",
		"/v1/tools/logs/sources", "/v1/tools/logs?source=unit:ssh&lines=5",
		"/v1/devices",
	} {
		st, b := get(p)
		mark := "✔"
		if st != 200 {
			mark = "✖"
		}
		fmt.Printf("%s %-42s %3d  %6d bytes  %s\n", mark, p, st, len(b), preview(b))
	}

	// 4. Verkeerd token moet 401 geven
	req, _ := http.NewRequest("GET", "https://"+host+"/v1/system", nil)
	req.Header.Set("Authorization", "Bearer fout-token")
	if rr, err := client.Do(req); err == nil {
		mark := "✔"
		if rr.StatusCode != 401 {
			mark = "✖"
		}
		fmt.Printf("%s verkeerd token → %d (verwacht 401)\n", mark, rr.StatusCode)
		rr.Body.Close()
	}

	// 5. Whitelist-bypass moet geweigerd worden
	st, _ := get("/v1/tools/logs?source=file:/etc/shadow&lines=5")
	mark := "✔"
	if st != 403 {
		mark = "✖"
	}
	fmt.Printf("%s /etc/shadow via logs → %d (verwacht 403)\n", mark, st)

	if d := os.Getenv("SI_DUMP"); d != "" {
		_, b := get(d)
		var v any
		json.Unmarshal(b, &v)
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return
	}

	// 6. SSE-stream
	fmt.Println()
	testStream(client, er.APIToken)
}

func testStream(c *http.Client, token string) {
	req, _ := http.NewRequest("GET", "https://"+host+"/v1/stream?backfill=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.Do(req)
	if err != nil {
		fmt.Println("✖ stream:", err)
		return
	}
	defer resp.Body.Close()
	buf := make([]byte, 8192)
	var events, samples int
	start := time.Now()
	var acc strings.Builder
	done := make(chan struct{})
	go func() { time.Sleep(4 * time.Second); resp.Body.Close(); close(done) }()
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			acc.WriteString(chunk)
			events += strings.Count(chunk, "event:")
			samples += strings.Count(chunk, "event: sample")
		}
		if err != nil {
			break
		}
	}
	<-done
	fmt.Printf("✔ SSE-stream: %d events in %.1fs (waarvan %d live samples)\n", events, time.Since(start).Seconds(), samples)
	s := acc.String()
	if i := strings.Index(s, "event: sample"); i >= 0 {
		line := s[i:]
		if j := strings.Index(line, "\n\n"); j > 0 {
			line = line[:j]
		}
		if len(line) > 260 {
			line = line[:260] + "…"
		}
		fmt.Println("  eerste sample:", line)
	}
}

func preview(b []byte) string {
	s := strings.ReplaceAll(string(b), "\n", " ")
	if len(s) > 68 {
		s = s[:68] + "…"
	}
	return s
}

func shorten(s string) string {
	if len(s) > 60 {
		return "…" + s[len(s)-60:]
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Println("FOUT:", err)
		os.Exit(1)
	}
}
