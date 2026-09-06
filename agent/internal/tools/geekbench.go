package tools

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// GeekbenchResult komt terug zodra de run klaar is; dit is de samenvatting
// uit Geekbench's platte-tekstoutput gelicht, zodat de app niet zelf hoeft
// te parsen om de score groot te kunnen tonen.
type GeekbenchResult struct {
	SingleCore int    `json:"single_core_score,omitempty"`
	MultiCore  int    `json:"multi_core_score,omitempty"`
	ResultURL  string `json:"result_url,omitempty"`
}

var (
	gbSingleRe = regexp.MustCompile(`Single-Core Score\s+(\d+)`)
	gbMultiRe  = regexp.MustCompile(`Multi-Core Score\s+(\d+)`)
	gbURLRe    = regexp.MustCompile(`https://browser\.geekbench\.com/\S+`)
	// Geekbench print deze twee regels als section-kop, los op hun eigen
	// regel, vlak voordat het de bijbehorende subtests start.
	gbSectionRe = regexp.MustCompile(`^(Single-Core|Multi-Core)\s*$`)
)

// geekbenchJob draait de gratis, anonieme flow: geen account nodig, het
// resultaat wordt automatisch geüpload en de CLI print er een link bij die je
// zonder in te loggen kunt bekijken.
//
// Bewust géén Pro-licentie hier. De CLI kent alleen `--unlock EMAIL KEY`, en
// dat is een eenmalige activering van de machine — geen vlag die je per run
// meegeeft. (Er bestaan geen --username/--password vlaggen; die stonden hier
// eerder wél en zouden een run met ingevulde licentie juist laten falen.)
// Zie: support.primatelabs.com/kb/geekbench/geekbench-6-command-line-tool
func (r *Runner) geekbenchJob(ctx context.Context, id string) (any, error) {
	bin, ok := geekbenchBinary()
	if !ok {
		return nil, fmt.Errorf("no Geekbench build exists for this architecture")
	}
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = childEnv()
	// Geekbench forkt een apart workerproces per benchmark; zonder dit sterft
	// bij het stoppen alleen de launcher en blijft de worker (met de
	// stdout-pipe nog open) gewoon doordraaien — zie setpgroupCancel.
	setpgroupCancel(cmd)
	// stdout én stderr door dezelfde pipe: Geekbench schrijft zijn voortgang,
	// de scores en een eventuele foutmelding door elkaar heen, en we willen ze
	// alle drie zien — de foutmelding om hem door te kunnen geven aan de app.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	r.progress(id, "single", 0, 0, 0)
	var res GeekbenchResult
	// Laatste regels vasthouden: als Geekbench faalt staat de reden in zijn
	// eigen output, en zonder dit bleef er voor de gebruiker niets over dan
	// "exit status 255" — een getal waar niemand iets aan heeft.
	var tail []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if t := strings.TrimSpace(line); t != "" {
			tail = append(tail, t)
			if len(tail) > 12 {
				tail = tail[1:]
			}
		}
		if m := gbSectionRe.FindStringSubmatch(line); m != nil {
			phase := "single"
			if m[1] == "Multi-Core" {
				phase = "multi"
			}
			r.progress(id, phase, 0, 0, 0)
		}
		if m := gbSingleRe.FindStringSubmatch(line); m != nil {
			res.SingleCore, _ = strconv.Atoi(m[1])
		}
		if m := gbMultiRe.FindStringSubmatch(line); m != nil {
			res.MultiCore, _ = strconv.Atoi(m[1])
		}
		if m := gbURLRe.FindString(line); m != "" {
			res.ResultURL = m
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // gestopt door de gebruiker, niet mislukt
		}
		if msg := gbFailure(tail); msg != "" {
			return nil, fmt.Errorf("geekbench: %s", msg)
		}
		return nil, fmt.Errorf("geekbench: %v", err)
	}
	if res.ResultURL == "" && res.SingleCore == 0 && res.MultiCore == 0 {
		return nil, fmt.Errorf("geekbench produced no readable result")
	}
	return res, nil
}

// gbFailure haalt uit de laatste regels output de regel die zegt wát er mis
// ging. Geekbench meldt een mislukte upload als "unknown error (internal code
// 35)" — dat getal is een libcurl-code, dus voor de bekende gevallen zetten we
// er iets bij waar een mens wél iets aan heeft.
func gbFailure(tail []string) string {
	for i := len(tail) - 1; i >= 0; i-- {
		line := tail[i]
		if !strings.Contains(strings.ToLower(line), "error") {
			continue
		}
		if m := gbCurlCodeRe.FindStringSubmatch(line); m != nil {
			if hint, ok := curlHints[m[1]]; ok {
				return line + " — " + hint
			}
		}
		return line
	}
	return ""
}

var gbCurlCodeRe = regexp.MustCompile(`internal code (\d+)`)

// De libcurl-codes die bij het uploaden van een resultaat realistisch zijn.
// Geekbench print alleen het nummer; dit vertaalt het naar de oorzaak.
var curlHints = map[string]string{
	"6":  "could not resolve the Geekbench host (DNS)",
	"7":  "could not connect to the Geekbench server",
	"28": "the upload timed out",
	"35": "TLS handshake with the Geekbench server failed",
	"60": "could not verify the Geekbench server's certificate",
}
