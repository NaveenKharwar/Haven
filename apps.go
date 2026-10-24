package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// Each app lives in <data>/apps/<id>/ as three plain files:
//   compose.yml  - a standard Docker Compose file
//   .env         - the settings you chose
//   haven.json   - a little metadata for the UI
// and its data lives in <data>/appdata/<id>/. You can always manage an app by
// hand with `docker compose` in that folder, even without Haven.

type AppMeta struct {
	ID          string    `json:"id"`
	CatalogID   string    `json:"catalogId,omitempty"`
	Name        string    `json:"name"`
	Icon        string    `json:"icon,omitempty"`
	Category    string    `json:"category,omitempty"`
	Version     string    `json:"version,omitempty"`
	Web         *WebSpec  `json:"web,omitempty"`
	Custom      bool      `json:"custom,omitempty"`
	InstalledAt time.Time `json:"installedAt"`
}

type InstalledApp struct {
	AppMeta
	Status          string      `json:"status"` // running, partial, starting, stopped, error
	Containers      []Container `json:"containers"`
	WebPort         string      `json:"webPort,omitempty"`
	Busy            string      `json:"busy,omitempty"`
	UpdateAvailable string      `json:"updateAvailable,omitempty"`
	Notes           string      `json:"notes,omitempty"`
	Usage           *AppUsage   `json:"usage,omitempty"`
}

type Check struct {
	Name    string `json:"name"`
	Level   string `json:"level"` // ok, warn, fail
	Message string `json:"message"`
}

type Manager struct {
	dir       string
	docker    *Docker
	cat       *Catalog
	jobs      *Jobs
	usage     *UsageMonitor   // optional; nil in tests
	onUpdated func(id string) // called after an update finishes
	mu        sync.Mutex
	busy      map[string]string
}

func NewManager(dir string, d *Docker, c *Catalog, j *Jobs) *Manager {
	return &Manager{dir: dir, docker: d, cat: c, jobs: j, busy: map[string]string{}}
}

func projectName(id string) string { return "haven-" + id }

func (m *Manager) appDir(id string) string  { return filepath.Join(m.dir, "apps", id) }
func (m *Manager) dataDir(id string) string { return filepath.Join(m.dir, "appdata", id) }

func (m *Manager) compose(id string, args ...string) []string {
	dir := m.appDir(id)
	base := []string{"compose", "--progress", "plain", "--project-name", projectName(id),
		"--project-directory", dir, "-f", filepath.Join(dir, "compose.yml"), "--env-file", filepath.Join(dir, ".env")}
	return append(base, args...)
}

func (m *Manager) lock(id, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b := m.busy[id]; b != "" {
		return fmt.Errorf("%s is busy (%s in progress)", id, b)
	}
	m.busy[id] = action
	return nil
}

func (m *Manager) unlock(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.busy, id)
}

func (m *Manager) busyFor(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[id]
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (m *Manager) readMeta(id string) (*AppMeta, error) {
	b, err := os.ReadFile(filepath.Join(m.appDir(id), "haven.json"))
	if err != nil {
		return nil, err
	}
	var meta AppMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, err
	}
	meta.ID = id
	return &meta, nil
}

func (m *Manager) writeMeta(meta *AppMeta) error {
	b, _ := json.MarshalIndent(meta, "", "  ")
	return writeFileAtomic(filepath.Join(m.appDir(meta.ID), "haven.json"), b, 0o644)
}

// --- .env files -----------------------------------------------------------

type envFile struct {
	keys []string
	vals map[string]string
}

func readEnv(path string) (*envFile, error) {
	e := &envFile{vals: map[string]string{}}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return e, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'' || v[0] == '"' && v[len(v)-1] == '"') {
			v = v[1 : len(v)-1]
		}
		e.set(strings.TrimSpace(k), v)
	}
	return e, sc.Err()
}

func (e *envFile) set(k, v string) {
	if _, ok := e.vals[k]; !ok {
		e.keys = append(e.keys, k)
	}
	e.vals[k] = v
}

func (e *envFile) write(path string) error {
	var b strings.Builder
	b.WriteString("# Managed by Haven. Single quotes keep values literal.\n")
	for _, k := range e.keys {
		fmt.Fprintf(&b, "%s='%s'\n", k, e.vals[k])
	}
	return writeFileAtomic(path, []byte(b.String()), 0o600)
}

