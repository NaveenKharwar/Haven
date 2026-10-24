package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cleanup: safe, explained ways to get disk space back. Nothing here touches
// app data or volumes. Each item says what it frees before anything runs.

type CleanupItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Info  string `json:"info"`
	Bytes uint64 `json:"bytes"` // about how much it would free
	Note  string `json:"note,omitempty"`
	Hint  string `json:"hint,omitempty"` // shown in place of a size when it can't be measured
	Path  string `json:"path,omitempty"` // where these files live, so the UI can open it in Files
}

type Cleaner struct {
	docker *Docker
	jobs   *Jobs
}

func NewCleaner(d *Docker, jobs *Jobs) *Cleaner { return &Cleaner{docker: d, jobs: jobs} }

func dockerRoot(ctx context.Context) string {
	out, err := runHost(ctx, 10*time.Second, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil || !strings.HasPrefix(out, "/") {
		return "/var/lib/docker"
	}
	return strings.TrimSpace(out)
}

// dockerReclaimable reads `docker system df` (Type, Reclaimable like "1.2GB (50%)").
func dockerReclaimable(ctx context.Context) map[string]uint64 {
	res := map[string]uint64{}
	out, err := runHost(ctx, 30*time.Second, "docker", "system", "df", "--format", "{{json .}}")
	if err != nil {
		return res
	}
	for _, line := range strings.Split(out, "\n") {
		var row struct{ Type, Reclaimable string }
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		s := row.Reclaimable
		if i := strings.Index(s, " "); i > 0 {
			s = s[:i]
		}
		res[row.Type] = parseHumanSize(s)
	}
	return res
}

func (c *Cleaner) Scan(ctx context.Context) []CleanupItem {
	var items []CleanupItem
	rec := dockerReclaimable(ctx)
	root := dockerRoot(ctx)
	// Newer Docker keeps images in containerd instead of overlay2.
	imgPath := firstExisting(filepath.Join(root, "overlay2"), "/var/lib/containerd", root)

	var dangling uint64
	if out, err := runHost(ctx, 20*time.Second, "docker", "images", "--filter", "dangling=true", "--format", "{{.Size}}"); err == nil {
		for _, l := range strings.Fields(out) {
			dangling += parseHumanSize(l)
		}
	}
	items = append(items, CleanupItem{ID: "docker-dangling", Title: "Leftover image pieces", Bytes: dangling,
		Info: "Old layers left behind after apps are updated. Nothing uses them. Safe to remove.", Path: imgPath})
	items = append(items, CleanupItem{ID: "docker-images", Title: "All unused images", Bytes: rec["Images"],
		Info: "Every downloaded image no app is using, including the previous version Haven keeps for rollback. Apps that are stopped keep their image.",
		Note: "After this you can't roll back an update.", Path: imgPath})
	items = append(items, CleanupItem{ID: "docker-cache", Title: "Docker build cache", Bytes: rec["Build Cache"],
		Info: "Temporary files from building images. Docker rebuilds them if needed.", Path: root})

	if out, err := runHost(ctx, 10*time.Second, "sh", "-c", "du -sb "+shellQuote(filepath.Join(root, "containers"))+"/*/*-json.log 2>/dev/null | awk '{s+=$1} END {print s+0}'"); err == nil {
		n, _ := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
		items = append(items, CleanupItem{ID: "docker-logs", Title: "App log files", Bytes: n,
			Info: "The text logs Docker keeps for every app. They grow forever. Emptying them doesn't affect the apps, you only lose old log lines.", Path: filepath.Join(root, "containers")})
	}

	{
		if out, err := runHost(ctx, 10*time.Second, "journalctl", "--disk-usage"); err == nil {
			if i := strings.Index(out, "take up "); i >= 0 {
				rest := out[i+len("take up "):]
				if j := strings.Index(rest, " "); j > 0 {
					sz := parseHumanSize(strings.NewReplacer("K", "KB", "M", "MB", "G", "GB", "T", "TB").Replace(rest[:j]))
					var free uint64
					if sz > 200e6 {
						free = sz - 200e6
					}
					items = append(items, CleanupItem{ID: "journal", Title: "Old system logs", Bytes: free,
						Info: "Keeps the newest 200 MB of the server's system log and removes the rest.", Path: "/var/log/journal"})
				}
			}
		}
	}

	if out, err := runHost(ctx, 10*time.Second, "sh", "-c", "du -sb /var/cache/apt/archives 2>/dev/null | cut -f1"); err == nil {
		n, _ := strconv.ParseUint(strings.TrimSpace(out), 10, 64)
		items = append(items, CleanupItem{ID: "apt-cache", Title: "Downloaded Ubuntu packages", Bytes: n,
			Info: "Package files kept after installing updates. They can be downloaded again if ever needed.", Path: "/var/cache/apt/archives"})
	}
	if out, err := runHost(ctx, 20*time.Second, "sh", "-c", "apt-get -s autoremove 2>/dev/null | grep -c '^Remv'"); err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(out))
		if n > 0 {
			items = append(items, CleanupItem{ID: "apt-autoremove", Title: "Ubuntu packages nothing needs", Hint: fmt.Sprintf("%d package(s)", n),
				Info: "Software that was installed only to support something since removed."})
		}
	}
	return items
}

