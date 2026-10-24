package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// AppUsage is how much of the server one app uses, adding up all its containers.
type AppUsage struct {
	CPU        float64   `json:"cpu"`              // percent of the whole server's CPU
	Mem        uint64    `json:"mem"`              // bytes of RAM in use
	MemPercent float64   `json:"memPercent"`       // percent of the server's RAM
	Images     uint64    `json:"images"`           // bytes taken by the app's images
	Data       uint64    `json:"data"`             // bytes in the app's data folder
	DataAt     time.Time `json:"dataAt,omitempty"` // when Data was measured; zero = not yet
	Live       bool      `json:"live"`             // CPU/memory numbers are fresh
}

type containerStats struct {
	cpu float64 // percent of one core, as Docker reports it
	mem uint64
}

type dataSize struct {
	bytes uint64
	at    time.Time
}

// UsageMonitor samples per-container CPU and memory, image sizes and data
// folder sizes in the background, so the API never waits on Docker.
//
// CPU and memory are only sampled while someone is looking at Haven, so an
// idle server isn't kept busy. Folder sizes are measured every few minutes,
// because walking a large folder (a photo library, say) is slow.
type UsageMonitor struct {
	docker *Docker
	dir    string

	mu       sync.Mutex
	stats    map[string]containerStats // by container name
	statsAt  time.Time
	images   map[string]uint64 // by app id
	data     map[string]dataSize
	scanning map[string]bool
	wanted   time.Time
	sampling bool
	cores    int
	memTotal uint64
	kick     chan struct{}
}

const (
	statsEvery  = 5 * time.Second
	imagesEvery = time.Minute
	dataEvery   = 5 * time.Minute
	idleAfter   = 90 * time.Second
)

func NewUsageMonitor(dir string, d *Docker) *UsageMonitor {
	u := &UsageMonitor{
		docker: d, dir: dir,
		stats: map[string]containerStats{}, images: map[string]uint64{},
		data: map[string]dataSize{}, scanning: map[string]bool{},
		kick: make(chan struct{}, 1),
	}
	if n, err := cpu.Counts(true); err == nil && n > 0 {
		u.cores = n
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		u.memTotal = vm.Total
	}
	return u
}

func (u *UsageMonitor) Run(ctx context.Context) {
	u.sampleImages(ctx)
	go u.scanAll()
	stats := time.NewTicker(statsEvery)
	images := time.NewTicker(imagesEvery)
	data := time.NewTicker(dataEvery)
	defer stats.Stop()
	defer images.Stop()
	defer data.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.kick:
			u.sampleStats(ctx)
		case <-stats.C:
			if u.isWanted() {
				u.sampleStats(ctx)
			}
		case <-images.C:
			if u.isWanted() {
				u.sampleImages(ctx)
			}
		case <-data.C:
			go u.scanAll()
		}
	}
}

func (u *UsageMonitor) isWanted() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return time.Since(u.wanted) < idleAfter
}

// Wanted marks that someone is looking. If the numbers are stale (Haven was
// idle), it asks for a fresh sample right away.
func (u *UsageMonitor) Wanted() {
	u.mu.Lock()
	u.wanted = time.Now()
	stale := time.Since(u.statsAt) > 2*statsEvery
	u.mu.Unlock()
	if stale {
		select {
		case u.kick <- struct{}{}:
		default:
		}
	}
}

// Changed refreshes an app's numbers after it is installed, updated or removed.
func (u *UsageMonitor) Changed(id string, removed bool) {
	if removed {
		u.mu.Lock()
		delete(u.images, id)
		delete(u.data, id)
		u.mu.Unlock()
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		u.sampleImages(ctx)
		u.scan(id)
	}()
}

type statsLine struct {
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
}

func (u *UsageMonitor) sampleStats(ctx context.Context) {
	u.mu.Lock()
	if u.sampling {
		u.mu.Unlock()
		return
	}
	u.sampling = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.sampling = false
		u.mu.Unlock()
	}()

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := u.docker.run(cctx, "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		return
	}
	stats := parseStats(out)
	u.mu.Lock()
	u.stats = stats
	u.statsAt = time.Now()
	u.mu.Unlock()
}

func parseStats(out []byte) map[string]containerStats {
	stats := map[string]containerStats{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		var l statsLine
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Name == "" {
			continue
		}
		cpuPct, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(l.CPUPerc), "%"), 64)
		used, _, _ := strings.Cut(l.MemUsage, "/")
		stats[l.Name] = containerStats{cpu: cpuPct, mem: parseHumanSize(used)}
	}
	return stats
}

