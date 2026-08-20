// Package pki beheert de per-server certificaatautoriteit: CA, servercertificaat
// en het uitgeven van client-certificaten bij enrollment.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	key     *ecdsa.PrivateKey
	dir     string
}

func caPaths(dir string) (string, string) {
	return filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
}

// LoadOrCreateCA laadt de CA of maakt hem aan als hij nog niet bestaat.
func LoadOrCreateCA(dir string) (*CA, error) {
	certPath, keyPath := caPaths(dir)
	if b, err := os.ReadFile(certPath); err == nil {
		kb, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("CA-sleutel ontbreekt: %w", err)
		}
		cert, err := parseCertPEM(b)
		if err != nil {
			return nil, err
		}
		key, err := parseECKeyPEM(kb)
		if err != nil {
			return nil, err
		}
		return &CA{Cert: cert, CertPEM: b, key: key, dir: dir}, nil
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Node Status CA (" + host + ")", Organization: []string{"Node Status"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	return &CA{Cert: cert, CertPEM: certPEM, key: key, dir: dir}, nil
}

// Fingerprint is de SHA-256 van het CA-certificaat; dit staat in de koppel-QR
// zodat de app de server al bij enrollment kan valideren.
func (ca *CA) Fingerprint() string { return fingerprint(ca.Cert.Raw) }

// ServerCertDays is bewust 397 en niet langer. Apple weigert sinds iOS 13
// elk TLS-servercertificaat met een looptijd van meer dan 398 dagen
// ("Certificate exceeds maximum temporal validity period"), ook als het door
// een vertrouwde eigen CA is uitgegeven. De CA zelf mag wél 10 jaar: die regel
// geldt alleen voor het leaf-certificaat.
const ServerCertDays = 397

// renewBefore bepaalt hoe ruim voor het verlopen we vernieuwen.
const renewBefore = 30 * 24 * time.Hour

// EnsureServerCert maakt of vernieuwt het servercertificaat met SAN's voor
// alle lokale adressen plus de hostname. Geeft true terug als er iets is
// geschreven.
func (ca *CA) EnsureServerCert(certPath, keyPath string, force bool) error {
	_, err := ca.ensureServerCert(certPath, keyPath, force)
	return err
}

func (ca *CA) ensureServerCert(certPath, keyPath string, force bool) (bool, error) {
	if !force {
		if b, err := os.ReadFile(certPath); err == nil {
			if c, err := parseCertPEM(b); err == nil && time.Now().Before(c.NotAfter.Add(-renewBefore)) {
				if _, err := os.Stat(keyPath); err == nil {
					return false, nil
				}
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, err
	}
	host, _ := os.Hostname()
	dns := []string{"localhost"}
	if host != "" {
		dns = append(dns, host, host+".local")
	}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				ips = append(ips, ipn.IP)
			}
		}
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: host, Organization: []string{"Node Status"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, ServerCertDays),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return false, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return false, err
	}
	// Leaf + CA in één bestand. Zo krijgt de app de CA al tijdens de
	// TLS-handshake te zien en kan hij de fingerprint uit de koppel-QR
	// controleren vóórdat hij de koppelcode verstuurt.
	chain := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		ca.CertPEM...,
	)
	if err := os.WriteFile(certPath, chain, 0o644); err != nil {
		return false, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// CertManager houdt het actieve servercertificaat vast en vernieuwt het
// automatisch. Nodig omdat het certificaat maar 397 dagen geldig mag zijn.
type CertManager struct {
	ca       *CA
	certPath string
	keyPath  string

	mu   sync.RWMutex
	cert *tls.Certificate
}

func NewCertManager(ca *CA, certPath, keyPath string) (*CertManager, error) {
	m := &CertManager{ca: ca, certPath: certPath, keyPath: keyPath}
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

// GetCertificate wordt door crypto/tls per handshake aangeroepen, zodat een
// vernieuwing meteen actief is zonder herstart.
func (m *CertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert, nil
}

func (m *CertManager) reload() error {
	c, err := tls.LoadX509KeyPair(m.certPath, m.keyPath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cert = &c
	m.mu.Unlock()
	return nil
}

// MaybeRenew vernieuwt het certificaat als het binnen 30 dagen verloopt.
func (m *CertManager) MaybeRenew() (bool, error) {
	renewed, err := m.ca.ensureServerCert(m.certPath, m.keyPath, false)
	if err != nil || !renewed {
		return false, err
	}
	return true, m.reload()
}

// ExpiresAt geeft de vervaldatum van het actieve certificaat.
func (m *CertManager) ExpiresAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil || len(m.cert.Certificate) == 0 {
		return time.Time{}
	}
	c, err := x509.ParseCertificate(m.cert.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return c.NotAfter
}

// IssueClient tekent een client-certificaat voor een publieke sleutel die het
// toestel zelf heeft gegenereerd. De private sleutel verlaat het toestel nooit.
func (ca *CA) IssueClient(pub *ecdsa.PublicKey, commonName string, days int) (certPEM []byte, fp string, notAfter time.Time, err error) {
	cn := sanitizeCN(commonName)
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"Node Status Device"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(0, 0, days),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, pub, ca.key)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), fingerprint(der), tmpl.NotAfter, nil
}

func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

// Fingerprint van een certificaat: SHA-256 over de DER, als hex.
func fingerprint(der []byte) string {
	s := sha256.Sum256(der)
	return hex.EncodeToString(s[:])
}

func CertFingerprint(c *x509.Certificate) string { return fingerprint(c.Raw) }

func serial() *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, _ := rand.Int(rand.Reader, max)
	return n
}

func parseCertPEM(b []byte) (*x509.Certificate, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("geen PEM-blok gevonden")
	}
	return x509.ParseCertificate(blk.Bytes)
}

func parseECKeyPEM(b []byte) (*ecdsa.PrivateKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("geen PEM-blok gevonden")
	}
	return x509.ParseECPrivateKey(blk.Bytes)
}

// ParseP256PublicKey leest een X9.63 uncompressed point (65 bytes, 0x04-prefix),
// zoals iOS die exporteert met SecKeyCopyExternalRepresentation.
func ParseP256PublicKey(raw []byte) (*ecdsa.PublicKey, error) {
	if len(raw) != 65 || raw[0] != 0x04 {
		return nil, fmt.Errorf("verwacht 65-byte uncompressed P-256 punt, kreeg %d bytes", len(raw))
	}
	x := new(big.Int).SetBytes(raw[1:33])
	y := new(big.Int).SetBytes(raw[33:65])
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, fmt.Errorf("punt ligt niet op de P-256 curve")
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, nil
}

func sanitizeCN(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r < 127 && r != '"' && r != '\\' {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "device"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
