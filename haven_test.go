package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"net/http/cookiejar"
	"testing/fstest"

	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"runtime"

	"crypto/x509"
	"syscall"
)

func TestCatalogLoadsAndIsValid(t *testing.T) {
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.All()) < 20 {
		t.Fatalf("expected at least 20 apps, got %d", len(c.All()))
	}
	for _, a := range c.All() {
		if got, ok := c.Get(a.ID); !ok || got.ID != a.ID {
			t.Errorf("Get(%q) failed", a.ID)
		}
		if a.Icon == "" || !strings.HasPrefix(a.Icon, "https://") {
			t.Errorf("%s: icon must be an https URL", a.ID)
		}
		if a.Web == nil {
			t.Errorf("%s: every store app should have a web UI", a.ID)
		}
	}
}

// Two store apps must never default to the same port, or installing both
// with default settings would fail. DNS (53) is the one shared exception,
// because Pi-hole and AdGuard are alternatives to each other.
func TestDefaultPortsAreUnique(t *testing.T) {
	c, _ := LoadCatalog()
	owner := map[int]string{8484: "Haven"}
	for _, a := range c.All() {
		var ports []int
		for _, s := range a.Settings {
			if s.Type == "port" && s.Default != "" {
				p, _ := strconv.Atoi(s.Default)
				ports = append(ports, p)
			}
		}
		for _, hp := range a.HostPorts {
			p, _, _ := parseHostPort(hp)
			ports = append(ports, p)
		}
		for _, p := range ports {
			if p == 53 {
				continue
			}
			if o, dup := owner[p]; dup && o != a.ID {
				t.Errorf("port %d is the default for both %s and %s", p, o, a.ID)
			}
			owner[p] = a.ID
		}
	}
}