// parseHumanSize reads Docker's sizes like "4.289MiB", "8.19kB" or "0B".
func parseHumanSize(s string) uint64 {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (s[i] == '.' || (s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{
		"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
		"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
	}[strings.TrimSpace(s[i:])]
	if mult == 0 {
		return 0
	}
	return uint64(math.Round(n * mult))
}

// sampleImages adds up the images each app's containers use. An image shared
// by two containers of the same app counts once.
func (u *UsageMonitor) sampleImages(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	containers, err := u.docker.ListContainers(cctx)
	if err != nil {
		return
	}
	var names []string
	appOf := map[string]string{}
	for _, c := range containers {
		if id, ok := strings.CutPrefix(c.Project, "haven-"); ok {
			names = append(names, c.Name)
			appOf[c.Name] = id
		}
	}
	images := map[string]uint64{}
	if len(names) > 0 {
		out, err := u.docker.run(cctx, append([]string{"inspect", "--format", "{{.Name}}|{{.Image}}"}, names...)...)
		if err != nil {
			return
		}
		imageOf := map[string]string{}
		unique := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			name, img, ok := strings.Cut(strings.TrimSpace(line), "|")
			if ok {
				imageOf[strings.TrimPrefix(name, "/")] = img
				unique[img] = true
			}
		}
		ids := make([]string, 0, len(unique))
		for id := range unique {
			ids = append(ids, id)
		}
		out, err = u.docker.run(cctx, append([]string{"image", "inspect", "--format", "{{.Id}}|{{.Size}}"}, ids...)...)
		if err != nil {
			return
		}
		size := map[string]uint64{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			id, n, ok := strings.Cut(strings.TrimSpace(line), "|")
			if ok {
				v, _ := strconv.ParseUint(n, 10, 64)
				size[id] = v
			}
		}
		counted := map[string]bool{}
		for name, app := range appOf {
			img := imageOf[name]
			if img == "" || counted[app+img] {
				continue
			}
			counted[app+img] = true
			images[app] += size[img]
		}
	}
	u.mu.Lock()
	u.images = images
	u.mu.Unlock()
}

func (u *UsageMonitor) scanAll() {
	entries, _ := os.ReadDir(filepath.Join(u.dir, "apps"))
	for _, e := range entries {
		if e.IsDir() && appIDRe.MatchString(e.Name()) {
			u.scan(e.Name())
		}
	}
}

// scan measures one app's data folder. Only one scan per app runs at a time.
func (u *UsageMonitor) scan(id string) {
	u.mu.Lock()
	if u.scanning[id] {
		u.mu.Unlock()
		return
	}
	u.scanning[id] = true
	u.mu.Unlock()
	n := dirSize(filepath.Join(u.dir, "appdata", id))
	u.mu.Lock()
	u.data[id] = dataSize{bytes: n, at: time.Now()}
	delete(u.scanning, id)
	u.mu.Unlock()
}

// dirSize adds up regular files under root. Symlinks are not followed and
// unreadable folders are skipped rather than failing the whole count.
func dirSize(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += uint64(info.Size())
			}
		}
		return nil
	})
	return total
}

// Attach fills in an app's usage and its containers' CPU and memory.
func (u *UsageMonitor) Attach(a *InstalledApp) {
	u.mu.Lock()
	defer u.mu.Unlock()
	us := &AppUsage{Images: u.images[a.ID], Live: time.Since(u.statsAt) < 6*statsEvery}
	for i := range a.Containers {
		c := &a.Containers[i]
		if s, ok := u.stats[c.Name]; ok && c.State == "running" {
			c.CPU = u.share(s.cpu)
			c.Mem = s.mem
			us.CPU += c.CPU
			us.Mem += s.mem
		}
	}
	if u.memTotal > 0 {
		us.MemPercent = float64(us.Mem) / float64(u.memTotal) * 100
	}
	if d, ok := u.data[a.ID]; ok {
		us.Data, us.DataAt = d.bytes, d.at
	} else if !u.scanning[a.ID] {
		go u.scan(a.ID) // a new app: measure it now rather than in 5 minutes
	}
	a.Usage = us
}

// AttachContainer fills in CPU and memory for a container Haven didn't install.
func (u *UsageMonitor) AttachContainer(c *Container) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if s, ok := u.stats[c.Name]; ok && c.State == "running" {
		c.CPU = u.share(s.cpu)
		c.Mem = s.mem
	}
}

// share turns Docker's "percent of one core" into percent of the whole server.
func (u *UsageMonitor) share(perCore float64) float64 {
	if u.cores > 0 {
		return perCore / float64(u.cores)
	}
	return perCore
}
