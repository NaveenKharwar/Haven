package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// Diagnosis: run many small checks on an app (or the whole server), then
// explain in plain words what is most likely wrong and how to fix it.

type Finding struct {
	Level    string   `json:"level"` // ok, info, warn, bad
	Area     string   `json:"area"`  // Containers, Network, Storage, Logs, Server...
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Fix      string   `json:"fix,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type Diagnosis struct {
	Target   string    `json:"target"`
	Level    string    `json:"level"` // worst level found
	Verdict  string    `json:"verdict"`
	Cause    *Finding  `json:"cause,omitempty"` // the most likely cause
	Findings []Finding `json:"findings"`
	Report   string    `json:"report"` // plain text, secrets removed, for copying
	RanAt    time.Time `json:"ranAt"`
}

var levelRank = map[string]int{"ok": 0, "info": 1, "warn": 2, "bad": 3}

// --- docker inspect -------------------------------------------------------

type inspectState struct {
	Status     string `json:"Status"`
	Running    bool   `json:"Running"`
	Restarting bool   `json:"Restarting"`
	OOMKilled  bool   `json:"OOMKilled"`
	ExitCode   int    `json:"ExitCode"`
	Error      string `json:"Error"`
	StartedAt  string `json:"StartedAt"`
	Health     *struct {
		Status        string `json:"Status"`
		FailingStreak int    `json:"FailingStreak"`
		Log           []struct {
			ExitCode int    `json:"ExitCode"`
			Output   string `json:"Output"`
		} `json:"Log"`
	} `json:"Health"`
}

type inspectContainer struct {
	Name         string       `json:"Name"`
	Image        string       `json:"Image"`
	RestartCount int          `json:"RestartCount"`
	State        inspectState `json:"State"`
	Config       struct {
		Image  string            `json:"Image"`
		User   string            `json:"User"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
}

// exitMeaning explains common container exit codes.
func exitMeaning(code int, oom bool) string {
	switch {
	case oom:
		return "it ran out of memory and was killed"
	case code == 0:
		return "it finished normally"
	case code == 1:
		return "the app stopped with an error (see its logs)"
	case code == 125:
		return "Docker couldn't start it"
	case code == 126:
		return "its start command couldn't be run (permission problem)"
	case code == 127:
		return "its start command wasn't found inside the image"
	case code == 137:
		return "it was killed, usually for using too much memory"
	case code == 139:
		return "it crashed (segmentation fault)"
	case code == 143:
		return "it was asked to stop"
	}
	return fmt.Sprintf("it stopped with code %d", code)
}

// --- log patterns ---------------------------------------------------------

type logPattern struct {
	re    *regexp.Regexp
	level string
	title string
	fix   string
}

