package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Updates: find out which installed apps have newer images, and update them
// one at a time or all together (with the same automatic rollback as a single
// update).
//
// How a check works: for each image an app uses, Haven asks the registry
// (Docker Hub, GitHub, ...) for the current fingerprint (digest) of that tag,
// the same way `docker pull` does, but without downloading anything. If the
// registry's fingerprint is not one Haven already has locally, the tag has
// moved on and an update is available. An app that has downloaded a newer
// image but not restarted yet is reported as "restart needed".

type ImgStatus struct {
	Ref    string `json:"ref"`
	Status string `json:"status"` // current, update, restart, unknown
	Detail string `json:"detail,omitempty"`
}

type AppUpdate struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Icon           string      `json:"icon,omitempty"`
	Version        string      `json:"version,omitempty"`
	CatalogVersion string      `json:"catalogVersion,omitempty"` // a newer store version exists
	Images         []ImgStatus `json:"images"`
	HasUpdate      bool        `json:"hasUpdate"`
	Checked        time.Time   `json:"checked"`
	Busy           string      `json:"busy,omitempty"`
}

type SystemUpdates struct {
	Supported     bool   `json:"supported"`
	DockerCurrent string `json:"dockerCurrent,omitempty"`
	DockerNew     string `json:"dockerNew,omitempty"`
	Upgradable    int    `json:"upgradable"` // Ubuntu packages with updates
	Command       string `json:"command,omitempty"`
}

type UpdatesSnapshot struct {
	Apps      []AppUpdate   `json:"apps"`
	Checked   time.Time     `json:"checked"`
	Checking  bool          `json:"checking"`
	System    SystemUpdates `json:"system"`
	JobID     string        `json:"jobId,omitempty"`
	LastError string        `json:"lastError,omitempty"`
}

// --- registry client ---------------------------------------------------------

type imageRef struct{ host, repo, tag string }

// parseImageRef splits "ghcr.io/org/app:1.2" into registry, repository, tag.
// It returns ok=false for images pinned by digest, which never change.
func parseImageRef(ref string) (imageRef, bool) {
	if strings.Contains(ref, "@") {
		return imageRef{}, false
	}
	tag := "latest"
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref, tag = ref[:i], ref[i+1:]
	}
	host, repo := "docker.io", ref
	if i := strings.Index(ref, "/"); i > 0 {
		first := ref[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			host, repo = first, ref[i+1:]
		}
	}
	switch host {
	case "docker.io", "index.docker.io":
		host = "registry-1.docker.io"
		if !strings.Contains(repo, "/") {
			repo = "library/" + repo
		}
	case "lscr.io": // LinuxServer's front door; the images live on GitHub's registry
		host = "ghcr.io"
	}
	return imageRef{host: host, repo: repo, tag: tag}, true
}

var authParamRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// parseChallenge reads a registry's `WWW-Authenticate: Bearer realm=...` header.
func parseChallenge(h string) (realm, service, scope string) {
	for _, m := range authParamRe.FindAllStringSubmatch(h, -1) {
		switch m[1] {
		case "realm":
			realm = m[2]
		case "service":
			service = m[2]
		case "scope":
			scope = m[2]
		}
	}
	return
}

type registryClient struct {
	http   *http.Client
	mu     sync.Mutex
	tokens map[string]string
}

func newRegistryClient() *registryClient {
	return &registryClient{http: &http.Client{Timeout: 20 * time.Second}, tokens: map[string]string{}}
}

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

