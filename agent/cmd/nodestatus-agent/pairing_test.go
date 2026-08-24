package main

import "testing"

func TestResolvableHost(t *testing.T) {
	cases := []struct {
		name  string
		host  string
		addrs []string
		want  string
	}{
		{"public FQDN + public IP (a.mest.dev case)", "a.mest.dev",
			[]string{"194.163.173.139", "172.17.0.1", "100.96.231.2"}, "a.mest.dev"},
		{"bare hostname stays on IP", "DebianG3",
			[]string{"192.168.1.102"}, ""},
		{"mDNS name is LAN-only", "raspberrypi.local",
			[]string{"192.168.1.50"}, ""},
		{"FQDN but only private IPs", "nas.example.com",
			[]string{"192.168.1.100"}, ""},
		{"empty hostname", "", []string{"1.2.3.4"}, ""},
	}
	for _, c := range cases {
		if got := resolvableHost(c.host, c.addrs); got != c.want {
			t.Errorf("%s: resolvableHost(%q) = %q, want %q", c.name, c.host, got, c.want)
		}
	}
}