var logPatterns = []logPattern{
	{regexp.MustCompile(`(?i)permission denied|operation not permitted|\bEACCES\b`), "bad",
		"The app can't read or write some of its files (permission denied)",
		"Its folders are owned by a different user than the one the app runs as. Check the folder owners below. For a folder only this app uses, run: sudo chown -R 1000:1000 <folder>"},
	{regexp.MustCompile(`(?i)address already in use|EADDRINUSE|port is already allocated`), "bad",
		"A port the app needs is already in use",
		"Another app or program uses the same port. Change this app's port in its Settings, or stop the other program."},
	{regexp.MustCompile(`(?i)no space left on device|ENOSPC`), "bad",
		"The disk is full",
		"Free up space: remove apps you don't use, delete old files, or move media to a bigger disk."},
	{regexp.MustCompile(`(?i)out of memory|cannot allocate memory|oom-?kill`), "bad",
		"The app ran out of memory",
		"Stop apps you don't need, or add swap space to the server."},
	{regexp.MustCompile(`(?i)exec format error`), "bad",
		"The app's image was made for a different type of CPU",
		"Use an image built for this server's CPU (" + runtime.GOARCH + ")."},
	{regexp.MustCompile(`(?i)temporary failure in name resolution|could not resolve host|no such host|ENOTFOUND|EAI_AGAIN|name or service not known`), "warn",
		"The app can't look up internet addresses (DNS)",
		"Check the server's internet connection. If Pi-hole or AdGuard runs on this server and the server uses it for DNS, make sure it is running."},
	{regexp.MustCompile(`(?i)connection refused|ECONNREFUSED`), "warn",
		"The app can't reach something it depends on",
		"Often its database or another of its containers isn't ready yet. Restart the app; if it repeats, look at the other containers' logs."},
	{regexp.MustCompile(`(?i)database is locked|database disk image is malformed|database .*corrupt`), "bad",
		"The app's database looks locked or damaged",
		"Stop the app and back up its data folder before anything else. Then start it again; if the error stays, restore the database from a backup."},
	{regexp.MustCompile(`(?i)not a trusted domain|trusted_domains|untrusted domain`), "warn",
		"The app refuses the address you used to open it",
		"Add that address (for example haven.local, or the server's IP) to the app's trusted addresses in its Settings tab."},
	{regexp.MustCompile(`(?i)read-only file system`), "bad",
		"The app tried to write to a read-only location",
		"A folder is mounted read-only, or the disk had errors and Linux switched it to read-only. Check the disk."},
	{regexp.MustCompile(`(?i)too many open files|EMFILE`), "warn",
		"The app hit the open-files limit",
		"Restart the app. If it keeps happening, it needs a higher file limit."},
	{regexp.MustCompile(`(?i)x509:|certificate (verify failed|has expired|is not valid)|ssl handshake`), "warn",
		"Secure-connection (certificate) errors",
		"Check that the server's date and time are correct, and any https addresses in the app's settings."},
}

var errorLineRe = regexp.MustCompile(`(?i)\b(error|fatal|panic|exception|failed)\b`)

func analyzeLogs(logs string) []Finding {
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	var out []Finding
	for _, p := range logPatterns {
		var ev []string
		for _, l := range lines {
			if p.re.MatchString(l) {
				ev = append(ev, trimLine(l))
			}
		}
		if len(ev) > 0 {
			if len(ev) > 3 {
				ev = ev[len(ev)-3:]
			}
			out = append(out, Finding{Level: p.level, Area: "Logs", Title: p.title, Fix: p.fix, Evidence: ev})
		}
	}
	var errs []string
	for _, l := range lines {
		if errorLineRe.MatchString(l) {
			errs = append(errs, trimLine(l))
		}
	}
	if len(errs) > 0 {
		ev := errs
		if len(ev) > 4 {
			ev = ev[len(ev)-4:]
		}
		out = append(out, Finding{Level: "info", Area: "Logs",
			Title:  fmt.Sprintf("%d lines in the recent logs mention errors", len(errs)),
			Detail: "Not every error line is a problem; many apps log harmless errors. The latest ones are below.", Evidence: ev})
	} else if len(out) == 0 && len(lines) > 0 && lines[0] != "" {
		out = append(out, Finding{Level: "ok", Area: "Logs", Title: "No errors in the recent logs"})
	}
	return out
}

func trimLine(l string) string {
	l = strings.TrimSpace(l)
	if len(l) > 300 {
		l = l[:300] + "…"
	}
	return l
}

// --- app diagnosis --------------------------------------------------------

