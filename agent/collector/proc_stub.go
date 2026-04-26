//go:build !linux

package collector

import "github.com/a-digi/coco-observe/payload"

// ProcCollector is a no-op on non-Linux platforms.
type ProcCollector struct{ name string }

func NewProcCollector(name string) *ProcCollector { return &ProcCollector{name: name} }

func (c *ProcCollector) Name() string { return c.name }

func (c *ProcCollector) Collect() (*payload.ProcessMetrics, error) {
	return &payload.ProcessMetrics{
		Name:  c.name,
		Found: false,
		Error: "process monitoring via /proc is only supported on Linux",
	}, nil
}
