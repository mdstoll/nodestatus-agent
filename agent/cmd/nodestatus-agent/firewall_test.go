package main

import (
	"reflect"
	"testing"
)

func TestActiveZones(t *testing.T) {
	out := "public (default)\n  interfaces: eth0\ntrusted\n  sources: 10.0.0.0/8\n"
	if got := activeZones(out); !reflect.DeepEqual(got, []string{"public", "trusted"}) {
		t.Errorf("got %v", got)
	}
}

func TestZoneAllowsPort(t *testing.T) {
	closed := "public (active)\n  target: default\n  services: ssh http https\n  ports: 8090/tcp 295000/tcp\n  rich rules:\n"
	if zoneAllowsPort(closed, 29500) {
		t.Error("port not listed, must not count as allowed")
	}
	cases := map[string]string{
		"port":      "  ports: 80/tcp 29500/tcp\n",
		"rich rule": "  rich rules:\n\trule family=\"ipv4\" source address=\"192.168.1.0/24\" port port=\"29500\" protocol=\"tcp\" accept\n",
		"target":    "trusted (active)\n  target: ACCEPT\n",
	}
	for name, out := range cases {
		if !zoneAllowsPort(out, 29500) {
			t.Errorf("%s: should count as allowed", name)
		}
	}
}

func TestUfwBlocks(t *testing.T) {
	head := "Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n\nTo  Action  From\n"
	if !ufwBlocks(head+"22/tcp  ALLOW IN  Anywhere\n", 29500) {
		t.Error("active ufw without a rule blocks the port")
	}
	if ufwBlocks(head+"29500/tcp  ALLOW IN  Anywhere\n", 29500) {
		t.Error("rule present, not blocked")
	}
	if ufwBlocks("Status: inactive\n", 29500) {
		t.Error("inactive ufw blocks nothing")
	}
	if ufwBlocks("Status: active\nDefault: allow (incoming), allow (outgoing)\n", 29500) {
		t.Error("default allow blocks nothing")
	}
}
