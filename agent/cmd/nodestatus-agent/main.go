// Command nodestatus-agent is de lightweight monitoring-daemon.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"nodestatus/internal/api"
	"nodestatus/internal/collect"
	"nodestatus/internal/config"
	"nodestatus/internal/control"
	"nodestatus/internal/pki"
	"nodestatus/internal/store"
	"nodestatus/internal/tools"
)

var version = "dev"

func main() {
	// Zonder argumenten tonen we hulp en starten we níet de daemon. Een
	// tweede daemon die per ongeluk start, pakt de controlesocket af van de
	// draaiende instantie en sterft daarna op de bezette poort — waarna
	// 'enroll --new' onbereikbaar is. Dat is precies één keer te vaak gebeurd.
	if len(os.Args) == 1 {
		usage()
		return
	}
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-V" {
			fmt.Println("nodestatus-agent", version)
			return
		}
		if a == "--help" || a == "-h" {
			usage()
			return
		}
	}
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd := os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
		switch cmd {
		case "enroll":
			cmdEnroll()
			return
		case "devices":
			cmdDevices()
			return
		case "bootstrap":
			cmdBootstrap()
			return
		case "version":
			fmt.Println("nodestatus-agent", version)
			return
		case "run":
			// door naar run
		default:
			fmt.Fprintf(os.Stderr, "onbekend commando %q\n", cmd)
			usage()
			os.Exit(2)
		}
	}
	run()
}

func usage() {
	fmt.Printf(`Node Status agent %s — real-time monitoring for Linux servers

USAGE
  nodestatus-agent <command> [options]

COMMANDS
  run                     Start the agent in the foreground. Normally systemd
                          does this; use "systemctl start nodestatus-agent".
  enroll --new            Open a 15-minute pairing window and print the pairing
                          code plus a QR code for the app.
  enroll --cancel         Close the pairing window right away.
  devices list            List every paired device.
  devices revoke <id>     Revoke one device. Takes effect immediately, also on
                          connections that are already open.
  bootstrap               Create the CA and server certificate. The installer
                          does this; you rarely need it by hand.
  version                 Print the version.

OPTIONS
  --config <path>         Config file (default: %s)
  --version, -V           Print the version
  --help, -h              Show this text

SERVICE
  systemctl status nodestatus-agent
  journalctl -u nodestatus-agent -f

DOCS
  https://github.com/mdstoll/node-status
`, version, defaultConfigPath())
}

func defaultConfigPath() string {
	if p := os.Getenv("NODESTATUS_CONFIG"); p != "" {
		return p
	}
	return "/etc/nodestatus-agent/config.toml"
}

func run() {
	cfgPath := flag.String("config", defaultConfigPath(), "pad naar config.toml")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("configuratie laden mislukt", "err", err)
		os.Exit(1)
	}

	// HOME voor child-processen: de speedtest-CLI heeft er een nodig en
	// bewaart er zijn licentie-acceptatie.
	tools.SetHome(cfg.StateDir)
	found := tools.Discover()
	caps := capabilities(cfg, found)

	// De CA en het servercertificaat worden bij installatie aangemaakt
	// ('nodestatus-agent bootstrap'). Tijdens het draaien is /etc read-only
	// dankzij ProtectSystem=strict, dus hier alleen laden.
	ca, err := pki.LoadOrCreateCA(cfg.CADir)
	if err != nil {
		log.Error("CA laden mislukt — draai eerst 'nodestatus-agent bootstrap'", "err", err)
		os.Exit(1)
	}
	st, err := store.New(cfg.StateDir, cfg.EnrollMaxAttempts)
	if err != nil {
		log.Error("apparatenlijst laden mislukt", "err", err)
		os.Exit(1)
	}
	sampler := collect.NewSampler(cfg.HistorySize, cfg.SampleHz, cfg.Features.GPU)
	srv := api.New(cfg, ca, st, sampler, version, caps, log)

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		ErrorLog:          nil,
	}

	ln, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		log.Error("kan niet luisteren — draait er al een agent?", "bind", cfg.Bind, "err", err)
		os.Exit(1)
	}

	// Pas hierna de controlesocket openen. Zou dat eerder gebeuren, dan pakt
	// een instantie die alsnog op de poort strandt de socket af van de agent
	// die wél draait, en is 'enroll --new' daarna onbereikbaar.
	ctl := control.NewServer(cfg.StateDir, st, ca, time.Duration(cfg.EnrollWindowMinutes)*time.Minute, log)
	if err := ctl.Start(); err != nil {
		log.Error("controlesocket kon niet worden geopend", "err", err)
		os.Exit(1)
	}
	defer ctl.Close()
	if cfg.TLSEnabled() {
		certs, err := pki.NewCertManager(ca, cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			log.Error("servercertificaat laden mislukt — draai 'nodestatus-agent bootstrap'", "err", err)
			os.Exit(1)
		}
		// Het servercertificaat mag maar 397 dagen geldig zijn (Apple weigert
		// langer), dus vernieuwen we het automatisch ruim voor het verloopt.
		if renewed, err := certs.MaybeRenew(); err != nil {
			log.Warn("certificaat vernieuwen mislukt", "err", err)
		} else if renewed {
			log.Info("servercertificaat vernieuwd", "verloopt", certs.ExpiresAt().Format("2006-01-02"))
		}
		go func() {
			for range time.Tick(12 * time.Hour) {
				if renewed, err := certs.MaybeRenew(); err != nil {
					log.Warn("certificaat vernieuwen mislukt", "err", err)
				} else if renewed {
					log.Info("servercertificaat vernieuwd", "verloopt", certs.ExpiresAt().Format("2006-01-02"))
				}
			}
		}()
		log.Info("servercertificaat geldig", "tot", certs.ExpiresAt().Format("2006-01-02"))
		ln = tls.NewListener(ln, srv.TLSConfig(certs))
	}

	log.Info("nodestatus-agent gestart",
		"version", version, "bind", cfg.Bind, "mode", cfg.Mode,
		"tls", cfg.TLSEnabled(), "devices", st.Count(), "capabilities", strings.Join(caps, ","))

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server gestopt", "err", err)
			os.Exit(1)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("afsluiten")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

func capabilities(cfg *config.Config, found map[string]string) []string {
	caps := []string{"metrics", "stream", "sensors", "processes"}
	add := func(c string) { caps = append(caps, c) }
	if cfg.Features.SMART && found["smartctl"] != "" {
		add("smart")
	}
	if cfg.Features.GPU && found["nvidia-smi"] != "" {
		add("gpu.nvidia")
	}
	if cfg.Features.Speedtest {
		if found["speedtest"] != "" {
			add("speedtest.ookla")
		} else if found["librespeed-cli"] != "" {
			add("speedtest.librespeed")
		}
	}
	if found["whois"] != "" {
		add("whois")
	}
	if found["dig"] != "" {
		add("dns")
	}
	if found["ping"] != "" {
		add("ping")
	}
	if found["traceroute"] != "" {
		add("traceroute")
	}
	if found["lsblk"] != "" {
		add("disks")
	}
	if cfg.Features.Logs && found["journalctl"] != "" {
		add("journal")
	}
	if cfg.Features.APT {
		add("apt")
	}
	return caps
}
