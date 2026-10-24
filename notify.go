package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Notifications: things worth knowing about Haven and the server, kept in a
// small list that survives restarts. They come from watchers (an app
// stopped working, the disk is filling up), from finished jobs (an update
// failed) and from the update checker.

type Notification struct {
	ID    uint64    `json:"id"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info, ok, warn, bad
	Title string    `json:"title"`
	Body  string    `json:"body,omitempty"`
	Key   string    `json:"key,omitempty"`   // the same key is never added twice
	Route string    `json:"route,omitempty"` // page to open: updates, apps, server
	Job   string    `json:"job,omitempty"`   // job whose log explains this
	Read  bool      `json:"read"`
}

type notifyFile struct {
	Seq     uint64         `json:"seq"`
	Items   []Notification `json:"items"`
	Version string         `json:"version"` // Haven version that last ran
}

const maxNotifications = 120

type Notifier struct {
	mu   sync.Mutex
	path string
	f    notifyFile
}

func NewNotifier(path string) *Notifier {
	n := &Notifier{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &n.f)
	}
	return n
}

func (n *Notifier) saveLocked() {
	if n.path == "" {
		return
	}
	b, _ := json.Marshal(n.f)
	_ = writeFileAtomic(n.path, b, 0o600)
}

// Add records a notification. A key that is already in the list is ignored,
// so a problem that lasts a day produces one notification, not hundreds.
func (n *Notifier) Add(level, title, body, key, route string) {
	n.AddJob(level, title, body, key, route, "")
}

func (n *Notifier) AddJob(level, title, body, key, route, job string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if key != "" {
		for _, it := range n.f.Items {
			if it.Key == key {
				return
			}
		}
	}
	n.f.Seq++
	n.f.Items = append(n.f.Items, Notification{ID: n.f.Seq, Time: time.Now(), Level: level, Title: title, Body: body, Key: key, Route: route, Job: job})
	if len(n.f.Items) > maxNotifications {
		n.f.Items = n.f.Items[len(n.f.Items)-maxNotifications:]
	}
	n.saveLocked()
}

// List returns newest first, and how many are unread.
func (n *Notifier) List() ([]Notification, int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Notification, 0, len(n.f.Items))
	unread := 0
	for i := len(n.f.Items) - 1; i >= 0; i-- {
		out = append(out, n.f.Items[i])
		if !n.f.Items[i].Read {
			unread++
		}
	}
	return out, unread
}

func (n *Notifier) MarkRead(ids []uint64, all bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	want := map[uint64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for i := range n.f.Items {
		if all || want[n.f.Items[i].ID] {
			n.f.Items[i].Read = true
		}
	}
	n.saveLocked()
}

// Clear removes read notifications, or everything when all is true. Keys of
// removed ones are forgotten, so a condition that comes back is reported again.
func (n *Notifier) Clear(all bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	keep := n.f.Items[:0]
	for _, it := range n.f.Items {
		if !all && !it.Read {
			keep = append(keep, it)
		}
	}
	n.f.Items = keep
	n.saveLocked()
}

// NoteVersion reports a Haven upgrade the first time the new version runs.
func (n *Notifier) NoteVersion(v string) {
	n.mu.Lock()
	prev := n.f.Version
	n.f.Version = v
	n.saveLocked()
	n.mu.Unlock()
	if prev != "" && prev != v {
		n.Add("info", "Haven was updated", fmt.Sprintf("Now running version %s (was %s).", v, prev), "version:"+v, "")
	}
}

// --- watchers ---------------------------------------------------------------------------

// Watcher checks the server and the apps every so often and raises
// notifications when something changes for the worse.
type Watcher struct {
	n          *Notifier
	apps       *Manager
	sys        *SystemMonitor
	remote     *Remote
	prev       map[string]string // app id -> last status
	bad        map[string]int    // app id -> consecutive bad samples
	dockerDown int
}

func NewWatcher(n *Notifier, apps *Manager, sys *SystemMonitor, remote *Remote) *Watcher {
	return &Watcher{n: n, apps: apps, sys: sys, remote: remote, prev: map[string]string{}, bad: map[string]int{}}
}

func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	slow := 0
	select {
	case <-time.After(20 * time.Second): // let things settle after a restart
	case <-ctx.Done():
		return
	}
	for {
		w.watchApps(ctx)
		if slow%10 == 0 {
			w.watchServer(ctx)
		}
		slow++
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// problem tells whether a status counts as the app having a problem.
func problem(status string) bool { return status == "error" }

func (w *Watcher) watchApps(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	apps, _, err := w.apps.List(cctx)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, a := range apps {
		seen[a.ID] = true
		if a.Busy != "" || a.Status == "starting" {
			continue // being changed by Haven right now
		}
		prev := w.prev[a.ID]
		if problem(a.Status) {
			w.bad[a.ID]++
			// Two bad samples in a row (about a minute), so a normal restart isn't reported.
			if w.bad[a.ID] == 2 {
				w.n.Add("bad", a.Name+" has a problem", "It keeps crashing or failing its health check. Open the app and use Diagnose to find out why.", fmt.Sprintf("appbad:%s:%d", a.ID, time.Now().Unix()/3600), "apps")
			}
		} else {
			if w.bad[a.ID] >= 2 && a.Status == "running" {
				w.n.Add("ok", a.Name+" is running again", "", fmt.Sprintf("appok:%s:%d", a.ID, time.Now().Unix()/60), "apps")
			}
			w.bad[a.ID] = 0
		}
		w.prev[a.ID] = a.Status
		_ = prev
	}
	for id := range w.prev {
		if !seen[id] {
			delete(w.prev, id)
			delete(w.bad, id)
		}
	}
}

func (w *Watcher) watchServer(ctx context.Context) {
	day := time.Now().Format("2006-01-02")
	s := w.sys.Snapshot()
	if s.Docker == "" && s.Hostname != "" {
		w.dockerDown++
		if w.dockerDown >= 2 {
			w.n.Add("bad", "Docker isn't running", "Your apps are offline. On the server run: sudo systemctl start docker", "docker:"+day, "server")
		}
	} else {
		w.dockerDown = 0
	}
	for _, d := range s.Disks {
		if d.Percent > 90 && (d.Mount == "/" || strings.HasPrefix(d.Mount, "/opt") || strings.HasPrefix(d.Mount, "/srv")) {
			w.n.Add("warn", fmt.Sprintf("%s is %.0f%% full", d.Mount, d.Percent), "Apps fail in odd ways when the disk is full. Remove apps you don't use, or old images.", "disk:"+d.Mount+":"+day, "updates")
		}
	}
	if s.TempC != nil && *s.TempC > 85 {
		w.n.Add("warn", fmt.Sprintf("The CPU is hot (%.0f°C)", *s.TempC), "Check air flow around the server. A closed laptop lid can trap heat.", "temp:"+day, "server")
	}
	if p := PowerState(ctx); p.Supported {
		if b := p.Battery; b != nil && !b.ACOnline {
			lvl := "warn"
			if b.Percent < 20 {
				lvl = "bad"
			}
			w.n.Add(lvl, fmt.Sprintf("Running on battery (%d%%)", b.Percent), "The charger is unplugged or the power is out. The server stops when the battery runs out.", fmt.Sprintf("battery:%s:%d", day, time.Now().Hour()/6), "server")
		}
	}
	if w.remote != nil {
		if r := w.remote.Status(ctx); r.State == "Running" && r.KeyExpiry != nil && time.Until(*r.KeyExpiry) < 14*24*time.Hour {
			w.n.Add("warn", "Remote access sign-in expires soon", "It ends on "+r.KeyExpiry.Format("2 Jan 2006")+". In the Tailscale admin console, open this machine and choose Disable key expiry.", "tskey:"+r.KeyExpiry.Format("2006-01-02"), "server")
		}
	}
}

// jobDone turns a finished job into a notification when it is worth one:
// every failure, and the successful installs and updates (which the user
// may have started and walked away from).
func (n *Notifier) jobDone(j *Job) {
	v := j.View(false)
	name := v.App
	what := map[string]string{"install": "Install", "update": "Update", "update-all": "Updates", "uninstall": "Uninstall", "start": "Start", "stop": "Stop",
		"restart": "Restart", "reconfigure": "Settings change", "wifi-connect": "Wi-Fi", "network-ip": "IP address change", "network-setup": "Network setup",
		"remote-access": "Remote access setup", "local-name": "Local address setup", "cleanup": "Cleanup", "haven-update": "Haven update"}[v.Action]
	if what == "" {
		return
	}
	switch v.Status {
	case "failed":
		title := fmt.Sprintf("%s failed", what)
		if name != "" && name != "server" && name != "updates" {
			title = fmt.Sprintf("%s of %s failed", what, name)
		}
		n.Add("bad", title, v.Error, "job:"+v.ID, routeFor(v.Action))
	case "success":
		if v.Action == "install" || v.Action == "update" || v.Action == "update-all" {
			title := fmt.Sprintf("%s finished", what)
			if name != "" && name != "server" && name != "updates" {
				title = fmt.Sprintf("%s of %s finished", what, name)
			}
			n.Add("ok", title, "", "job:"+v.ID, routeFor(v.Action))
		}
	}
}

func routeFor(action string) string {
	switch action {
	case "update", "update-all":
		return "updates"
	case "install", "uninstall", "start", "stop", "restart", "reconfigure":
		return "apps"
	}
	return "server"
}
