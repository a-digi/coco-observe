//go:build linux

package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/a-digi/coco-observe/payload"
)

// OSCollector gathers machine-level metrics from /proc and syscall.
// It keeps the previous CPU sample to compute usage percentage.
type OSCollector struct {
	mu      sync.Mutex
	prevCPU cpuSample
}

func NewOSCollector() *OSCollector {
	c := &OSCollector{}
	c.prevCPU, _ = readCPUSample()
	return c
}

func (c *OSCollector) Collect() (payload.OSMetrics, error) {
	mem, err := readMemInfo()
	if err != nil {
		return payload.OSMetrics{}, fmt.Errorf("meminfo: %w", err)
	}

	load, err := readLoadAvg()
	if err != nil {
		return payload.OSMetrics{}, fmt.Errorf("loadavg: %w", err)
	}

	c.mu.Lock()
	curr, err := readCPUSample()
	usagePct := 0.0
	if err == nil {
		usagePct = cpuUsagePct(c.prevCPU, curr)
		c.prevCPU = curr
	}
	c.mu.Unlock()

	disks, _ := readDiskStats()
	net, _ := readNetStats()

	return payload.OSMetrics{
		MemTotalBytes:     mem.total,
		MemUsedBytes:      mem.total - mem.available,
		MemAvailableBytes: mem.available,
		SwapTotalBytes:    mem.swapTotal,
		SwapUsedBytes:     mem.swapTotal - mem.swapFree,
		CPULoad1m:         load[0],
		CPULoad5m:         load[1],
		CPULoad15m:        load[2],
		CPUUsagePct:       usagePct,
		Disks:             disks,
		Network:           net,
	}, nil
}

// --- memory ---

type memInfo struct {
	total, available, swapTotal, swapFree uint64
}

func readMemInfo() (memInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return memInfo{}, err
	}
	defer f.Close()

	var m memInfo
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, _ := strconv.ParseUint(fields[1], 10, 64)
		bytes := kb * 1024
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			m.total = bytes
		case "MemAvailable":
			m.available = bytes
		case "SwapTotal":
			m.swapTotal = bytes
		case "SwapFree":
			m.swapFree = bytes
		}
	}
	return m, scanner.Err()
}

// --- load average ---

func readLoadAvg() ([3]float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return [3]float64{}, err
	}
	fields := strings.Fields(string(data))
	var loads [3]float64
	for i := range loads {
		if i < len(fields) {
			loads[i], _ = strconv.ParseFloat(fields[i], 64)
		}
	}
	return loads, nil
}

// --- CPU ---

type cpuSample struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (s cpuSample) total() uint64 {
	return s.user + s.nice + s.system + s.idle + s.iowait + s.irq + s.softirq + s.steal
}

func (s cpuSample) idleTotal() uint64 {
	return s.idle + s.iowait
}

func cpuUsagePct(prev, curr cpuSample) float64 {
	totalDiff := float64(curr.total() - prev.total())
	idleDiff := float64(curr.idleTotal() - prev.idleTotal())
	if totalDiff <= 0 {
		return 0
	}
	return (1 - idleDiff/totalDiff) * 100
}

func readCPUSample() (cpuSample, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			break
		}
		var s cpuSample
		parse := func(i int) uint64 { v, _ := strconv.ParseUint(fields[i], 10, 64); return v }
		s.user, s.nice, s.system, s.idle = parse(1), parse(2), parse(3), parse(4)
		s.iowait, s.irq, s.softirq, s.steal = parse(5), parse(6), parse(7), parse(8)
		return s, nil
	}
	return cpuSample{}, fmt.Errorf("cpu line not found in /proc/stat")
}

// --- disk ---

var pseudoFS = map[string]bool{
	"tmpfs": true, "proc": true, "sysfs": true, "devtmpfs": true,
	"devpts": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"hugetlbfs": true, "mqueue": true, "securityfs": true, "debugfs": true,
	"tracefs": true, "fusectl": true, "binfmt_misc": true, "overlay": true,
}

func readDiskStats() ([]payload.DiskMetrics, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	var disks []payload.DiskMetrics

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		fsType := fields[2]
		mount := fields[1]
		if pseudoFS[fsType] || seen[mount] {
			continue
		}
		seen[mount] = true

		var st syscall.Statfs_t
		if err := syscall.Statfs(mount, &st); err != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		avail := st.Bavail * uint64(st.Bsize)
		used := total - st.Bfree*uint64(st.Bsize)
		disks = append(disks, payload.DiskMetrics{
			Mount:          mount,
			TotalBytes:     total,
			AvailableBytes: avail,
			UsedBytes:      used,
		})
	}
	return disks, scanner.Err()
}

// --- network ---

func readNetStats() ([]payload.NetMetrics, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var nets []payload.NetMetrics
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip header lines
		}
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colonIdx])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 10 {
			continue
		}
		parse := func(i int) uint64 { v, _ := strconv.ParseUint(fields[i], 10, 64); return v }
		nets = append(nets, payload.NetMetrics{
			Iface:    iface,
			RxBytes:  parse(0),
			RxErrors: parse(2),
			TxBytes:  parse(8),
			TxErrors: parse(10),
		})
	}
	return nets, scanner.Err()
}