// --- listing --------------------------------------------------------------

func (m *Manager) List(ctx context.Context) ([]InstalledApp, []Container, error) {
	entries, err := os.ReadDir(filepath.Join(m.dir, "apps"))
	if err != nil {
		return nil, nil, err
	}
	if m.usage != nil {
		m.usage.Wanted()
	}
	containers, cerr := m.docker.ListContainers(ctx)
	byProject := map[string][]Container{}
	for _, c := range containers {
		byProject[c.Project] = append(byProject[c.Project], c)
	}
	var apps []InstalledApp
	mine := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() || !appIDRe.MatchString(e.Name()) {
			continue
		}
		meta, err := m.readMeta(e.Name())
		if err != nil {
			continue
		}
		mine[projectName(meta.ID)] = true
		a := InstalledApp{AppMeta: *meta, Containers: byProject[projectName(meta.ID)], Busy: m.busyFor(meta.ID)}
		if a.Containers == nil {
			a.Containers = []Container{}
		}
		a.Status = appStatus(a.Containers)
		if cerr != nil {
			a.Status = "unknown"
		}
		if meta.Web != nil {
			if _, err := strconv.Atoi(meta.Web.Port); err == nil {
				a.WebPort = meta.Web.Port
			} else if env, err := readEnv(filepath.Join(m.appDir(meta.ID), ".env")); err == nil {
				a.WebPort = env.vals[meta.Web.Port]
			}
		}
		if ca, ok := m.cat.Get(meta.CatalogID); ok && !meta.Custom {
			if ca.Version != meta.Version {
				a.UpdateAvailable = ca.Version
			}
			a.Notes = ca.Notes
		}
		if m.usage != nil {
			m.usage.Attach(&a)
		}
		apps = append(apps, a)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })

	other := []Container{}
	for _, c := range containers {
		if !mine[c.Project] {
			if m.usage != nil {
				m.usage.AttachContainer(&c)
			}
			other = append(other, c)
		}
	}
	if apps == nil {
		apps = []InstalledApp{}
	}
	return apps, other, cerr
}

func appStatus(cs []Container) string {
	if len(cs) == 0 {
		return "stopped"
	}
	running, bad, starting := 0, 0, 0
	for _, c := range cs {
		switch c.State {
		case "running":
			running++
			if c.Health == "unhealthy" {
				bad++
			} else if c.Health == "starting" {
				starting++
			}
		case "restarting", "dead":
			bad++
		case "exited":
			if !strings.HasPrefix(c.Status, "Exited (0)") {
				bad++
			}
		}
	}
	switch {
	case bad > 0:
		return "error"
	case running == len(cs) && starting > 0:
		return "starting"
	case running == len(cs):
		return "running"
	case running > 0:
		return "partial"
	default:
		return "stopped"
	}
}

func (m *Manager) Get(id string) (*AppMeta, string, map[string]string, error) {
	if !appIDRe.MatchString(id) {
		return nil, "", nil, errors.New("invalid app id")
	}
	meta, err := m.readMeta(id)
	if err != nil {
		return nil, "", nil, errors.New("app not found")
	}
	compose, _ := os.ReadFile(filepath.Join(m.appDir(id), "compose.yml"))
	env, _ := readEnv(filepath.Join(m.appDir(id), ".env"))
	vals := map[string]string{}
	if env != nil {
		vals = env.vals
	}
	return meta, string(compose), vals, nil
}

// --- pre-install checks ---------------------------------------------------

