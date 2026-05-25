.PHONY: build build-linux build-windows build-darwin build-all clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -s -w"

build:
	go build $(LDFLAGS) -o nerdyrmm-agent ./cmd/agent

build-linux:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-arm64 ./cmd/agent
	GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -o dist/nerdyrmm-agent-linux-armv7 ./cmd/agent

build-windows:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-windows-amd64.exe ./cmd/agent
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-windows-arm64.exe ./cmd/agent

build-darwin:
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyrmm-agent-darwin-arm64 ./cmd/agent

build-all: build-linux build-windows build-darwin

clean:
	rm -rf dist/ nerdyrmm-agent nerdyrmm-agent.exe
