package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"serverinfo/internal/config"
	"serverinfo/internal/control"
	"serverinfo/internal/pki"
)

func stateDir() string {
	cfg, err := config.Load(defaultConfigPath())
	if err != nil {
		return "/var/lib/serverinfo-agent"
	}
	return cfg.StateDir
}

func cmdEnroll() {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	newWin := fs.Bool("new", false, "open een nieuw koppelvenster")
	cancel := fs.Bool("cancel", false, "sluit het koppelvenster")
	port := fs.Int("port", 0, "poort om in de QR te zetten (standaard uit config)")
	fs.Parse(os.Args[1:])

	if *cancel {
		resp, err := control.Call(stateDir(), control.Request{Cmd: "enroll-cancel"})
		check(err)
		fmt.Println("✔", resp.Message)
		return
	}
	if !*newWin {
		fs.Usage()
		os.Exit(2)
	}
	resp, err := control.Call(stateDir(), control.Request{Cmd: "enroll-new"})
	check(err)
	if !resp.OK {
		fmt.Fprintln(os.Stderr, "✖", resp.Message)
		os.Exit(1)
	}
	var info control.EnrollInfo
	b, _ := json.Marshal(resp.Data)
	_ = json.Unmarshal(b, &info)

	p := *port
	if p == 0 {
		if cfg, err := config.Load(defaultConfigPath()); err == nil {
			if _, portStr, ok := strings.Cut(cfg.Bind, ":"); ok {
				fmt.Sscanf(portStr, "%d", &p)
			}
		}
		if p == 0 {
			p = 29500
		}
	}
	host := info.Hostname
	if len(info.Addresses) > 0 {
		host = info.Addresses[0]
	}
	printPairing(info, host, p)
}

func printPairing(info control.EnrollInfo, host string, port int) {
	url := fmt.Sprintf("serverinfo://enroll?h=%s&p=%d&fp=%s&c=%s&n=%s",
		host, port, info.Fingerprint, info.Code, info.Hostname)

	expires := time.Unix(info.ExpiresAt, 0).Format("15:04")
	fmt.Println()
	fmt.Printf("  \033[1mKoppel dit apparaat\033[0m\n\n")
	fmt.Printf("    Host         %s:%d\n", host, port)
	if len(info.Addresses) > 1 {
		fmt.Printf("    Ook via      %s\n", strings.Join(info.Addresses[1:], ", "))
	}
	fmt.Printf("    Koppelcode   \033[1;36m%s-%s\033[0m   (geldig tot %s)\n",
		info.Code[:4], info.Code[4:], expires)
	fmt.Printf("    Fingerprint  %s…%s\n\n", info.Fingerprint[:8], info.Fingerprint[len(info.Fingerprint)-8:])

	if _, err := exec.LookPath("qrencode"); err == nil {
		fmt.Println("    Scan deze QR in de Server Info-app:")
		fmt.Println()
		cmd := exec.Command("qrencode", "-t", "UTF8", "-m", "4", url)
		cmd.Stdout = os.Stdout
		_ = cmd.Run()
	} else {
		fmt.Println("    (installeer 'qrencode' voor een scanbare QR-code)")
		fmt.Println("    Koppel-URL:")
		fmt.Println("   ", url)
	}
	fmt.Println()
}

func cmdDevices() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "gebruik: serverinfo-agent devices list|revoke <id>")
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "list":
		resp, err := control.Call(stateDir(), control.Request{Cmd: "devices-list"})
		check(err)
		var devs []struct {
			ID         string    `json:"id"`
			Name       string    `json:"name"`
			EnrolledAt time.Time `json:"enrolled_at"`
			LastSeen   time.Time `json:"last_seen"`
			ExpiresAt  time.Time `json:"expires_at"`
		}
		b, _ := json.Marshal(resp.Data)
		_ = json.Unmarshal(b, &devs)
		if len(devs) == 0 {
			fmt.Println("Geen gekoppelde apparaten. Open een venster met:")
			fmt.Println("  sudo serverinfo-agent enroll --new")
			return
		}
		fmt.Printf("%-10s %-24s %-12s %-16s %s\n", "ID", "NAAM", "GEKOPPELD", "LAATST GEZIEN", "VERLOOPT")
		for _, d := range devs {
			fmt.Printf("%-10s %-24s %-12s %-16s %s\n",
				d.ID, truncate(d.Name, 24), d.EnrolledAt.Format("2006-01-02"),
				humanAgo(d.LastSeen), d.ExpiresAt.Format("2006-01-02"))
		}
	case "revoke":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "gebruik: serverinfo-agent devices revoke <id>")
			os.Exit(2)
		}
		resp, err := control.Call(stateDir(), control.Request{Cmd: "devices-revoke", Arg: os.Args[2]})
		check(err)
		if !resp.OK {
			fmt.Fprintln(os.Stderr, "✖", resp.Message)
			os.Exit(1)
		}
		fmt.Printf("✔ %s ingetrokken. Bestaande verbindingen zijn verbroken.\n", resp.Message)
	default:
		fmt.Fprintln(os.Stderr, "onbekend subcommando", sub)
		os.Exit(2)
	}
}

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "zojuist"
	case d < time.Hour:
		return fmt.Sprintf("%d min geleden", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d uur geleden", int(d.Hours()))
	}
	return fmt.Sprintf("%d dagen geleden", int(d.Hours()/24))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "✖", err)
		os.Exit(1)
	}
}

// cmdBootstrap maakt de CA en het servercertificaat aan. Draait bij installatie
// als de serverinfo-gebruiker, zodat de daemon zelf nooit naar /etc hoeft te
// schrijven en ProtectSystem=strict kan blijven staan.
func cmdBootstrap() {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "pad naar config.toml")
	force := fs.Bool("force", false, "vernieuw het servercertificaat ook als het nog geldig is")
	fs.Parse(os.Args[1:])

	cfg, err := config.Load(*cfgPath)
	check(err)
	ca, err := pki.LoadOrCreateCA(cfg.CADir)
	check(err)
	if cfg.TLSEnabled() {
		check(ca.EnsureServerCert(cfg.TLSCert, cfg.TLSKey, *force))
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		check(err)
	}
	fmt.Printf("CA-fingerprint %s\n", ca.Fingerprint())
}