func TestValidateCatalogAppRejectsBrokenEntries(t *testing.T) {
	base := func() CatalogApp {
		return CatalogApp{ID: "demo", Name: "Demo", Category: "X", Tagline: "t", Version: "1", Archs: []string{"amd64"},
			Settings: []Setting{{Key: "WEB_PORT", Label: "Port", Type: "port", Default: "8000"}},
			Web:      &WebSpec{Port: "WEB_PORT"},
			Compose:  "services:\n  web:\n    image: nginx:1\n    restart: unless-stopped\n    ports: [\"${WEB_PORT}:80\"]\n"}
	}
	ok := base()
	if err := validateCatalogApp(&ok); err != nil {
		t.Fatalf("valid app rejected: %v", err)
	}
	cases := map[string]func(a *CatalogApp){
		"undefined variable": func(a *CatalogApp) { a.Compose += "    environment:\n      X: ${NOPE}\n" },
		"no tag":             func(a *CatalogApp) { a.Compose = strings.Replace(a.Compose, "nginx:1", "nginx", 1) },
		"container_name":     func(a *CatalogApp) { a.Compose += "    container_name: web\n" },
		"no restart":         func(a *CatalogApp) { a.Compose = strings.Replace(a.Compose, "    restart: unless-stopped\n", "", 1) },
		"bad web port":       func(a *CatalogApp) { a.Web.Port = "MISSING" },
		"bad default port":   func(a *CatalogApp) { a.Settings[0].Default = "99999" },
		"reserved key":       func(a *CatalogApp) { a.Settings[0].Key = "APP_DATA" },
		"bad yaml":           func(a *CatalogApp) { a.Compose = "services: [" },
		"bad arch":           func(a *CatalogApp) { a.Archs = []string{"mips"} },
	}
	for name, mutate := range cases {
		a := base()
		mutate(&a)
		if err := validateCatalogApp(&a); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestValidateValue(t *testing.T) {
	port := &Setting{Type: "port"}
	path := &Setting{Type: "path"}
	sel := &Setting{Type: "select", Options: []string{"a", "b"}}
	pw := &Setting{Type: "password"}
	good := []struct {
		s *Setting
		v string
	}{{port, "80"}, {port, "65535"}, {path, "/srv/media"}, {sel, "a"}, {pw, "Abc123!@#"}}
	bad := []struct {
		s *Setting
		v string
	}{{port, "0"}, {port, "70000"}, {port, "abc"}, {path, "relative"}, {path, "/srv/../etc"}, {path, "/a:/b"},
		{sel, "c"}, {pw, "has'quote"}, {pw, "line\nbreak"}, {pw, "dollar$sign"}}
	for _, c := range good {
		if err := validateValue(c.s, c.v); err != nil {
			t.Errorf("%s %q should be valid: %v", c.s.Type, c.v, err)
		}
	}
	for _, c := range bad {
		if err := validateValue(c.s, c.v); err == nil {
			t.Errorf("%s %q should be rejected", c.s.Type, c.v)
		}
	}
}

func TestEnvRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	e := &envFile{vals: map[string]string{}}
	e.set("A", "hello world")
	e.set("B", "p@ss#word=1")
	e.set("C", "")
	if err := e.write(p); err != nil {
		t.Fatal(err)
	}
	got, err := readEnv(p)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range e.vals {
		if got.vals[k] != v {
			t.Errorf("%s: got %q want %q", k, got.vals[k], v)
		}
	}
	if strings.Join(got.keys, ",") != "A,B,C" {
		t.Errorf("order not kept: %v", got.keys)
	}
}

func TestAppDataSubdirs(t *testing.T) {
	compose := "volumes:\n  - ${APP_DATA}/config:/config\n  - ${APP_DATA}/a/b:/x:ro\n  - ${APP_DATA}/config:/again\n  - /srv:/srv\n"
	got := appDataSubdirs(compose)
	if strings.Join(got, ",") != "config,a/b" {
		t.Errorf("got %v", got)
	}
}

func TestAppStatus(t *testing.T) {
	cases := []struct {
		cs   []Container
		want string
	}{
		{nil, "stopped"},
		{[]Container{{State: "running"}}, "running"},
		{[]Container{{State: "running", Health: "starting"}}, "starting"},
		{[]Container{{State: "running", Health: "unhealthy"}}, "error"},
		{[]Container{{State: "running"}, {State: "exited", Status: "Exited (0) 1 minute ago"}}, "partial"},
		{[]Container{{State: "running"}, {State: "exited", Status: "Exited (1) 1 minute ago"}}, "error"},
		{[]Container{{State: "restarting"}}, "error"},
		{[]Container{{State: "exited", Status: "Exited (0) 2 hours ago"}}, "stopped"},
	}
	for i, c := range cases {
		if got := appStatus(c.cs); got != c.want {
			t.Errorf("case %d: got %s want %s", i, got, c.want)
		}
	}
}

func TestStoreSetupAndLogin(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.NeedsSetup() {
		t.Fatal("fresh store should need setup")
	}
	tok, err := s.EnsureSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Setup("WRONG", "admin", "password123"); err == nil {
		t.Fatal("wrong setup code accepted")
	}
	if err := s.Setup(tok, "admin", "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := s.Setup(tok, "admin", "password123"); err != nil {
		t.Fatal(err)
	}
	if err := s.Setup(tok, "admin2", "password123"); err == nil {
		t.Fatal("second setup should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "setup-token")); !os.IsNotExist(err) {
		t.Fatal("setup token file should be removed after setup")
	}
	s2, _ := LoadStore(dir) // survives restart
	if !s2.CheckPassword("admin", "password123") || s2.CheckPassword("admin", "nope") || s2.CheckPassword("ghost", "password123") {
		t.Fatal("password check wrong")
	}
	if err := s2.ChangePassword("admin", "password123", "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if !s2.CheckPassword("admin", "newpassword1") {
		t.Fatal("new password not saved")
	}
	info, _ := os.Stat(filepath.Join(dir, "state.json"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("state.json should be private, got %v", info.Mode().Perm())
	}
}

func TestLoginLimiter(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < maxFails-1; i++ {
		l.Fail("1.2.3.4")
	}
	if b, _ := l.Blocked("1.2.3.4"); b {
		t.Fatal("blocked too early")
	}
	l.Fail("1.2.3.4")
	if b, left := l.Blocked("1.2.3.4"); !b || left <= 0 || left > lockoutTime {
		t.Fatal("should be blocked")
	}
	if b, _ := l.Blocked("5.6.7.8"); b {
		t.Fatal("other IPs must not be blocked")
	}
}

func TestSessionsPersistHashedTokens(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sessions.json")
	s := NewSessions(p)
	tok := s.Create("admin")
	tv := s.CreateTV("admin")
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), tok) || strings.Contains(string(b), tv) {
		t.Fatal("raw session tokens must never be written to disk")
	}
	s2 := NewSessions(p) // simulates a restart
	if u, ok := s2.Get(tok); !ok || u != "admin" {
		t.Fatal("session lost after restart")
	}
	if _, ok := s2.Get(tv); !ok {
		t.Fatal("TV session lost after restart")
	}
	s2.DeleteUserExcept("admin", tok)
	if _, ok := NewSessions(p).Get(tv); ok {
		t.Fatal("signing out other devices must also sign out the TV")
	}
}

func TestTVPairing(t *testing.T) {
	p := NewPairings()
	code, id, err := p.Start()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 7 || code[3] != '-' {
		t.Fatalf("unexpected code format %q", code)
	}
	if st, _ := p.Poll(id); st != "pending" {
		t.Fatalf("got %s", st)
	}
	if p.Approve("AAA-AAA", "admin") && !strings.EqualFold(code, "AAA-AAA") {
		t.Fatal("wrong code approved")
	}
	// codes are typed loosely on phones: lowercase, no dash, spaces
	loose := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	if !p.Approve(loose, "admin") {
		t.Fatal("valid code rejected")
	}
	if p.Approve(code, "someone-else") {
		t.Fatal("a code must only be approved once")
	}
	if st, u := p.Poll(id); st != "approved" || u != "admin" {
		t.Fatalf("got %s %s", st, u)
	}
	if st, _ := p.Poll(id); st != "expired" {
		t.Fatal("an approved pairing must be consumed")
	}
	if st, _ := p.Poll(strings.Repeat("0", 64)); st != "expired" {
		t.Fatal("unknown pairing must not be pending")
	}
	for i := 0; i < maxPendingPairs+1; i++ {
		_, _, err = p.Start()
	}
	if err == nil {
		t.Fatal("pending pairings must be capped")
	}
}

func TestSessions(t *testing.T) {
	s := NewSessions("")
	a := s.Create("admin")
	b := s.Create("admin")
	if u, ok := s.Get(a); !ok || u != "admin" {
		t.Fatal("session not found")
	}
	s.DeleteUserExcept("admin", a)
	if _, ok := s.Get(b); ok {
		t.Fatal("other session should be removed")
	}
	if _, ok := s.Get(a); !ok {
		t.Fatal("kept session removed")
	}
	s.mu.Lock()
	s.m[hashToken(a)] = session{User: "admin", Exp: time.Now().Add(-time.Second)}
	s.mu.Unlock()
	if _, ok := s.Get(a); ok {
		t.Fatal("expired session accepted")
	}
}

func TestJobWriterSplitsLines(t *testing.T) {
	j := &Job{}
	j.Write([]byte("one\ntw"))
	j.Write([]byte("o\rthree\n"))
	v := j.View(true)
	if strings.Join(v.Log, "|") != "one|two|three" {
		t.Errorf("got %v", v.Log)
	}
}

func TestParseHumanSize(t *testing.T) {
	mib := 4.289
	cases := map[string]uint64{
		"0B": 0, "512B": 512, "8.19kB": 8190, "4.289MiB": uint64(mib * (1 << 20)),
		" 1.5GiB ": 1 << 30 * 3 / 2, "2GB": 2e9, "nonsense": 0, "12XB": 0,
	}
	for in, want := range cases {
		if got := parseHumanSize(in); got != want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseStats(t *testing.T) {
	out := []byte(`{"CPUPerc":"12.50%","MemUsage":"4.289MiB / 3.813GiB","Name":"haven-ittools-it-tools-1"}
not json
{"CPUPerc":"0.00%","MemUsage":"0B / 0B","Name":"idle"}
`)
	s := parseStats(out)
	if len(s) != 2 {
		t.Fatalf("got %d entries", len(s))
	}
	mib := 4.289
	if c := s["haven-ittools-it-tools-1"]; c.cpu != 12.5 || c.mem != uint64(mib*(1<<20)) {
		t.Errorf("got %+v", c)
	}
}

func TestDirSize(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(root, "one"), make([]byte, 100), 0o644)
	os.WriteFile(filepath.Join(root, "a", "b", "two"), make([]byte, 250), 0o644)
	os.Symlink(filepath.Join(root, "one"), filepath.Join(root, "link")) // must not be counted
	if got := dirSize(root); got != 350 {
		t.Errorf("got %d, want 350", got)
	}
	if got := dirSize(filepath.Join(root, "missing")); got != 0 {
		t.Errorf("missing folder should be 0, got %d", got)
	}
}

func TestUsageAttach(t *testing.T) {
	u := NewUsageMonitor(t.TempDir(), &Docker{})
	u.cores, u.memTotal = 4, 1000
	u.stats = map[string]containerStats{"web": {cpu: 80, mem: 300}, "db": {cpu: 20, mem: 100}, "gone": {cpu: 50, mem: 999}}
	u.statsAt = time.Now()
	u.images["demo"] = 5000
	u.data["demo"] = dataSize{bytes: 42, at: time.Now()}
	a := InstalledApp{AppMeta: AppMeta{ID: "demo"}, Containers: []Container{
		{Name: "web", State: "running"}, {Name: "db", State: "running"}, {Name: "gone", State: "exited"},
	}}
	u.Attach(&a)
	us := a.Usage
	// 80% + 20% of one core on a 4-core server = 25% of the server
	if us.CPU != 25 || us.Mem != 400 || us.MemPercent != 40 || us.Images != 5000 || us.Data != 42 || !us.Live {
		t.Errorf("got %+v", *us)
	}
	if a.Containers[2].Mem != 0 {
		t.Error("stopped containers must not report memory")
	}
}

func TestParseBusctlString(t *testing.T) {
	if got := parseBusctlString(`s "ignore"`); got != "ignore" {
		t.Errorf("got %q", got)
	}
	if got := parseBusctlString("garbage"); got != "unknown" {
		t.Errorf("got %q", got)
	}
}

func TestParseAvahiHostName(t *testing.T) {
	log := "Found user 'avahi'\nServer startup complete. Host name is haven.local. Local service cookie is 123.\nHost name conflict, retrying with haven-2\nServer startup complete. Host name is haven-2.local. Local service cookie is 456."
	// Avahi prints the cookie on the same line; we only want the name.
	if got := parseAvahiHostName(log); !strings.HasPrefix(got, "haven-2.local") {
		t.Errorf("got %q", got)
	}
}

func TestParseTailscaleStatus(t *testing.T) {
	sample := `{"Version":"1.80.2","BackendState":"Running","AuthURL":"",
	 "Self":{"HostName":"haven","DNSName":"haven.tail1234.ts.net.","TailscaleIPs":["100.101.102.103","fd7a:115c::1"],"KeyExpiry":"2027-04-01T10:00:00Z"},
	 "CurrentTailnet":{"Name":"me@example.com","MagicDNSSuffix":"tail1234.ts.net","MagicDNSEnabled":true},
	 "Peer":{"a":{"Online":true},"b":{"Online":false}}}`
	ts, err := parseTailscaleStatus([]byte(sample))
	if err != nil || ts.BackendState != "Running" || ts.Self.DNSName != "haven.tail1234.ts.net." || len(ts.Peer) != 2 || ts.Self.KeyExpiry == nil {
		t.Fatalf("bad parse: %+v %v", ts, err)
	}
	r := &Remote{port: "8484", port80: &ExtraListener{}}
	urls := r.urls(RemoteStatus{MagicDNS: true, DNSName: "haven.tail1234.ts.net", IPs: ts.Self.TailscaleIPs})
	want := []string{"http://haven:8484", "http://haven.tail1234.ts.net:8484", "http://100.101.102.103:8484"}
	if strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Errorf("urls = %v", urls)
	}
}

func TestKeepSettingsArgs(t *testing.T) {
	msg := "Error: changing settings via 'tailscale up' requires mentioning all\nnon-default flags. To proceed, either re-run your command with --reset or\nuse the command below to explicitly mention the current value of\nall non-default settings:\n\n\ttailscale up --advertise-exit-node --hostname=old\n"
	got := keepSettingsArgs(msg)
	if strings.Join(got, " ") != "up --advertise-exit-node --hostname=old" {
		t.Errorf("got %v", got)
	}
	if keepSettingsArgs("\ttailscale up --ok; rm -rf /") != nil {
		t.Error("anything that isn't a --flag must be refused")
	}
}

func TestAnalyzeLogs(t *testing.T) {
	logs := "starting\nopen /config/db.sqlite: permission denied\nlisten tcp 0.0.0.0:8080: bind: address already in use\nall good\n"
	fs := analyzeLogs(logs)
	titles := ""
	for _, f := range fs {
		titles += f.Title + "|"
	}
	if !strings.Contains(titles, "permission denied") || !strings.Contains(titles, "already in use") {
		t.Errorf("missing findings: %s", titles)
	}
	if clean := analyzeLogs("server started\nlistening on :80\n"); len(clean) != 1 || clean[0].Level != "ok" {
		t.Errorf("clean logs should give one ok finding, got %+v", clean)
	}
}

func TestExitMeaningAndPorts(t *testing.T) {
	if !strings.Contains(exitMeaning(137, false), "killed") || !strings.Contains(exitMeaning(0, true), "memory") {
		t.Error("exit code meanings wrong")
	}
	cases := map[string]int{
		"driver failed programming external connectivity on endpoint x: Bind for 0.0.0.0:8080 failed: port is already allocated": 8080,
		"listen tcp4 0.0.0.0:53: bind: address already in use":                                                                   53,
		"something else": 0,
	}
	for in, want := range cases {
		if got := portInError(in); got != want {
			t.Errorf("portInError(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestDiagnosisVerdictAndRedaction(t *testing.T) {
	d := &Diagnosis{Target: "Demo", Findings: []Finding{
		{Level: "ok", Area: "Server", Title: "Docker is running"},
		{Level: "bad", Area: "Logs", Title: "The disk is full", Evidence: []string{"login with password hunter2secret failed"}},
		{Level: "warn", Area: "Memory", Title: "Low memory"},
	}}
	finish(d, map[string]string{"ADMIN_PASSWORD": "hunter2secret", "WEB_PORT": "8080"})
	if d.Level != "bad" || d.Cause == nil || d.Cause.Title != "The disk is full" || d.Findings[0].Level != "bad" {
		t.Errorf("wrong verdict: %+v", d)
	}
	if strings.Contains(d.Report, "hunter2secret") {
		t.Error("report must not contain passwords")
	}
	if !strings.Contains(d.Report, "[hidden]") {
		t.Error("password should be replaced with [hidden]")
	}
}

func TestHostNameRule(t *testing.T) {
	for _, ok := range []string{"haven", "home-server", "a", "nas2"} {
		if !hostNameRe.MatchString(ok) {
			t.Errorf("%q should be allowed", ok)
		}
	}
	for _, bad := range []string{"", "-x", "x-", "Haven", "my server", "a.b", strings.Repeat("a", 40)} {
		if hostNameRe.MatchString(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestCauseBeforeSymptom(t *testing.T) {
	d := &Diagnosis{Findings: []Finding{
		{Level: "bad", Area: "Network", Title: "Nothing answers on port 8080"},
		{Level: "bad", Area: "Logs", Title: "Permission denied"},
	}}
	finish(d, nil)
	if d.Cause.Title != "Permission denied" {
		t.Errorf("cause should be the log problem, got %q", d.Cause.Title)
	}
	fs := analyzeLogs("open /data/x: permission denied\n")
	for _, f := range fs {
		if f.Level == "ok" {
			t.Error("must not say 'no errors' when a problem was found")
		}
	}
}

func TestParseNews(t *testing.T) {
	feed := `<?xml version="1.0"?><rss><channel>
	<item><title>Old story - The Hindu</title><link>https://news.google.com/a</link><pubDate>Fri, 23 Oct 2026 08:00:00 GMT</pubDate><source url="x">The Hindu</source></item>
	<item><title>New story - Deccan Herald</title><link>https://news.google.com/b</link><pubDate>Sat, 24 Oct 2026 08:00:00 GMT</pubDate><source url="y">Deccan Herald</source></item>
	<item><title>Bad link</title><link>javascript:alert(1)</link><pubDate>Sat, 24 Oct 2026 09:00:00 GMT</pubDate></item>
	</channel></rss>`
	items, err := parseNews([]byte(feed), 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("got %d items, %v", len(items), err)
	}
	if items[0].Title != "New story" || items[0].Source != "Deccan Herald" {
		t.Errorf("newest first with the source removed from the title, got %+v", items[0])
	}
}

func TestParseChart(t *testing.T) {
	b := []byte(`{"chart":{"result":[{"meta":{"symbol":"GC=F","currency":"USD","regularMarketPrice":4010,"shortName":"Gold Dec 26"},
	 "indicators":{"quote":[{"close":[3990,null,4060.4,4010]}]}}],"error":null}}`)
	q, err := parseChart(b)
	if err != nil {
		t.Fatal(err)
	}
	if q.Name != "Gold" || len(q.History) != 3 || q.Change > -50 || q.Change < -51 {
		t.Errorf("got %+v", q)
	}
	if _, err := parseChart([]byte(`{"chart":{"result":null,"error":{"description":"No data found, symbol may be delisted"}}}`)); err == nil {
		t.Error("expected an error for an unknown symbol")
	}
}

func TestCleanUI(t *testing.T) {
	u := UISettings{BackgroundTV: Background{Dim: 400, Speed: -1}, Widgets: WidgetSettings{Home: "inr", Currencies: []string{"usd"}, Markets: []string{" reliance.ns "}}}
	if err := cleanUI(&u); err != nil {
		t.Fatal(err)
	}
	if u.BackgroundTV.Dim != 90 || u.BackgroundTV.Speed != 0 || u.Widgets.Home != "INR" || u.Widgets.Currencies[0] != "USD" || u.Widgets.Markets[0] != "RELIANCE.NS" {
		t.Errorf("not cleaned: %+v", u)
	}
	bad := UISettings{Widgets: WidgetSettings{Home: "INR", Markets: []string{"rm -rf /"}}}
	if cleanUI(&bad) == nil {
		t.Error("a symbol with spaces must be refused")
	}
	if d := defaultWidgetSettings(); d.Home == "" || len(d.Markets) == 0 {
		t.Errorf("bad defaults %+v", d)
	}
}

func TestSplitTerse(t *testing.T) {
	got := splitTerse(`*:My\:Cafe\\WiFi:78:WPA2`)
	want := []string{"*", `My:Cafe\WiFi`, "78", "WPA2"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q", got)
	}
}

func TestParseWifiList(t *testing.T) {
	out := " :Home:60:WPA2\n :Home:82:WPA2\n*:Neighbour:40:WPA2 WPA3\n :Cafe:55:--\n :: 30:WPA2"
	nets := parseWifiList(out)
	if len(nets) != 3 || nets[0].SSID != "Neighbour" || !nets[0].InUse {
		t.Fatalf("got %+v", nets)
	}
	if nets[1].SSID != "Home" || nets[1].Signal != 82 || nets[2].Security != "" {
		t.Errorf("strongest duplicate kept, open network has empty security: %+v", nets)
	}
}

func TestValidWifiAndStatic(t *testing.T) {
	if validWifi("", "") == nil || validWifi("ok", "short") == nil || validWifi(strings.Repeat("x", 33), "") == nil || validWifi("ok", "good-password") != nil || validWifi("Open", "") != nil {
		t.Error("wifi validation wrong")
	}
	good := StaticIP{Address: "192.168.1.50", Prefix: 24, Gateway: "192.168.1.1"}
	if err := validStatic(&good); err != nil || len(good.DNS) != 2 {
		t.Errorf("good static rejected: %v %+v", err, good)
	}
	for _, bad := range []StaticIP{
		{Address: "192.168.2.50", Prefix: 24, Gateway: "192.168.1.1"},
		{Address: "192.168.1.1", Prefix: 24, Gateway: "192.168.1.1"},
		{Address: "nope", Prefix: 24, Gateway: "192.168.1.1"},
		{Address: "192.168.1.50", Prefix: 31, Gateway: "192.168.1.1"},
		{Address: "192.168.1.50", Prefix: 24, Gateway: "192.168.1.1", DNS: []string{"x"}},
	} {
		b := bad
		if validStatic(&b) == nil {
			t.Errorf("should be rejected: %+v", bad)
		}
	}
	if err := validStatic(&StaticIP{Auto: true}); err != nil {
		t.Error("auto needs no address")
	}
}

func TestAccessLogNoiseAndSilentConns(t *testing.T) {
	a := NewAccessLog()
	h := a.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			http.Error(w, "no", 500)
			return
		}
		w.Write([]byte("hello"))
	}), "8484")
	for _, p := range []string{"/", "/assets/x.js", "/api/apps", "/api/auth/status", "/boom"} {
		req := httptest.NewRequest("GET", p+"?secret=1", nil)
		req.Host = "Haven.local:8484"
		req.RemoteAddr = "192.168.1.9:5555"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	s := a.Snapshot(50)
	if len(s.Entries) != 3 || s.Hidden != 2 {
		t.Fatalf("want 3 listed and 2 hidden, got %d and %d", len(s.Entries), s.Hidden)
	}
	if s.Entries[0].Path != "/boom" || s.Entries[0].Status != 500 || s.Entries[0].Host != "haven.local:8484" || strings.Contains(s.Entries[0].Path, "secret") {
		t.Errorf("bad entry %+v", s.Entries[0])
	}
	if len(s.Clients) != 1 || s.Clients[0].Requests != 5 || s.Clients[0].Errors != 1 || !a.RecentLocalHit(time.Minute) {
		t.Errorf("bad client info %+v", s.Clients)
	}
	// a connection that never speaks is reported while still open
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	cs := a.ConnState("80")
	cs(c1, http.StateNew)
	a.mu.Lock()
	for _, p := range a.pending {
		p.at = time.Now().Add(-20 * time.Second)
	}
	a.mu.Unlock()
	a.Sweep()
	a.Sweep() // must not report twice
	cs(c1, http.StateClosed)
	if ev := a.Snapshot(1).Events; len(ev) != 1 || ev[0].Port != "80" {
		t.Errorf("want exactly one silent-connection event, got %+v", ev)
	}
}

func TestSetIniKey(t *testing.T) {
	in := "[server]\n#allow-interfaces=eth0\nhost-name=x\n[wide-area]\nenable-wide-area=yes\n"
	out := setIniKey(in, "server", "allow-interfaces", "wlo1")
	if !strings.Contains(out, "\nallow-interfaces=wlo1\n") || strings.Contains(out, "#allow-interfaces") || !strings.Contains(out, "enable-wide-area=yes") {
		t.Errorf("replace failed:\n%s", out)
	}
	out = setIniKey("[server]\nhost-name=x\n[wide-area]\nfoo=1\n", "server", "allow-interfaces", "a,b")
	if strings.Index(out, "allow-interfaces=a,b") > strings.Index(out, "[wide-area]") {
		t.Errorf("must be added inside [server]:\n%s", out)
	}
	if out2 := setIniKey("[other]\nx=1\n", "server", "k", "v"); !strings.Contains(out2, "[server]\nk=v") {
		t.Errorf("missing section must be added:\n%s", out2)
	}
}

func TestMultiCitySettings(t *testing.T) {
	old := WidgetSettings{Home: "INR", Place: &Place{Name: "Pune", Lat: 18.5, Lon: 73.8}}
	old.normalize()
	if len(old.Places) != 1 || old.Place != nil || old.Places[0].Name != "Pune" {
		t.Fatalf("single city not migrated: %+v", old)
	}
	u := UISettings{Widgets: WidgetSettings{Home: "INR"}}
	for i := 0; i < 6; i++ {
		u.Widgets.Places = append(u.Widgets.Places, Place{Name: "City", Lat: 1, Lon: 1})
	}
	if cleanUI(&u) == nil {
		t.Error("more than 5 cities must be refused")
	}
	u.Widgets.Places = u.Widgets.Places[:2]
	u.Widgets.Places[1].Lat = 123
	if cleanUI(&u) == nil {
		t.Error("an invalid coordinate must be refused")
	}
	r := collect([]*Weather{{Place: "A"}, nil}, []string{"", "can't reach x"})
	if r.Data == nil || len(*r.Data) != 1 || r.Error == "" {
		t.Errorf("collect must keep good cities and report the error: %+v", r)
	}
}

func TestFeedParsingAndSafety(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>Daily &amp; Co</title>
	<item><title>One</title><link>https://x.test/1</link><description>&lt;p&gt;Hello &lt;b&gt;world&lt;/b&gt; &amp;amp; more&lt;/p&gt;</description><pubDate>Sat, 24 Oct 2026 08:00:00 +0530</pubDate></item>
	<item><title>No link</title></item></channel></rss>`
	title, items, err := parseFeed([]byte(rss), 10)
	if err != nil || title != "Daily & Co" || len(items) != 1 || items[0].Summary != "Hello world & more" || items[0].Published.IsZero() {
		t.Fatalf("rss: %q %+v %v", title, items, err)
	}
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><title>A Feed</title><entry><title>Entry</title><link rel="alternate" href="https://y.test/e"/><summary>Sum</summary><updated>2026-10-24T08:00:00Z</updated></entry></feed>`
	title, items, err = parseFeed([]byte(atom), 10)
	if err != nil || title != "A Feed" || len(items) != 1 || items[0].Link != "https://y.test/e" {
		t.Fatalf("atom: %q %+v %v", title, items, err)
	}
	if _, _, err := parseFeed([]byte("<html><body>not a feed</body></html>"), 10); err == nil {
		t.Error("a web page is not a feed")
	}
	for _, bad := range []string{"http://x.test/f", "ftp://x.test", "https://user:pw@x.test/f", "not a url", ""} {
		if validFeedURL(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	for ip, want := range map[string]bool{"8.8.8.8": true, "127.0.0.1": false, "192.168.1.10": false, "10.0.0.5": false, "169.254.1.1": false, "100.101.102.103": false, "::1": false, "fe80::1": false, "2606:4700::1111": true} {
		if isPublicIP(net.ParseIP(ip)) != want {
			t.Errorf("isPublicIP(%s) should be %v", ip, want)
		}
	}
	// the safe client must refuse a local server even when asked politely
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer srv.Close()
	if _, err := NewWidgets().getWith(context.Background(), publicClient(), srv.URL); err == nil {
		t.Error("the feed client must not reach servers on this machine or network")
	}
}

// Every catalog app, with its default settings, must be accepted by Docker
// Compose itself. Skipped when Docker isn't installed.
func TestCatalogComposeRendersWithDocker(t *testing.T) {
	if exec.Command("docker", "compose", "version").Run() != nil {
		t.Skip("docker compose not available")
	}
	c, _ := LoadCatalog()
	for _, a := range c.All() {
		dir := t.TempDir()
		env := &envFile{vals: map[string]string{}}
		env.set("APP_DATA", filepath.Join(dir, "data"))
		env.set("PUID", "1000")
		env.set("PGID", "1000")
		env.set("TZ", "UTC")
		for _, s := range a.Settings {
			v := s.Default
			if v == "" {
				v = "dummyvalue123"
			}
			env.set(s.Key, v)
		}
		os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(a.Compose), 0o644)
		env.write(filepath.Join(dir, ".env"))
		out, err := exec.Command("docker", "compose", "--project-directory", dir, "-f", filepath.Join(dir, "compose.yml"),
			"--env-file", filepath.Join(dir, ".env"), "-p", "haven-"+a.ID, "config", "--quiet").CombinedOutput()
		if err != nil {
			t.Errorf("%s: %s", a.ID, strings.TrimSpace(string(out)))
		}
	}
}

func TestParseImageRef(t *testing.T) {
	cases := map[string]imageRef{
		"nginx":                        {"registry-1.docker.io", "library/nginx", "latest"},
		"nginx:1.27":                   {"registry-1.docker.io", "library/nginx", "1.27"},
		"louislam/uptime-kuma:1":       {"registry-1.docker.io", "louislam/uptime-kuma", "1"},
		"ghcr.io/immich-app/server:v3": {"ghcr.io", "immich-app/server", "v3"},
		"lscr.io/linuxserver/sonarr":   {"ghcr.io", "linuxserver/sonarr", "latest"},
		"docker.io/valkey/valkey:9":    {"registry-1.docker.io", "valkey/valkey", "9"},
		"localhost:5000/team/app:dev":  {"localhost:5000", "team/app", "dev"},
	}
	for in, want := range cases {
		got, ok := parseImageRef(in)
		if !ok || got != want {
			t.Errorf("%s: got %+v ok=%v, want %+v", in, got, ok, want)
		}
	}
	if _, ok := parseImageRef("nginx@sha256:abc"); ok {
		t.Error("an image pinned by digest never changes and must not be checked")
	}
}

func TestParseChallengeAndDigestIn(t *testing.T) {
	realm, service, scope := parseChallenge(`Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`)
	if realm != "https://auth.docker.io/token" || service != "registry.docker.io" || scope != "repository:library/nginx:pull" {
		t.Errorf("got %q %q %q", realm, service, scope)
	}
	if !digestIn("sha256:aaa", []string{"nginx@sha256:bbb", "docker.io/library/nginx@sha256:aaa"}) || digestIn("sha256:zzz", []string{"nginx@sha256:bbb"}) || digestIn("sha256:aaa", nil) {
		t.Error("digestIn wrong")
	}
}

func TestRegistryDigestWithTokenFlow(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Write([]byte(`{"token":"tok123"}`))
		case strings.HasPrefix(r.URL.Path, "/v2/team/app/manifests/"):
			if r.Header.Get("Authorization") != "Bearer tok123" {
				w.Header().Set("Www-Authenticate", `Bearer realm="`+srv.URL+`/token",service="x"`)
				w.WriteHeader(401)
				return
			}
			w.Header().Set("Docker-Content-Digest", "sha256:feed")
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := newRegistryClient()
	c.http = srv.Client()
	host := strings.TrimPrefix(srv.URL, "https://")
	d, err := c.Digest(context.Background(), imageRef{host: host, repo: "team/app", tag: "latest"})
	if err != nil || d != "sha256:feed" {
		t.Fatalf("got %q %v", d, err)
	}
	if _, err := c.Digest(context.Background(), imageRef{host: host, repo: "team/missing", tag: "x"}); err == nil {
		t.Error("an unknown image must be an error")
	}
}

func TestNotifierDedupeReadClearPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.json")
	n := NewNotifier(p)
	n.Add("warn", "Disk is full", "", "disk:/:d1", "updates")
	n.Add("warn", "Disk is full", "", "disk:/:d1", "updates") // same key: ignored
	n.Add("bad", "App failed", "why", "", "apps")
	items, unread := n.List()
	if len(items) != 2 || unread != 2 || items[0].Title != "App failed" {
		t.Fatalf("got %+v unread=%d", items, unread)
	}
	n.MarkRead([]uint64{items[0].ID}, false)
	if _, u := n.List(); u != 1 {
		t.Errorf("unread should be 1, got %d", u)
	}
	n2 := NewNotifier(p) // survives a restart
	if it, u := n2.List(); len(it) != 2 || u != 1 {
		t.Errorf("not persisted: %+v %d", it, u)
	}
	n2.Clear(false) // removes read ones only
	if it, _ := n2.List(); len(it) != 1 || it[0].Title != "Disk is full" {
		t.Errorf("clear read failed: %+v", it)
	}
	n2.Add("warn", "Disk is full", "", "disk:/:d1", "updates") // same key again: now ignored (still present)
	if it, _ := n2.List(); len(it) != 1 {
		t.Error("key still present, must not duplicate")
	}
	n2.Clear(true)
	n2.Add("warn", "Disk is full", "", "disk:/:d1", "updates") // cleared: reported again
	if it, _ := n2.List(); len(it) != 1 {
		t.Error("after clearing, the same problem is reported again")
	}
	for i := 0; i < maxNotifications+30; i++ {
		n2.Add("info", "x", "", "", "")
	}
	if it, _ := n2.List(); len(it) != maxNotifications {
		t.Errorf("list must be capped, got %d", len(it))
	}
}

func TestNotifyOnJobDone(t *testing.T) {
	n := NewNotifier("")
	jobs := NewJobs()
	jobs.OnDone = n.jobDone
	var wg sync.WaitGroup
	wg.Add(1)
	jobs.Start("immich", "update", time.Minute, func(ctx context.Context, j *Job) error { defer wg.Done(); return errors.New("health check failed") })
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	it, _ := n.List()
	if len(it) != 1 || it[0].Level != "bad" || !strings.Contains(it[0].Title, "immich") || it[0].Route != "updates" {
		t.Errorf("got %+v", it)
	}
	// quiet actions don't notify on success
	wg.Add(1)
	jobs.Start("immich", "restart", time.Minute, func(ctx context.Context, j *Job) error { defer wg.Done(); return nil })
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	if it, _ := n.List(); len(it) != 1 {
		t.Errorf("a successful restart must stay silent, got %d", len(it))
	}
}

func TestPullFailureExplainsRateLimit(t *testing.T) {
	j := &Job{}
	j.Write([]byte("Error response from daemon: unknown: failed to resolve reference: 429 Too Many Requests\n"))
	if err := pullFailure(j, "fallback"); !strings.Contains(err.Error(), "Docker Hub is limiting") || !strings.Contains(err.Error(), "docker login") {
		t.Errorf("got %v", err)
	}
	j2 := &Job{}
	j2.Write([]byte("write /var/lib/docker: no space left on device\n"))
	if err := pullFailure(j2, "fallback"); !strings.Contains(err.Error(), "disk is full") {
		t.Errorf("got %v", err)
	}
	if err := pullFailure(&Job{}, "fallback"); err.Error() != "fallback" {
		t.Error("unknown failures keep the plain message")
	}
}

func TestExplainFailure(t *testing.T) {
	h := explainFailure([]string{"toomanyrequests: You have reached your unauthenticated pull rate limit"}, "pull failed")
	if h.Command != "sudo docker login" {
		t.Fatalf("rate limit not explained: %+v", h)
	}
	if h := explainFailure([]string{"write: no space left on device"}, ""); h.Title != "The disk is full" {
		t.Fatalf("disk: %+v", h)
	}
	if h := explainFailure([]string{"???"}, "x"); h.Title != "Something went wrong" || h.Fix == "" {
		t.Fatalf("fallback: %+v", h)
	}
}

func TestJobsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	js := NewJobsAt(path)
	j := js.Start("x", "install", time.Minute, func(ctx context.Context, j *Job) error {
		j.Logf("hello")
		return errors.New("boom")
	})
	for i := 0; i < 100 && j.View(false).Status == "running"; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	back, ok := NewJobsAt(path).Get(j.ID)
	if !ok || back.View(true).Status != "failed" || len(back.View(true).Log) < 2 {
		t.Fatalf("job not restored: %v %+v", ok, back)
	}
}

func TestFilesSafeRootsAndSymlinks(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	st, _ := LoadStore(dir)
	f := NewFiles(st, dir)
	app := filepath.Join(dir, "appdata", "x")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Resolve(app); err != nil {
		t.Fatalf("app data should be allowed: %v", err)
	}
	if _, err := f.Resolve(filepath.Join(dir, "state.json")); err == nil {
		t.Fatal("haven state must be outside safe roots")
	}
	if _, err := f.Resolve("/etc/passwd"); err == nil {
		t.Fatal("/etc must be blocked in safe mode")
	}
	if _, err := f.Resolve(app + "/../../state.json"); err == nil {
		t.Fatal("dot-dot escape must be blocked")
	}
	link := filepath.Join(app, "out")
	if err := os.Symlink("/etc", link); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Resolve(link + "/passwd"); err == nil {
		t.Fatal("symlink escape must be blocked")
	}
	if _, err := f.Resolve("relative/path"); err == nil {
		t.Fatal("relative path must be refused")
	}
	// operations
	if err := f.Mkdir(filepath.Join(app, "new")); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteText(filepath.Join(app, "new", "a.txt"), "hi"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ReadText(filepath.Join(app, "new", "a.txt")); got != "hi" {
		t.Fatalf("read back %q", got)
	}
	if err := f.Move([]string{filepath.Join(app, "new")}, filepath.Join(app, "new", "sub"), true); err == nil {
		t.Fatal("copying a folder into itself must fail")
	}
	if err := f.Delete([]string{filepath.Join(dir, "appdata")}); err == nil {
		t.Fatal("deleting the app data root must be refused")
	}
	if err := f.Delete([]string{filepath.Join(app, "new")}); err != nil {
		t.Fatal(err)
	}
	// full-disk mode
	_ = st.UpdateSettings(func(h *HostSettings) { h.FilesFullDisk = true })
	if _, err := f.Resolve("/etc/passwd"); err == nil {
		t.Fatal("full-disk must not apply to non-admins")
	}
	f = f.As(true)
	if _, err := f.Resolve("/etc/passwd"); err != nil {
		t.Fatalf("full mode should allow /etc: %v", err)
	}
	for _, p := range []string{"/etc", "/bin", "/usr/bin", "/var/lib", "/home", "/"} {
		if real, err := f.Resolve(p); err != nil || !f.protected(real) {
			t.Fatalf("%s must stay protected in full mode (resolved %q)", p, real)
		}
	}
}

// roleServer builds a real Haven server with one admin and one person per role.
func roleServer(t *testing.T) (*httptest.Server, map[string]*http.Client, *Store) {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, sub := range []string{"apps", "appdata"} {
		_ = os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}
	st, _ := LoadStore(dir)
	tok, _ := st.EnsureSetupToken()
	if err := st.Setup(tok, "boss", "bosspass123"); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs()
	docker := &Docker{}
	apps := NewManager(dir, docker, cat, jobs)
	srv := NewServer(st, NewSessions(""), apps, cat, NewSystemMonitor(dir, docker), jobs, docker, fstest.MapFS{"index.html": {Data: []byte("x")}})
	srv.files = NewFiles(st, dir)
	srv.cleaner = NewCleaner(docker, jobs)
	srv.checks = NewStatusChecker(st, nil)
	srv.notes = NewNotifier("")
	srv.updates = NewUpdateChecker(apps)
	srv.access = NewAccessLog()
	hs := httptest.NewServer(srv.Routes())
	t.Cleanup(hs.Close)
	for _, p := range []struct{ n, role string }{{"mia", "manager"}, {"moe", "member"}, {"gus", "guest"}} {
		if _, err := st.AddPerson(p.n, "password123", p.role, []string{"jellyfin"}); err != nil {
			t.Fatal(err)
		}
	}
	clients := map[string]*http.Client{}
	for n, pw := range map[string]string{"boss": "bosspass123", "mia": "password123", "moe": "password123", "gus": "password123"} {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar}
		body := strings.NewReader(`{"username":"` + n + `","password":"` + pw + `"}`)
		req, _ := http.NewRequest("POST", hs.URL+"/api/auth/login", body)
		req.Header.Set("X-Haven", "1")
		resp, err := c.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("login %s failed: %v", n, err)
		}
		clients[n] = c
	}
	return hs, clients, st
}

func call(t *testing.T, c *http.Client, method, url, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("X-Haven", "1")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestRolesAreEnforced(t *testing.T) {
	hs, c, st := roleServer(t)
	type tc struct {
		method, path, body           string
		boss, manager, member, guest bool // true = allowed (not 401/403)
		denyOnly                     bool // don't run the real handler as admin (needs the host)
	}
	cases := []tc{
		{"GET", "/api/apps", "", true, true, true, true, false},
		{"GET", "/api/system", "", true, true, true, true, false},
		{"GET", "/api/catalog", "", true, true, false, false, false},
		{"GET", "/api/updates", "", true, true, false, false, false},
		{"GET", "/api/files/info", "", true, true, false, false, false},
		{"GET", "/api/jobs", "", true, true, false, false, false},
		{"POST", "/api/apps/custom", `{}`, true, false, false, false, false},
		{"GET", "/api/network", "", true, false, false, false, true},
		{"POST", "/api/files/access", `{}`, true, false, false, false, false},
		{"GET", "/api/cleanup/sizes?path=/srv", "", true, false, false, false, true},
		{"GET", "/api/users", "", true, false, false, false, false},
		{"POST", "/api/users", `{}`, true, false, false, false, false},
		{"PUT", "/api/ui", `{}`, true, false, false, false, false},
		{"POST", "/api/apps/other/start", "", true, true, false, false, false},
		{"POST", "/api/apps/jellyfin/stop", "", true, true, false, false, false},
		{"POST", "/api/apps/other/restart", "", true, true, false, false, false},
		{"POST", "/api/apps/jellyfin/restart", "", true, true, true, false, false},
	}
	allowed := func(code int) bool { return code != 401 && code != 403 }
	for _, k := range cases {
		for who, want := range map[string]bool{"boss": k.boss, "mia": k.manager, "moe": k.member, "gus": k.guest} {
			if who == "boss" && k.denyOnly {
				continue
			}
			got := allowed(call(t, c[who], k.method, hs.URL+k.path, k.body))
			if got != want {
				t.Errorf("%s %s as %s: allowed=%v want %v", k.method, k.path, who, got, want)
			}
		}
	}
	// A member restarting an app they weren't given is refused.
	if code := call(t, c["moe"], "POST", hs.URL+"/api/apps/other/restart", ""); code != 403 {
		t.Errorf("member restart of unshared app = %d, want 403", code)
	}
	// Disabling signs the person out at once.
	if code := call(t, c["boss"], "PUT", hs.URL+"/api/users/moe", `{"disabled":true}`); code != 200 {
		t.Fatalf("disable = %d", code)
	}
	if code := call(t, c["moe"], "GET", hs.URL+"/api/apps", ""); code != 401 {
		t.Errorf("disabled member still has access: %d", code)
	}
	// There is exactly one admin, and it can't be removed.
	if code := call(t, c["boss"], "DELETE", hs.URL+"/api/users/boss", ""); code == 200 {
		t.Error("admin was deleted")
	}
	if code := call(t, c["boss"], "POST", hs.URL+"/api/users", `{"username":"second","password":"password123","role":"admin"}`); code == 200 {
		t.Error("a second admin was created")
	}
	if code := call(t, c["boss"], "PUT", hs.URL+"/api/users/gus", `{"role":"admin"}`); code == 200 {
		t.Error("promoting to admin must be refused")
	}
	// Handing over the admin role keeps exactly one admin.
	if code := call(t, c["boss"], "POST", hs.URL+"/api/users/transfer", `{"to":"mia","password":"bosspass123"}`); code != 200 {
		t.Fatalf("transfer = %d", code)
	}
	admins := 0
	for _, p := range st.People() {
		if p.Role == RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Errorf("admins after transfer = %d, want 1", admins)
	}
	if code := call(t, c["boss"], "GET", hs.URL+"/api/users", ""); code != 403 {
		t.Errorf("old admin still administers: %d", code)
	}
}

func TestMembersOnlySeeTheirApps(t *testing.T) {
	_, _, st := roleServer(t)
	p, _ := st.Person("moe")
	if !allowedApp(p, "jellyfin") || allowedApp(p, "qbittorrent") {
		t.Fatal("member app list not honoured")
	}
	m, _ := st.Person("mia")
	if !allowedApp(m, "anything") {
		t.Fatal("manager should see every app")
	}
}

func TestVersionComparison(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		newer           bool
	}{
		{"0.2.0", "0.1.0", true},
		{"v0.2.0", "0.1.9", true},
		{"0.1.0", "0.1.0-dev", true},
		{"0.1.0-dev", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"1.0.0", "0.9.9", true},
		{"0.1.10", "0.1.9", true},
		{"0.1.0", "dev", false},
		{"nonsense", "0.1.0", false},
	} {
		if got := newerVersion(c.latest, c.current); got != c.newer {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.newer)
		}
	}
}

// fakeRelease serves a GitHub-shaped release with the given binary signed by key.
func fakeRelease(t *testing.T, key ed25519.PrivateKey, version string, binary []byte, corrupt string) *httptest.Server {
	t.Helper()
	arch := runtime.GOARCH
	name := "haven-linux-" + arch
	sum := sha256.Sum256(binary)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, sums))
	if corrupt == "sig" {
		sig = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte("something else")))
	}
	if corrupt == "binary" {
		binary = append([]byte("#!/bin/sh\necho tampered\n"), binary...)
	}
	var base string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		asset := func(n string) map[string]string {
			return map[string]string{"name": n, "url": base + "/repos/o/r/releases/assets/" + n}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v" + version, "body": "Faster\nand better", "html_url": "https://example.test/r",
			"assets": []map[string]string{asset(name), asset("SHA256SUMS"), asset("SHA256SUMS.sig")},
		})
	})
	mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, r *http.Request) {
		rel := func(v string, pre bool) map[string]any {
			return map[string]any{"tag_name": "v" + v, "body": "Notes for " + v, "html_url": "https://example.test/" + v, "prerelease": pre}
		}
		_ = json.NewEncoder(w).Encode([]any{rel(version, false), rel("9.9.7", false), rel("9.9.8", false), rel("9.9.6-rc1", true), rel("0.0.1", false)})
	})
	mux.HandleFunc("/repos/o/r/releases/assets/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/repos/o/r/releases/assets/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) { w.Write(sums) })
	mux.HandleFunc("/repos/o/r/releases/assets/SHA256SUMS.sig", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sig)) })
	hs := httptest.NewServer(mux)
	base = hs.URL
	t.Cleanup(hs.Close)
	return hs
}

