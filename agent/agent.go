// Package agent ties the collectors, buffer, and pusher together into a
// single run loop. Call Run to start; it blocks until ctx is cancelled.
package agent

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/a-digi/coco-observe/agent/buffer"
	"github.com/a-digi/coco-observe/agent/collector"
	"github.com/a-digi/coco-observe/agent/pusher"
	"github.com/a-digi/coco-observe/payload"
)

// Agent collects and pushes metrics on a schedule.
type Agent struct {
	cfg      *Config
	os       *collector.OSCollector
	runtimes []*collector.RuntimeCollector
	buf      *buffer.Buffer
	push     *pusher.Pusher
	seq      atomic.Int64
}

// New constructs an Agent from cfg. Returns an error if the buffer
// directory cannot be created.
func New(cfg *Config) (*Agent, error) {
	buf, err := buffer.New(cfg.BufferDir, cfg.BufferRetention)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}

	var runtimes []*collector.RuntimeCollector
	for _, p := range cfg.Processes {
		runtimes = append(runtimes, collector.NewRuntimeCollector(p.ScrapeURL))
	}

	return &Agent{
		cfg:      cfg,
		os:       collector.NewOSCollector(),
		runtimes: runtimes,
		buf:      buf,
		push:     pusher.New(cfg.AggregatorURL, cfg.APIKey, cfg.APISecret),
	}, nil
}

// Run starts the collection+push loop. Blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	// Replay any buffered batches from prior runs before the first live push.
	a.replayBuffer()

	ticker := time.NewTicker(a.cfg.PushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collect()
		}
	}
}

func (a *Agent) collect() {
	batch := a.buildBatch(false)
	if err := a.push.Push(batch); err != nil {
		log.Printf("observe agent: push failed (buffering): %v", err)
		if writeErr := a.buf.Write(batch); writeErr != nil {
			log.Printf("observe agent: buffer write failed: %v", writeErr)
		}
		return
	}
	log.Printf("observe agent: pushed batch seq=%d", batch.Sequence)
}

func (a *Agent) replayBuffer() {
	pending, err := a.buf.Pending()
	if err != nil {
		log.Printf("observe agent: buffer list failed: %v", err)
		return
	}
	for _, batch := range pending {
		batch.Buffered = true
		if err := a.push.Push(batch); err != nil {
			log.Printf("observe agent: replay push failed, will retry later: %v", err)
			return
		}
		a.buf.Delete(batch)
		log.Printf("observe agent: replayed buffered batch seq=%d", batch.Sequence)
	}
}

func (a *Agent) buildBatch(buffered bool) *payload.Batch {
	seq := a.seq.Add(1)

	osMetrics, err := a.os.Collect()
	if err != nil {
		log.Printf("observe agent: OS collect error: %v", err)
	}

	var procs []payload.ProcessMetrics
	for i, rc := range a.runtimes {
		name := a.cfg.Processes[i].Name
		scrapeURL := a.cfg.Processes[i].ScrapeURL
		rt, err := rc.Collect()
		pm := payload.ProcessMetrics{Name: name, ScrapeURL: scrapeURL}
		if err != nil {
			pm.ScrapeError = err.Error()
		} else {
			pm.Runtime = rt
		}
		procs = append(procs, pm)
	}

	return &payload.Batch{
		AgentID:   a.cfg.APIKey,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Sequence:  seq,
		Buffered:  buffered,
		OS:        osMetrics,
		Processes: procs,
	}
}
