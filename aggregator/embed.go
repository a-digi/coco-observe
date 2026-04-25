package aggregator

import _ "embed"

// EmbeddedAgentAmd64 and EmbeddedAgentArm64 are the base Linux agent
// binaries cross-compiled and embedded at server build time.
// Populate aggregator/binaries/ before building coco-iam:
//
//	cd plugins/coco-observe && make build-agent-linux GOARCH=amd64
//	cd plugins/coco-observe && make build-agent-linux GOARCH=arm64
//
// Both are then embedded automatically when `go build` runs on the server.

//go:embed binaries/observe-agent-linux-amd64
var EmbeddedAgentAmd64 []byte

//go:embed binaries/observe-agent-linux-arm64
var EmbeddedAgentArm64 []byte
