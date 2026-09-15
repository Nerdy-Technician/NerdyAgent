.PHONY: build build-linux build-windows build-darwin build-all checksums clean test

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -s -w"
export CGO_ENABLED=0

build:
	go build $(LDFLAGS) -o nerdyrmm-agent ./cmd/agent

build-linux:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-arm64 ./cmd/agent
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-armv7 ./cmd/agent
	# Single binary also runs the tray (`--tray`). Publish a dedicated asset name for operators.
	cp dist/nerdyrmm-agent-linux-amd64 dist/nerdyrmm-agent-tray-linux-amd64
	cp dist/nerdyrmm-agent-linux-arm64 dist/nerdyrmm-agent-tray-linux-arm64
	cp dist/nerdyrmm-agent-linux-armv7 dist/nerdyrmm-agent-tray-linux-armv7

build-windows:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-windows-amd64.exe ./cmd/agent
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-windows-arm64.exe ./cmd/agent

build-darwin:
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-darwin-arm64 ./cmd/agent

checksums:
	mkdir -p dist
	cd dist && sha256sum $$(ls -1 | grep -v -E '^SHA256SUMS$$') > SHA256SUMS

build-all: build-linux build-windows build-darwin checksums

test:
	go test ./...

clean:
	rm -rf dist/ nerdyrmm-agent nerdyrmm-agent.exe
