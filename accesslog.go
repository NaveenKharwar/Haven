package main

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Access log: what reached Haven, from where, under which name, and how it
// went. Kept in memory (it resets when Haven restarts). Haven's own page
// refreshing in the background is counted but not listed, so the list stays
// readable.
//
// It also notices connections that opened but never sent a request. That is
// what a page "loading forever" often looks like from the server's side.
// If nothing shows up at all while a page spins, the request never arrived:
// the problem is the name lookup or the network, not Haven.

type AccessEntry struct {
	Seq    uint64    `json:"seq"`
	Time   time.Time `json:"time"`
	IP     string    `json:"ip"`
	Host   string    `json:"host"` // the name or address the browser used
	Port   string    `json:"port"` // which Haven listener: 8484, 80
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
	Ms     int64     `json:"ms"`
	Bytes  int64     `json:"bytes"`
	Agent  string    `json:"agent"`
}

type ConnEvent struct {
	Seq    uint64    `json:"seq"`
	Time   time.Time `json:"time"`
	IP     string    `json:"ip"`
	Port   string    `json:"port"`
	Kind   string    `json:"kind"` // silent (opened, sent nothing)
	Waited int64     `json:"waitedMs"`
	Note   string    `json:"note"`
}

type ClientInfo struct {
	IP       string    `json:"ip"`
	LastSeen time.Time `json:"lastSeen"`
	Hosts    []string  `json:"hosts"`
	Requests int       `json:"requests"`
	Errors   int       `json:"errors"`
	Agent    string    `json:"agent"`
}

type AccessSnapshot struct {
	Entries []AccessEntry `json:"entries"`
	Events  []ConnEvent   `json:"events"`
	Clients []ClientInfo  `json:"clients"`
	Hidden  uint64        `json:"hidden"` // background refreshes not listed
	Since   time.Time     `json:"since"`
}

type pendingConn struct {
	at       time.Time
	ip, port string
	reported bool
}

type AccessLog struct {
	mu      sync.Mutex
	seq     uint64
	entries []AccessEntry
	events  []ConnEvent
	clients map[string]*ClientInfo
	hidden  uint64
	since   time.Time
	pending map[net.Conn]*pendingConn
}

const (
	maxEntries  = 800
	maxEvents   = 100
	silentAfter = 8 * time.Second
)

func NewAccessLog() *AccessLog {
	return &AccessLog{clients: map[string]*ClientInfo{}, pending: map[net.Conn]*pendingConn{}, since: time.Now()}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(c int) {
	if w.status == 0 {
		w.status = c
	}
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Wrap records every request that passes through h. port names the listener.
func (a *AccessLog) Wrap(h http.Handler, port string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		a.record(AccessEntry{
			Time: start, IP: clientIP(r), Host: cleanHost(r.Host), Port: port, Method: r.Method,
			Path: r.URL.Path, Status: sw.status, Ms: time.Since(start).Milliseconds(), Bytes: sw.bytes, Agent: shortAgent(r.UserAgent()),
		})
	})
}

func cleanHost(h string) string {
	if len(h) > 80 {
		h = h[:80]
	}
	return strings.ToLower(h)
}

// shortAgent keeps just enough of the browser string to tell devices apart.
func shortAgent(ua string) string {
	switch {
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	case strings.Contains(ua, "curl"):
		return "curl"
	}
	if len(ua) > 30 {
		ua = ua[:30]
	}
	return ua
}

// isNoise marks Haven's own background polling, which would otherwise bury
// the interesting requests. Errors are never hidden.
func isNoise(e AccessEntry) bool {
	if e.Method != http.MethodGet || e.Status >= 400 {
		return false
	}
	if strings.HasPrefix(e.Path, "/assets/") || e.Path == "/favicon.svg" {
		return true
	}
	return strings.HasPrefix(e.Path, "/api/") && e.Path != "/api/auth/status"
}