func testUpdater(t *testing.T, hs *httptest.Server, pub ed25519.PublicKey) (*SelfUpdate, string, *bool) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "haven")
	if err := os.WriteFile(bin, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	exited := false
	u := &SelfUpdate{repo: "o/r", api: hs.URL, dir: dir, binPath: bin, pub: pub, client: &http.Client{}, jobs: NewJobs(),
		goos: "linux", exit: func() { exited = true }}
	return u, dir, &exited
}

func waitJob(t *testing.T, j *Job) JobView {
	t.Helper()
	for i := 0; i < 200; i++ {
		if v := j.View(true); v.Status != "running" {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return JobView{}
}

func TestSelfUpdateInstallsVerifiedRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	pub, key, _ := ed25519.GenerateKey(nil)
	script := []byte("#!/bin/sh\necho haven 9.9.9\n")
	hs := fakeRelease(t, key, "9.9.9", script, "")
	u, dir, _ := testUpdater(t, hs, pub)
	if s := u.Check(context.Background()); !s.Available || s.Latest != "9.9.9" || s.Notes == "" {
		t.Fatalf("check: %+v", s)
	}
	j, err := u.Apply()
	if err != nil {
		t.Fatal(err)
	}
	v := waitJob(t, j)
	if v.Status != "success" {
		t.Fatalf("update failed: %s\n%v", v.Error, v.Log)
	}
	if b, _ := os.ReadFile(u.binPath); string(b) != string(script) {
		t.Fatal("new binary not installed")
	}
	if b, _ := os.ReadFile(u.binPath + ".prev"); string(b) != "old binary" {
		t.Fatal("old binary not kept")
	}
	if _, err := os.Stat(markerPath(dir)); err != nil {
		t.Fatal("rollback marker missing")
	}
}

func TestSelfUpdateRefusesTamperedReleases(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	for _, corrupt := range []string{"sig", "binary"} {
		hs := fakeRelease(t, key, "9.9.9", []byte("#!/bin/sh\necho haven 9.9.9\n"), corrupt)
		u, _, exited := testUpdater(t, hs, pub)
		u.Check(context.Background())
		j, err := u.Apply()
		if err != nil {
			t.Fatal(err)
		}
		if v := waitJob(t, j); v.Status != "failed" {
			t.Fatalf("%s: tampered release was installed", corrupt)
		}
		if b, _ := os.ReadFile(u.binPath); string(b) != "old binary" || *exited {
			t.Fatalf("%s: the running binary was changed", corrupt)
		}
	}
	// A key that isn't Haven's is rejected as well.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	hs := fakeRelease(t, key, "9.9.9", []byte("#!/bin/sh\necho haven 9.9.9\n"), "")
	u, _, _ := testUpdater(t, hs, otherPub)
	u.Check(context.Background())
	j, _ := u.Apply()
	if v := waitJob(t, j); v.Status != "failed" {
		t.Fatal("release signed by the wrong key was installed")
	}
}

func TestSelfUpdateRefusesForeignAssetURLs(t *testing.T) {
	u := &SelfUpdate{repo: "o/r", api: "https://api.github.com"}
	rel := &ReleaseInfo{assets: map[string]string{"x": "https://evil.example/repos/o/r/releases/assets/1"}}
	if _, err := u.assetURL(rel, "x"); err == nil {
		t.Fatal("download from another host must be refused")
	}
}

func TestStartupGuardRollsBackACrashLoop(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "haven")
	_ = os.WriteFile(bin, []byte("new"), 0o755)
	_ = os.WriteFile(bin+".prev", []byte("old"), 0o755)
	old := Version
	Version = "2.0.0"
	defer func() { Version = old }()
	_ = writeMarker(dir, updateMarker{From: "1.0.0", To: "2.0.0", Prev: bin + ".prev", Bin: bin})
	for i := 1; i <= 2; i++ {
		if StartupGuard(dir) {
			t.Fatalf("rolled back too early at try %d", i)
		}
	}
	if !StartupGuard(dir) {
		t.Fatal("did not roll back after repeated failures")
	}
	if b, _ := os.ReadFile(bin); string(b) != "old" {
		t.Fatal("previous version not restored")
	}
	if _, err := os.Stat(markerPath(dir)); err == nil {
		t.Fatal("marker should be cleared after rollback")
	}
}

