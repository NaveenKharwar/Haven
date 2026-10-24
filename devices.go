package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Devices on the local network. Haven reads the server's neighbour table (the
// devices it has recently talked to). A scan nudges every address in the local
// network so that quiet devices show up too. Nothing leaves the network, and
// nothing is installed on the other devices.

type deviceRecord struct {
	Label     string    `json:"label,omitempty"`
	Name      string    `json:"name,omitempty"`
	LastIP    string    `json:"lastIp"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Baseline  bool      `json:"baseline,omitempty"` // there when Haven first looked, so not "new"
}

type LanDevice struct {
	MAC        string    `json:"mac"`
	IP         string    `json:"ip"`
	Name       string    `json:"name,omitempty"`   // from the network (reverse DNS)
	Label      string    `json:"label,omitempty"`  // chosen by the admin
	Vendor     string    `json:"vendor,omitempty"` // maker of the network chip, when known
	Randomized bool      `json:"randomized"`       // a private address that phones change on purpose
	Online     bool      `json:"online"`
	New        bool      `json:"new"` // first seen in the last day
	Self       bool      `json:"self"`
	Router     bool      `json:"router"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
	Agent      string    `json:"agent,omitempty"` // what it used to open Haven
	Requests   int       `json:"requests,omitempty"`
}

type DevicesSnapshot struct {
	Devices []LanDevice `json:"devices"`
	Subnets []string    `json:"subnets"`
	Scanned time.Time   `json:"scanned,omitempty"`
	Note    string      `json:"note,omitempty"`
}

type Devices struct {
	mu      sync.Mutex
	path    string
	known   map[string]*deviceRecord
	fresh   bool // the file did not exist: the first look only sets the baseline
	notify  func(level, title, body, key, route string)
	access  *AccessLog
	scanned time.Time
	scanMu  sync.Mutex
}

func NewDevices(dir string, access *AccessLog, notify func(level, title, body, key, route string)) *Devices {
	d := &Devices{path: filepath.Join(dir, "devices.json"), known: map[string]*deviceRecord{}, access: access, notify: notify}
	b, err := os.ReadFile(d.path)
	if err != nil {
		d.fresh = true
	} else {
		_ = json.Unmarshal(b, &d.known)
	}
	return d
}

func (d *Devices) saveLocked() {
	b, _ := json.MarshalIndent(d.known, "", "  ")
	_ = writeFileAtomic(d.path, b, 0o600)
}

// --- reading the neighbour table -------------------------------------------------------

type neighbour struct {
	IP  string
	MAC string
	If  string
}

var macRe = regexp.MustCompile(`^([0-9a-f]{1,2}:){5}[0-9a-f]{1,2}$`)

func normMAC(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if !macRe.MatchString(m) {
		return ""
	}
	parts := strings.Split(m, ":")
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	m = strings.Join(parts, ":")
	if m == "00:00:00:00:00:00" || m == "ff:ff:ff:ff:ff:ff" {
		return ""
	}
	return m
}

// parseIPNeigh reads `ip -4 neigh show`.
func parseIPNeigh(out string) []neighbour {
	var res []neighbour
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 || net.ParseIP(f[0]) == nil {
			continue
		}
		var mac, dev string
		for i := 1; i+1 < len(f); i++ {
			switch f[i] {
			case "lladdr":
				mac = normMAC(f[i+1])
			case "dev":
				dev = f[i+1]
			}
		}
		state := f[len(f)-1]
		if mac == "" || state == "FAILED" || state == "INCOMPLETE" {
			continue
		}
		res = append(res, neighbour{IP: f[0], MAC: mac, If: dev})
	}
	return res
}

// parseArp reads `arp -an` (macOS and BSD) and /proc/net/arp-like output.
func parseArp(out string) []neighbour {
	var res []neighbour
	re := regexp.MustCompile(`\((\d+\.\d+\.\d+\.\d+)\) at ([0-9a-fA-F:]+) on (\S+)`)
	for _, l := range strings.Split(out, "\n") {
		if m := re.FindStringSubmatch(l); m != nil {
			if mac := normMAC(m[2]); mac != "" {
				res = append(res, neighbour{IP: m[1], MAC: mac, If: m[3]})
			}
		}
	}
	return res
}

func readNeighbours(ctx context.Context) []neighbour {
	if isLinux() && hasCmd("ip") {
		if s, err := runHost(ctx, 4*time.Second, "ip", "-4", "neigh", "show"); err == nil {
			return parseIPNeigh(s)
		}
	}
	if hasCmd("arp") {
		if s, err := runHost(ctx, 4*time.Second, "arp", "-an"); err == nil {
			return parseArp(s)
		}
	}
	return nil
}

// --- the local network -----------------------------------------------------------------