func (a *AccessLog) record(e AccessEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.clients[e.IP]
	if c == nil {
		c = &ClientInfo{IP: e.IP}
		a.clients[e.IP] = c
		if len(a.clients) > 200 { // forget the oldest, keep memory bounded
			var oldest string
			var t time.Time
			for ip, x := range a.clients {
				if oldest == "" || x.LastSeen.Before(t) {
					oldest, t = ip, x.LastSeen
				}
			}
			delete(a.clients, oldest)
		}
	}
	c.LastSeen, c.Requests, c.Agent = e.Time, c.Requests+1, e.Agent
	if e.Status >= 400 {
		c.Errors++
	}
	known := false
	for _, h := range c.Hosts {
		known = known || h == e.Host
	}
	if !known && len(c.Hosts) < 5 {
		c.Hosts = append(c.Hosts, e.Host)
	}
	if isNoise(e) {
		a.hidden++
		return
	}
	a.seq++
	e.Seq = a.seq
	a.entries = append(a.entries, e)
	if len(a.entries) > maxEntries {
		a.entries = a.entries[len(a.entries)-maxEntries:]
	}
}

func (a *AccessLog) addEvent(ev ConnEvent) {
	a.seq++
	ev.Seq = a.seq
	a.events = append(a.events, ev)
	if len(a.events) > maxEvents {
		a.events = a.events[len(a.events)-maxEvents:]
	}
}

// ConnState is installed on each http.Server to watch connections that never
// say anything.
func (a *AccessLog) ConnState(port string) func(net.Conn, http.ConnState) {
	return func(c net.Conn, s http.ConnState) {
		a.mu.Lock()
		defer a.mu.Unlock()
		switch s {
		case http.StateNew:
			ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
			a.pending[c] = &pendingConn{at: time.Now(), ip: ip, port: port}
		case http.StateActive, http.StateHijacked:
			delete(a.pending, c)
		case http.StateClosed:
			if p := a.pending[c]; p != nil {
				delete(a.pending, c)
				waited := time.Since(p.at)
				if !p.reported && waited > time.Second {
					a.addEvent(ConnEvent{Time: p.at, IP: p.ip, Port: p.port, Kind: "silent", Waited: waited.Milliseconds(),
						Note: "Connected, sent no request, then closed. The browser gave up or the connection was cut."})
				}
			}
		}
	}
}

// Sweep reports connections that have been silent for a while, while they are
// still open, so a hanging page shows up as it happens.
func (a *AccessLog) Sweep() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		if !p.reported && time.Since(p.at) > silentAfter {
			p.reported = true
			a.addEvent(ConnEvent{Time: p.at, IP: p.ip, Port: p.port, Kind: "silent", Waited: time.Since(p.at).Milliseconds(),
				Note: "Connected and still waiting for the request to arrive. Often a network or firewall problem between that device and the server."})
		}
	}
}

func (a *AccessLog) Run() {
	for range time.Tick(3 * time.Second) {
		a.Sweep()
	}
}

func (a *AccessLog) Snapshot(limit int) AccessSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := AccessSnapshot{Hidden: a.hidden, Since: a.since, Entries: []AccessEntry{}, Events: []ConnEvent{}, Clients: []ClientInfo{}}
	es := a.entries
	if len(es) > limit {
		es = es[len(es)-limit:]
	}
	for i := len(es) - 1; i >= 0; i-- { // newest first
		s.Entries = append(s.Entries, es[i])
	}
	for i := len(a.events) - 1; i >= 0; i-- {
		s.Events = append(s.Events, a.events[i])
	}
	for _, c := range a.clients {
		s.Clients = append(s.Clients, *c)
	}
	sort.Slice(s.Clients, func(i, j int) bool { return s.Clients[i].LastSeen.After(s.Clients[j].LastSeen) })
	return s
}

// RecentLocalHit tells whether any request arrived using a .local name lately.
func (a *AccessLog) RecentLocalHit(within time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := a.entries[i]
		if time.Since(e.Time) > within {
			return false
		}
		if strings.Contains(e.Host, ".local") {
			return true
		}
	}
	return false
}
