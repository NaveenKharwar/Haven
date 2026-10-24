package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Haven updating itself from GitHub Releases.
//
// A release carries haven-linux-<arch> binaries, a SHA256SUMS file and a
// signature of that file made with a private key only the maintainer holds.
// Haven checks the signature with the public key built into it, so a stolen
// GitHub account or a tampered download can't push code to people's servers.

// UpdateRepo ("owner/name") is set at build time: -ldflags "-X main.UpdateRepo=owner/name".
// The release workflow sets it to the repository it runs in, so renaming or
// recreating the repository needs no code change.
var UpdateRepo = ""

// updatePublicKey verifies release signatures (ed25519, base64).
// Author is shown in the About page and the footer.
const Author = "Naveen Kharwar"

// AuthorURL is the author's GitHub profile.
const AuthorURL = "https://github.com/NaveenKharwar"

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// safeRepo returns the repository name only if it is a plain owner/name, so
// the link the interface builds from it can't be anything else.
func safeRepo(r string) string {
	if repoRe.MatchString(r) {
		return r
	}
	return ""
}

const updatePublicKey = "thmggXn8pLspMPdqTAe0Q8CuoG6Q5rygnMY6MrgNf3o="

const (
	githubAPI        = "https://api.github.com"
	maxBinaryBytes   = 120 << 20
	maxSmallAsset    = 64 << 10
	updateHealthWait = 30 * time.Second
)

type ReleaseInfo struct {
	Version   string            `json:"version"`
	Tag       string            `json:"tag"`
	Notes     string            `json:"notes,omitempty"`
	URL       string            `json:"url,omitempty"`
	Published time.Time         `json:"published,omitempty"`
	assets    map[string]string // asset name -> API url
}

// ReleaseNote is one release between this install and the newest one.
type ReleaseNote struct {
	Version   string    `json:"version"`
	Notes     string    `json:"notes,omitempty"`
	URL       string    `json:"url,omitempty"`
	Published time.Time `json:"published,omitempty"`
}

const maxNotes = 20