// Digest returns the registry's current digest for a tag.
func (c *registryClient) Digest(ctx context.Context, r imageRef) (string, error) {
	u := fmt.Sprintf("https://%s/v2/%s/manifests/%s", r.host, r.repo, url.PathEscape(r.tag))
	key := r.host + "/" + r.repo
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", manifestAccept)
		req.Header.Set("User-Agent", userAgent)
		c.mu.Lock()
		tok := c.tokens[key]
		c.mu.Unlock()
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("can't reach %s", r.host)
		}
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK:
			if d := resp.Header.Get("Docker-Content-Digest"); d != "" {
				return d, nil
			}
			return "", errors.New("the registry didn't say what the current version is")
		case resp.StatusCode == http.StatusUnauthorized && attempt == 0:
			realm, service, scope := parseChallenge(resp.Header.Get("Www-Authenticate"))
			if realm == "" {
				return "", errors.New("the registry needs a login")
			}
			if scope == "" {
				scope = "repository:" + r.repo + ":pull"
			}
			t, err := c.token(ctx, realm, service, scope)
			if err != nil {
				return "", err
			}
			c.mu.Lock()
			c.tokens[key] = t
			c.mu.Unlock()
		case resp.StatusCode == http.StatusNotFound:
			return "", errors.New("this version isn't on the registry any more")
		case resp.StatusCode == http.StatusTooManyRequests:
			return "", errors.New("the registry asked us to slow down")
		default:
			return "", fmt.Errorf("%s answered %d", r.host, resp.StatusCode)
		}
	}
	return "", errors.New("the registry refused access (the image may be private)")
}

func (c *registryClient) token(ctx context.Context, realm, service, scope string) (string, error) {
	u, err := url.Parse(realm)
	if err != nil || u.Scheme != "https" {
		return "", errors.New("unexpected registry login address")
	}
	q := u.Query()
	if service != "" {
		q.Set("service", service)
	}
	q.Set("scope", scope)
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", errors.New("can't reach the registry login")
	}
	defer resp.Body.Close()
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&body) != nil {
		return "", errors.New("the registry login failed")
	}
	if body.Token != "" {
		return body.Token, nil
	}
	return body.AccessToken, nil
}

// --- deciding ---------------------------------------------------------------------

// digestIn reports whether a registry digest appears in a local image's
// RepoDigests ("repo@sha256:...").
func digestIn(remote string, repoDigests []string) bool {
	for _, d := range repoDigests {
		if _, after, ok := strings.Cut(d, "@"); ok && after == remote {
			return true
		}
	}
	return false
}

// --- the checker --------------------------------------------------------------------

type UpdateChecker struct {
	m        *Manager
	reg      *registryClient
	notify   func(level, title, body, key, route string)
	mu       sync.Mutex
	apps     map[string]*AppUpdate
	checked  time.Time
	checking bool
	lastErr  string
	system   SystemUpdates
	sysAt    time.Time
	jobID    string
}

func NewUpdateChecker(m *Manager) *UpdateChecker {
	return &UpdateChecker{m: m, reg: newRegistryClient(), apps: map[string]*AppUpdate{}}
}

// Run checks shortly after start, then a few times a day.
func (u *UpdateChecker) Run(ctx context.Context) {
	select {
	case <-time.After(90 * time.Second):
	case <-ctx.Done():
		return
	}
	for {
		u.CheckNow()
		select {
		case <-time.After(6 * time.Hour):
		case <-ctx.Done():
			return
		}
	}
}

// CheckNow starts a check in the background (does nothing if one is running).
func (u *UpdateChecker) CheckNow() {
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return
	}
	u.checking = true
	u.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		u.checkAll(ctx)
		u.mu.Lock()
		u.checking = false
		u.mu.Unlock()
	}()
}

// Forget drops an app's result so the next view shows it as unchecked, then
// re-checks that app (used right after an update).
func (u *UpdateChecker) Recheck(id string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		apps, _, err := u.m.List(ctx)
		if err != nil {
			return
		}
		for _, a := range apps {
			if a.ID == id {
				res := u.checkApp(ctx, a)
				u.mu.Lock()
				u.apps[id] = res
				u.mu.Unlock()
			}
		}
	}()
}

