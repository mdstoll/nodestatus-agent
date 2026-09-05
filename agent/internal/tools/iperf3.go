package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Iperf3Result — zelfde vorm als SpeedtestResult (up/down in bits/s), zodat
// de app hem met exact dezelfde widgets kan tonen als de gewone speedtest.
type Iperf3Result struct {
	UploadBps   float64 `json:"upload_bps"`
	DownloadBps float64 `json:"download_bps"`
	Target      string  `json:"target"`
	Port        int     `json:"port"`
}

// Matcht zowel de per-seconde regels als de twee samenvattingsregels aan het
// eind; groep 5 is leeg voor een per-seconde regel en "sender"/"receiver"
// voor een samenvatting.
var iperf3LineRe = regexp.MustCompile(
	`^\[\s*\d+\]\s+[\d.]+-([\d.]+)\s+sec\s+[\d.]+\s+\S*Bytes\s+([\d.]+)\s+([KMG]?)bits/sec(?:\s+\S+)*?(\s+(sender|receiver))?\s*$`)

func iperf3Bps(value, prefix string) float64 {
	v, _ := strconv.ParseFloat(value, 64)
	switch prefix {
	case "K":
		return v * 1e3
	case "M":
		return v * 1e6
	case "G":
		return v * 1e9
	}
	return v
}

// iperf3Job meet in twee losse runs na elkaar — upload (het standaardgedrag
// van iperf3: de client stuurt) en download (-R, de server stuurt) — zodat
// de app dezelfde twee cijfers krijgt als bij de gewone speedtest.
func (r *Runner) iperf3Job(ctx context.Context, id string, req JobRequest) (any, error) {
	target, err := ValidTarget(req.Target)
	if err != nil {
		return nil, err
	}
	port := ClampInt(req.Port, 1, 65535, 5201)
	const duration = 10 // seconden per richting — iperf3's eigen default

	up, err := r.iperf3Direction(ctx, id, target, port, duration, false, 0)
	if err != nil {
		return nil, err
	}
	down, err := r.iperf3Direction(ctx, id, target, port, duration, true, 0.5)
	if err != nil {
		return nil, err
	}
	return Iperf3Result{UploadBps: up, DownloadBps: down, Target: target, Port: port}, nil
}

func (r *Runner) iperf3Direction(ctx context.Context, id, target string, port, duration int, reverse bool, progressBase float64) (float64, error) {
	phase := "upload"
	// --forceflush: zonder een tty is iperf3's stdout volledig gebufferd (niet
	// per regel), dus zonder deze vlag komen alle per-seconde regels pas in
	// één klap binnen zodra het proces stopt — de live snelheid bleef daardoor
	// de hele richting op 0 staan, ongeacht hoe kort de job-timeout was.
	args := []string{"-c", target, "-p", strconv.Itoa(port), "-t", strconv.Itoa(duration), "-i", "1", "--forceflush"}
	if reverse {
		phase = "download"
		args = append(args, "-R")
	}
	path, ok := Bin("iperf3")
	if !ok {
		return 0, fmt.Errorf("iperf3 is not installed")
	}
	// Meteen bij het starten van deze richting, niet pas bij de eerste
	// per-seconde regel: anders blijft de laatste bps van de vórige richting
	// nog even op het scherm staan onder het nieuwe label — iperf3's eigen
	// verbindingsopzet kost al ~1 s voor die eerste regel er is.
	r.progress(id, phase, progressBase, 0, 0)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = childEnv()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	// iperf3 schrijft "unable to connect to server: Connection refused" en
	// "the server is busy running a test" naar stderr. Zonder dit op te vangen
	// bleef daar alleen "exit status 1" van over — een foutmelding waar de
	// gebruiker niets aan heeft, terwijl iperf3 zelf precies zegt wat er mis is.
	var errb bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &errb, n: 8 << 10}
	if err := cmd.Start(); err != nil {
		return 0, err
	}

	var finalBps float64
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		m := iperf3LineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		bps := iperf3Bps(m[2], m[3])
		switch m[5] {
		case "": // per-seconde tussenmeting
			endSec, _ := strconv.ParseFloat(m[1], 64)
			p := progressBase + min(endSec/float64(duration), 1)*0.5
			r.progress(id, phase, p, bps, 0)
		case "sender":
			if !reverse {
				finalBps = bps // wat wíj hebben verstuurd
			}
		case "receiver":
			if reverse {
				finalBps = bps // wat wíj hebben ontvangen
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return 0, fmt.Errorf("iperf3: %s", firstLine(msg))
		}
		return 0, fmt.Errorf("iperf3: %v", err)
	}
	if finalBps == 0 {
		return 0, fmt.Errorf("iperf3 produced no result — is a server listening on %s:%d?", target, port)
	}
	return finalBps, nil
}
