package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"nodestatus/internal/collect"
	"nodestatus/internal/config"
	"nodestatus/internal/control"
	"nodestatus/internal/pki"
	"nodestatus/internal/selfupdate"
	"nodestatus/internal/tools"
)

func stateDir() string {
	cfg, err := config.Load(defaultConfigPath())
	if err != nil {
		return "/var/lib/nodestatus-agent"
	}
	return cfg.StateDir
}

func cmdEnroll() {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	newWin := fs.Bool("new", false, "open a new pairing window")
	cancel := fs.Bool("cancel", false, "close the pairing window")
	port := fs.Int("port", 0, "port to embed in the QR code (default: from config)")
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
	// Een hostnaam die publiek naar déze machine wijst is beter dan het kale
	// IP-adres: mobiele netwerken zijn vaak IPv6-only met NAT64/DNS64, en dat
	// vertaalt alleen namen die via DNS gaan — een letterlijk IPv4-adres is
	// daar simpelweg onbereikbaar. Koppelen op wifi lukt dan wel en op 4G/5G
	// niet, wat lastig te plaatsen is. Alleen overnemen als de naam ook echt
	// naar een van onze eigen adressen resolvet, zodat een verzonnen of
	// interne hostnaam de QR niet onbruikbaar maakt.
	if h := resolvableHost(info.Hostname, info.Addresses); h != "" {
		host = h
	}
	printPairing(info, host, p)
}

// localSuffixes zijn namen die per definitie het LAN niet verlaten; die als
// koppeladres aanbieden helpt buitenshuis niemand.
var localSuffixes = []string{".local", ".lan", ".home", ".internal", ".localdomain"}

func resolvableHost(hostname string, addrs []string) string {
	h := strings.ToLower(strings.TrimSuffix(hostname, "."))
	if h == "" || !strings.Contains(h, ".") {
		return "" // geen FQDN; "raspberrypi" helpt niemand buiten het LAN
	}
	for _, s := range localSuffixes {
		if strings.HasSuffix(h, s) {
			return ""
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resolved, _ := net.DefaultResolver.LookupHost(ctx, h)

	own := map[string]bool{}
	for _, a := range addrs {
		own[a] = true
	}
	sawUsable := false
	for _, r := range resolved {
		ip := net.ParseIP(r)
		// Debian zet standaard "127.0.1.1 <fqdn>" in /etc/hosts, en Go leest
		// dat bestand vóór DNS. De naam lijkt dan naar loopback te wijzen
		// terwijl hij publiek prima klopt; zulke antwoorden zeggen dus niets.
		if ip != nil && ip.IsLoopback() {
			continue
		}
		sawUsable = true
		if own[r] {
			return h
		}
	}
	if sawUsable {
		return "" // resolvet echt ergens anders heen — niet onze machine
	}

	// Geen bruikbaar antwoord (alleen loopback, of DNS onbereikbaar vanaf de
	// server zelf). Een publiek IP plus een echte FQDN is dan genoeg reden om
	// de naam aan te bieden: die werkt wél op een IPv6-only mobiel netwerk.
	// Klopt hij toch niet, dan staat het IP-adres er nog steeds onder en is
	// het adres in de app te wijzigen.
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && isPublicIP(ip) {
			return h
		}
	}
	return ""
}

func isPublicIP(ip net.IP) bool {
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}

func printPairing(info control.EnrollInfo, host string, port int) {
	url := fmt.Sprintf("nodestatus://enroll?h=%s&p=%d&fp=%s&c=%s&n=%s",
		host, port, info.Fingerprint, info.Code, info.Hostname)

	expires := time.Unix(info.ExpiresAt, 0).Format("15:04")
	fmt.Println()
	fmt.Printf("  \033[1mPair this device\033[0m\n\n")
	fmt.Printf("    Host         %s:%d\n", host, port)
	if len(info.Addresses) > 1 {
		fmt.Printf("    Also at      %s\n", strings.Join(info.Addresses[1:], ", "))
	}
	fmt.Printf("    Pairing code \033[1;36m%s-%s\033[0m   (valid until %s)\n",
		info.Code[:4], info.Code[4:], expires)
	// Volledig, niet afgekort: bij handmatig koppelen (QR scannen lukt niet
	// altijd) moet de hele fingerprint over te typen zijn. In groepjes van
	// acht, zodat je je plek niet kwijtraakt.
	fmt.Printf("    Fingerprint  %s\n", groupsOf(info.Fingerprint, 8, 4))
	fmt.Println()

	if _, err := exec.LookPath("qrencode"); err == nil {
		fmt.Println("    Scan this QR code in the Node Status app:")
		fmt.Println()
		cmd := exec.Command("qrencode", "-t", "UTF8", "-m", "4", url)
		cmd.Stdout = os.Stdout
		_ = cmd.Run()
	} else {
		fmt.Println("    (install 'qrencode' for a scannable QR code)")
		fmt.Println("    Pairing URL:")
		fmt.Println("   ", url)
	}
	fmt.Println()
}

func cmdDevices() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: nodestatus-agent devices list|revoke <id>")
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
			fmt.Println("No paired devices. Open a pairing window with:")
			fmt.Println("  sudo nodestatus-agent enroll --new")
			return
		}
		fmt.Printf("%-10s %-24s %-12s %-16s %s\n", "ID", "NAME", "PAIRED", "LAST SEEN", "EXPIRES")
		for _, d := range devs {
			fmt.Printf("%-10s %-24s %-12s %-16s %s\n",
				d.ID, truncate(d.Name, 24), d.EnrolledAt.Format("2006-01-02"),
				humanAgo(d.LastSeen), d.ExpiresAt.Format("2006-01-02"))
		}
	case "revoke":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "gebruik: nodestatus-agent devices revoke <id>")
			os.Exit(2)
		}
		resp, err := control.Call(stateDir(), control.Request{Cmd: "devices-revoke", Arg: os.Args[2]})
		check(err)
		if !resp.OK {
			fmt.Fprintln(os.Stderr, "✖", resp.Message)
			os.Exit(1)
		}
		fmt.Printf("✔ %s revoked. Existing connections were closed.\n", resp.Message)
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand", sub)
		os.Exit(2)
	}
}

func humanAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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
	cfgPath := fs.String("config", defaultConfigPath(), "path to config.toml")
	force := fs.Bool("force", false, "renew the server certificate even if it is still valid")
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

// cmdSudoers drukt de sudoers-regels af die deze agent nodig heeft. Ze komen
// uit dezelfde code die de commando's ook echt uitvoert, zodat de argumenten
// nooit kunnen afwijken van wat sudo toestaat.
func cmdSudoers() {
	fs := flag.NewFlagSet("sudoers", flag.ExitOnError)
	user := fs.String("user", "nodestatus", "user the rules apply to")
	fs.Parse(os.Args[1:])
	rules := collect.SudoersRules(*user)
	if len(rules) == 0 {
		fmt.Fprintln(os.Stderr, "no optional tools found that need sudo")
		return
	}
	fmt.Println("# Generated by nodestatus-agent sudoers — do not edit by hand.")
	for _, r := range rules {
		fmt.Println(r)
	}
}

// cmdExtras installs optional software directly through the agent —
// dependencies, iperf3, Geekbench — without needing install.sh again. Meant
// for adding something after the fact (a server that was set up before this
// existed, or someone who only wants iperf3 without re-running the whole
// installer).
func cmdExtras() {
	fs := flag.NewFlagSet("extras", flag.ExitOnError)
	fs.Parse(os.Args[1:])
	args := fs.Args()
	if len(args) < 1 || args[0] != "install" {
		fmt.Fprintln(os.Stderr, "usage: nodestatus-agent extras install [deps|iperf3|geekbench|all]")
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "✖ this needs root — run with sudo")
		os.Exit(1)
	}
	want := args[1:]
	if len(want) == 0 {
		want = []string{"all"}
	}
	has := func(name string) bool {
		for _, w := range want {
			if w == name || w == "all" {
				return true
			}
		}
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	report := func(step tools.InstallStep) {
		if step.OK {
			fmt.Printf("  \033[0;32m✔\033[0m %s\n", step.Name)
		} else {
			fmt.Printf("  \033[0;33m–\033[0m %-12s %s\n", step.Name, step.Note)
		}
	}

	if has("deps") {
		fmt.Println("installing dependencies…")
		for _, s := range tools.InstallStandardExtras(ctx) {
			report(s)
		}
	}
	if has("iperf3") {
		fmt.Println("installing iperf3…")
		report(tools.InstallAptPackage(ctx, "iperf3"))
	}
	if has("geekbench") {
		fmt.Println("installing Geekbench…")
		report(tools.InstallGeekbench(ctx))
	}
	fmt.Println("\nRestart the agent so it picks up what's newly available:")
	fmt.Println("  sudo systemctl restart nodestatus-agent")
}

