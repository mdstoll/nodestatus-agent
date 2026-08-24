VERSION ?= 0.2.5
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist

.PHONY: all build release clean deploy test

all: build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(DIST)/nodestatus-agent ./cmd/nodestatus-agent

# install.sh staat ook in de repo-root zodat de curl-one-liner werkt.
#
# Deze vier dekken elke Linux-machine die deze agent aankan:
#   amd64 — 64-bit Debian/Ubuntu (en elke andere x86_64-distro)
#   386   — 32-bit Debian/Ubuntu op x86 (i386/i686), ook oudere hardware
#   arm64 — Raspberry Pi 3/4/5 en Zero 2 W op een 64-bit OS
#   arm   — GOARM 6, draait op àlle overige Pi's: Zero/Zero W en Pi 1 (armv6)
#           t/m Pi 2/3/4/5 op een 32-bit OS (armv7). Eén build dekt beide,
#           ten koste van armv7-only FPU-instructies die deze workload
#           toch nooit gebruikte.
#
# Bewust géén darwin/macOS: de hele collectielaag leest /proc en /sys, en
# macOS heeft geen procfs. Dat compileert wél maar rapporteert niets — zie
# docs/PLATFORMS.md voor wat een echte macOS-poort zou vragen.
release: clean
	@cp packaging/install.sh ../install.sh
	@for arch in amd64 386 arm64 arm; do \
	  echo "→ linux/$$arch"; \
	  mkdir -p $(DIST)/$$arch; \
	  GOARM=6 CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
	    -o $(DIST)/$$arch/nodestatus-agent ./cmd/nodestatus-agent; \
	  cp packaging/install.sh packaging/uninstall.sh packaging/nodestatus-agent.service $(DIST)/$$arch/; \
	  tar -czf $(DIST)/nodestatus-agent_linux_$$arch.tar.gz -C $(DIST)/$$arch .; \
	done
	@cd $(DIST) && shasum -a 256 *.tar.gz > SHA256SUMS
	@ls -lh $(DIST)/*.tar.gz $(DIST)/SHA256SUMS

test:
	go vet ./...
	go test ./...

clean:
	rm -rf $(DIST)