type SelfStatus struct {
	Supported bool      `json:"supported"`
	Reason    string    `json:"reason,omitempty"`
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	Notes     string    `json:"notes,omitempty"`
	URL       string    `json:"url,omitempty"`
	Checked   time.Time `json:"checked,omitempty"`
	Error     string    `json:"error,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	// Every release the update installs, newest first. Skipping versions is
	// fine, so the person is shown what each skipped version changed.
	Versions []ReleaseNote `json:"versions,omitempty"`
	More     int           `json:"more,omitempty"` // older releases not listed
}

type SelfUpdate struct {
	repo    string
	api     string // GitHub API base, replaced in tests
	dir     string // Haven's data directory
	binPath string // the running binary
	pub     ed25519.PublicKey
	client  *http.Client
	jobs    *Jobs
	notify  func(level, title, body, key, route string)
	exit    func() // called to restart after a successful swap
	goos    string

	mu      sync.Mutex
	latest  *ReleaseInfo
	between []ReleaseNote
	more    int
	checked time.Time
	lastErr string
}

func NewSelfUpdate(dir string, jobs *Jobs, notify func(level, title, body, key, route string)) *SelfUpdate {
	pub, _ := base64.StdEncoding.DecodeString(updatePublicKey)
	bin, _ := os.Executable()
	if real, err := filepath.EvalSymlinks(bin); err == nil {
		bin = real
	}
	return &SelfUpdate{repo: UpdateRepo, api: githubAPI, dir: dir, binPath: bin, pub: pub, client: &http.Client{}, // every call has its own context deadline
		jobs: jobs, notify: notify, goos: runtime.GOOS, exit: func() { os.Exit(0) }}
}

// supported says whether this install can update itself, and why not if it can't.
func (u *SelfUpdate) supported() (bool, string) {
	switch {
	case u.repo == "":
		return false, "This build wasn't told where its releases are published."
	case u.goos != "linux":
		return false, "Self-update runs on the Linux server."
	case filepath.Base(u.binPath) != "haven":
		return false, "Haven isn't running from the installed location."
	}
	return true, ""
}

func (u *SelfUpdate) get(ctx context.Context, url, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "haven/"+Version)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no release was found for this project")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("the download is larger than expected")
	}
	return b, nil
}

// Check asks GitHub for the newest release.
func (u *SelfUpdate) Check(ctx context.Context) SelfStatus {
	if ok, _ := u.supported(); ok {
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		b, err := u.get(cctx, u.api+"/repos/"+u.repo+"/releases/latest", "application/vnd.github+json", 1<<20)
		u.mu.Lock()
		u.checked = time.Now()
		if err != nil {
			u.lastErr = err.Error()
		} else if rel, perr := parseRelease(b); perr != nil {
			u.lastErr = perr.Error()
		} else {
			u.lastErr = ""
			u.latest = rel
		}
		u.mu.Unlock()
		if err == nil {
			u.collectNotes(cctx)
		}
	}
	return u.Status()
}

// collectNotes lists every release newer than this install, up to the newest,
// so an update that skips versions still shows what each one changed.
func (u *SelfUpdate) collectNotes(ctx context.Context) {
	b, err := u.get(ctx, u.api+"/repos/"+u.repo+"/releases?per_page=50", "application/vnd.github+json", 4<<20)
	var all []ReleaseNote
	if err == nil {
		var raw []json.RawMessage
		if json.Unmarshal(b, &raw) == nil {
			for _, r := range raw {
				if rel, perr := parseRelease(r); perr == nil && newerVersion(rel.Version, Version) {
					all = append(all, ReleaseNote{Version: rel.Version, Notes: rel.Notes, URL: rel.URL, Published: rel.Published})
				}
			}
		}
	}
	sort.SliceStable(all, func(a, b int) bool { return newerVersion(all[a].Version, all[b].Version) })
	more := 0
	if len(all) > maxNotes {
		more = len(all) - maxNotes
		all = all[:maxNotes]
	}
	u.mu.Lock()
	u.between, u.more = all, more
	u.mu.Unlock()
}

func parseRelease(b []byte) (*ReleaseInfo, error) {
	var r struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		URL       string    `json:"html_url"`
		Published time.Time `json:"published_at"`
		Draft     bool      `json:"draft"`
		Pre       bool      `json:"prerelease"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.Tag == "" {
		return nil, errors.New("unexpected answer from GitHub")
	}
	if r.Draft || r.Pre {
		return nil, errors.New("the newest release is a draft or pre-release")
	}
	rel := &ReleaseInfo{Version: strings.TrimPrefix(r.Tag, "v"), Tag: r.Tag, Notes: strings.TrimSpace(r.Body), URL: r.URL, Published: r.Published, assets: map[string]string{}}
	for _, a := range r.Assets {
		rel.assets[a.Name] = a.URL
	}
	return rel, nil
}

func (u *SelfUpdate) Status() SelfStatus {
	ok, why := u.supported()
	u.mu.Lock()
	defer u.mu.Unlock()
	s := SelfStatus{Supported: ok, Reason: why, Current: Version, Checked: u.checked, Error: u.lastErr, Repo: u.repo}
	if u.latest != nil {
		s.Latest, s.Notes, s.URL = u.latest.Version, u.latest.Notes, u.latest.URL
		s.Available = ok && newerVersion(u.latest.Version, Version)
		if s.Available {
			s.Versions, s.More = u.between, u.more
			// If the list couldn't be read, at least show the newest release.
			if len(s.Versions) == 0 {
				s.Versions = []ReleaseNote{{Version: u.latest.Version, Notes: u.latest.Notes, URL: u.latest.URL, Published: u.latest.Published}}
			}
		}
	}
	return s
}

// Run checks once a day and adds a notification when a new version appears.
func (u *SelfUpdate) Run(ctx context.Context) {
	if ok, _ := u.supported(); !ok {
		return
	}
	select {
	case <-time.After(2 * time.Minute):
	case <-ctx.Done():
		return
	}
	for {
		s := u.Check(ctx)
		if s.Available && u.notify != nil {
			first := plainMarkdown(firstLine(s.Notes))
			u.notify("info", "Haven "+s.Latest+" is available", first, "selfupdate:"+s.Latest, "updates")
		}
		select {
		case <-time.After(24 * time.Hour):
		case <-ctx.Done():
			return
		}
	}
}

