package tools

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
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
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
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
		return nil, fmt.Errorf("geekbench: %v", err)
	}
	if res.ResultURL == "" && res.SingleCore == 0 && res.MultiCore == 0 {
		return nil, fmt.Errorf("geekbench produced no readable result")
	}
	return res, nil
}
