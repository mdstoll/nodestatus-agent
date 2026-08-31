package tools

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

// GeekbenchResult komt terug zodra de run klaar is; de live regels
// (Job.Lines) zijn intussen al naar de app gestreamd — dit is puur de
// samenvatting eruit gelicht, zodat de app niet zelf tekst hoeft te parsen
// om de score groot te kunnen tonen.
type GeekbenchResult struct {
	SingleCore int    `json:"single_core_score,omitempty"`
	MultiCore  int    `json:"multi_core_score,omitempty"`
	ResultURL  string `json:"result_url,omitempty"`
}

var (
	gbSingleRe = regexp.MustCompile(`Single-Core Score\s+(\d+)`)
	gbMultiRe  = regexp.MustCompile(`Multi-Core Score\s+(\d+)`)
	gbURLRe    = regexp.MustCompile(`https://browser\.geekbench\.com/\S+`)
)

// geekbenchJob draait de gratis, anonieme flow: geen account nodig, het
// resultaat wordt automatisch geüpload en de CLI print er een link bij die
// je zonder in te loggen kunt bekijken. Alleen met --username/--password
// (Pro-account) wordt onder die account geüpload in plaats van anoniem.
func (r *Runner) geekbenchJob(ctx context.Context, id string, req JobRequest) (any, error) {
	bin, ok := geekbenchBinary()
	if !ok {
		return nil, fmt.Errorf("no Geekbench build exists for this architecture")
	}
	args := []string{}
	if req.Username != "" && req.Password != "" {
		args = append(args, "--username", req.Username, "--password", req.Password)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = childEnv()
	// stdout én stderr door elkaar: Geekbench schrijft voortgang en de
	// uiteindelijke link door elkaar heen, en de app toont dit als één
	// doorlopende terminal — precies zoals je het lokaal ook zou zien.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var res GeekbenchResult
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		r.appendLine(id, line)
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