func (m *Manager) Preflight(ctx context.Context, app *CatalogApp, id string, values map[string]string) []Check {
	var checks []Check
	add := func(name, level, msg string) { checks = append(checks, Check{name, level, msg}) }

	if v, err := m.docker.Version(ctx); err != nil {
		add("Docker", "fail", "Docker is not running or not reachable: "+err.Error())
	} else if cv, err := m.docker.ComposeVersion(ctx); err != nil {
		add("Docker", "fail", "Docker Compose plugin is missing. Install docker-compose-plugin.")
	} else {
		add("Docker", "ok", fmt.Sprintf("Docker %s, Compose %s", v, cv))
	}

	archOK := false
	for _, a := range app.Archs {
		if a == runtime.GOARCH {
			archOK = true
		}
	}
	if archOK {
		add("CPU type", "ok", runtime.GOARCH+" is supported")
	} else {
		add("CPU type", "fail", fmt.Sprintf("%s does not support %s (supports: %s)", app.Name, runtime.GOARCH, strings.Join(app.Archs, ", ")))
	}

	if !appIDRe.MatchString(id) {
		add("Name", "fail", "app name must be lowercase letters, numbers and dashes")
	} else if exists(m.appDir(id)) {
		add("Name", "fail", fmt.Sprintf("an app called %q is already installed", id))
	} else if exists(m.dataDir(id)) {
		add("Name", "warn", fmt.Sprintf("existing data in %s will be reused", m.dataDir(id)))
	} else {
		add("Name", "ok", id+" is available")
	}

	used := m.portsInUseByApps()
	for i := range app.Settings {
		s := &app.Settings[i]
		v := values[s.Key]
		if v == "" {
			v = s.Default
		}
		if v == "" {
			if s.Required && !s.Generate {
				add(s.Label, "fail", "is required")
			}
			continue
		}
		if err := validateValue(s, v); err != nil {
			add(s.Label, "fail", err.Error())
			continue
		}
		switch s.Type {
		case "port":
			port, _ := strconv.Atoi(v)
			protos := s.Protocols
			if len(protos) == 0 {
				protos = []string{"tcp"}
			}
			if owner, ok := used[port]; ok {
				add(s.Label, "fail", fmt.Sprintf("port %d is already assigned to %s", port, owner))
				continue
			}
			free := true
			for _, p := range protos {
				if !portFree(port, p) {
					free = false
					hint := ""
					if port == 53 {
						hint = " On Ubuntu, systemd-resolved uses port 53; see this app's notes."
					}
					add(s.Label, "fail", fmt.Sprintf("port %d/%s is already in use by another program.%s", port, p, hint))
				}
			}
			if free {
				add(s.Label, "ok", fmt.Sprintf("port %d is free", port))
			}
		case "path":
			if exists(v) {
				add(s.Label, "ok", v+" exists")
			} else {
				add(s.Label, "ok", v+" will be created")
			}
		}
	}

	for _, hp := range app.HostPorts {
		port, proto, err := parseHostPort(hp)
		if err != nil {
			continue
		}
		if owner, ok := used[port]; ok {
			add("Port "+hp, "fail", fmt.Sprintf("port %d is needed by %s and also used by %s", port, app.Name, owner))
		} else if !portFree(port, proto) {
			add("Port "+hp, "fail", fmt.Sprintf("%s needs port %s, which is already in use", app.Name, hp))
		} else {
			add("Port "+hp, "ok", "free")
		}
	}

	if u, err := disk.Usage(m.dir); err == nil {
		gb := float64(u.Free) / (1 << 30)
		if gb < 1 {
			add("Disk space", "fail", fmt.Sprintf("only %.1f GB free in %s", gb, m.dir))
		} else if gb < 5 {
			add("Disk space", "warn", fmt.Sprintf("only %.1f GB free in %s", gb, m.dir))
		} else {
			add("Disk space", "ok", fmt.Sprintf("%.0f GB free", gb))
		}
	}
	if app.MinRAMMB > 0 {
		if vm, err := mem.VirtualMemory(); err == nil {
			totalMB := int(vm.Total / (1 << 20))
			if totalMB < app.MinRAMMB {
				add("Memory", "warn", fmt.Sprintf("%s recommends %d MB RAM; this server has %d MB", app.Name, app.MinRAMMB, totalMB))
			} else {
				add("Memory", "ok", fmt.Sprintf("%d MB RAM available (needs %d MB)", totalMB, app.MinRAMMB))
			}
		}
	}
	return checks
}

