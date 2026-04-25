// Package payload defines the wire types shared between the agent and
// the aggregator. Both sides import this package — it has no other
// dependencies so it stays stable.
package payload

// Batch is the JSON body the agent POSTs to the aggregator on every push.
type Batch struct {
	AgentID  string `json:"agent_id"`
	// Timestamp is the UTC wall-clock time the metrics were captured (RFC 3339).
	Timestamp string `json:"timestamp"`
	// Sequence increments by one on every push from this agent.
	// The aggregator uses it to detect gaps caused by buffered replays.
	Sequence int64 `json:"sequence"`
	// Buffered is true when this batch was stored locally and is being
	// replayed after a prior push failure.
	Buffered bool `json:"buffered"`

	// OS holds machine-level metrics (one set per agent, not per process).
	OS OSMetrics `json:"os"`

	// Processes is one entry per monitored Go process on this machine.
	Processes []ProcessMetrics `json:"processes"`
}

// OSMetrics covers the host machine regardless of what processes run on it.
type OSMetrics struct {
	MemTotalBytes     uint64  `json:"mem_total_bytes"`
	MemUsedBytes      uint64  `json:"mem_used_bytes"`
	MemAvailableBytes uint64  `json:"mem_available_bytes"`
	SwapTotalBytes    uint64  `json:"swap_total_bytes"`
	SwapUsedBytes     uint64  `json:"swap_used_bytes"`
	CPULoad1m         float64 `json:"cpu_load_1m"`
	CPULoad5m         float64 `json:"cpu_load_5m"`
	CPULoad15m        float64 `json:"cpu_load_15m"`
	CPUUsagePct       float64 `json:"cpu_usage_pct"`
	Disks             []DiskMetrics `json:"disks"`
	Network           []NetMetrics  `json:"network"`
}

// DiskMetrics covers one mount point.
type DiskMetrics struct {
	Mount          string `json:"mount"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
}

// NetMetrics covers one network interface.
type NetMetrics struct {
	Iface    string `json:"iface"`
	RxBytes  uint64 `json:"rx_bytes"`
	TxBytes  uint64 `json:"tx_bytes"`
	RxErrors uint64 `json:"rx_errors"`
	TxErrors uint64 `json:"tx_errors"`
}

// ProcessMetrics covers one monitored Go process.
type ProcessMetrics struct {
	// Name is the human label from the agent config (e.g. "coco-iam-api").
	Name      string `json:"name"`
	// ScrapeURL is the /debug/vars endpoint this data was scraped from.
	ScrapeURL string `json:"scrape_url"`
	// Runtime is nil when the process was unreachable at scrape time.
	Runtime   *RuntimeMetrics `json:"runtime,omitempty"`
	// ScrapeError is set when Runtime is nil.
	ScrapeError string `json:"scrape_error,omitempty"`
}

// RuntimeMetrics holds Go runtime stats scraped from /debug/vars.
type RuntimeMetrics struct {
	HeapInUseBytes  uint64  `json:"heap_in_use_bytes"`
	HeapAllocBytes  uint64  `json:"heap_alloc_bytes"`
	HeapSysBytes    uint64  `json:"heap_sys_bytes"`
	NumGC           uint32  `json:"num_gc"`
	GCPauseLastMS   float64 `json:"gc_pause_last_ms"`
	Goroutines      int64   `json:"goroutines"`
	UptimeSeconds   int64   `json:"uptime_seconds"`
}