func (m *Manager) Diagnose(ctx context.Context, id string) (*Diagnosis, error) {
	meta, _, env, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	d := &Diagnosis{Target: meta.Name, RanAt: time.Now()}
	add := func(f Finding) { d.Findings = append(d.Findings, f) }

	if v, err := m.docker.Version(ctx); err != nil {
		add(Finding{Level: "bad", Area: "Server", Title: "Docker isn't running", Detail: err.Error(),
			Fix: "Start Docker: sudo systemctl start docker"})
		return finish(d, env), nil
	} else {
		add(Finding{Level: "ok", Area: "Server", Title: "Docker is running (" + v + ")"})
	}

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	if _, err := m.docker.run(cctx, m.compose(id, "config", "--quiet")...); err != nil {
		add(Finding{Level: "bad", Area: "Setup", Title: "The app's Compose file has a mistake", Detail: lastLines(err.Error(), 3),
			Fix: "Open the Compose tab to check it. For store apps, reinstalling restores a working file."})
	}
	cancel()

	all, _ := m.docker.ListContainers(ctx)
	var ids []string
	for _, c := range all {
		if c.Project == projectName(id) {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		add(Finding{Level: "bad", Area: "Containers", Title: "The app has no containers",
			Fix: "Press Start. Haven will create them from the Compose file."})
		return finish(d, env), nil
	}

	out, err := m.docker.run(ctx, append([]string{"inspect"}, ids...)...)
	var cs []inspectContainer
	if err == nil {
		_ = json.Unmarshal(out, &cs)
	}
	running := 0
	images := map[string]bool{}
	for _, c := range cs {
		name := strings.TrimPrefix(c.Name, "/")
		images[c.Image] = true
		st := c.State
		started, _ := time.Parse(time.RFC3339Nano, st.StartedAt)
		crashLoop := st.Restarting || (c.RestartCount >= 3 && time.Since(started) < 5*time.Minute)
		switch {
		case st.Running && !crashLoop:
			running++
		case crashLoop:
			add(Finding{Level: "bad", Area: "Containers", Title: name + " keeps crashing",
				Detail: fmt.Sprintf("Restarted %d times. Last time %s.", c.RestartCount, exitMeaning(st.ExitCode, st.OOMKilled)),
				Fix:    "The logs below usually show why. Fix that, then restart the app."})
		case st.Status == "exited" && st.ExitCode == 0:
			add(Finding{Level: "info", Area: "Containers", Title: name + " has finished", Detail: "It exited normally; some containers only run once at setup."})
		default:
			add(Finding{Level: "bad", Area: "Containers", Title: name + " is not running",
				Detail: "Status: " + st.Status + ". Last time " + exitMeaning(st.ExitCode, st.OOMKilled) + ".",
				Fix:    "Press Start. If it stops again, check the findings below."})
		}
		if st.OOMKilled {
			add(Finding{Level: "bad", Area: "Memory", Title: name + " was killed for using too much memory",
				Fix: "Stop apps you don't need, add swap, or give the server more RAM."})
		}
		if st.Error != "" {
			f := Finding{Level: "bad", Area: "Containers", Title: "Docker couldn't start " + name, Evidence: []string{st.Error}}
			if strings.Contains(st.Error, "port is already allocated") || strings.Contains(st.Error, "address already in use") {
				f.Title = "A port " + name + " needs is taken"
				f.Fix = "Change this app's port in Settings, or stop whatever else uses it."
				if p := portInError(st.Error); p > 0 {
					if who := portOwner(ctx, p); who != "" {
						f.Detail = fmt.Sprintf("Port %d is used by: %s", p, who)
					}
				}
			} else if strings.Contains(st.Error, "no such file or directory") || strings.Contains(st.Error, "not a directory") {
				f.Fix = "A folder or file the app mounts is missing. Check the folders in its Settings."
			}
			add(f)
		}
		if h := st.Health; h != nil && h.Status == "unhealthy" {
			f := Finding{Level: "bad", Area: "Health", Title: name + " is failing its health check",
				Detail: fmt.Sprintf("Failed %d checks in a row.", h.FailingStreak),
				Fix:    "The app is up but not working properly. Its logs usually explain why."}
			if n := len(h.Log); n > 0 {
				f.Evidence = []string{trimLine(h.Log[n-1].Output)}
			}
			add(f)
		} else if h != nil && h.Status == "starting" {
			add(Finding{Level: "info", Area: "Health", Title: name + " is still starting up", Detail: "Give it a minute and run the diagnosis again."})
		}
	}
	if running == len(cs) && len(cs) > 0 {
		add(Finding{Level: "ok", Area: "Containers", Title: fmt.Sprintf("All %d containers are running", len(cs))})
	}

	// CPU type of each image.
	for img := range images {
		arch, err := m.docker.run(ctx, "image", "inspect", "--format", "{{.Architecture}}", img)
		if a := strings.TrimSpace(string(arch)); err == nil && a != "" && a != runtime.GOARCH {
			add(Finding{Level: "bad", Area: "Image", Title: "An image is built for " + a + ", but this server is " + runtime.GOARCH,
				Fix: "It will be slow or fail to start. Choose an image made for " + runtime.GOARCH + "."})
		}
	}

	// Can the web page be reached?
	if meta.Web != nil {
		port := meta.Web.Port
		if _, err := strconv.Atoi(port); err != nil {
			port = env[port]
		}
		if p, err := strconv.Atoi(port); err == nil {
			add(checkWeb(ctx, meta.Web, p, running > 0))
		}
	}

	// Folders: missing sources, and owners (only if logs mention permissions).
	logs, _ := m.Logs(ctx, id, 300)
	logFindings := analyzeLogs(logs)
	permIssue := false
	for _, f := range logFindings {
		if strings.Contains(f.Title, "permission denied") {
			permIssue = true
		}
	}
	var owners []string
	for _, c := range cs {
		for _, mnt := range c.Mounts {
			if mnt.Type != "bind" || strings.HasPrefix(mnt.Source, "/var/run") || strings.HasPrefix(mnt.Source, "/run") {
				continue
			}
			info, err := os.Stat(mnt.Source)
			if err != nil {
				add(Finding{Level: "warn", Area: "Storage", Title: "A folder the app uses doesn't exist: " + mnt.Source,
					Fix: "Create it, or pick another folder in the app's Settings."})
				continue
			}
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				owners = append(owners, fmt.Sprintf("%s → owner %d:%d, mode %s", mnt.Source, st.Uid, st.Gid, info.Mode().Perm()))
			}
		}
	}
	if permIssue && len(owners) > 0 {
		uid, gid := hostIDs()
		add(Finding{Level: "info", Area: "Storage", Title: "Folder owners (the app normally runs as " + uid + ":" + gid + ")", Evidence: owners})
	}

	// Disk and memory on the server.
	if u, err := disk.Usage(m.dir); err == nil {
		free := float64(u.Free) / (1 << 30)
		switch {
		case u.UsedPercent > 95:
			add(Finding{Level: "bad", Area: "Storage", Title: fmt.Sprintf("The disk is almost full (%.0f%% used, %.1f GB free)", u.UsedPercent, free),
				Fix: "Apps fail in odd ways when the disk is full. Free up space."})
		case u.UsedPercent > 90:
			add(Finding{Level: "warn", Area: "Storage", Title: fmt.Sprintf("The disk is getting full (%.0f%% used)", u.UsedPercent)})
		default:
			add(Finding{Level: "ok", Area: "Storage", Title: fmt.Sprintf("Enough disk space (%.0f GB free)", free)})
		}
	}
	if vm, err := mem.VirtualMemory(); err == nil && vm.UsedPercent > 92 {
		add(Finding{Level: "warn", Area: "Memory", Title: fmt.Sprintf("The server is low on memory (%.0f%% used)", vm.UsedPercent),
			Fix: "Apps may be slow or get killed. Stop apps you don't need."})
	}

	for _, f := range logFindings {
		add(f)
	}

	// A recent failed Haven action on this app.
	for _, jv := range m.jobs.List() {
		if jv.App == id && jv.Status == "failed" && time.Since(jv.Started) < 24*time.Hour {
			add(Finding{Level: "info", Area: "Haven", Title: "A recent " + jv.Action + " failed", Evidence: []string{jv.Error}})
			break
		}
	}
	if ca, ok := m.cat.Get(meta.CatalogID); ok && !meta.Custom && ca.Version != meta.Version {
		add(Finding{Level: "info", Area: "Image", Title: "Version " + ca.Version + " is available",
			Fix: "Updating sometimes fixes bugs. Haven rolls back automatically if the update fails."})
	}
	return finish(d, env), nil
}

