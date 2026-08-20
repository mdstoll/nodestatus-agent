// Package store bewaart gekoppelde apparaten en beheert het enrollment-venster.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Device struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Fingerprint string    `json:"fingerprint"` // SHA-256 van het client-certificaat
	TokenHash   string    `json:"token_hash"`  // SHA-256 van het bearer token
	EnrolledAt  time.Time `json:"enrolled_at"`
	LastSeen    time.Time `json:"last_seen"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	devices map[string]*Device // key = fingerprint

	// enrollment-venster
	enrollCode     string
	enrollExpires  time.Time
	enrollAttempts int
	maxAttempts    int
}

func New(dir string, maxAttempts int) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path:        filepath.Join(dir, "devices.json"),
		devices:     map[string]*Device{},
		maxAttempts: maxAttempts,
	}
	if b, err := os.ReadFile(s.path); err == nil {
		var list []*Device
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("devices.json onleesbaar: %w", err)
		}
		for _, d := range list {
			s.devices[d.Fingerprint] = d
		}
	}
	return s, nil
}

func (s *Store) save() error {
	list := make([]*Device, 0, len(s.devices))
	for _, d := range s.devices {
		list = append(list, d)
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}

func (s *Store) List() []*Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Device, 0, len(s.devices))
	for _, d := range s.devices {
		c := *d
		out = append(out, &c)
	}
	return out
}

// Lookup zoekt een apparaat op certificaat-fingerprint. Dit is de allowlist:
// een ingetrokken apparaat staat er niet meer in en is dus meteen geweigerd.
func (s *Store) Lookup(fp string) (*Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.devices[fp]
	return d, ok
}

func (s *Store) VerifyToken(d *Device, token string) bool {
	sum := sha256.Sum256([]byte(token))
	want, err := hex.DecodeString(d.TokenHash)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}

func (s *Store) Touch(fp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.devices[fp]; ok {
		d.LastSeen = time.Now()
	}
}

func (s *Store) Add(name, fingerprint string, expires time.Time) (*Device, string, error) {
	token, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(token))
	d := &Device{
		ID:          "d_" + randHex(4),
		Name:        name,
		Fingerprint: fingerprint,
		TokenHash:   hex.EncodeToString(sum[:]),
		EnrolledAt:  time.Now(),
		LastSeen:    time.Now(),
		ExpiresAt:   expires,
	}
	s.mu.Lock()
	s.devices[fingerprint] = d
	err = s.save()
	s.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	return d, token, nil
}

// Replace vervangt het certificaat van een bestaand apparaat (vernieuwing).
func (s *Store) Replace(oldFP, newFP string, expires time.Time) (*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[oldFP]
	if !ok {
		return nil, fmt.Errorf("onbekend apparaat")
	}
	delete(s.devices, oldFP)
	d.Fingerprint = newFP
	d.ExpiresAt = expires
	s.devices[newFP] = d
	return d, s.save()
}

func (s *Store) Revoke(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for fp, d := range s.devices {
		if d.ID == id || strings.EqualFold(d.Name, id) {
			name := d.Name
			delete(s.devices, fp)
			return name, s.save()
		}
	}
	return "", fmt.Errorf("apparaat %q niet gevonden", id)
}

// ---- enrollment-venster ----

func (s *Store) OpenEnrollment(d time.Duration) string {
	code := enrollCode()
	s.mu.Lock()
	s.enrollCode = code
	s.enrollExpires = time.Now().Add(d)
	s.enrollAttempts = 0
	s.mu.Unlock()
	return code
}

func (s *Store) CloseEnrollment() {
	s.mu.Lock()
	s.enrollCode = ""
	s.enrollExpires = time.Time{}
	s.mu.Unlock()
}

func (s *Store) EnrollmentOpen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enrollCode != "" && time.Now().Before(s.enrollExpires)
}

func (s *Store) EnrollmentExpiry() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enrollExpires
}

// CheckEnrollCode vergelijkt in constante tijd en telt pogingen. Na te veel
// mislukte pogingen sluit het venster zichzelf.
func (s *Store) CheckEnrollCode(code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enrollCode == "" || time.Now().After(s.enrollExpires) {
		return false
	}
	got := normalizeCode(code)
	ok := subtle.ConstantTimeCompare([]byte(got), []byte(s.enrollCode)) == 1
	if !ok {
		s.enrollAttempts++
		if s.enrollAttempts >= s.maxAttempts {
			s.enrollCode = ""
			s.enrollExpires = time.Time{}
		}
	}
	return ok
}

const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ" // Crockford base32, zonder I L O U

func enrollCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	out := make([]byte, 8)
	for i := range out {
		out[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(out)
}

func normalizeCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.NewReplacer("-", "", " ", "", "I", "1", "L", "1", "O", "0", "U", "V").Replace(s)
	return s
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
