package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
)

type DiskInfo struct {
	Mount   string  `json:"mount"`
	Device  string  `json:"device"`
	Fstype  string  `json:"fstype"`
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	Free    uint64  `json:"free"`
	Percent float64 `json:"percent"`
}

type SystemInfo struct {
	Hostname   string     `json:"hostname"`
	OS         string     `json:"os"`
	Kernel     string     `json:"kernel"`
	Arch       string     `json:"arch"`
	Uptime     uint64     `json:"uptime"`
	CPUModel   string     `json:"cpuModel"`
	CPUCores   int        `json:"cpuCores"`
	CPUPercent float64    `json:"cpuPercent"`
	PerCore    []float64  `json:"perCore"`
	Load       [3]float64 `json:"load"`
	MemTotal   uint64     `json:"memTotal"`
	MemUsed    uint64     `json:"memUsed"`
	MemPercent float64    `json:"memPercent"`
	SwapTotal  uint64     `json:"swapTotal"`
	SwapUsed   uint64     `json:"swapUsed"`
	Disks      []DiskInfo `json:"disks"`
	NetRx      float64    `json:"netRx"` // bytes/second
	NetTx      float64    `json:"netTx"`
	NetRxTotal uint64     `json:"netRxTotal"`
	NetTxTotal uint64     `json:"netTxTotal"`
	TempC      *float64   `json:"tempC"`
	Docker     string     `json:"docker"`
	Version    string     `json:"version"`
	History    []Point    `json:"history"`
}

type Point struct {
	T   int64   `json:"t"`
	CPU float64 `json:"cpu"`
	Mem float64 `json:"mem"`
	Rx  float64 `json:"rx"`
	Tx  float64 `json:"tx"`
}

// SystemMonitor samples the host every few seconds in the background, so API
// calls are instant and don't each spend CPU measuring.
type SystemMonitor struct {
	dataDir string
	docker  *Docker
	mu      sync.RWMutex
	info    SystemInfo
	history []Point
}

func NewSystemMonitor(dataDir string, d *Docker) *SystemMonitor {
	return &SystemMonitor{dataDir: dataDir, docker: d}
}

func (m *SystemMonitor) Snapshot() SystemInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.info
	s.History = append([]Point(nil), m.history...)
	s.Disks = append([]DiskInfo(nil), m.info.Disks...)
	return s
}

