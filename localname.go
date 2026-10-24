package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Local name: reach the server as http://<name>.local instead of an IP
// address that changes whenever the network does.
//
// It uses mDNS (Avahi), the same system Macs and iPhones use to find
// printers. Avahi announces the server's current address automatically, so
// the name keeps working when the IP changes.

var hostNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$`)

type LocalNameStatus struct {
	Supported   bool     `json:"supported"`
	Reason      string   `json:"reason,omitempty"`
	Hostname    string   `json:"hostname"`
	Installed   bool     `json:"installed"`  // avahi-daemon is installed
	Running     bool     `json:"running"`    // and running
	Advertised  string   `json:"advertised"` // the .local name actually announced
	Resolves    bool     `json:"resolves"`   // the name resolves on the server itself
	Port80      bool     `json:"port80"`
	Port80Free  bool     `json:"port80Free"`
	Addresses   []string `json:"addresses"`
	Firewall    bool     `json:"firewall"` // ufw is active
	HavenPort   string   `json:"havenPort"`
	JobID       string   `json:"jobId,omitempty"`
	SuggestName string   `json:"suggestName"`
}

type LocalName struct {
	store  *Store
	jobs   *Jobs
	port80 *ExtraListener
	port   string // Haven's main port, e.g. "8484"
	mu     sync.Mutex
	jobID  string
}

func (l *LocalName) Status(ctx context.Context) LocalNameStatus {
	st := LocalNameStatus{HavenPort: l.port, Port80: l.port80.On(), Addresses: lanAddresses()}
	st.Hostname, _ = os.Hostname()
	st.Hostname = strings.ToLower(strings.TrimSuffix(st.Hostname, ".local"))
	st.SuggestName = "haven"
	if hostNameRe.MatchString(st.Hostname) {
		st.SuggestName = st.Hostname
	}
	st.Port80Free = st.Port80 || portFree(80, "tcp")
	l.mu.Lock()
	if j, ok := l.jobs.Get(l.jobID); ok && j.View(false).Status == "running" {
		st.JobID = l.jobID
	}
	l.mu.Unlock()
	if !isLinux() {
		st.Reason = "The local name is set up on the Linux server. This computer already announces itself as " + st.Hostname + ".local."
		st.Advertised = st.Hostname + ".local"
		return st
	}
	st.Supported = true
	st.Installed = hasCmd("avahi-daemon")
	if st.Installed {
		out, _ := runHost(ctx, 5*time.Second, "systemctl", "is-active", "avahi-daemon")
		st.Running = out == "active"
	}
	st.Advertised = st.Hostname + ".local"
	if st.Running {
		// Avahi renames itself (haven-2.local) if another device has the name.
		if out, err := runHost(ctx, 5*time.Second, "journalctl", "-u", "avahi-daemon", "-n", "200", "--no-pager", "-o", "cat"); err == nil {
			if n := parseAvahiHostName(out); n != "" {
				st.Advertised = n
			}
		}
		_, err := runHost(ctx, 4*time.Second, "getent", "hosts", st.Advertised)
		st.Resolves = err == nil
	}
	st.Firewall = ufwActive(ctx)
	return st
}

// parseAvahiHostName finds the last "Host name is X." in Avahi's log.
func parseAvahiHostName(log string) string {
	name := ""
	for _, line := range strings.Split(log, "\n") {
		if _, rest, ok := strings.Cut(line, "Host name is "); ok {
			name = strings.TrimSuffix(strings.TrimSpace(rest), ".")
		}
	}
	return name
}

// Setup renames the server (if asked), installs and starts Avahi, opens the
// firewall for it, and optionally turns on port 80. It runs as a job.
func (l *LocalName) Setup(name string, port80 bool) (*Job, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !hostNameRe.MatchString(name) {
		return nil, errors.New("use 1–32 lowercase letters, numbers or dashes, like haven or home-server")
	}
	if !isLinux() {
		return nil, errors.New("this works on the Linux server only")
	}
	if port80 && !l.port80.On() && !portFree(80, "tcp") {
		return nil, errors.New("port 80 is already used by another program; turn that option off or free the port")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if j, ok := l.jobs.Get(l.jobID); ok && j.View(false).Status == "running" {
		return nil, errors.New("setup is already running")
	}
	job := l.jobs.Start("server", "local-name", 20*time.Minute, func(ctx context.Context, j *Job) error {
		current, _ := os.Hostname()
		if !strings.EqualFold(current, name) {
			j.Logf("Renaming this server from %s to %s", current, name)
			if err := hostStream(ctx, j, nil, "hostnamectl", "set-hostname", name); err != nil {
				return fmt.Errorf("could not rename the server: %w", err)
			}
			if err := updateEtcHosts(name); err != nil {
				j.Logf("Note: could not update /etc/hosts (%v); this is harmless", err)
			}
		}
		if !hasCmd("avahi-daemon") {
			j.Logf("Installing Avahi (mDNS) so devices can find %s.local", name)
			if err := aptInstall(ctx, j, "avahi-daemon", "libnss-mdns"); err != nil {
				return err
			}
		}
		j.Logf("Starting Avahi")
		if err := hostStream(ctx, j, nil, "systemctl", "enable", "avahi-daemon"); err != nil {
			return err
		}
		if err := hostStream(ctx, j, nil, "systemctl", "restart", "avahi-daemon"); err != nil {
			return err
		}
		if ufwActive(ctx) {
			j.Logf("Firewall is on: allowing mDNS (5353/udp) and Haven's ports")
			_ = hostStream(ctx, j, nil, "ufw", "allow", "5353/udp", "comment", "mDNS for Haven")
			_ = hostStream(ctx, j, nil, "ufw", "allow", l.port+"/tcp", "comment", "Haven")
			if port80 {
				_ = hostStream(ctx, j, nil, "ufw", "allow", "80/tcp", "comment", "Haven")
			}
		}
		if err := l.SetPort80(port80); err != nil {
			j.Logf("Could not turn on port 80: %v", err)
		}
		time.Sleep(2 * time.Second)
		st := l.Status(ctx)
		addr := "http://" + st.Advertised
		if !st.Port80 {
			addr += ":" + l.port
		}
		if st.Advertised != name+".local" {
			j.Logf("Another device already uses %s.local, so this server is %s", name, st.Advertised)
		}
		if st.Resolves {
			j.Logf("Ready: open %s from any device on this network", addr)
		} else {
			j.Logf("Set up. If %s doesn't open yet, wait a minute; some devices cache names", addr)
		}
		return nil
	})
	l.jobID = job.ID
	return job, nil
}

func updateEtcHosts(name string) error {
	b, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	found := false
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "127.0.1.1" {
			lines[i] = "127.0.1.1\t" + name
			found = true
		}
	}
	if !found {
		lines = append(lines[:len(lines)-1], "127.0.1.1\t"+name, "")
	}
	return writeFileAtomic("/etc/hosts", []byte(strings.Join(lines, "\n")), 0o644)
}

// SetPort80 turns the extra port-80 listener on or off and remembers it.
func (l *LocalName) SetPort80(on bool) error {
	if err := l.port80.Set(on); err != nil {
		return err
	}
	return l.store.UpdateSettings(func(s *HostSettings) { s.Port80 = on })
}

// ExtraListener serves Haven on a second address (port 80) that can be
// switched on and off while Haven runs.
type ExtraListener struct {
	addr    string
	handler http.Handler
	conn    func(net.Conn, http.ConnState)
	mu      sync.Mutex
	srv     *http.Server
}

func (e *ExtraListener) On() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.srv != nil
}

func (e *ExtraListener) Set(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if on && e.srv == nil {
		ln, err := net.Listen("tcp", e.addr)
		if err != nil {
			return fmt.Errorf("port 80 is not available: %w", err)
		}
		e.srv = &http.Server{Handler: e.handler, ReadHeaderTimeout: 10 * time.Second, ConnState: e.conn}
		go func(s *http.Server) { _ = s.Serve(ln) }(e.srv)
	} else if !on && e.srv != nil {
		_ = e.srv.Close()
		e.srv = nil
	}
	return nil
}

// --- name test and fix ----------------------------------------------------------

// lanInterfaceNames lists the real network interfaces that have an IPv4
// address (not Docker, VPN or loopback).
func lanInterfaceNames() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		n := ifc.Name
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtualName(n) || strings.HasPrefix(n, "utun") || strings.HasPrefix(n, "bridge") || strings.HasPrefix(n, "awdl") || strings.HasPrefix(n, "llw") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// setIniKey sets key=value inside [section] of an INI file's text, replacing
// a commented-out or existing line, or adding it when missing.
func setIniKey(content, section, key, value string) string {
	lines := strings.Split(content, "\n")
	in, last, found := false, -1, false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			in = t == "["+section+"]"
			if in {
				last = i
			}
			continue
		}
		if !in {
			continue
		}
		last = i
		bare := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if k, _, ok := strings.Cut(bare, "="); ok && strings.TrimSpace(k) == key {
			if !strings.HasPrefix(t, "#") || !found {
				lines[i] = key + "=" + value
				found = true
			}
		}
	}
	if found {
		return strings.Join(lines, "\n")
	}
	if last < 0 {
		return strings.TrimRight(content, "\n") + "\n[" + section + "]\n" + key + "=" + value + "\n"
	}
	out := append([]string{}, lines[:last+1]...)
	out = append(out, key+"="+value)
	return strings.Join(append(out, lines[last+1:]...), "\n")
}

const avahiConf = "/etc/avahi/avahi-daemon.conf"

// AnnounceOnlyLAN tells Avahi to announce the .local name only on the real
// network. By default it also announces the Docker bridge (172.17.x.x) and
// VPN addresses, and a device that picks one of those waits forever.
func (l *LocalName) AnnounceOnlyLAN(ctx context.Context) ([]string, error) {
	ifs := lanInterfaceNames()
	if len(ifs) == 0 {
		return nil, errors.New("no network interface with an address was found")
	}
	b, err := os.ReadFile(avahiConf)
	if err != nil {
		return nil, errors.New("Avahi isn't installed yet; run Set up local address first")
	}
	_ = os.WriteFile(avahiConf+".haven-backup", b, 0o644)
	out := setIniKey(string(b), "server", "allow-interfaces", strings.Join(ifs, ","))
	if err := writeFileAtomic(avahiConf, []byte(out), 0o644); err != nil {
		return nil, err
	}
	if _, err := runHost(ctx, 20*time.Second, "systemctl", "restart", "avahi-daemon"); err != nil {
		return nil, err
	}
	return ifs, nil
}

// Test checks everything between a device and the .local name that can be
// checked from the server.
func (l *LocalName) Test(ctx context.Context, access *AccessLog) *Diagnosis {
	d := &Diagnosis{Target: "the local address", RanAt: time.Now()}
	add := func(f Finding) { d.Findings = append(d.Findings, f) }
	st := l.Status(ctx)
	if !st.Supported {
		add(Finding{Level: "info", Area: "Name", Title: st.Reason})
		return finish(d, nil)
	}
	if !st.Installed {
		add(Finding{Level: "bad", Area: "Name", Title: "The local name service (Avahi) isn't installed", Fix: "Press Set up local address."})
		return finish(d, nil)
	}
	if !st.Running {
		add(Finding{Level: "bad", Area: "Name", Title: "Avahi is installed but not running", Fix: "Run: sudo systemctl restart avahi-daemon  (or press Apply under Local address)."})
	} else {
		add(Finding{Level: "ok", Area: "Name", Title: "Avahi is running and announcing " + st.Advertised})
	}
	if st.Advertised != st.Hostname+".local" {
		add(Finding{Level: "warn", Area: "Name", Title: "The name was changed to " + st.Advertised + " because another device already has " + st.Hostname + ".local",
			Fix: "Use " + st.Advertised + ", or rename the other device, or pick a different name."})
	}

	// What does the name resolve to? It must be an address a phone or laptop can reach.
	lan := map[string]bool{}
	for _, a := range st.Addresses {
		lan[a] = true
	}
	if out, err := runHost(ctx, 6*time.Second, "getent", "ahostsv4", st.Advertised); err != nil {
		add(Finding{Level: "warn", Area: "Name", Title: st.Advertised + " doesn't resolve on the server itself", Fix: "Check that libnss-mdns is installed: sudo apt install libnss-mdns"})
	} else {
		seen := map[string]bool{}
		var bad []string
		for _, line := range strings.Split(out, "\n") {
			if f := strings.Fields(line); len(f) > 0 && !seen[f[0]] {
				seen[f[0]] = true
				if !lan[f[0]] && f[0] != "127.0.1.1" && f[0] != "127.0.0.1" {
					bad = append(bad, f[0])
				}
			}
		}
		if len(bad) > 0 {
			add(Finding{Level: "bad", Area: "Name", Title: st.Advertised + " also points to an address other devices can't reach",
				Detail: "It resolves to " + strings.Join(bad, ", ") + ", which isn't on your network (probably Docker or a VPN). A device that picks it waits forever, which looks like a page that keeps loading.",
				Fix:    "Press Announce only on my network below."})
		} else {
			add(Finding{Level: "ok", Area: "Name", Title: st.Advertised + " resolves to your network address (" + strings.Join(st.Addresses, ", ") + ")"})
		}
	}

	if b, err := os.ReadFile(avahiConf); err == nil {
		set := false
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "allow-interfaces=") {
				set = true
			}
		}
		virtual := false
		ifs, _ := net.Interfaces()
		for _, i := range ifs {
			if isVirtualName(i.Name) && i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
				virtual = true
			}
		}
		if !set && virtual {
			add(Finding{Level: "warn", Area: "Name", Title: "Avahi announces on every network, including Docker's",
				Fix: "Press Announce only on my network below."})
		}
	}

	if ufwActive(ctx) {
		if out, _ := runHost(ctx, 5*time.Second, "ufw", "status"); !strings.Contains(out, "5353") {
			add(Finding{Level: "bad", Area: "Firewall", Title: "The firewall blocks name lookups (port 5353/udp)", Fix: "Run: sudo ufw allow 5353/udp"})
		} else {
			add(Finding{Level: "ok", Area: "Firewall", Title: "The firewall lets name lookups through"})
		}
		if out, _ := runHost(ctx, 5*time.Second, "ufw", "status"); !strings.Contains(out, l.port) {
			add(Finding{Level: "bad", Area: "Firewall", Title: "The firewall blocks Haven's port " + l.port, Fix: "Run: sudo ufw allow " + l.port + "/tcp"})
		}
	}

	if l.port80.On() {
		add(Finding{Level: "ok", Area: "Port", Title: "Haven also answers on port 80"})
	}
	if access != nil {
		if access.RecentLocalHit(24 * time.Hour) {
			add(Finding{Level: "ok", Area: "Traffic", Title: "Requests have reached Haven using a .local name in the last 24 hours"})
		} else {
			add(Finding{Level: "info", Area: "Traffic", Title: "No request has arrived using the .local name yet",
				Detail: "Open the .local address on a device, then check the Access log below. If nothing appears while the page spins, the request never reached the server: the device couldn't find the name, or something blocks the network."})
		}
	}
	return finish(d, nil)
}
