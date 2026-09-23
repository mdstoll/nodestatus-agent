package pki

import (
	"crypto/x509"
	"net"
	"testing"
)

func TestCovers(t *testing.T) {
	c := &x509.Certificate{
		DNSNames:    []string{"localhost", "node", "node.local"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.1.10")},
	}
	dns := []string{"localhost", "NODE", "node.local"}
	if !covers(c, dns, []net.IP{net.ParseIP("192.168.1.10"), net.ParseIP("2001:db8::1")}) {
		t.Error("a new IPv6 address alone should not force a reissue")
	}
	if covers(c, dns, []net.IP{net.ParseIP("192.168.1.23")}) {
		t.Error("a new IPv4 address must force a reissue")
	}
	if covers(c, []string{"renamed"}, nil) {
		t.Error("a new hostname must force a reissue")
	}
}