func (m *SystemMonitor) Run(ctx context.Context) {
	var lastRx, lastTx uint64
	var lastT time.Time
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	slow := 0
	for {
		info := m.Snapshot()
		info.History = nil
		info.Version = Version
		info.Arch = runtime.GOARCH

		if pc, err := cpu.Percent(0, true); err == nil && len(pc) > 0 {
			info.PerCore = pc
			sum := 0.0
			for _, v := range pc {
				sum += v
			}
			info.CPUPercent = sum / float64(len(pc))
		}
		if vm, err := mem.VirtualMemory(); err == nil {
			info.MemTotal, info.MemUsed, info.MemPercent = vm.Total, vm.Used, vm.UsedPercent
		}
		if sw, err := mem.SwapMemory(); err == nil {
			info.SwapTotal, info.SwapUsed = sw.Total, sw.Used
		}
		if l, err := load.Avg(); err == nil {
			info.Load = [3]float64{l.Load1, l.Load5, l.Load15}
		}
		if rx, tx, err := netTotals(); err == nil {
			now := time.Now()
			if !lastT.IsZero() && rx >= lastRx && tx >= lastTx {
				secs := now.Sub(lastT).Seconds()
				info.NetRx = float64(rx-lastRx) / secs
				info.NetTx = float64(tx-lastTx) / secs
			}
			lastRx, lastTx, lastT = rx, tx, now
			info.NetRxTotal, info.NetTxTotal = rx, tx
		}
		info.TempC = cpuTemp()

		// Slower-changing things: refresh every ~30s.
		if slow%10 == 0 {
			if h, err := host.Info(); err == nil {
				info.Hostname = h.Hostname
				info.OS = strings.TrimSpace(h.Platform + " " + h.PlatformVersion)
				info.Kernel = h.KernelVersion
				info.Uptime = h.Uptime
			}
			if ci, err := cpu.Info(); err == nil && len(ci) > 0 {
				info.CPUModel = ci[0].ModelName
			}
			if n, err := cpu.Counts(true); err == nil {
				info.CPUCores = n
			}
			info.Disks = listDisks()
			dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if v, err := m.docker.Version(dctx); err == nil {
				info.Docker = v
			} else {
				info.Docker = ""
			}
			cancel()
		} else if h, err := host.Uptime(); err == nil {
			info.Uptime = h
		}
		slow++

		m.mu.Lock()
		m.info = info
		m.history = append(m.history, Point{T: time.Now().Unix(), CPU: info.CPUPercent, Mem: info.MemPercent, Rx: info.NetRx, Tx: info.NetTx})
		if len(m.history) > 120 { // ~6 minutes of history
			m.history = m.history[len(m.history)-120:]
		}
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// netTotals sums traffic on physical interfaces only. Docker's virtual
// interfaces would otherwise count the same bytes twice.
func netTotals() (uint64, uint64, error) {
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return 0, 0, err
	}
	var rx, tx uint64
	for _, c := range counters {
		n := c.Name
		if n == "lo" || strings.HasPrefix(n, "lo") || strings.HasPrefix(n, "veth") || strings.HasPrefix(n, "docker") ||
			strings.HasPrefix(n, "br-") || strings.HasPrefix(n, "virbr") || strings.HasPrefix(n, "tailscale") ||
			strings.HasPrefix(n, "utun") || strings.HasPrefix(n, "bridge") || strings.HasPrefix(n, "awdl") || strings.HasPrefix(n, "llw") {
			continue
		}
		rx += c.BytesRecv
		tx += c.BytesSent
	}
	return rx, tx, nil
}

var realFS = map[string]bool{
	"ext4": true, "ext3": true, "ext2": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true, "apfs": true, "nfs": true, "nfs4": true, "cifs": true,
}

func listDisks() []DiskInfo {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []DiskInfo
	for _, p := range parts {
		if !realFS[p.Fstype] || seen[p.Device] {
			continue
		}
		if strings.HasPrefix(p.Mountpoint, "/boot") || strings.HasPrefix(p.Mountpoint, "/snap") ||
			strings.HasPrefix(p.Mountpoint, "/System/Volumes") || strings.Contains(p.Mountpoint, "/docker/") {
			continue
		}
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		seen[p.Device] = true
		out = append(out, DiskInfo{Mount: p.Mountpoint, Device: p.Device, Fstype: p.Fstype, Total: u.Total, Used: u.Used, Free: u.Free, Percent: u.UsedPercent})
	}
	return out
}

// cpuTemp reads the CPU temperature from Linux sysfs. Returns nil when unknown.
func cpuTemp() *float64 {
	best := -1.0
	// hwmon sensors with a known CPU driver name are the most accurate.
	hw, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, d := range hw {
		name, _ := os.ReadFile(filepath.Join(d, "name"))
		n := strings.TrimSpace(string(name))
		if n != "coretemp" && n != "k10temp" && n != "zenpower" && n != "cpu_thermal" && n != "soc_thermal" {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(d, "temp*_input"))
		for _, f := range inputs {
			if v, ok := readMilliC(f); ok && v > best {
				best = v
			}
		}
	}
	if best < 0 {
		zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
		for _, z := range zones {
			t, _ := os.ReadFile(filepath.Join(z, "type"))
			typ := strings.ToLower(string(t))
			if !strings.Contains(typ, "cpu") && !strings.Contains(typ, "x86_pkg") && !strings.Contains(typ, "soc") {
				continue
			}
			if v, ok := readMilliC(filepath.Join(z, "temp")); ok && v > best {
				best = v
			}
		}
	}
	if best < 0 {
		return nil
	}
	return &best
}

func readMilliC(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v / 1000, true
}