func portFree(port int, proto string) bool {
	addr := fmt.Sprintf(":%d", port)
	if proto == "udp" {
		c, err := net.ListenPacket("udp", addr)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// portsInUseByApps finds ports claimed by installed apps, including stopped
// ones, so two apps never get the same port.
func (m *Manager) portsInUseByApps() map[int]string {
	out := map[int]string{}
	entries, _ := os.ReadDir(filepath.Join(m.dir, "apps"))
	for _, e := range entries {
		meta, err := m.readMeta(e.Name())
		if err != nil {
			continue
		}
		ca, ok := m.cat.Get(meta.CatalogID)
		if !ok {
			continue
		}
		env, err := readEnv(filepath.Join(m.appDir(meta.ID), ".env"))
		if err != nil {
			continue
		}
		for _, s := range ca.Settings {
			if s.Type == "port" {
				if p, err := strconv.Atoi(env.vals[s.Key]); err == nil {
					out[p] = meta.Name
				}
			}
		}
		for _, hp := range ca.HostPorts {
			if p, _, err := parseHostPort(hp); err == nil {
				out[p] = meta.Name
			}
		}
	}
	return out
}

func hasFail(checks []Check) error {
	var msgs []string
	for _, c := range checks {
		if c.Level == "fail" {
			msgs = append(msgs, c.Name+": "+c.Message)
		}
	}
	if len(msgs) > 0 {
		return errors.New(strings.Join(msgs, "; "))
	}
	return nil
}

func hostTZ() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(b)); tz != "" {
			return tz
		}
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	return "UTC"
}

// owner IDs used for app files. 1000 is the first normal user on Ubuntu.
func hostIDs() (string, string) {
	uid, gid := "1000", "1000"
	if v := os.Getenv("HAVEN_PUID"); v != "" {
		uid = v
	}
	if v := os.Getenv("HAVEN_PGID"); v != "" {
		gid = v
	}
	return uid, gid
}

// mkdirOwned creates a folder and, if Haven created it, gives it to the app
// user. This prevents the classic "permission denied" on first start.
func mkdirOwned(p string) error {
	if exists(p) {
		return nil
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	uid, gid := hostIDs()
	u, _ := strconv.Atoi(uid)
	g, _ := strconv.Atoi(gid)
	_ = os.Chown(p, u, g) // only works as root; harmless otherwise
	return nil
}

// --- install --------------------------------------------------------------

// pullFailure explains why an image download failed. Docker Hub limits how
// many images an anonymous server can download in a few hours, and the plain
// "check your internet connection" would send people the wrong way.
func pullFailure(j *Job, fallback string) error {
	log := strings.ToLower(strings.Join(j.View(true).Log, "\n"))
	switch {
	case strings.Contains(log, "429") || strings.Contains(log, "toomanyrequests") || strings.Contains(log, "rate limit"):
		return errors.New("Docker Hub is limiting downloads from this server (too many in the last few hours). Wait a few hours and try again, or sign this server in to a free Docker Hub account to raise the limit: run `docker login` on the server")
	case strings.Contains(log, "no space left on device"):
		return errors.New("the disk is full, so the images can't be downloaded. Free up some space and try again")
	case strings.Contains(log, "denied") || strings.Contains(log, "unauthorized") || strings.Contains(log, "pull access denied"):
		return errors.New("the registry refused the download. The image may be private or no longer exist")
	case strings.Contains(log, "no matching manifest") || strings.Contains(log, "exec format"):
		return errors.New("this app has no version for this server's CPU type")
	case strings.Contains(log, "lookup") || strings.Contains(log, "no such host") || strings.Contains(log, "timeout") || strings.Contains(log, "dial tcp"):
		return errors.New("could not reach the image registry; check the server's internet connection and DNS")
	}
	return errors.New(fallback)
}

type InstallError struct {
	Checks []Check
	Err    error
}

func (e *InstallError) Error() string { return e.Err.Error() }

func (m *Manager) Install(ctx context.Context, catalogID, id string, values map[string]string) (*Job, error) {
	app, ok := m.cat.Get(catalogID)
	if !ok {
		return nil, errors.New("unknown app")
	}
	if id == "" {
		id = app.ID
	}
	checks := m.Preflight(ctx, app, id, values)
	if err := hasFail(checks); err != nil {
		return nil, &InstallError{Checks: checks, Err: err}
	}
	if err := m.lock(id, "install"); err != nil {
		return nil, err
	}

	env := &envFile{vals: map[string]string{}}
	uid, gid := hostIDs()
	env.set("APP_DATA", m.dataDir(id))
	env.set("PUID", uid)
	env.set("PGID", gid)
	env.set("TZ", hostTZ())
	var paths []string
	for _, s := range app.Settings {
		v := values[s.Key]
		if v == "" {
			v = s.Default
		}
		if v == "" && s.Generate {
			v = randomPassword(40)
		}
		env.set(s.Key, v)
		if s.Type == "path" && v != "" {
			paths = append(paths, v)
		}
	}

	return m.jobs.Start(id, "install", 45*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		dataExisted := exists(m.dataDir(id))
		cleanup := func() {
			j.Logf("Cleaning up after failed install")
			_ = m.docker.stream(context.Background(), j, m.compose(id, "down", "--remove-orphans")...)
			_ = os.RemoveAll(m.appDir(id))
			if !dataExisted {
				_ = os.RemoveAll(m.dataDir(id))
			}
		}

		j.Logf("Preparing %s", app.Name)
		if err := os.MkdirAll(m.appDir(id), 0o755); err != nil {
			return err
		}
		if err := mkdirOwned(m.dataDir(id)); err != nil {
			cleanup()
			return err
		}
		for _, sub := range appDataSubdirs(app.Compose) {
			paths = append(paths, filepath.Join(m.dataDir(id), sub))
		}
		for _, p := range paths {
			if err := mkdirOwned(p); err != nil {
				cleanup()
				return fmt.Errorf("cannot create %s: %w", p, err)
			}
		}
		if err := writeFileAtomic(filepath.Join(m.appDir(id), "compose.yml"), []byte(app.Compose), 0o644); err != nil {
			cleanup()
			return err
		}
		if err := env.write(filepath.Join(m.appDir(id), ".env")); err != nil {
			cleanup()
			return err
		}
		meta := &AppMeta{ID: id, CatalogID: app.ID, Name: app.Name, Icon: app.Icon, Category: app.Category,
			Version: app.Version, Web: app.Web, InstalledAt: time.Now()}
		if err := m.writeMeta(meta); err != nil {
			cleanup()
			return err
		}

		j.Logf("Checking configuration")
		if err := m.docker.stream(ctx, j, m.compose(id, "config", "--quiet")...); err != nil {
			cleanup()
			return errors.New("the app configuration is invalid")
		}
		j.Logf("Downloading images (this can take a few minutes)")
		if err := m.docker.stream(ctx, j, m.compose(id, "pull")...); err != nil {
			cleanup()
			return pullFailure(j, "could not download the app images")
		}
		j.Logf("Starting %s", app.Name)
		if err := m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...); err != nil {
			m.dumpLogs(j, id)
			cleanup()
			return errors.New("the app failed to start")
		}
		m.usageChanged(id, false)
		if err := m.waitHealthy(ctx, j, id, startTimeout(app)); err != nil {
			m.dumpLogs(j, id)
			return fmt.Errorf("%w. The app was kept so you can read its logs, change settings, or uninstall it", err)
		}
		j.Logf("%s is running", app.Name)
		return nil
	}), nil
}

