package tools

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ---------- probes ----------

func probeIperf3(ctx context.Context) Capability {
	if !Has("iperf3") {
		return Capability{ID: "iperf3", Reason: "iperf3 is not installed",
			Fix: "sudo nodestatus-agent extras install iperf3"}
	}
	return Capability{ID: "iperf3", OK: true}
}

// GeekbenchInstalled is the public check the API handler uses to reject a
// job before it even starts, same as Has("iperf3") does for that job type.
func GeekbenchInstalled() bool {
	p, ok := geekbenchBinary()
	if !ok {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func probeGeekbench(ctx context.Context) Capability {
	if GeekbenchInstalled() {
		return Capability{ID: "geekbench", OK: true}
	}
	if !geekbenchSupported() {
		return Capability{ID: "geekbench", Reason: "no Geekbench build exists for this architecture (" + runtime.GOARCH + ")"}
	}
	return Capability{ID: "geekbench", Reason: "Geekbench is not installed",
		Fix: "sudo nodestatus-agent extras install geekbench"}
}

// ---------- installation ----------

// StandardPackages is exactly install.sh's own list — the two are meant to
// stay identical, so a change here without a matching change there (or vice
// versa) is a bug, not a style choice.
var StandardPackages = []string{
	"smartmontools", "whois", "dnsutils", "iputils-ping", "traceroute",
	"qrencode", "lm-sensors",
}

// InstallStep is one package/tool's outcome, reported back to whoever asked
// for the install (the CLI, or eventually an app-triggered cold request).
type InstallStep struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

// InstallAptPackage runs `apt-get install` for exactly one package. One at a
// time and not all together: a single package apt-get can't resolve (wrong
// architecture, renamed since) fails the *entire* call, which used to take
// every other package down with it — see install.sh for the same fix.
//
// Wijkt bewust af van de pakketregels bovenaan exec.go (absoluut pad uit
// Discover, minimale env): dit draait vanuit de CLI, waar Discover() niet
// gedraaid heeft, en apt-get heeft zijn eigen PATH en omgeving nodig om
// maintainer-scripts te kunnen uitvoeren.
func InstallAptPackage(ctx context.Context, pkg string) InstallStep {
	cmd := exec.CommandContext(ctx, "apt-get", "install", "-y", "-qq", pkg)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return InstallStep{Name: pkg, Note: firstLine(string(out))}
	}
	return InstallStep{Name: pkg, OK: true}
}

// InstallStandardExtras installs the same optional dependencies install.sh
// installs (minus intel-gpu-tools, which is x86-only and handled there by
// its own arch check — this entry point is for iperf3/Geekbench, not a
// second implementation of install.sh's own step).
func InstallStandardExtras(ctx context.Context) []InstallStep {
	_ = exec.CommandContext(ctx, "apt-get", "update", "-qq").Run()
	steps := make([]InstallStep, 0, len(StandardPackages))
	for _, pkg := range StandardPackages {
		steps = append(steps, InstallAptPackage(ctx, pkg))
	}
	return steps
}

// geekbenchExtrasDir is fixed rather than PATH-based: Geekbench isn't a
// distro package, so there's nothing for a package manager to put on PATH.
// Root-owned, matching where the rest of the agent's own files live.
const geekbenchExtrasDir = "/opt/nodestatus-agent/extras/geekbench"

// geekbenchVersion is pinned, not "latest" — Geekbench has no stable
// "latest" URL (see the download function below), so this needs a manual
// bump when a newer release is worth picking up. Bump both together: the
// stable and preview builds are versioned in lockstep.
const geekbenchVersion = "6.7.0"

func geekbenchSupported() bool {
	return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
}

// geekbenchBinary geeft het pad terug dat InstallGeekbench zou gebruiken,
// ongeacht of het bestand er al staat.
func geekbenchBinary() (string, bool) {
	switch runtime.GOARCH {
	case "amd64":
		return filepath.Join(geekbenchExtrasDir, "Geekbench-"+geekbenchVersion+"-Linux", "geekbench6"), true
	case "arm64":
		return filepath.Join(geekbenchExtrasDir, "Geekbench-"+geekbenchVersion+"-LinuxARMPreview", "geekbench6"), true
	}
	return "", false
}

// InstallGeekbench downloads and extracts Geekbench 6 for this machine's
// architecture. amd64 gets the stable Linux build; arm64 gets Primate Labs'
// own "LinuxARMPreview" build — there is no non-preview Linux ARM release,
// preview is simply what ships for that architecture. Neither 32-bit x86 nor
// 32-bit ARM has a Geekbench 6 build at all.
func InstallGeekbench(ctx context.Context) InstallStep {
	if !geekbenchSupported() {
		return InstallStep{Name: "geekbench", Note: "no Geekbench 6 build exists for " + runtime.GOARCH}
	}
	var filename string
	switch runtime.GOARCH {
	case "amd64":
		filename = fmt.Sprintf("Geekbench-%s-Linux.tar.gz", geekbenchVersion)
	case "arm64":
		filename = fmt.Sprintf("Geekbench-%s-LinuxARMPreview.tar.gz", geekbenchVersion)
	}
	url := "https://cdn.geekbench.com/" + filename

	if err := os.MkdirAll(geekbenchExtrasDir, 0o755); err != nil {
		return InstallStep{Name: "geekbench", Note: err.Error()}
	}

	dlCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return InstallStep{Name: "geekbench", Note: err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InstallStep{Name: "geekbench", Note: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return InstallStep{Name: "geekbench", Note: fmt.Sprintf("download failed: HTTP %d for %s", resp.StatusCode, url)}
	}

	if err := extractTarGz(resp.Body, geekbenchExtrasDir); err != nil {
		return InstallStep{Name: "geekbench", Note: err.Error()}
	}
	// Eerst kijken óf het er staat, dan pas chmod: andersom meldt een gewijzigde
	// archiefindeling zich als een verwarrende chmod-fout in plaats van als het
	// echte probleem.
	bin, _ := geekbenchBinary()
	if _, err := os.Stat(bin); err != nil {
		return InstallStep{Name: "geekbench", Note: "extracted but " + bin + " is missing — archive layout may have changed"}
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return InstallStep{Name: "geekbench", Note: "extracted, but couldn't make it executable: " + err.Error()}
	}
	return InstallStep{Name: "geekbench", OK: true}
}

// extractTarGz is a plain, non-symlink-following extractor: Geekbench's own
// tarball, not user-supplied input, but there is no reason to trust archive
// paths any further than necessary.
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.Clean(hdr.Name))
		if !isWithin(dest, target) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.CopyN(f, tr, hdr.Size); err != nil && err != io.EOF {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