// --- versions -------------------------------------------------------------------------

func parseVer(s string) (n [3]int, pre string, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		pre = s[i+1:]
		if s[i] == '+' {
			pre = ""
		}
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return n, "", false
	}
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return n, "", false
		}
		n[i] = v
	}
	return n, pre, true
}

// newerVersion reports whether latest is a newer release than current.
// 1.2.0 is newer than 1.2.0-dev, and a build that isn't a version is never "behind".
func newerVersion(latest, current string) bool {
	ln, lp, ok1 := parseVer(latest)
	cn, cp, ok2 := parseVer(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if ln[i] != cn[i] {
			return ln[i] > cn[i]
		}
	}
	switch {
	case lp == cp:
		return false
	case lp == "":
		return true
	case cp == "":
		return false
	}
	return lp > cp
}

// --- applying -------------------------------------------------------------------------

// Apply downloads, verifies and installs the latest release, then restarts.
func (u *SelfUpdate) Apply() (*Job, error) {
	if ok, why := u.supported(); !ok {
		return nil, errors.New(why)
	}
	s := u.Status()
	if !s.Available {
		return nil, errors.New("Haven is already up to date")
	}
	u.mu.Lock()
	rel := *u.latest
	u.mu.Unlock()
	return u.jobs.Start("haven", "haven-update", 15*time.Minute, func(ctx context.Context, j *Job) error {
		return u.install(ctx, j, &rel)
	}), nil
}

func (u *SelfUpdate) assetURL(rel *ReleaseInfo, name string) (string, error) {
	url, ok := rel.assets[name]
	if !ok {
		return "", fmt.Errorf("this release has no %s file", name)
	}
	// Only ever fetch from this repository's release assets on GitHub.
	// GitHub ignores capitals in repository names and answers with the name as the
	// owner wrote it, so compare without regard to case.
	prefix := u.api + "/repos/" + u.repo + "/releases/assets/"
	if len(url) < len(prefix) || !strings.EqualFold(url[:len(prefix)], prefix) {
		return "", errors.New("the release points somewhere unexpected; refusing to download")
	}
	return url, nil
}

func (u *SelfUpdate) install(ctx context.Context, j *Job, rel *ReleaseInfo) error {
	arch := runtime.GOARCH
	binName := "haven-linux-" + arch
	j.Logf("Updating Haven %s to %s", Version, rel.Version)
	u.mu.Lock()
	var names []string
	for _, v := range u.between {
		names = append(names, v.Version)
	}
	u.mu.Unlock()
	if len(names) > 1 {
		j.Logf("This installs %d releases at once: %s", len(names), strings.Join(names, ", "))
	}

	fetch := func(name string, limit int64) ([]byte, error) {
		url, err := u.assetURL(rel, name)
		if err != nil {
			return nil, err
		}
		dctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		return u.get(dctx, url, "application/octet-stream", limit)
	}

	j.Logf("Downloading the checksum list and its signature")
	sums, err := fetch("SHA256SUMS", maxSmallAsset)
	if err != nil {
		return err
	}
	sigText, err := fetch("SHA256SUMS.sig", maxSmallAsset)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(u.pub) != ed25519.PublicKeySize || !ed25519.Verify(u.pub, sums, sig) {
		return errors.New("the release signature is not valid, so Haven will not install it")
	}
	j.Logf("✓ Signature is valid")
	want := ""
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == binName {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return fmt.Errorf("the checksum list has no entry for %s", binName)
	}

	j.Logf("Downloading %s", binName)
	data, err := fetch(binName, maxBinaryBytes)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return errors.New("the download doesn't match its checksum, so Haven will not install it")
	}
	j.Logf("✓ Checksum matches")

	newPath := u.binPath + ".new"
	if err := os.WriteFile(newPath, data, 0o755); err != nil {
		return fmt.Errorf("could not save the new version: %w", err)
	}
	defer os.Remove(newPath)
	out, err := runHost(ctx, 15*time.Second, newPath, "-version")
	if err != nil || !strings.Contains(out, rel.Version) {
		return fmt.Errorf("the new version won't start on this server (%s)", lastLines(out, 2))
	}
	j.Logf("✓ The new version starts (%s)", strings.TrimSpace(out))

	prev := u.binPath + ".prev"
	if err := writeMarker(u.dir, updateMarker{From: Version, To: rel.Version, Prev: prev, Bin: u.binPath, At: time.Now()}); err != nil {
		return err
	}
	_ = os.Remove(prev)
	if err := os.Rename(u.binPath, prev); err != nil {
		_ = os.Remove(markerPath(u.dir))
		return fmt.Errorf("could not keep the old version: %w", err)
	}
	if err := os.Rename(newPath, u.binPath); err != nil {
		_ = os.Rename(prev, u.binPath)
		_ = os.Remove(markerPath(u.dir))
		return fmt.Errorf("could not install the new version: %w", err)
	}
	j.Logf("Installed. Haven restarts in a moment; this page reconnects by itself.")
	go func() {
		time.Sleep(2 * time.Second)
		u.exit()
	}()
	return nil
}