func startTimeout(app *CatalogApp) time.Duration {
	if app != nil && app.StartTimeout > 0 {
		return time.Duration(app.StartTimeout) * time.Second
	}
	return 120 * time.Second
}

// InstallCustom installs a user-supplied compose file.
func (m *Manager) InstallCustom(ctx context.Context, id, name, compose, webPort string) (*Job, error) {
	if !appIDRe.MatchString(id) {
		return nil, errors.New("app id must be lowercase letters, numbers and dashes (max 31)")
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	if len(compose) > 256*1024 || strings.TrimSpace(compose) == "" {
		return nil, errors.New("compose file is empty or too large")
	}
	if webPort != "" {
		if p, err := strconv.Atoi(webPort); err != nil || p < 1 || p > 65535 {
			return nil, errors.New("web port must be a number between 1 and 65535")
		}
	}
	if exists(m.appDir(id)) {
		return nil, fmt.Errorf("an app called %q is already installed", id)
	}
	if err := m.lock(id, "install"); err != nil {
		return nil, err
	}
	fail := func(err error) (*Job, error) {
		_ = os.RemoveAll(m.appDir(id))
		m.unlock(id)
		return nil, err
	}
	if err := os.MkdirAll(m.appDir(id), 0o755); err != nil {
		return fail(err)
	}
	if err := writeFileAtomic(filepath.Join(m.appDir(id), "compose.yml"), []byte(compose), 0o644); err != nil {
		return fail(err)
	}
	env := &envFile{vals: map[string]string{}}
	uid, gid := hostIDs()
	env.set("APP_DATA", m.dataDir(id))
	env.set("PUID", uid)
	env.set("PGID", gid)
	env.set("TZ", hostTZ())
	if err := env.write(filepath.Join(m.appDir(id), ".env")); err != nil {
		return fail(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := m.docker.run(cctx, m.compose(id, "config", "--quiet")...); err != nil {
		return fail(fmt.Errorf("compose file is invalid: %v", err))
	}
	meta := &AppMeta{ID: id, Name: name, Custom: true, Category: "Custom", InstalledAt: time.Now()}
	if webPort != "" {
		meta.Web = &WebSpec{Port: webPort}
	}
	if err := m.writeMeta(meta); err != nil {
		return fail(err)
	}
	return m.jobs.Start(id, "install", 45*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		if err := mkdirOwned(m.dataDir(id)); err != nil {
			return err
		}
		j.Logf("Downloading images")
		if err := m.docker.stream(ctx, j, m.compose(id, "pull", "--ignore-buildable")...); err != nil {
			return pullFailure(j, "could not download images")
		}
		j.Logf("Starting %s", name)
		if err := m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...); err != nil {
			m.dumpLogs(j, id)
			return errors.New("the app failed to start")
		}
		m.usageChanged(id, false)
		if err := m.waitHealthy(ctx, j, id, 120*time.Second); err != nil {
			m.dumpLogs(j, id)
			return err
		}
		return nil
	}), nil
}

// waitHealthy waits until every container is running (and healthy, if it
// has a health check) for several checks in a row. This catches apps that
// start and then crash a few seconds later.
func (m *Manager) waitHealthy(ctx context.Context, j *Job, id string, timeout time.Duration) error {
	j.Logf("Waiting for the app to become healthy (up to %s)", timeout)
	deadline := time.Now().Add(timeout)
	stable := 0
	seenRestart := map[string]int{}
	for {
		cs, err := m.docker.ListContainers(ctx)
		if err != nil {
			return err
		}
		var mine []Container
		for _, c := range cs {
			if c.Project == projectName(id) {
				mine = append(mine, c)
			}
		}
		allUp := len(mine) > 0
		startingOnly := allUp
		for _, c := range mine {
			switch c.State {
			case "running":
				if c.Health == "unhealthy" {
					allUp, startingOnly = false, false
				} else if c.Health == "starting" {
					allUp = false
				}
			case "restarting":
				seenRestart[c.Name]++
				allUp, startingOnly = false, false
				if seenRestart[c.Name] >= 3 {
					return fmt.Errorf("container %s keeps crashing and restarting", c.Name)
				}
			case "exited", "dead":
				if strings.HasPrefix(c.Status, "Exited (0)") {
					continue // one-off setup containers finish with code 0
				}
				return fmt.Errorf("container %s stopped: %s", c.Name, c.Status)
			default:
				allUp, startingOnly = false, false
			}
		}
		if allUp {
			stable++
			if stable >= 3 {
				return nil
			}
		} else {
			stable = 0
		}
		if time.Now().After(deadline) {
			if startingOnly {
				j.Logf("The app is running but still finishing its first start; it should be ready shortly")
				return nil
			}
			return errors.New("the app did not start in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *Manager) dumpLogs(j *Job, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	j.Logf("Recent app logs:")
	_ = m.docker.stream(ctx, j, m.compose(id, "logs", "--tail", "40", "--no-color")...)
}

func (m *Manager) usageChanged(id string, removed bool) {
	if m.usage != nil {
		m.usage.Changed(id, removed)
	}
}

// --- lifecycle ------------------------------------------------------------

func (m *Manager) Action(id, action string) (*Job, error) {
	if !appIDRe.MatchString(id) || !exists(filepath.Join(m.appDir(id), "haven.json")) {
		return nil, errors.New("app not found")
	}
	var args []string
	switch action {
	case "start":
		args = []string{"up", "-d", "--remove-orphans"}
	case "stop":
		args = []string{"stop"}
	case "restart":
		args = []string{"restart"}
	default:
		return nil, errors.New("unknown action")
	}
	if err := m.lock(id, action); err != nil {
		return nil, err
	}
	return m.jobs.Start(id, action, 10*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		if err := m.docker.stream(ctx, j, m.compose(id, args...)...); err != nil {
			return fmt.Errorf("%s failed", action)
		}
		if action != "stop" {
			meta, _ := m.readMeta(id)
			var ca *CatalogApp
			if meta != nil {
				ca, _ = m.cat.Get(meta.CatalogID)
			}
			if err := m.waitHealthy(ctx, j, id, startTimeout(ca)); err != nil {
				m.dumpLogs(j, id)
				return err
			}
		}
		return nil
	}), nil
}

func (m *Manager) Uninstall(id string, removeData bool) (*Job, error) {
	if !appIDRe.MatchString(id) || !exists(m.appDir(id)) {
		return nil, errors.New("app not found")
	}
	if err := m.lock(id, "uninstall"); err != nil {
		return nil, err
	}
	return m.jobs.Start(id, "uninstall", 10*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		args := []string{"down", "--remove-orphans"}
		if removeData {
			args = append(args, "--volumes")
		}
		j.Logf("Stopping and removing containers")
		if err := m.docker.stream(ctx, j, m.compose(id, args...)...); err != nil {
			// Fall back to removing by label in case the compose file is broken.
			j.Logf("compose down failed, removing containers directly")
			out, _ := m.docker.run(ctx, "ps", "-aq", "--filter", "label=com.docker.compose.project="+projectName(id))
			if ids := strings.Fields(string(out)); len(ids) > 0 {
				if err := m.docker.stream(ctx, j, append([]string{"rm", "-f"}, ids...)...); err != nil {
					return errors.New("could not remove containers")
				}
			}
		}
		if err := os.RemoveAll(m.appDir(id)); err != nil {
			return err
		}
		if removeData {
			j.Logf("Deleting app data in %s", m.dataDir(id))
			if err := os.RemoveAll(m.dataDir(id)); err != nil {
				return err
			}
		} else {
			j.Logf("App data kept in %s (reinstalling will reuse it)", m.dataDir(id))
		}
		m.usageChanged(id, true)
		return nil
	}), nil
}

// Update moves an app to the catalog's current version (or re-pulls its
// images). If the new version doesn't come up healthy, it rolls back to the
// exact previous images and settings.
func (m *Manager) Update(id string) (*Job, error) {
	meta, err := m.readMeta(id)
	if err != nil || !appIDRe.MatchString(id) {
		return nil, errors.New("app not found")
	}
	var app *CatalogApp
	if !meta.Custom {
		a, ok := m.cat.Get(meta.CatalogID)
		if !ok {
			return nil, errors.New("this app is no longer in the catalog")
		}
		app = a
	}
	if err := m.lock(id, "update"); err != nil {
		return nil, err
	}
	return m.jobs.Start(id, "update", 45*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		err := m.runUpdate(ctx, j, id, meta, app)
		if m.onUpdated != nil {
			m.onUpdated(id)
		}
		return err
	}), nil
}

// runUpdate does one app's update with automatic rollback. The caller holds
// the app's lock.
func (m *Manager) runUpdate(ctx context.Context, j *Job, id string, meta *AppMeta, app *CatalogApp) error {
	dir := m.appDir(id)
	composePath, envPath := filepath.Join(dir, "compose.yml"), filepath.Join(dir, ".env")
	oldCompose, err := os.ReadFile(composePath)
	if err != nil {
		return err
	}
	oldEnv, err := os.ReadFile(envPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.WriteFile(composePath+".bak", oldCompose, 0o644)
	_ = os.WriteFile(envPath+".bak", oldEnv, 0o600)

	j.Logf("Recording current versions for rollback")
	oldImages, err := m.docker.ImageIDs(ctx, projectName(id))
	if err != nil {
		return err
	}
	restoreFiles := func() {
		_ = writeFileAtomic(composePath, oldCompose, 0o644)
		_ = writeFileAtomic(envPath, oldEnv, 0o600)
	}

	if app != nil {
		env, err := readEnv(envPath)
		if err != nil {
			return err
		}
		for _, s := range app.Settings { // add any settings new in this version
			if _, ok := env.vals[s.Key]; !ok {
				v := s.Default
				if v == "" && s.Generate {
					v = randomPassword(40)
				}
				env.set(s.Key, v)
			}
		}
		if err := env.write(envPath); err != nil {
			return err
		}
		if err := writeFileAtomic(composePath, []byte(app.Compose), 0o644); err != nil {
			restoreFiles()
			return err
		}
		for _, sub := range appDataSubdirs(app.Compose) {
			_ = mkdirOwned(filepath.Join(m.dataDir(id), sub))
		}
	}

	j.Logf("Downloading new images")
	if err := m.docker.stream(ctx, j, m.compose(id, "pull", "--ignore-buildable")...); err != nil {
		restoreFiles()
		return pullFailure(j, "could not download new images") // nothing was changed
	}
	j.Logf("Restarting with the new version")
	upErr := m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...)
	if upErr == nil {
		upErr = m.waitHealthy(ctx, j, id, startTimeout(app))
	}
	m.usageChanged(id, false)
	if upErr == nil {
		if app != nil {
			meta.Version = app.Version
			meta.Icon, meta.Web, meta.Name = app.Icon, app.Web, app.Name
			_ = m.writeMeta(meta)
		}
		return nil
	}

	m.dumpLogs(j, id)
	j.Logf("Update failed (%v). Rolling back to the previous version", upErr)
	restoreFiles()
	for ref, imgID := range oldImages {
		_ = m.docker.stream(ctx, j, "tag", imgID, ref)
	}
	if err := m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...); err != nil {
		return errors.New("update failed and rollback could not start the app; check the logs")
	}
	if err := m.waitHealthy(ctx, j, id, startTimeout(app)); err != nil {
		return errors.New("update failed; rolled back, but the app is not healthy yet")
	}
	return errors.New("update failed; the previous version was restored and is running")
}

