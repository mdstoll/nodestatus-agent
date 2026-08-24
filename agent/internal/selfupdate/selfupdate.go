// Package selfupdate implements `nodestatus-agent update`: check GitHub
// releases, download the matching tarball, verify it against SHA256SUMS, and
// replace the running binary in place.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repo = "mdstoll/nodestatus-agent"

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckResult is what both the CLI and the API expose.
type CheckResult struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	UpdateURL string `json:"release_url"`
	Available bool   `json:"available"`
}

func httpClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

// semver does a plain major.minor.patch comparison. Good enough for our own
// tags (vX.Y.Z); anything that doesn't parse cleanly is treated as older, so
// a malformed or missing version never wins a comparison it shouldn't.
type semver [3]int

func parseSemver(s string) (semver, bool) {
	var v semver
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 1 {
		return v, false
	}
	ok := true
	for i, p := range parts {
		if i > 2 {
			break
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			ok = false
			break
		}
		v[i] = n
	}
	return v, ok
}

// newer reports whether a is a strictly greater version than b. If either
// fails to parse, it refuses to call a newer — an update check must never
// treat "dev" or a malformed tag as newer than a real release.
func newer(a, b string) bool {
	va, oka := parseSemver(a)
	vb, okb := parseSemver(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func fetchRelease() (*release, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("unreadable response: %w", err)
	}
	return &r, nil
}

// Check compares the running version against the latest GitHub release.
// Never fails loudly: a network hiccup should not break /v1/system.
func Check(currentVersion string) CheckResult {
	res := CheckResult{Current: currentVersion}
	r, err := fetchRelease()
	if err != nil {
		return res
	}
	res.Latest = strings.TrimPrefix(r.TagName, "v")
	res.UpdateURL = "https://github.com/" + repo + "/releases/tag/" + r.TagName
	res.Available = currentVersion != "dev" && newer(res.Latest, currentVersion)
	return res
}

func archName() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	case "arm":
		return "arm", nil
	default:
		return "", fmt.Errorf("no release is published for %s", runtime.GOARCH)
	}
}

// Apply downloads the latest release, verifies it, and replaces the binary
// currently running. It does not restart the service — the caller (the CLI,
// run by hand or from a cron job) does that, so the exit status of this
// process is unambiguous.
func Apply(currentVersion string) (newVersion string, err error) {
	arch, err := archName()
	if err != nil {
		return "", err
	}
	r, err := fetchRelease()
	if err != nil {
		return "", err
	}
	latest := strings.TrimPrefix(r.TagName, "v")
	if !newer(latest, currentVersion) {
		return latest, errAlreadyCurrent
	}

	tarballName := fmt.Sprintf("nodestatus-agent_linux_%s.tar.gz", arch)
	var tarballURL, sumsURL string
	for _, a := range r.Assets {
		switch a.Name {
		case tarballName:
			tarballURL = a.BrowserDownloadURL
		case "SHA256SUMS":
			sumsURL = a.BrowserDownloadURL
		}
	}
	if tarballURL == "" {
		return "", fmt.Errorf("release %s has no asset for this architecture (%s)", r.TagName, arch)
	}

	tmpDir, err := os.MkdirTemp("", "nodestatus-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, tarballName)
	if err := download(tarballURL, tarPath); err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	if sumsURL != "" {
		sumsPath := filepath.Join(tmpDir, "SHA256SUMS")
		if err := download(sumsURL, sumsPath); err != nil {
			return "", fmt.Errorf("could not fetch SHA256SUMS: %w", err)
		}
		if err := verify(tarPath, tarballName, sumsPath); err != nil {
			return "", err
		}
	}

	binPath, err := extractBinary(tarPath, tmpDir)
	if err != nil {
		return "", err
	}

	self, err := os.Executable()
	if err != nil {
		self = "/usr/local/bin/nodestatus-agent"
	}
	if err := replaceBinary(binPath, self); err != nil {
		return "", err
	}
	return latest, nil
}

var errAlreadyCurrent = fmt.Errorf("already up to date")

func IsAlreadyCurrent(err error) bool { return err == errAlreadyCurrent }

func download(url, dest string) error {
	resp, err := httpClient().Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, 64<<20)) // 64 MB is generous for a 3 MB tarball
	return err
}

func verify(tarPath, name, sumsPath string) error {
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	var want string
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			want = f[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("checksum mismatch: expected %s, got %s — refusing to install", want, got)
	}
	return nil
}

func extractBinary(tarPath, dir string) (string, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) != "nodestatus-agent" {
			continue
		}
		out := filepath.Join(dir, "nodestatus-agent.new")
		w, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			return "", err
		}
		w.Close()
		return out, nil
	}
	return "", fmt.Errorf("tarball did not contain the nodestatus-agent binary")
}

// replaceBinary swaps the file at dest for src. Rename is atomic on the same
// filesystem; /tmp and /usr/local/bin are not guaranteed to be, so fall back
// to a copy when the rename fails across devices.
func replaceBinary(src, dest string) error {
	if err := os.Chmod(src, 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()
	return os.Rename(tmp, dest)
}

// RestartService restarts the systemd unit so the new binary takes over.
func RestartService() error {
	return exec.Command("systemctl", "restart", "nodestatus-agent").Run()
}

// Checker caches the result of Check() and refreshes it in the background,
// so /v1/system can report "an update is available" without hitting GitHub
// on every request. GitHub's unauthenticated rate limit is 60 requests per
// hour per IP — several agents behind the same NAT checking every request
// would burn through that on their own.
type Checker struct {
	mu      sync.RWMutex
	result  CheckResult
	version string
	// last houdt bij wanneer er voor het laatst écht bij GitHub is gekeken,
	// zodat een geforceerde controle niet als knop-spam de rate limit opeet.
	last time.Time
}

// checkInterval is deliberately measured in hours: nobody needs to know
// about a new release within the minute, and this keeps every agent well
// under the shared rate limit even behind a NAT with several of them.
const checkInterval = 6 * time.Hour

// minForcedInterval begrenst hoe vaak "nu controleren" echt het netwerk op mag.
const minForcedInterval = 30 * time.Second

func NewChecker(version string) *Checker {
	c := &Checker{result: CheckResult{Current: version}, version: version}
	go func() {
		c.refresh(version)
		for range time.Tick(checkInterval) {
			c.refresh(version)
		}
	}()
	return c
}

func (c *Checker) refresh(version string) {
	res := Check(version)
	c.mu.Lock()
	c.result = res
	c.last = time.Now()
	c.mu.Unlock()
}

// Refresh kijkt nu meteen bij GitHub in plaats van te wachten op de volgende
// ronde van zes uur. Bedoeld voor de "nu controleren"-knop in de app: zonder
// dit kon je na het uitbrengen van een release tot zes uur naar een verouderd
// antwoord zitten kijken zonder te weten of het klopte.
//
// Er zit een ondergrens op zodat herhaald tikken de ongeauthenticeerde
// GitHub-limiet (60 per uur) niet opmaakt; binnen die tijd krijg je het
// laatste antwoord terug.
func (c *Checker) Refresh() CheckResult {
	c.mu.RLock()
	recent := time.Since(c.last) < minForcedInterval
	c.mu.RUnlock()
	if !recent {
		c.refresh(c.version)
	}
	return c.Result()
}

func (c *Checker) Result() CheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.result
}
