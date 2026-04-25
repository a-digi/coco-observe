package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a-digi/coco-observe/payload"
)

// RuntimeCollector scrapes the /debug/vars endpoint of a Go process
// and extracts runtime.MemStats + optional goroutine count.
type RuntimeCollector struct {
	client    *http.Client
	scrapeURL string
	startTime time.Time
}

func NewRuntimeCollector(scrapeURL string) *RuntimeCollector {
	return &RuntimeCollector{
		scrapeURL: scrapeURL,
		startTime: time.Now(),
		client:    &http.Client{Timeout: 5 * time.Second},
	}
}

// debugVars is the shape of the /debug/vars JSON response.
type debugVars struct {
	MemStats struct {
		HeapInuse uint64  `json:"HeapInuse"`
		HeapAlloc uint64  `json:"HeapAlloc"`
		HeapSys   uint64  `json:"HeapSys"`
		NumGC     uint32  `json:"NumGC"`
		PauseNs   []uint64 `json:"PauseNs"`
	} `json:"memstats"`
	// Goroutines is non-standard — processes must explicitly export it via
	// expvar.Publish("goroutines", expvar.Func(func() any { return runtime.NumGoroutine() }))
	Goroutines *int64 `json:"goroutines"`
}

func (c *RuntimeCollector) Collect() (*payload.RuntimeMetrics, error) {
	resp, err := c.client.Get(c.scrapeURL)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", c.scrapeURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: HTTP %d", c.scrapeURL, resp.StatusCode)
	}

	var vars debugVars
	if err := json.NewDecoder(resp.Body).Decode(&vars); err != nil {
		return nil, fmt.Errorf("decode %s: %w", c.scrapeURL, err)
	}

	m := &payload.RuntimeMetrics{
		HeapInUseBytes: vars.MemStats.HeapInuse,
		HeapAllocBytes: vars.MemStats.HeapAlloc,
		HeapSysBytes:   vars.MemStats.HeapSys,
		NumGC:          vars.MemStats.NumGC,
		UptimeSeconds:  int64(time.Since(c.startTime).Seconds()),
	}

	// Last GC pause: PauseNs is a circular buffer of 256 entries.
	// Index (NumGC+255)%256 holds the most recent pause in nanoseconds.
	if vars.MemStats.NumGC > 0 && len(vars.MemStats.PauseNs) > 0 {
		idx := (vars.MemStats.NumGC + 255) % 256
		if int(idx) < len(vars.MemStats.PauseNs) {
			m.GCPauseLastMS = float64(vars.MemStats.PauseNs[idx]) / 1e6
		}
	}

	if vars.Goroutines != nil {
		m.Goroutines = *vars.Goroutines
	}

	return m, nil
}