// Reconfigure changes an installed catalog app's settings and restarts it,
// restoring the old settings if the app fails to come back.
func (m *Manager) Reconfigure(ctx context.Context, id string, values map[string]string) (*Job, error) {
	meta, err := m.readMeta(id)
	if err != nil || !appIDRe.MatchString(id) {
		return nil, errors.New("app not found")
	}
	app, ok := m.cat.Get(meta.CatalogID)
	if !ok || meta.Custom {
		return nil, errors.New("only store apps can be reconfigured here")
	}
	envPath := filepath.Join(m.appDir(id), ".env")
	env, err := readEnv(envPath)
	if err != nil {
		return nil, err
	}
	oldEnv, _ := os.ReadFile(envPath)
	var errs []string
	var newPaths []string
	others := m.portsInUseByApps()
	for i := range app.Settings {
		s := &app.Settings[i]
		v, ok := values[s.Key]
		if !ok || v == env.vals[s.Key] {
			continue
		}
		if v == "" && s.Required {
			errs = append(errs, s.Label+" is required")
			continue
		}
		if v != "" {
			if err := validateValue(s, v); err != nil {
				errs = append(errs, s.Label+": "+err.Error())
				continue
			}
		}
		if s.Type == "port" {
			p, _ := strconv.Atoi(v)
			if owner, used := others[p]; used && owner != meta.Name {
				errs = append(errs, fmt.Sprintf("%s: port %d is assigned to %s", s.Label, p, owner))
				continue
			}
			if !portFree(p, "tcp") {
				errs = append(errs, fmt.Sprintf("%s: port %d is in use", s.Label, p))
				continue
			}
		}
		if s.Type == "path" && v != "" {
			newPaths = append(newPaths, v)
		}
		env.set(s.Key, v)
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	if err := m.lock(id, "reconfigure"); err != nil {
		return nil, err
	}
	return m.jobs.Start(id, "reconfigure", 15*time.Minute, func(ctx context.Context, j *Job) error {
		defer m.unlock(id)
		for _, p := range newPaths {
			if err := mkdirOwned(p); err != nil {
				return err
			}
		}
		if err := env.write(envPath); err != nil {
			return err
		}
		j.Logf("Applying new settings")
		err := m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...)
		if err == nil {
			err = m.waitHealthy(ctx, j, id, startTimeout(app))
		}
		if err == nil {
			return nil
		}
		m.dumpLogs(j, id)
		j.Logf("New settings failed; restoring previous settings")
		_ = writeFileAtomic(envPath, oldEnv, 0o600)
		_ = m.docker.stream(ctx, j, m.compose(id, "up", "-d", "--remove-orphans")...)
		return errors.New("the new settings did not work, so the previous settings were restored")
	}), nil
}

func (m *Manager) Logs(ctx context.Context, id string, tail int) (string, error) {
	if !appIDRe.MatchString(id) || !exists(m.appDir(id)) {
		return "", errors.New("app not found")
	}
	out, err := m.docker.run(ctx, m.compose(id, "logs", "--tail", strconv.Itoa(tail), "--no-color", "--timestamps")...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