type lanNet struct {
	IP   net.IP
	Net  *net.IPNet // always no wider than /24, so a scan stays small and polite
	Name string
}

func lanNetworks() []lanNet {
	var out []lanNet
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if k := ifaceKind(ifc.Name); k == "Docker" || k == "VPN" || k == "Virtual" {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			ip4 := ipn.IP.To4()
			ones, bits := ipn.Mask.Size()
			if ones < 24 {
				ones = 24
			}
			mask := net.CIDRMask(ones, bits)
			out = append(out, lanNet{IP: ip4, Net: &net.IPNet{IP: ip4.Mask(mask), Mask: mask}, Name: ifc.Name})
		}
	}
	return out
}

// nudge sends one tiny UDP packet to every address, which makes the system ask
// "who has this address?" on the network. The answers land in the neighbour table.
func nudge(n lanNet) {
	ones, bits := n.Net.Mask.Size()
	count := 1 << (bits - ones)
	base := n.Net.IP.To4()
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := 1; i < count-1; i++ {
		ip := net.IPv4(base[0], base[1], base[2], base[3]+byte(i))
		if ip.Equal(n.IP) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()
			c, err := net.DialTimeout("udp4", net.JoinHostPort(ip.String(), "9"), 300*time.Millisecond)
			if err != nil {
				return
			}
			_, _ = c.Write([]byte{0})
			_ = c.Close()
		}(ip)
	}
	wg.Wait()
}

// --- names and makers ------------------------------------------------------------------

func isRandomMAC(mac string) bool {
	if len(mac) < 2 {
		return false
	}
	b := mac[1] // the second hex digit holds the "locally administered" bit
	return b == '2' || b == '6' || b == 'a' || b == 'e'
}

var (
	ouiOnce sync.Once
	ouiMap  map[string]string
)

// loadOUI reads a vendor list if the server has one (installed with ieee-data
// or nmap). Haven doesn't bundle one, so without it the maker is left blank.
func loadOUI() {
	ouiMap = map[string]string{}
	for _, p := range []string{"/usr/share/ieee-data/oui.txt", "/var/lib/ieee-data/oui.txt", "/usr/share/nmap/nmap-mac-prefixes", "/usr/local/share/nmap/nmap-mac-prefixes"} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := sc.Text()
			if strings.Contains(l, "(base 16)") { // oui.txt: "286FB9   (base 16)   Nokia"
				parts := strings.SplitN(l, "(base 16)", 2)
				ouiMap[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
			} else if len(l) > 7 && !strings.HasPrefix(l, "#") && l[6] == ' ' { // nmap: "286FB9 Nokia"
				ouiMap[strings.ToLower(l[:6])] = strings.TrimSpace(l[7:])
			}
		}
		f.Close()
		if len(ouiMap) > 0 {
			return
		}
	}
}

func vendorOf(mac string) string {
	ouiOnce.Do(loadOUI)
	return ouiMap[strings.ReplaceAll(mac[:8], ":", "")]
}

func reverseNames(ips []string) map[string]string {
	out := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			defer cancel()
			names, err := net.DefaultResolver.LookupAddr(ctx, ip)
			if (err != nil || len(names) == 0) && hasCmd("avahi-resolve-address") {
				// Many devices announce their own name on the network (mDNS) even when the router doesn't know it.
				if o, e := runHost(ctx, 1500*time.Millisecond, "avahi-resolve-address", ip); e == nil {
					if f := strings.Fields(o); len(f) == 2 {
						names, err = []string{f[1]}, nil
					}
				}
			}
			if err != nil || len(names) == 0 {
				return
			}
			n := strings.TrimSuffix(names[0], ".")
			n = strings.TrimSuffix(n, ".local")
			n = strings.TrimSuffix(n, ".lan")
			mu.Lock()
			out[ip] = n
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	return out
}

// --- putting it together ---------------------------------------------------------------

