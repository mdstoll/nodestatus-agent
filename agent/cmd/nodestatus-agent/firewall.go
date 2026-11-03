package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// firewallBlock kijkt of een firewall op déze machine de poort van de agent
// tegenhoudt, en geeft dan zijn naam en het commando dat het oplost. Leeg als
// er niets aan de hand is of als het niet vast te stellen is (geen firewall,
// geen root): een loos alarm is erger dan geen melding.
//
// De reden dat dit bestaat: de agent luistert dan gewoon, de zelftest op
// 127.0.0.1 slaagt, en van buiten is alleen "No route to host" te zien. Op
// b.mest.dev (CyberPanel, dus firewalld) leek dat wekenlang op een firewall bij
// de hoster, terwijl de server zelf de verbindingen weigerde.
func firewallBlock(port int) (name, fix string) {
	if out, ok := runCheck("firewall-cmd", "--state"); ok && strings.TrimSpace(out) == "running" {
		if firewalldAllows(port) {
			return "", ""
		}
		return "firewalld", fmt.Sprintf(
			"sudo firewall-cmd --permanent --add-port=%d/tcp && sudo firewall-cmd --reload", port)
	}
	if out, ok := runCheck("ufw", "status", "verbose"); ok && ufwBlocks(out, port) {
		return "ufw", fmt.Sprintf("sudo ufw allow %d/tcp comment 'Node Status'", port)
	}
	return "", ""
}

// runCheck voert een controle uit met een korte timeout: firewall-cmd blijft hangen
// als dbus niet meewerkt, en dan mag `enroll --new` niet mee blijven hangen.
func runCheck(name string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err == nil
}

// firewalldAllows bekijkt alle actieve zones: de interface hoeft niet in de
// standaardzone te zitten. Lukt het opvragen niet, dan geen alarm.
func firewalldAllows(port int) bool {
	zones, ok := runCheck("firewall-cmd", "--get-active-zones")
	if !ok {
		return true
	}
	for _, zone := range activeZones(zones) {
		out, ok := runCheck("firewall-cmd", "--zone="+zone, "--list-all")
		if !ok || zoneAllowsPort(out, port) {
			return true
		}
	}
	return false
}

// activeZones leest de uitvoer van --get-active-zones: zonenamen staan
// links uitgelijnd, hun interfaces en bronnen eronder ingesprongen.
func activeZones(out string) []string {
	var zones []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		zones = append(zones, strings.Fields(line)[0])
	}
	return zones
}

func zoneAllowsPort(listAll string, port int) bool {
	p := fmt.Sprint(port)
	if strings.Contains(listAll, "target: ACCEPT") {
		return true
	}
	for _, f := range strings.Fields(listAll) {
		if f == p+"/tcp" || f == `port="`+p+`"` {
			return true
		}
	}
	return false
}

func ufwBlocks(status string, port int) bool {
	if !strings.Contains(status, "Status: active") || strings.Contains(status, "allow (incoming)") {
		return false
	}
	p := fmt.Sprint(port)
	sc := bufio.NewScanner(strings.NewReader(status))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && (f[0] == p || f[0] == p+"/tcp") && f[1] == "ALLOW" {
			return false
		}
	}
	return true
}

// printFirewallWarning is gedeeld door `enroll --new` en `doctor`.
func printFirewallWarning(port int) bool {
	name, fix := firewallBlock(port)
	if name == "" {
		return false
	}
	fmt.Printf("\n  \033[0;33m!\033[0m %s on this machine blocks port %d, so the app cannot reach the agent.\n", name, port)
	fmt.Printf("    To allow it:\n\n      %s\n\n", fix)
	return true
}