// --- rollback guard -------------------------------------------------------------------

type updateMarker struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Prev     string    `json:"prev"`
	Bin      string    `json:"bin"`
	Attempts int       `json:"attempts"`
	At       time.Time `json:"at"`
}

func markerPath(dir string) string { return filepath.Join(dir, "update-pending.json") }

func writeMarker(dir string, m updateMarker) error {
	b, _ := json.Marshal(m)
	return writeFileAtomic(markerPath(dir), b, 0o600)
}

// StartupGuard runs first thing at start. After an update, the new version
// has a few tries to come up and stay healthy. If it keeps dying, the old
// version is put back. It returns true if it restored the old version, and the
// caller should exit so the service manager starts the old one.
func StartupGuard(dir string) (rolledBack bool) {
	b, err := os.ReadFile(markerPath(dir))
	if err != nil {
		return false
	}
	var m updateMarker
	if json.Unmarshal(b, &m) != nil || m.To != Version {
		_ = os.Remove(markerPath(dir)) // stale or from a different run
		return false
	}
	m.Attempts++
	if m.Attempts > 2 {
		log.Printf("update to %s keeps failing; going back to %s", m.To, m.From)
		if err := os.Rename(m.Prev, m.Bin); err != nil {
			log.Printf("could not restore the previous version: %v", err)
			return false
		}
		_ = os.Remove(markerPath(dir))
		return true
	}
	_ = writeMarker(dir, m)
	return false
}

// ConfirmUpdate clears the marker once the new version has stayed up.
func ConfirmUpdate(dir string, notify func(level, title, body, key, route string)) {
	b, err := os.ReadFile(markerPath(dir))
	if err != nil {
		return
	}
	var m updateMarker
	if json.Unmarshal(b, &m) != nil || m.To != Version {
		return
	}
	time.Sleep(updateHealthWait)
	_ = os.Remove(markerPath(dir))
	log.Printf("update to %s confirmed", m.To)
	if notify != nil {
		notify("ok", "Haven was updated", fmt.Sprintf("Now running version %s (was %s).", m.To, m.From), "selfupdated:"+m.To, "")
	}
}

// --- HTTP -----------------------------------------------------------------------------

func (s *Server) selfStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.self.Status())
}

func (s *Server) selfCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.self.Check(r.Context()))
}

func (s *Server) selfApply(w http.ResponseWriter, r *http.Request) {
	job, err := s.self.Apply()
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("people: %s started a Haven update", currentUser(r))
	writeJSON(w, http.StatusAccepted, job.View(false))
}

// firstLine is the first line of the notes that has any text.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

var (
	mdLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdHead = regexp.MustCompile(`^\s*(#{1,6}|[-*+])\s+`)
)

// plainMarkdown strips Markdown marks so a line reads as plain text in a notification.
func plainMarkdown(s string) string {
	s = mdHead.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	return strings.TrimSpace(strings.NewReplacer("**", "", "`", "").Replace(s))
}
