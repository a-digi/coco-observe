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
	cfg   *Config
	os    *collector.OSCollector // nil when track_os is false
	procs []*collector.ProcCollector
	buf   *buffer.Buffer
	push  *pusher.Pusher
	seq   atomic.Int64
}

// New constructs an Agent from cfg. Returns an error if the buffer
// directory cannot be created.
func New(cfg *Config) (*Agent, error) {
	buf, err := buffer.New(cfg.BufferDir, cfg.BufferRetention)
	if err != nil {
		return nil, fmt.Errorf("agent: %w", err)
	}

	var procs []*collector.ProcCollector
	for _, p := range cfg.Processes {
		procs = append(procs, collector.NewProcCollector(p.Name))
	}

	var osCol *collector.OSCollector
	if cfg.TrackOS {
		osCol = collector.NewOSCollector()
	}

	return &Agent{
		cfg:   cfg,
		os:    osCol,
		procs: procs,
		buf:   buf,
		push:  pusher.New(cfg.AggregatorURL, cfg.APIKey, cfg.APISecret),
	}, nil
}

// Run starts the collection+push loop. Blocks until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
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

	var osMetrics *payload.OSMetrics
	if a.os != nil {
		m, err := a.os.Collect()
		if err != nil {
			log.Printf("observe agent: OS collect error: %v", err)
		} else {
			osMetrics = &m
		}
	}

	procs := make([]payload.ProcessMetrics, 0, len(a.procs))
	for _, pc := range a.procs {
		pm, err := pc.Collect()
		if err != nil {
			// Collect returns an error only for fatal I/O failures (e.g. /proc
			// unreadable). Include the metric with the error set so the
			// aggregator can surface it rather than silently dropping it.
			log.Printf("observe agent: proc collect error: %v", err)
			procs = append(procs, payload.ProcessMetrics{Name: pc.Name(), Error: err.Error()})
			continue
		}
		procs = append(procs, *pm)
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