func TestReachReasonExplainsNetworkErrors(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}, "DNS"},
		{&net.DNSError{Err: "timeout", Name: "x", IsTimeout: true}, "DNS didn't answer"},
		{context.DeadlineExceeded, "timed out"},
		{syscall.ECONNREFUSED, "refused"},
		{syscall.ENETUNREACH, "no route"},
		{x509.UnknownAuthorityError{}, "certificate"},
		{errors.New("boom"), "failed"},
	} {
		if got := reachReason(c.err); !strings.Contains(got, c.want) {
			t.Errorf("reachReason(%v) = %q, want it to mention %q", c.err, got, c.want)
		}
	}
}

func TestWidgetTestOnlyContactsWidgetHosts(t *testing.T) {
	hs, c, _ := roleServer(t)
	if code := call(t, c["boss"], "GET", hs.URL+"/api/widgets/test?host=127.0.0.1", ""); code != 400 {
		t.Errorf("a host that isn't a widget source must be refused, got %d", code)
	}
	if code := call(t, c["mia"], "GET", hs.URL+"/api/widgets/test?host=news.google.com", ""); code != 403 {
		t.Errorf("only the admin may run the test, got %d", code)
	}
}

func TestWidgetOrderIsCleaned(t *testing.T) {
	w := WidgetSettings{Order: []string{"news", "bogus", "weather", "news"}, Hidden: []string{"markets", "x", "markets"}}
	w.normalize()
	if got := strings.Join(w.Order, ","); got != "news,weather,markets,currency,clock,notes,links,status" {
		t.Errorf("order = %s", got)
	}
	if got := strings.Join(w.Hidden, ","); got != "markets" {
		t.Errorf("hidden = %s", got)
	}
	empty := WidgetSettings{}
	empty.normalize()
	if got := strings.Join(empty.Order, ","); got != "weather,markets,currency,news,clock,notes,links,status" {
		t.Errorf("default order = %s", got)
	}
}