func (u *UpdateChecker) checkAll(ctx context.Context) {
	apps, _, err := u.m.List(ctx)
	if err != nil {
		u.mu.Lock()
		u.lastErr = "Docker isn't answering: " + err.Error()
		u.mu.Unlock()
		return
	}
	results := make([]*AppUpdate, len(apps))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, a := range apps {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = u.checkApp(ctx, a)
		}()
	}
	wg.Wait()
	u.mu.Lock()
	old := u.apps
	u.apps = map[string]*AppUpdate{}
	for _, r := range results {
		u.apps[r.ID] = r
	}
	u.checked, u.lastErr = time.Now(), ""
	u.mu.Unlock()
	if u.notify != nil {
		for _, r := range results {
			if r.HasUpdate && (old[r.ID] == nil || !old[r.ID].HasUpdate) {
				u.notify("info", r.Name+" has an update", "A newer version is available. Open Updates to install it.", "update:"+r.ID+":"+updateFingerprint(r), "updates")
			}
		}
	}
	u.refreshSystem(ctx, true)
}

func updateFingerprint(a *AppUpdate) string {
	var parts []string
	for _, i := range a.Images {
		if i.Status == "update" || i.Status == "restart" {
			parts = append(parts, i.Ref)
		}
	}
	sort.Strings(parts)
	return a.CatalogVersion + "|" + strings.Join(parts, ",")
}

func (u *UpdateChecker) checkApp(ctx context.Context, a InstalledApp) *AppUpdate {
	res := &AppUpdate{ID: a.ID, Name: a.Name, Icon: a.Icon, Version: a.Version, Images: []ImgStatus{}, Checked: time.Now(), Busy: a.Busy}
	if a.UpdateAvailable != "" {
		res.CatalogVersion = a.UpdateAvailable
		res.HasUpdate = true
	}
	d := u.m.docker
	out, err := d.run(ctx, u.m.compose(a.ID, "config", "--images")...)
	if err != nil {
		return res
	}
	refs := strings.Fields(string(out))
	sort.Strings(refs)
	// What each running container was started from, to spot "downloaded but not restarted".
	running := map[string]string{} // ref -> image id
	if ids, err := d.run(ctx, "ps", "-aq", "--filter", "label=com.docker.compose.project="+projectName(a.ID)); err == nil && strings.TrimSpace(string(ids)) != "" {
		args := append([]string{"inspect", "--format", "{{.Config.Image}} {{.Image}}"}, strings.Fields(string(ids))...)
		if o, err := d.run(ctx, args...); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(o)), "\n") {
				if ref, id, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
					running[ref] = id
				}
			}
		}
	}
	for _, ref := range refs {
		st := ImgStatus{Ref: ref, Status: "unknown"}
		parsed, ok := parseImageRef(ref)
		insp, ierr := d.run(ctx, "image", "inspect", "--format", "{{.Id}}|{{json .RepoDigests}}", ref)
		var localID string
		var repoDigests []string
		if ierr == nil {
			id, rd, _ := strings.Cut(strings.TrimSpace(string(insp)), "|")
			localID = id
			_ = json.Unmarshal([]byte(rd), &repoDigests)
		}
		switch {
		case !ok:
			st.Status, st.Detail = "current", "pinned to an exact version"
		case ierr != nil:
			st.Detail = "not downloaded yet"
		case len(repoDigests) == 0:
			st.Detail = "built on this server, nothing to compare"
		default:
			remote, err := u.reg.Digest(ctx, parsed)
			switch {
			case err != nil:
				st.Detail = err.Error()
			case !digestIn(remote, repoDigests):
				st.Status = "update"
			case running[ref] != "" && running[ref] != localID:
				st.Status, st.Detail = "restart", "newer version downloaded, restart to use it"
			default:
				st.Status = "current"
			}
		}
		if st.Status == "update" || st.Status == "restart" {
			res.HasUpdate = true
		}
		res.Images = append(res.Images, st)
	}
	return res
}

