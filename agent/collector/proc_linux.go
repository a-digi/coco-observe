//go:build linux

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-digi/coco-observe/payload"
)

// clockTickHz is SC_CLK_TCK — the number of jiffies per second.
// 100 is correct on virtually all Linux systems.
const clockTickHz = 100

// ProcCollector monitors a process by name, reading stats from /proc.
type ProcCollector struct {
	name          string
	mu            sync.Mutex
	prevPID       int
	prevStartTime uint64 // field 22 of /proc/<pid>/stat — detects PID reuse
	prevTicks     uint64
	prevTime      time.Time
}

func NewProcCollector(name string) *ProcCollector {
	return &ProcCollector{name: name}
}

func (c *ProcCollector) Name() string { return c.name }

func (c *ProcCollector) Collect() (*payload.ProcessMetrics, error) {
	pid, err := findPIDByName(c.name)
	if err != nil {
		return &payload.ProcessMetrics{Name: c.name, Found: false}, nil
	}

	ticks, startTime, statErr := readProcTicks(pid)
	rss, threads := readProcStatus(pid)

	now := time.Now()

	c.mu.Lock()
	prevPID := c.prevPID
	prevStartTime := c.prevStartTime
	prevTicks := c.prevTicks
	prevTime := c.prevTime
	c.prevPID = pid
	c.prevStartTime = startTime
	c.prevTicks = ticks
	c.prevTime = now
	c.mu.Unlock()

	var cpuPct float64
	// Only compute delta if this is the same process instance (same PID and starttime).
	sameProcess := pid == prevPID && startTime == prevStartTime && startTime != 0
	if !prevTime.IsZero() && sameProcess && statErr == nil {
		elapsed := now.Sub(prevTime).Seconds()
		if elapsed > 0 && ticks >= prevTicks {
			cpuPct = float64(ticks-prevTicks) * 100 / (elapsed * clockTickHz)
		}
	}

	pm := &payload.ProcessMetrics{
		Name:     c.name,
		PID:      pid,
		Found:    true,
		CPUPct:   cpuPct,
		RSSBytes: rss,
		Threads:  threads,
	}
	if statErr != nil {
		pm.Error = statErr.Error()
	}
	return pm, nil
}

// findPIDByName scans /proc/*/cmdline to find the first process whose
// argv[0] basename matches name.
func findPIDByName(name string) (int, error) {
	entries, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return 0, err
	}
	for _, p := range entries {
		data, err := os.ReadFile(p)
		if err != nil {
			continue // process may have exited or be unreadable (EPERM)
		}
		if len(data) == 0 {
			continue // kernel thread — no cmdline
		}
		// cmdline is NUL-separated; first token is argv[0]
		arg0 := strings.SplitN(string(data), "\x00", 2)[0]
		if filepath.Base(arg0) == name {
			parts := strings.Split(p, "/")
			if len(parts) >= 3 {
				if pid, err := strconv.Atoi(parts[2]); err == nil {
					return pid, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("process %q not found in /proc", name)
}

// readProcTicks returns utime+stime (total CPU jiffies) and starttime (field 22
// after the closing ')') from /proc/<pid>/stat. starttime uniquely identifies a
// process instance — used to detect PID reuse between collections.
func readProcTicks(pid int) (ticks, startTime uint64, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	// Format: pid (comm) state ppid ... utime stime ...
	// Use LastIndex to skip past the closing ')' of comm (comm may contain spaces and parens).
	rParen := strings.LastIndex(string(data), ")")
	if rParen < 0 {
		return 0, 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data)[rParen+1:])
	// After ')': state(0) ppid(1) pgrp(2) session(3) tty(4) tpgid(5)
	// flags(6) minflt(7) cminflt(8) majflt(9) cmajflt(10) utime(11) stime(12)
	// ... cutime(13) cstime(14) priority(15) nice(16) num_threads(17)
	// itrealvalue(18) starttime(19)
	if len(fields) < 20 {
		return 0, 0, fmt.Errorf("too few fields in /proc/%d/stat", pid)
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse utime in /proc/%d/stat: %w", pid, err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse stime in /proc/%d/stat: %w", pid, err)
	}
	st, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse starttime in /proc/%d/stat: %w", pid, err)
	}
	return utime + stime, st, nil
}

// readProcStatus parses VmRSS and Threads from /proc/<pid>/status.
func readProcStatus(pid int) (rssBytes uint64, threads int) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, _ := strconv.ParseUint(fields[1], 10, 64)
				rssBytes = val * 1024 // kB → bytes
			}
		case strings.HasPrefix(line, "Threads:"):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				threads, _ = strconv.Atoi(fields[1])
			}
		}
	}
	return
}
