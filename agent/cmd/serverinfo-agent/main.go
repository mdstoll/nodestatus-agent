// Command serverinfo-agent is de lightweight monitoring-daemon.
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

	"serverinfo/internal/api"
	"serverinfo/internal/collect"
	"serverinfo/internal/config"
	"serverinfo/internal/control"
	"serverinfo/internal/pki"
	"serverinfo/internal/store"
	"serverinfo/internal/tools"
)

var version = "dev"

func main() {
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
			fmt.Println("serverinfo-agent", version)
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
	fmt.Fprint(os.Stderr, `serverinfo-agent — monitoring-daemon

  serverinfo-agent run --config <pad>     start de agent (standaard)
  serverinfo-agent bootstrap              maak CA + servercertificaat aan
  serverinfo-agent enroll --new           open een koppelvenster + toon QR
  serverinfo-agent enroll --cancel        sluit het koppelvenster
  serverinfo-agent devices list           toon gekoppelde apparaten
  serverinfo-agent devices revoke <id>    trek een apparaat in
  serverinfo-agent version
`)
}

func defaultConfigPath() string {
	if p := os.Getenv("SERVERINFO_CONFIG"); p != "" {
		return p
	}
	return "/etc/serverinfo-agent/config.toml"
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
	// ('serverinfo-agent bootstrap'). Tijdens het draaien is /etc read-only
	// dankzij ProtectSystem=strict, dus hier alleen laden.
	ca, err := pki.LoadOrCreateCA(cfg.CADir)
	if err != nil {
		log.Error("CA laden mislukt — draai eerst 'serverinfo-agent bootstrap'", "err", err)
		os.Exit(1)
	}
	st, err := store.New(cfg.StateDir, cfg.EnrollMaxAttempts)
	if err != nil {
		log.Error("apparatenlijst laden mislukt", "err", err)
		os.Exit(1)
	}
	sampler := collect.NewSampler(cfg.HistorySize, cfg.SampleHz, cfg.Features.GPU)
	srv := api.New(cfg, ca, st, sampler, version, caps, log)

	// Controlesocket voor de CLI-subcommando's.
	ctl := control.NewServer(cfg.StateDir, st, ca, time.Duration(cfg.EnrollWindowMinutes)*time.Minute, log)
	if err := ctl.Start(); err != nil {
		log.Warn("controlesocket niet beschikbaar", "err", err)
	}
	defer ctl.Close()

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		ErrorLog:          nil,
	}

	ln, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		log.Error("kan niet luisteren", "bind", cfg.Bind, "err", err)
		os.Exit(1)
	}
	if cfg.TLSEnabled() {
		certs, err := pki.NewCertManager(ca, cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			log.Error("servercertificaat laden mislukt — draai 'serverinfo-agent bootstrap'", "err", err)
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

	log.Info("serverinfo-agent gestart",
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