func checkWeb(ctx context.Context, web *WebSpec, port int, running bool) Finding {
	if !running {
		return Finding{Level: "info", Area: "Network", Title: fmt.Sprintf("Web page not checked (port %d): the app isn't running", port)}
	}
	scheme := web.Scheme
	if scheme == "" {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, port, web.Path)
	client := &http.Client{Timeout: 5 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // self-signed is fine here
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		var ne net.Error
		f := Finding{Level: "bad", Area: "Network", Title: fmt.Sprintf("Nothing answers on port %d", port), Evidence: []string{err.Error()}}
		if errors.As(err, &ne) && ne.Timeout() {
			f.Title = fmt.Sprintf("The app is very slow to answer on port %d", port)
			f.Fix = "It may be overloaded or stuck. Restart it and check the logs."
		} else {
			f.Fix = "If it just started, wait a minute. Otherwise it may be listening on a different port than Haven expects, or failing at startup; see the logs."
		}
		return f
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return Finding{Level: "bad", Area: "Network", Title: fmt.Sprintf("The web page answers with an error (%d %s)", resp.StatusCode, http.StatusText(resp.StatusCode)),
			Fix: "The app is running but broken inside. Its logs usually say why."}
	case resp.StatusCode == 404:
		return Finding{Level: "warn", Area: "Network", Title: fmt.Sprintf("The web page answers, but that page isn't found (port %d)", port)}
	default:
		return Finding{Level: "ok", Area: "Network", Title: fmt.Sprintf("The web page answers on port %d (%d)", port, resp.StatusCode)}
	}
}

