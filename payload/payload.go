// Package payload defines the wire types shared between the agent and
// the aggregator. Both sides import this package — it has no other
// dependencies so it stays stable.
package payload

// Batch is the JSON body the agent POSTs to the aggregator on every push.
type Batch struct {
	AgentID string `json:"agent_id"`
	// Timestamp is the UTC wall-clock time the metrics were captured (RFC 3339).
	Timestamp string `json:"timestamp"`
	// Sequence increments by one on every push from this agent.
	Sequence int64 `json:"sequence"`
	// Buffered is true when this batch was stored locally and is being
	// replayed after a prior push failure.
	Buffered bool `json:"buffered"`

	// OS holds machine-level metrics. Nil when the agent has track_os disabled.
	OS *OSMetrics `json:"os"`

	// Processes is one entry per monitored process on this machine.
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

// ProcessMetrics covers one monitored process, discovered by name via /proc.
type ProcessMetrics struct {
	// Name is the process name from the agent config (e.g. "coco-iam-api").
	Name string `json:"name"`
	// PID is the discovered process ID. Zero when Found is false.
	PID int `json:"pid,omitempty"`
	// Found indicates whether the process was running at collection time.
	Found bool `json:"found"`
	// CPUPct is the CPU usage percentage since the last collection interval.
	// May exceed 100% on multi-core systems.
	CPUPct float64 `json:"cpu_pct"`
	// RSSBytes is the resident set size in bytes.
	RSSBytes uint64 `json:"rss_bytes"`
	// Threads is the number of OS threads the process has.
	Threads int `json:"threads"`
	// Error is set when Found is true but reading stats failed.
	Error string `json:"error,omitempty"`
}