func TestPlainMarkdownForNotifications(t *testing.T) {
	for in, want := range map[string]string{
		"- Rearrange the **Overview** widgets":        "Rearrange the Overview widgets",
		"## Added `Edit layout` and [docs](http://x)": "Added Edit layout and docs",
		"plain text": "plain text",
	} {
		if got := plainMarkdown(in); got != want {
			t.Errorf("plainMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
	if got := firstLine("\n\n  - hello\nworld"); got != "- hello" {
		t.Errorf("firstLine = %q", got)
	}
}

func TestSafeRepoOnlyAllowsOwnerSlashName(t *testing.T) {
	for in, want := range map[string]string{"NaveenKharwar/haven": "NaveenKharwar/haven", "a/b.c-d_e": "a/b.c-d_e", "": "", "no-slash": "", "a/b/c": "", "a/b c": "", "javascript:alert(1)": "", "a/b?x=1": ""} {
		if got := safeRepo(in); got != want {
			t.Errorf("safeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtraWidgetSettingsAreValidated(t *testing.T) {
	ok := func(mut func(*WidgetSettings)) error {
		u := UISettings{Widgets: defaultWidgetSettings()}
		mut(&u.Widgets)
		return cleanUI(&u)
	}
	if err := ok(func(w *WidgetSettings) {
		w.Clocks = []string{"Asia/Kolkata", "America/New_York"}
		w.Notes = "milk"
		w.Links = []QuickLink{{"Router", "http://192.168.1.1"}}
		w.Checks = []StatusCheck{{"Router", "192.168.1.1:80"}, {"Site", "https://example.com/health"}}
	}); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	bad := map[string]func(*WidgetSettings){
		"bad zone":       func(w *WidgetSettings) { w.Clocks = []string{"Mars/Base"} },
		"4 clocks":       func(w *WidgetSettings) { w.Clocks = []string{"UTC", "UTC", "UTC", "UTC"} },
		"long note":      func(w *WidgetSettings) { w.Notes = strings.Repeat("x", 4001) },
		"js link":        func(w *WidgetSettings) { w.Links = []QuickLink{{"x", "javascript:alert(1)"}} },
		"link no name":   func(w *WidgetSettings) { w.Links = []QuickLink{{"", "https://a.b"}} },
		"link with auth": func(w *WidgetSettings) { w.Links = []QuickLink{{"x", "ftp://a.b"}} },
		"check bad":      func(w *WidgetSettings) { w.Checks = []StatusCheck{{"x", "not a target"}} },
		"check port":     func(w *WidgetSettings) { w.Checks = []StatusCheck{{"x", "host:99999"}} },
		"check dup":      func(w *WidgetSettings) { w.Checks = []StatusCheck{{"a", "h:80"}, {"b", "h:80"}} },
		"check userinfo": func(w *WidgetSettings) { w.Checks = []StatusCheck{{"x", "http://u:p@h"}} },
	}
	for name, mut := range bad {
		if ok(mut) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStatusProbeAndNotifications(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer up.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	if ok, _, _ := probe(context.Background(), up.URL); !ok {
		t.Error("an answering server (even 401) should be up")
	}
	if ok, _, why := probe(context.Background(), bad.URL); ok || why == "" {
		t.Error("a 500 should be down with a reason")
	}
	host := strings.TrimPrefix(up.URL, "http://")
	if ok, _, _ := probe(context.Background(), host); !ok {
		t.Error("an open TCP port should be up")
	}
	if ok, _, _ := probe(context.Background(), "127.0.0.1:1"); ok {
		t.Error("a closed port should be down")
	}

	dir := t.TempDir()
	st, _ := LoadStore(dir)
	u := st.UI()
	u.Widgets.Checks = []StatusCheck{{"Dead", "127.0.0.1:1"}}
	if err := cleanUI(&u); err != nil {
		t.Fatal(err)
	}
	_ = st.SetUI(u)
	var told []string
	c := NewStatusChecker(st, func(level, title, body, key, route string) { told = append(told, title) })
	c.once(context.Background())
	if len(told) != 0 {
		t.Fatal("one miss must not notify")
	}
	c.once(context.Background())
	c.once(context.Background())
	if len(told) != 1 || !strings.Contains(told[0], "is down") {
		t.Fatalf("expected one down notification, got %v", told)
	}
	if r := c.Results(u.Widgets.Checks); len(r) != 1 || r[0].Up || r[0].Pending {
		t.Fatalf("results: %+v", r)
	}
}

func TestUpdateShowsEverySkippedVersion(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	hs := fakeRelease(t, key, "9.9.9", []byte("#!/bin/sh\necho haven 9.9.9\n"), "")
	u, _, _ := testUpdater(t, hs, pub)
	s := u.Check(context.Background())
	var got []string
	for _, v := range s.Versions {
		got = append(got, v.Version)
	}
	if strings.Join(got, ",") != "9.9.9,9.9.8,9.9.7" {
		t.Fatalf("versions = %v, want newest first and only the ones being installed", got)
	}
	if s.Versions[0].Notes != "Notes for 9.9.9" || s.Versions[2].Notes != "Notes for 9.9.7" {
		t.Fatalf("notes not carried: %+v", s.Versions)
	}
}

// A minimal valid PNG header is enough for the content sniffer.
var tinyPNG = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 40)...)

func TestFaviconFindsAndRefusesIcons(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write(tinyPNG)
			return
		}
		http.NotFound(w, r)
	}))
	defer direct.Close()
	declared := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="stylesheet" href="/x.css"><link REL="shortcut icon" href="/static/logo.png"></head></html>`))
		case "/static/logo.png":
			w.Write(tinyPNG)
		default:
			http.NotFound(w, r)
		}
	}))
	defer declared.Close()
	notImage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><script>alert(1)</script></html>")) // even at /favicon.ico
	}))
	defer notImage.Close()
	svg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	}))
	defer svg.Close()

	w := NewWidgets()
	if b, ct, err := w.Favicon(context.Background(), direct.URL+"/some/page"); err != nil || ct != "image/png" || len(b) == 0 {
		t.Fatalf("direct favicon: %v %q", err, ct)
	}
	if _, ct, err := w.Favicon(context.Background(), declared.URL); err != nil || ct != "image/png" {
		t.Fatalf("declared favicon: %v %q", err, ct)
	}
	for name, srv := range map[string]*httptest.Server{"html": notImage, "svg": svg} {
		if _, _, err := w.Favicon(context.Background(), srv.URL); err == nil {
			t.Errorf("%s must not be served as an icon", name)
		}
	}
	if _, _, err := w.Favicon(context.Background(), "javascript:alert(1)"); err == nil {
		t.Error("a non-web address must be refused")
	}
}

func TestFaviconRouteOnlyServesConfiguredLinks(t *testing.T) {
	hs, c, st := roleServer(t)
	if code := call(t, c["mia"], "GET", hs.URL+"/api/widgets/favicon?i=0", ""); code != 404 {
		t.Errorf("no links configured: want 404, got %d", code)
	}
	u := st.UI()
	u.Widgets.Links = []QuickLink{{"A", "http://127.0.0.1:1"}}
	_ = st.SetUI(u)
	if code := call(t, c["mia"], "GET", hs.URL+"/api/widgets/favicon?i=5", ""); code != 404 {
		t.Errorf("index out of range: want 404, got %d", code)
	}
	if code := call(t, c["gus"], "GET", hs.URL+"/api/widgets/favicon?i=0", ""); code != 403 {
		t.Errorf("a guest must not reach it: got %d", code)
	}
}

func TestAssetURLIgnoresRepoCase(t *testing.T) {
	u := &SelfUpdate{repo: "NaveenKharwar/haven", api: githubAPI}
	rel := &ReleaseInfo{assets: map[string]string{"a": githubAPI + "/repos/NaveenKharwar/Haven/releases/assets/1", "b": githubAPI + "/repos/Someone/else/releases/assets/1"}}
	if _, err := u.assetURL(rel, "a"); err != nil {
		t.Fatalf("same repo, different case was refused: %v", err)
	}
	if _, err := u.assetURL(rel, "b"); err == nil {
		t.Fatal("another repository was accepted")
	}
}