// cmdUpdate downloads the latest release and installs it in place. This is a
// manual, explicit action — run by hand or from your own cron job — never
// triggered remotely by the app. The app can only ever tell you an update
// exists (via /v1/system); it cannot cause code to run on your server.
func cmdUpdate() {
	fmt.Printf("Current version: %s\n", version)
	fmt.Println("Checking for a newer release…")
	newVersion, err := selfupdate.Apply(version)
	if err != nil {
		if selfupdate.IsAlreadyCurrent(err) {
			fmt.Println("✔ Already up to date.")
			return
		}
		fmt.Fprintln(os.Stderr, "✖", err)
		os.Exit(1)
	}
	fmt.Printf("✔ Installed %s (was %s)\n", newVersion, version)
	if os.Geteuid() != 0 {
		fmt.Println("  Not running as root — restart the service yourself:")
		fmt.Println("    sudo systemctl restart nodestatus-agent")
		return
	}
	fmt.Println("Restarting the service…")
	if err := selfupdate.RestartService(); err != nil {
		fmt.Fprintln(os.Stderr, "✖ restart failed:", err)
		fmt.Println("  Restart it yourself: sudo systemctl restart nodestatus-agent")
		os.Exit(1)
	}
	fmt.Println("✔ Done.")
}

// groupsOf breekt een lange hex-string op in blokken, met na elke `perLine`
// blokken een nieuwe regel die uitlijnt onder de eerste. Puur om een
// fingerprint met het oog over te kunnen typen.
func groupsOf(s string, size, perLine int) string {
	var b strings.Builder
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		n := i / size
		if n > 0 {
			if n%perLine == 0 {
				b.WriteString("\n                 ")
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(s[i:end])
	}
	return b.String()
}

// cmdDoctor test elke optionele module op déze machine en zegt per stuk of hij
// werkt, en zo niet: waarom en wat eraan te doen is. install.sh draait dit aan
// het eind, zodat je meteen ziet wat je wel en niet in de app zult zien in
// plaats van er later tegenaan te lopen.
func cmdDoctor() {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config.toml")
	fs.Parse(os.Args[1:])

	if cfg, err := config.Load(*cfgPath); err == nil {
		tools.SetHome(cfg.StateDir)
	}
	tools.Discover()

	fmt.Println()
	fmt.Printf("  \033[1mNode Status agent — module check\033[0m\n\n")

	gpus := collect.DetectGPUs()
	rows := []struct {
		id, reason, fix string
		ok              bool
	}{}
	for _, c := range tools.ProbeAll(context.Background()) {
		rows = append(rows, struct {
			id, reason, fix string
			ok              bool
		}{c.ID, c.Reason, c.Fix, c.OK})
	}
	rows = append(rows, struct {
		id, reason, fix string
		ok              bool
	}{"gpu", "no GPU found on this machine", "", len(gpus) > 0})
	rows = append(rows, struct {
		id, reason, fix string
		ok              bool
	}{"sensors", "no hwmon/thermal sensors", "", tools.HasSensors()})

	missing := 0
	for _, r := range rows {
		if r.ok {
			fmt.Printf("    \033[0;32m✔\033[0m %-11s\n", r.id)
			continue
		}
		missing++
		fmt.Printf("    \033[0;33m–\033[0m %-11s %s\n", r.id, r.reason)
		if r.fix != "" {
			fmt.Printf("      %s└ %s\033[0m\n", "\033[2m", r.fix)
		}
	}
	fmt.Println()
	if missing == 0 {
		fmt.Println("  Everything works on this machine.")
	} else {
		fmt.Printf("  %d module(s) unavailable — the app hides these.\n", missing)
	}
	fmt.Println()
}
