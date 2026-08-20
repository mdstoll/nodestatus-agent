VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist

.PHONY: all build release clean deploy test

all: build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(DIST)/serverinfo-agent ./cmd/serverinfo-agent

release: clean
	@for arch in amd64 arm64; do \
	  echo "→ linux/$$arch"; \
	  mkdir -p $(DIST)/$$arch; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
	    -o $(DIST)/$$arch/serverinfo-agent ./cmd/serverinfo-agent; \
	  cp packaging/install.sh packaging/uninstall.sh packaging/serverinfo-agent.service $(DIST)/$$arch/; \
	  tar -czf $(DIST)/serverinfo-agent_$(VERSION)_linux_$$arch.tar.gz -C $(DIST)/$$arch .; \
	done
	@cd $(DIST) && shasum -a 256 *.tar.gz > SHA256SUMS
	@ls -lh $(DIST)/*.tar.gz $(DIST)/SHA256SUMS

test:
	go vet ./...
	go test ./...

clean:
	rm -rf $(DIST)