var cleanupCommands = map[string][]string{
	"docker-dangling": {"docker", "image", "prune", "-f"},
	"docker-images":   {"docker", "image", "prune", "-a", "-f"},
	"docker-cache":    {"docker", "builder", "prune", "-f"},
	"journal":         {"journalctl", "--vacuum-size=200M"},
	"apt-cache":       {"apt-get", "clean"},
	"apt-autoremove":  {"apt-get", "-y", "autoremove"},
}

func (c *Cleaner) Run(ids []string) (*Job, error) {
	if len(ids) == 0 {
		return nil, errors.New("choose what to clean")
	}
	for _, id := range ids {
		if _, ok := cleanupCommands[id]; !ok && id != "docker-logs" {
			return nil, errors.New("unknown cleanup item")
		}
	}
	return c.jobs.Start("server", "cleanup", 15*time.Minute, func(ctx context.Context, j *Job) error {
		for _, id := range ids {
			if id == "docker-logs" {
				j.Logf("Emptying app log files")
				root := filepath.Join(dockerRoot(ctx), "containers")
				files, _ := filepath.Glob(filepath.Join(root, "*", "*-json.log"))
				for _, f := range files {
					if err := os.Truncate(f, 0); err != nil {
						j.Logf("could not empty %s: %v", filepath.Base(f), err)
					}
				}
				j.Logf("Emptied %d log file(s)", len(files))
				continue
			}
			cmd := cleanupCommands[id]
			j.Logf("%s", strings.Join(cmd, " "))
			if err := hostStream(ctx, j, []string{"DEBIAN_FRONTEND=noninteractive"}, cmd[0], cmd[1:]...); err != nil {
				return fmt.Errorf("%s failed: %w", cmd[0], err)
			}
		}
		return nil
	}), nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return paths[len(paths)-1]
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

type FolderSize struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes uint64 `json:"bytes"`
}

// FolderSizes lists the biggest items directly inside a folder.
func FolderSizes(ctx context.Context, dir string) ([]FolderSize, error) {
	out, _ := runHost(ctx, 90*time.Second, "du", "-x", "-B1", "--max-depth=1", dir)
	var res []FolderSize
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 2)
		if len(f) != 2 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil || f[1] == dir {
			continue
		}
		res = append(res, FolderSize{Name: filepath.Base(f[1]), Path: f[1], Bytes: n})
	}
	sort.Slice(res, func(a, b int) bool { return res[a].Bytes > res[b].Bytes })
	if len(res) > 25 {
		res = res[:25]
	}
	return res, nil
}

func (s *Server) cleanupScan(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.cleaner.Scan(r.Context())})
}

func (s *Server) cleanupRun(w http.ResponseWriter, r *http.Request) {
	var req struct{ IDs []string }
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := s.cleaner.Run(req.IDs)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "cleanup %v", req.IDs)
	writeJSON(w, http.StatusAccepted, job.View(false))
}

func (s *Server) cleanupSizes(w http.ResponseWriter, r *http.Request) {
	real, err := s.filesFor(r).Resolve(r.URL.Query().Get("path"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, friendlyFSError(err))
		return
	}
	res, _ := FolderSizes(r.Context(), real)
	writeJSON(w, http.StatusOK, map[string]any{"path": real, "items": res})
}