func (u *UpdateChecker) refreshSystem(ctx context.Context, force bool) {
	u.mu.Lock()
	if !force && time.Since(u.sysAt) < time.Hour {
		u.mu.Unlock()
		return
	}
	u.sysAt = time.Now()
	u.mu.Unlock()
	s := SystemUpdates{}
	if isLinux() && hasCmd("apt-cache") {
		s.Supported = true
		if out, err := runHost(ctx, 15*time.Second, "apt-cache", "policy", "docker-ce"); err == nil {
			var inst, cand string
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if v, ok := strings.CutPrefix(line, "Installed:"); ok {
					inst = strings.TrimSpace(v)
				}
				if v, ok := strings.CutPrefix(line, "Candidate:"); ok {
					cand = strings.TrimSpace(v)
				}
			}
			if inst != "" && inst != "(none)" && cand != "" && inst != cand {
				s.DockerCurrent, s.DockerNew = inst, cand
			}
		}
		if out, err := runHost(ctx, 30*time.Second, "sh", "-c", "apt list --upgradable 2>/dev/null | grep -c upgradable"); err == nil {
			s.Upgradable, _ = strconv.Atoi(strings.TrimSpace(out))
		}
		s.Command = "sudo apt update && sudo apt upgrade"
		if s.DockerNew != "" {
			s.Command = "sudo apt update && sudo apt install --only-upgrade docker-ce docker-ce-cli containerd.io docker-compose-plugin"
		}
	}
	u.mu.Lock()
	u.system = s
	u.mu.Unlock()
}

func (u *UpdateChecker) Snapshot() UpdatesSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := UpdatesSnapshot{Apps: []AppUpdate{}, Checked: u.checked, Checking: u.checking, System: u.system, LastError: u.lastErr}
	for _, a := range u.apps {
		c := *a
		c.Busy = u.m.busyFor(a.ID)
		s.Apps = append(s.Apps, c)
	}
	sort.Slice(s.Apps, func(i, j int) bool {
		if s.Apps[i].HasUpdate != s.Apps[j].HasUpdate {
			return s.Apps[i].HasUpdate
		}
		return strings.ToLower(s.Apps[i].Name) < strings.ToLower(s.Apps[j].Name)
	})
	return s
}

// --- applying updates ------------------------------------------------------------------

// UpdateMany updates several apps one after another in a single job. One
// failing app doesn't stop the others, and each rolls back by itself.
func (m *Manager) UpdateMany(ids []string, prune bool) (*Job, error) {
	if len(ids) == 0 || len(ids) > 60 {
		return nil, errors.New("choose at least one app")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !appIDRe.MatchString(id) || !exists(filepath.Join(m.appDir(id), "haven.json")) || seen[id] {
			return nil, fmt.Errorf("unknown app %q", id)
		}
		seen[id] = true
	}
	return m.jobs.Start("updates", "update-all", 3*time.Hour, func(ctx context.Context, j *Job) error {
		var failed []string
		for n, id := range ids {
			meta, err := m.readMeta(id)
			if err != nil {
				failed = append(failed, id)
				continue
			}
			j.Logf("[%d/%d] Updating %s", n+1, len(ids), meta.Name)
			var app *CatalogApp
			if !meta.Custom {
				a, ok := m.cat.Get(meta.CatalogID)
				if !ok {
					j.Logf("%s is no longer in the catalog; skipped", meta.Name)
					failed = append(failed, meta.Name)
					continue
				}
				app = a
			}
			if err := m.lock(id, "update"); err != nil {
				j.Logf("Skipped %s: %v", meta.Name, err)
				failed = append(failed, meta.Name)
				continue
			}
			err = m.runUpdate(ctx, j, id, meta, app)
			m.unlock(id)
			if m.onUpdated != nil {
				m.onUpdated(id)
			}
			if err != nil {
				j.Logf("%s: %v", meta.Name, err)
				failed = append(failed, meta.Name)
			} else {
				j.Logf("%s is up to date", meta.Name)
			}
		}
		if prune {
			j.Logf("Removing old images that nothing uses")
			_ = m.docker.stream(ctx, j, "image", "prune", "-f")
		}
		if len(failed) > 0 {
			return fmt.Errorf("%d of %d updates didn't finish: %s", len(failed), len(ids), strings.Join(failed, ", "))
		}
		return nil
	}), nil
}
