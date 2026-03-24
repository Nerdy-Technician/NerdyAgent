.PHONY: build build-linux build-windows build-darwin clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-X main.Version=$(VERSION) -s -w"

build:
	go build $(LDFLAGS) -o nerdyagent ./cmd/agent

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyagent-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyagent-linux-arm64 ./cmd/agent

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyagent-windows-amd64.exe ./cmd/agent

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/nerdyagent-darwin-amd64 ./cmd/agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/nerdyagent-darwin-arm64 ./cmd/agent

build-all: build-linux build-windows build-darwin

clean:
	rm -rf dist/ nerdyagent nerdyagent.exe
