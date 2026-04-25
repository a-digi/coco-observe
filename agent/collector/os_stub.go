//go:build !linux

package collector

import (
	"fmt"

	"github.com/a-digi/coco-observe/payload"
)

// OSCollector is a stub on non-Linux platforms.
// The agent binary is built for Linux; this file only exists so the
// package compiles on macOS/Windows during development.
type OSCollector struct{}

func NewOSCollector() *OSCollector { return &OSCollector{} }

func (c *OSCollector) Collect() (payload.OSMetrics, error) {
	return payload.OSMetrics{}, fmt.Errorf("OS metrics collection is only supported on Linux")
}