var portInErrRe = regexp.MustCompile(`:(\d{1,5})\b.*(port is already allocated|address already in use)|(?:Bind for|bind:) [0-9.:\[\]]*:(\d{1,5})`)

func portInError(s string) int {
	m := portInErrRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	for _, g := range m[1:] {
		if n, err := strconv.Atoi(g); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	return 0
}

// portOwner names the program listening on a port, when the OS can tell us.
func portOwner(ctx context.Context, port int) string {
	if isLinux() && hasCmd("ss") {
		out, err := runHost(ctx, 5*time.Second, "ss", "-H", "-ltnp", fmt.Sprintf("sport = :%d", port))
		if err == nil && out != "" {
			if i := strings.Index(out, "users:(("); i >= 0 {
				return strings.TrimSuffix(out[i+len("users:(("):], "))")
			}
			return strings.TrimSpace(out)
		}
	} else if hasCmd("lsof") {
		out, err := runHost(ctx, 5*time.Second, "lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN")
		if err == nil {
			lines := strings.Split(out, "\n")
			if len(lines) > 1 {
				return strings.Fields(lines[1])[0]
			}
		}
	}
	return ""
}

// finish sorts findings (worst first), picks the likely cause, and builds
// a shareable plain-text report with secrets removed.
func finish(d *Diagnosis, env map[string]string) *Diagnosis {
	sort.SliceStable(d.Findings, func(i, j int) bool {
		a, b := d.Findings[i], d.Findings[j]
		if levelRank[a.Level] != levelRank[b.Level] {
			return levelRank[a.Level] > levelRank[b.Level]
		}
		return areaRank(a.Area) < areaRank(b.Area)
	})
	d.Level = "ok"
	if len(d.Findings) > 0 {
		d.Level = d.Findings[0].Level
	}
	switch d.Level {
	case "bad", "warn":
		c := d.Findings[0]
		d.Cause = &c
		d.Verdict = "Most likely cause: " + strings.ToLower(c.Title[:1]) + c.Title[1:] + "."
	case "info":
		d.Verdict = "Everything important looks fine. There are a few notes below."
	default:
		d.Verdict = "No problems found."
	}
	if d.Level == "ok" || d.Level == "info" {
		if d.Target != "this server" {
			d.Verdict += " If the app still misbehaves, run this again while the problem is happening, or check the app's own settings."
		}
	}
	d.Report = buildReport(d, env)
	return d
}

// areaRank orders findings of the same severity so that causes (a crash,
// a full disk, an error in the logs) come before symptoms (a dead web page).
func areaRank(area string) int {
	switch area {
	case "Docker", "Setup", "Containers", "Memory", "Image":
		return 0
	case "Storage", "Logs":
		return 1
	case "Health":
		return 2
	case "Network":
		return 3
	}
	return 4
}

var secretKeyRe = regexp.MustCompile(`(?i)pass|secret|token|key`)

func buildReport(d *Diagnosis, env map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Haven diagnosis: %s\n%s\nRan: %s · Haven %s · %s/%s\n\n", d.Target, d.Verdict, d.RanAt.Format(time.RFC1123), Version, runtime.GOOS, runtime.GOARCH)
	for _, f := range d.Findings {
		fmt.Fprintf(&b, "[%s] %s: %s\n", strings.ToUpper(f.Level), f.Area, f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, "    %s\n", f.Detail)
		}
		if f.Fix != "" {
			fmt.Fprintf(&b, "    Fix: %s\n", f.Fix)
		}
		for _, e := range f.Evidence {
			fmt.Fprintf(&b, "    > %s\n", e)
		}
	}
	report := b.String()
	// Never leak passwords or keys from the app's settings.
	for k, v := range env {
		if secretKeyRe.MatchString(k) && len(v) >= 4 {
			report = strings.ReplaceAll(report, v, "[hidden]")
		}
	}
	return report
}

