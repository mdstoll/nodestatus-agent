VERSION ?= 0.2.1
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist

.PHONY: all build release clean deploy test

all: build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(DIST)/nodestatus-agent ./cmd/nodestatus-agent

# install.sh staat ook in de repo-root zodat de curl-one-liner werkt.
# arm = GOARM 6, which runs on everything from a Pi Zero/1 (armv6) up through
# a Pi 3/4 in 32-bit mode (armv7) — one build covers both at the cost of not
# using armv7-only FPU instructions, which this workload never needed anyway.
release: clean
	@cp packaging/install.sh ../install.sh
	@for arch in amd64 arm64 arm; do \
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