// Refresh reads the neighbour table (after a nudge, when scan is true), updates
// what is remembered, and returns the list.
func (d *Devices) Refresh(ctx context.Context, scan bool) DevicesSnapshot {
	d.scanMu.Lock()
	defer d.scanMu.Unlock()
	nets := lanNetworks()
	snap := DevicesSnapshot{Devices: []LanDevice{}, Subnets: []string{}}
	for _, n := range nets {
		snap.Subnets = append(snap.Subnets, n.Net.String())
	}
	if scan {
		var wg sync.WaitGroup
		for _, n := range nets {
			wg.Add(1)
			go func(n lanNet) {
				defer wg.Done()
				nudge(n)
			}(n)
		}
		wg.Wait()
		select {
		case <-time.After(1200 * time.Millisecond): // let the answers arrive
		case <-ctx.Done():
		}
	}
	inLAN := func(ip string) bool {
		p := net.ParseIP(ip)
		for _, n := range nets {
			if n.Net.Contains(p) {
				return true
			}
		}
		return false
	}
	seen := map[string]neighbour{}
	for _, nb := range readNeighbours(ctx) {
		if inLAN(nb.IP) {
			seen[nb.MAC] = nb
		}
	}
	gw, _ := defaultGateway(ctx)
	mine := map[string]bool{}
	for _, n := range nets {
		mine[n.IP.String()] = true
	}
	var ips []string
	for _, nb := range seen {
		ips = append(ips, nb.IP)
	}
	names := reverseNames(ips)
	clients := map[string]ClientInfo{}
	if d.access != nil {
		for _, c := range d.access.Snapshot(0).Clients {
			clients[c.IP] = c
		}
	}

	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	var added []string
	for mac, nb := range seen {
		rec := d.known[mac]
		if rec == nil {
			rec = &deviceRecord{FirstSeen: now, Baseline: d.fresh}
			d.known[mac] = rec
			added = append(added, mac)
		}
		rec.LastIP, rec.LastSeen = nb.IP, now
		if n := names[nb.IP]; n != "" {
			rec.Name = n
		}
	}
	if scan {
		d.scanned = now
	}
	d.saveLocked()

	for mac, rec := range d.known {
		_, online := seen[mac]
		if !online && !scan && now.Sub(rec.LastSeen) > 30*24*time.Hour {
			continue
		}
		dev := LanDevice{MAC: mac, IP: rec.LastIP, Name: rec.Name, Label: rec.Label, Vendor: vendorOf(mac), Randomized: isRandomMAC(mac),
			Online: online, New: now.Sub(rec.FirstSeen) < 24*time.Hour && !rec.Baseline, FirstSeen: rec.FirstSeen, LastSeen: rec.LastSeen, Router: rec.LastIP == gw}
		if c, ok := clients[rec.LastIP]; ok {
			dev.Agent, dev.Requests = c.Agent, c.Requests
		}
		snap.Devices = append(snap.Devices, dev)
	}
	// This server isn't in its own neighbour table.
	if host, err := os.Hostname(); err == nil {
		for _, n := range nets {
			snap.Devices = append(snap.Devices, LanDevice{MAC: "self-" + n.Name, IP: n.IP.String(), Name: host, Label: "", Online: true, Self: true, LastSeen: now, FirstSeen: now})
			break
		}
	}
	sort.Slice(snap.Devices, func(i, j int) bool {
		a, b := snap.Devices[i], snap.Devices[j]
		if a.Online != b.Online {
			return a.Online
		}
		return ipLess(a.IP, b.IP)
	})
	snap.Scanned = d.scanned
	if len(nets) == 0 {
		snap.Note = "This server isn't on a local network right now."
	}

	// Tell the admin about devices that appear after the first look.
	if d.notify != nil && !d.fresh {
		for _, mac := range added {
			rec := d.known[mac]
			title := "New device on your network"
			body := rec.LastIP
			if rec.Name != "" {
				body = rec.Name + " (" + rec.LastIP + ")"
			}
			d.notify("info", title, body+". Open Server → Insights to name it or check it is yours.", "newdev:"+mac, "server")
		}
	}
	d.fresh = false
	return snap
}

func ipLess(a, b string) bool {
	x, y := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if x == nil || y == nil {
		return a < b
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

func (d *Devices) SetLabel(mac, label string) bool {
	mac = normMAC(mac)
	label = strings.TrimSpace(label)
	if len(label) > 60 {
		label = label[:60]
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rec := d.known[mac]
	if rec == nil {
		return false
	}
	rec.Label = label
	d.saveLocked()
	return true
}

// Forget drops a device that is no longer wanted in the list.
func (d *Devices) Forget(mac string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.known, normMAC(mac))
	d.saveLocked()
}

// Watch looks at the network every few minutes without scanning, so the list
// stays current and new devices are noticed.
func (d *Devices) Watch(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	d.Refresh(ctx, false)
	for {
		select {
		case <-t.C:
			d.Refresh(ctx, false)
		case <-ctx.Done():
			return
		}
	}
}

// --- HTTP ------------------------------------------------------------------------------

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.devices.Refresh(r.Context(), false))
}

func (s *Server) scanDevices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.devices.Refresh(ctx, true))
}

func (s *Server) labelDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MAC    string `json:"mac"`
		Label  string `json:"label"`
		Forget bool   `json:"forget"`
	}
	if err := readJSON(r, &in); err != nil {
		jsonError(w, http.StatusBadRequest, "couldn't read that")
		return
	}
	if in.Forget {
		s.devices.Forget(in.MAC)
	} else if !s.devices.SetLabel(in.MAC, in.Label) {
		jsonError(w, http.StatusNotFound, "that device isn't in the list")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
