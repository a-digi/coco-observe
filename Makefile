.PHONY: all build test tidy vendor lint \
        build-agent build-agent-linux build-agent-darwin \
        clean

# ── defaults ──────────────────────────────────────────────────────────────────

GOARCH ?= amd64   # target arch for cross-compiled Linux binary (amd64 or arm64)

# Output directory for compiled binaries (relative to this file).
BIN_DIR := ../../versions

# ── library ───────────────────────────────────────────────────────────────────

# Compile every package in the library (catches type errors on all platforms
# that the stub build tag guards).
all: build

build:
	go build ./...

test:
	go test ./...

tidy:
	go mod tidy

vendor:
	go mod vendor

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
	    echo "golangci-lint not found — install from https://golangci-lint.run/usage/install/"; \
	    exit 1; \
	}
	golangci-lint run ./...

# ── agent binary ──────────────────────────────────────────────────────────────

# Build the agent for the current host OS/arch (useful for local smoke-tests).
build-agent:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/observe-agent ./cmd/agent
	@echo "Built $(BIN_DIR)/observe-agent ($(shell go env GOOS)/$(shell go env GOARCH))"

# Cross-compile the agent for Linux and place the result into the embed
# source directory so the server binary is self-contained after `go build`.
# Run this once on the developer machine before building coco-iam:
#
#   cd plugins/coco-observe && make build-agent-linux
#
# This builds both amd64 and arm64 into aggregator/binaries/ which are then
# picked up by //go:embed in aggregator/embed.go when coco-iam is compiled.
build-agent-linux:
	@mkdir -p aggregator/binaries
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	    go build -o aggregator/binaries/observe-agent-linux-amd64 ./cmd/agent
	@echo "Built aggregator/binaries/observe-agent-linux-amd64"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
	    go build -o aggregator/binaries/observe-agent-linux-arm64 ./cmd/agent
	@echo "Built aggregator/binaries/observe-agent-linux-arm64"

# Build the agent for macOS (handy for local testing of the push path).
build-agent-darwin:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=$(shell go env GOARCH) \
	    go build -o $(BIN_DIR)/observe-agent-darwin ./cmd/agent
	@echo "Built $(BIN_DIR)/observe-agent-darwin"

# ── housekeeping ──────────────────────────────────────────────────────────────

clean:
	rm -f $(BIN_DIR)/observe-agent $(BIN_DIR)/observe-agent-linux-* $(BIN_DIR)/observe-agent-darwin