// --- server health check ---------------------------------------------------

func (s *Server) DiagnoseServer(ctx context.Context) *Diagnosis {
	d := &Diagnosis{Target: "this server", RanAt: time.Now()}
	add := func(f Finding) { d.Findings = append(d.Findings, f) }

	if v, err := s.docker.Version(ctx); err != nil {
		add(Finding{Level: "bad", Area: "Docker", Title: "Docker isn't running", Detail: err.Error(), Fix: "sudo systemctl start docker"})
	} else {
		add(Finding{Level: "ok", Area: "Docker", Title: "Docker is running (" + v + ")"})
		if out, err := s.docker.run(ctx, "system", "df", "--format", "{{.Type}}|{{.Reclaimable}}"); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if t, r, ok := strings.Cut(line, "|"); ok && t == "Images" && !strings.HasPrefix(r, "0B") {
					add(Finding{Level: "info", Area: "Storage", Title: "Unused images take " + strings.Fields(r)[0],
						Detail: "Old versions left behind by updates.", Fix: "To free the space: docker image prune"})
				}
			}
		}
	}

	parts, _ := disk.Partitions(false)
	seen := map[string]bool{}
	for _, p := range parts {
		if !realFS[p.Fstype] || seen[p.Device] || strings.HasPrefix(p.Mountpoint, "/boot") || strings.HasPrefix(p.Mountpoint, "/snap") || strings.HasPrefix(p.Mountpoint, "/System") {
			continue
		}
		seen[p.Device] = true
		if u, err := disk.Usage(p.Mountpoint); err == nil && u.Total > 0 {
			switch {
			case u.UsedPercent > 95:
				add(Finding{Level: "bad", Area: "Storage", Title: fmt.Sprintf("%s is almost full (%.0f%%)", p.Mountpoint, u.UsedPercent), Fix: "Free up space; apps fail when the disk is full."})
			case u.UsedPercent > 90:
				add(Finding{Level: "warn", Area: "Storage", Title: fmt.Sprintf("%s is getting full (%.0f%%)", p.Mountpoint, u.UsedPercent)})
			default:
				add(Finding{Level: "ok", Area: "Storage", Title: fmt.Sprintf("%s has space (%.0f%% used)", p.Mountpoint, u.UsedPercent)})
			}
		}
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		lvl, title := "ok", fmt.Sprintf("Memory is fine (%.0f%% used)", vm.UsedPercent)
		if vm.UsedPercent > 92 {
			lvl, title = "warn", fmt.Sprintf("Memory is nearly full (%.0f%% used)", vm.UsedPercent)
		}
		add(Finding{Level: lvl, Area: "Memory", Title: title})
	}
	if t := cpuTemp(); t != nil && *t > 85 {
		add(Finding{Level: "warn", Area: "Hardware", Title: fmt.Sprintf("The CPU is hot (%.0f°C)", *t),
			Fix: "It may slow itself down. Make sure air can flow; a closed laptop lid can trap heat."})
	}

	// Internet and DNS: needed to install and update apps.
	if _, err := net.DefaultResolver.LookupHost(ctx, "registry-1.docker.io"); err != nil {
		add(Finding{Level: "bad", Area: "Network", Title: "The server can't look up internet addresses (DNS)", Evidence: []string{err.Error()},
			Fix: "Installs and updates will fail. Check the network connection and DNS settings. If Pi-hole/AdGuard runs here, check it's running."})
	} else {
		client := &http.Client{Timeout: 6 * time.Second}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry-1.docker.io/v2/", nil)
		if resp, err := client.Do(req); err != nil {
			add(Finding{Level: "bad", Area: "Network", Title: "The server can't reach the internet", Evidence: []string{err.Error()}, Fix: "Installs and updates will fail until it can."})
		} else {
			resp.Body.Close()
			add(Finding{Level: "ok", Area: "Network", Title: "Internet and DNS work"})
		}
	}
	if isLinux() && hasCmd("timedatectl") {
		if out, err := runHost(ctx, 5*time.Second, "timedatectl", "show", "-p", "NTPSynchronized", "--value"); err == nil && out != "yes" {
			add(Finding{Level: "warn", Area: "Server", Title: "The clock isn't synchronised",
				Fix: "A wrong clock breaks secure connections. Run: sudo timedatectl set-ntp true"})
		}
	}

	if p := PowerState(ctx); p.Supported && p.Laptop {
		if !p.Effective {
			add(Finding{Level: "warn", Area: "Power", Title: "Closing the lid will stop the server",
				Fix: "Turn on Keep running with the lid closed on the Server page."})
		} else {
			add(Finding{Level: "ok", Area: "Power", Title: "The server keeps running with the lid closed"})
		}
		if b := p.Battery; b != nil && !b.ACOnline {
			lvl := "warn"
			if b.Percent < 20 {
				lvl = "bad"
			}
			add(Finding{Level: lvl, Area: "Power", Title: fmt.Sprintf("Running on battery (%d%%)", b.Percent),
				Fix: "The charger is unplugged or the power is out. The server stops when the battery runs out."})
		}
	}
	rs := s.remote.Status(ctx)
	if rs.Supported {
		switch {
		case rs.State == "Running" && rs.KeyExpiry != nil && time.Until(*rs.KeyExpiry) < 14*24*time.Hour:
			add(Finding{Level: "warn", Area: "Remote", Title: "Remote access sign-in expires on " + rs.KeyExpiry.Format("2 Jan 2006"),
				Fix: "In the Tailscale admin console, open this machine and choose Disable key expiry."})
		case rs.State == "Running":
			add(Finding{Level: "ok", Area: "Remote", Title: "Remote access is on (" + rs.DNSName + ")"})
		case rs.Installed:
			add(Finding{Level: "warn", Area: "Remote", Title: "Remote access is installed but not connected (" + rs.State + ")", Fix: "Open the Server page and press Connect."})
		}
	}
	if ln := s.localName.Status(ctx); ln.Supported && ln.Installed && !ln.Running {
		add(Finding{Level: "warn", Area: "Network", Title: "The local name service (Avahi) isn't running", Fix: "Run Set up local address again on the Server page."})
	}
	return finish(d, nil)
}
