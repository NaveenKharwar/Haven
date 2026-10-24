package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Network and Wi-Fi, through NetworkManager (nmcli).
//
// Passwords are sent to nmcli on its standard input (never as command-line
// arguments, which other users could see with `ps`). Every change that could
// cut the server off from the network is checked afterwards and undone if the
// server can no longer reach its network.

type NetDevice struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // ethernet or wifi
	State      string   `json:"state"`
	Connection string   `json:"connection,omitempty"`
	ConnUUID   string   `json:"connUuid,omitempty"`
	IPs        []string `json:"ips"`
	Gateway    string   `json:"gateway,omitempty"`
	DNS        []string `json:"dns"`
	Managed    bool     `json:"managed"`
}

type WifiNetwork struct {
	SSID     string `json:"ssid"`
	Signal   int    `json:"signal"`
	Security string `json:"security"` // empty = open
	InUse    bool   `json:"inUse"`
	Saved    bool   `json:"saved"`
}

type SavedWifi struct {
	Name   string `json:"name"`
	UUID   string `json:"uuid"`
	Auto   bool   `json:"auto"`
	Active bool   `json:"active"`
}

type NetworkStatus struct {
	Supported bool        `json:"supported"`
	Reason    string      `json:"reason,omitempty"`
	NMReady   bool        `json:"nmReady"`   // NetworkManager installed and running
	Installed bool        `json:"installed"` // installed, maybe not running
	Devices   []NetDevice `json:"devices"`
	WifiOn    bool        `json:"wifiOn"`
	WifiDev   string      `json:"wifiDev,omitempty"`
	Saved     []SavedWifi `json:"saved"`
	Internet  bool        `json:"internet"`
	JobID     string      `json:"jobId,omitempty"`
}

type Network struct {
	jobs  *Jobs
	mu    sync.Mutex // one network change at a time
	jobID string
}

// splitTerse splits one line of `nmcli -t` output. Fields are separated by
// ':' and a literal ':' or '\' inside a field is escaped with a backslash.
func splitTerse(line string) []string {
	var out []string
	var cur strings.Builder
	esc := false
	for _, r := range line {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ':':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

func nmcli(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	return runHost(ctx, timeout, "nmcli", args...)
}

func (n *Network) runningJob() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if j, ok := n.jobs.Get(n.jobID); ok && j.View(false).Status == "running" {
		return n.jobID
	}
	return ""
}

func (n *Network) Status(ctx context.Context) NetworkStatus {
	st := NetworkStatus{Devices: []NetDevice{}, Saved: []SavedWifi{}, JobID: n.runningJob()}
	if !isLinux() {
		st.Reason = "Network settings are managed on the Linux server."
		return st
	}
	st.Supported = true
	st.Installed = hasCmd("nmcli")
	if st.Installed {
		out, _ := runHost(ctx, 5*time.Second, "systemctl", "is-active", "NetworkManager")
		st.NMReady = out == "active"
	}
	// Try a few well-known addresses at once; some networks block one of them.
	ok := make(chan bool, 3)
	for _, a := range []string{"1.1.1.1:443", "8.8.8.8:443", "registry-1.docker.io:443"} {
		go func(a string) {
			c, err := net.DialTimeout("tcp", a, 3*time.Second)
			if err == nil {
				c.Close()
			}
			ok <- err == nil
		}(a)
	}
	for i := 0; i < 3 && !st.Internet; i++ {
		st.Internet = <-ok
	}
	if !st.NMReady {
		// Still show what the server is connected to.
		for _, a := range lanAddresses() {
			st.Devices = append(st.Devices, NetDevice{Name: "network", Type: "ethernet", State: "connected", IPs: []string{a}, DNS: []string{}})
		}
		return st
	}

	out, err := nmcli(ctx, 8*time.Second, "-t", "-f", "DEVICE,TYPE,STATE,CONNECTION", "device", "status")
	if err != nil {
		return st
	}
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) < 4 || (f[1] != "ethernet" && f[1] != "wifi") || isVirtualName(f[0]) {
			continue
		}
		d := NetDevice{Name: f[0], Type: f[1], State: f[2], Connection: f[3], IPs: []string{}, DNS: []string{}, Managed: !strings.HasPrefix(f[2], "unmanaged")}
		if info, err := nmcli(ctx, 5*time.Second, "-t", "-f", "GENERAL.CON-UUID,IP4.ADDRESS,IP4.GATEWAY,IP4.DNS", "device", "show", f[0]); err == nil {
			for _, l := range strings.Split(info, "\n") {
				k, v, _ := strings.Cut(l, ":")
				switch {
				case k == "GENERAL.CON-UUID":
					d.ConnUUID = v
				case strings.HasPrefix(k, "IP4.ADDRESS") && v != "":
					d.IPs = append(d.IPs, v)
				case strings.HasPrefix(k, "IP4.GATEWAY"):
					d.Gateway = v
				case strings.HasPrefix(k, "IP4.DNS") && v != "":
					d.DNS = append(d.DNS, v)
				}
			}
		}
		if d.Type == "wifi" && st.WifiDev == "" && d.State != "unavailable" && d.Managed {
			st.WifiDev = d.Name
		}
		st.Devices = append(st.Devices, d)
	}
	if r, err := nmcli(ctx, 4*time.Second, "radio", "wifi"); err == nil {
		st.WifiOn = r == "enabled"
	}
	if out, err := nmcli(ctx, 5*time.Second, "-t", "-f", "NAME,UUID,TYPE,AUTOCONNECT,ACTIVE", "connection", "show"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := splitTerse(line)
			if len(f) >= 5 && f[2] == "802-11-wireless" {
				st.Saved = append(st.Saved, SavedWifi{Name: f[0], UUID: f[1], Auto: f[3] == "yes", Active: f[4] == "yes"})
			}
		}
	}
	return st
}

func isVirtualName(n string) bool {
	for _, p := range []string{"docker", "br-", "veth", "virbr", "tailscale", "lo", "tun", "tap"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// Scan lists nearby Wi-Fi networks, strongest first, one entry per name.
func (n *Network) Scan(ctx context.Context) ([]WifiNetwork, error) {
	if !isLinux() || !hasCmd("nmcli") {
		return nil, errors.New("Wi-Fi is not available here")
	}
	out, err := nmcli(ctx, 25*time.Second, "-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "yes")
	if err != nil {
		return nil, err
	}
	nets := parseWifiList(out)
	saved := map[string]bool{}
	if so, err := nmcli(ctx, 5*time.Second, "-t", "-f", "NAME,TYPE", "connection", "show"); err == nil {
		for _, l := range strings.Split(so, "\n") {
			if f := splitTerse(l); len(f) >= 2 && f[1] == "802-11-wireless" {
				saved[f[0]] = true
			}
		}
	}
	for i := range nets {
		nets[i].Saved = saved[nets[i].SSID]
	}
	return nets, nil
}

func parseWifiList(out string) []WifiNetwork {
	best := map[string]WifiNetwork{}
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) < 4 || f[1] == "" {
			continue
		}
		sig, _ := strconv.Atoi(f[2])
		w := WifiNetwork{SSID: f[1], Signal: sig, Security: strings.TrimSpace(strings.Trim(f[3], "-")), InUse: f[0] == "*"}
		if cur, ok := best[w.SSID]; !ok || w.InUse || (!cur.InUse && w.Signal > cur.Signal) {
			best[w.SSID] = w
		}
	}
	list := make([]WifiNetwork, 0, len(best))
	for _, w := range best {
		list = append(list, w)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].InUse != list[j].InUse {
			return list[i].InUse
		}
		return list[i].Signal > list[j].Signal
	})
	return list
}

func validWifi(ssid, password string) error {
	if ssid == "" || len(ssid) > 32 || strings.ContainsAny(ssid, "\n\r\x00") {
		return errors.New("the network name must be 1–32 characters")
	}
	if password == "" {
		return nil // open network, or a saved one
	}
	if strings.ContainsAny(password, "\n\r\x00") {
		return errors.New("the password can't contain line breaks")
	}
	if len(password) < 8 || len(password) > 64 {
		return errors.New("a Wi-Fi password is 8 to 63 characters")
	}
	return nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

func hostStreamInput(ctx context.Context, w io.Writer, input string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Stdin = strings.NewReader(input)
	return cmd.Run()
}

func (n *Network) start(action string, timeout time.Duration, fn func(ctx context.Context, j *Job) error) (*Job, error) {
	if !isLinux() || !hasCmd("nmcli") {
		return nil, errors.New("NetworkManager isn't installed; press Set up networking first")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if j, ok := n.jobs.Get(n.jobID); ok && j.View(false).Status == "running" {
		return nil, errors.New("another network change is still running")
	}
	job := n.jobs.Start("server", action, timeout, fn)
	n.jobID = job.ID
	return job, nil
}

func (n *Network) activeWifi(ctx context.Context) (dev, conn string) {
	st := n.Status(ctx)
	for _, d := range st.Devices {
		if d.Type == "wifi" && d.Managed && d.State != "unavailable" {
			if dev == "" || d.State == "connected" {
				dev, conn = d.Name, ""
				if d.State == "connected" {
					conn = d.Connection
				}
			}
		}
	}
	return
}

// ConnectWifi joins a network. If it fails, the previous network is restored.
func (n *Network) ConnectWifi(ssid, password string, hidden bool) (*Job, error) {
	if err := validWifi(ssid, password); err != nil {
		return nil, err
	}
	return n.start("wifi-connect", 3*time.Minute, func(ctx context.Context, j *Job) error {
		dev, prev := n.activeWifi(ctx)
		if dev == "" {
			return errors.New("no usable Wi-Fi adapter was found. Check that it's switched on and that NetworkManager manages it")
		}
		if r, _ := nmcli(ctx, 4*time.Second, "radio", "wifi"); r != "enabled" {
			j.Logf("Switching Wi-Fi on")
			_, _ = runHost(ctx, 10*time.Second, "rfkill", "unblock", "wifi")
			if _, err := nmcli(ctx, 8*time.Second, "radio", "wifi", "on"); err != nil {
				return err
			}
			time.Sleep(3 * time.Second)
		}
		if password != "" { // a new password replaces any saved one for this network
			if out, err := nmcli(ctx, 5*time.Second, "-t", "-f", "NAME,UUID,TYPE", "connection", "show"); err == nil {
				for _, l := range strings.Split(out, "\n") {
					if f := splitTerse(l); len(f) >= 3 && f[2] == "802-11-wireless" && f[0] == ssid {
						j.Logf("Replacing the saved password for %s", ssid)
						_, _ = nmcli(ctx, 8*time.Second, "connection", "delete", "uuid", f[1])
					}
				}
			}
		}
		j.Logf("Connecting to %s (this can take up to 30 seconds)", ssid)
		args := []string{"-w", "30", "--ask", "device", "wifi", "connect", ssid, "ifname", dev}
		if hidden {
			args = append(args, "hidden", "yes")
		}
		input := ""
		if password != "" {
			input = password + "\n"
		}
		cctx, cancel := context.WithTimeout(ctx, 50*time.Second)
		defer cancel()
		err := hostStreamInput(cctx, j, input, "nmcli", args...)
		if err == nil && !n.waitHasIP(ctx, dev, 20*time.Second) {
			err = errors.New("connected, but no address was assigned")
		}
		if err != nil {
			j.Logf("Could not connect: check the password and that the network is in range")
			if prev != "" && prev != ssid {
				j.Logf("Going back to %s", prev)
				_, _ = nmcli(ctx, 40*time.Second, "connection", "up", "id", prev)
			}
			return errors.New("could not connect to " + ssid + ". The password may be wrong, or the signal too weak")
		}
		j.Logf("Connected to %s", ssid)
		return nil
	})
}

func (n *Network) waitHasIP(ctx context.Context, dev string, wait time.Duration) bool {
	end := time.Now().Add(wait)
	for time.Now().Before(end) {
		if out, err := nmcli(ctx, 4*time.Second, "-g", "IP4.ADDRESS", "device", "show", dev); err == nil && strings.TrimSpace(out) != "" {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func (n *Network) ForgetWifi(ctx context.Context, uuid string) error {
	if !uuidRe.MatchString(uuid) {
		return errors.New("invalid network")
	}
	_, err := nmcli(ctx, 10*time.Second, "connection", "delete", "uuid", uuid)
	return err
}

type StaticIP struct {
	UUID    string   `json:"uuid"` // the connection (from a device's name)
	Device  string   `json:"device"`
	Auto    bool     `json:"auto"` // go back to automatic (DHCP)
	Address string   `json:"address"`
	Prefix  int      `json:"prefix"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
}

func validStatic(s *StaticIP) error {
	if s.Auto {
		return nil
	}
	ip := net.ParseIP(s.Address).To4()
	gw := net.ParseIP(s.Gateway).To4()
	if ip == nil || gw == nil {
		return errors.New("enter the address and the router's address like 192.168.1.50 and 192.168.1.1")
	}
	if s.Prefix < 8 || s.Prefix > 30 {
		return errors.New("the network size (prefix) should be between 8 and 30; most home networks use 24")
	}
	mask := net.CIDRMask(s.Prefix, 32)
	if !ip.Mask(mask).Equal(gw.Mask(mask)) {
		return errors.New("the address and the router must be on the same network (the first numbers should match)")
	}
	if ip.Equal(gw) || ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
		return errors.New("that address can't be used")
	}
	if len(s.DNS) == 0 {
		s.DNS = []string{s.Gateway, "1.1.1.1"}
	}
	if len(s.DNS) > 4 {
		return errors.New("use up to 4 DNS servers")
	}
	for _, d := range s.DNS {
		if net.ParseIP(d) == nil {
			return fmt.Errorf("%q is not a valid DNS address", d)
		}
	}
	return nil
}

// SetStatic switches a connection to a fixed IP (or back to automatic), then
// checks the router answers. If it doesn't, the old settings come back.
func (n *Network) SetStatic(s StaticIP) (*Job, error) {
	if !uuidRe.MatchString(s.UUID) {
		return nil, errors.New("invalid connection")
	}
	if err := validStatic(&s); err != nil {
		return nil, err
	}
	return n.start("network-ip", 3*time.Minute, func(ctx context.Context, j *Job) error {
		old, _ := nmcli(ctx, 5*time.Second, "-t", "-f", "ipv4.method,ipv4.addresses,ipv4.gateway,ipv4.dns", "connection", "show", "uuid", s.UUID)
		restore := func() {
			j.Logf("Putting the previous settings back")
			prev := map[string]string{}
			for _, l := range strings.Split(old, "\n") {
				if k, v, ok := strings.Cut(l, ":"); ok {
					prev[k] = v
				}
			}
			m := prev["ipv4.method"]
			if m == "" {
				m = "auto"
			}
			_, _ = nmcli(ctx, 10*time.Second, "connection", "modify", "uuid", s.UUID, "ipv4.method", m,
				"ipv4.addresses", strings.ReplaceAll(prev["ipv4.addresses"], " | ", ","), "ipv4.gateway", prev["ipv4.gateway"], "ipv4.dns", strings.ReplaceAll(prev["ipv4.dns"], " | ", ","))
			_, _ = nmcli(ctx, 40*time.Second, "connection", "up", "uuid", s.UUID)
		}
		if s.Auto {
			j.Logf("Switching to an automatic address (DHCP)")
			if _, err := nmcli(ctx, 10*time.Second, "connection", "modify", "uuid", s.UUID, "ipv4.method", "auto", "ipv4.addresses", "", "ipv4.gateway", "", "ipv4.dns", ""); err != nil {
				return err
			}
		} else {
			j.Logf("Setting %s/%d with the router at %s", s.Address, s.Prefix, s.Gateway)
			if _, err := nmcli(ctx, 10*time.Second, "connection", "modify", "uuid", s.UUID, "ipv4.method", "manual",
				"ipv4.addresses", fmt.Sprintf("%s/%d", s.Address, s.Prefix), "ipv4.gateway", s.Gateway, "ipv4.dns", strings.Join(s.DNS, ",")); err != nil {
				return err
			}
		}
		j.Logf("Applying (you may lose this page for a moment)")
		if _, err := nmcli(ctx, 45*time.Second, "connection", "up", "uuid", s.UUID); err != nil {
			restore()
			return errors.New("the new settings didn't apply, so the old ones were put back")
		}
		if !s.Auto {
			time.Sleep(3 * time.Second)
			if hasCmd("ping") {
				if _, err := runHost(ctx, 10*time.Second, "ping", "-c", "2", "-W", "2", s.Gateway); err != nil {
					restore()
					return errors.New("the router didn't answer at the new address, so the old settings were put back. Check the address and router address")
				}
			}
			j.Logf("Done. Open Haven at http://%s:8484", s.Address)
		} else {
			j.Logf("Done. The server asks the router for an address again")
		}
		return nil
	})
}

// Setup installs NetworkManager. Interfaces already set up through netplan
// stay as they are, so a wired server keeps its connection; NetworkManager
// takes over Wi-Fi and anything netplan doesn't already manage.
func (n *Network) Setup() (*Job, error) {
	if !isLinux() {
		return nil, errors.New("this works on the Linux server only")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if j, ok := n.jobs.Get(n.jobID); ok && j.View(false).Status == "running" {
		return j, nil
	}
	job := n.jobs.Start("server", "network-setup", 20*time.Minute, func(ctx context.Context, j *Job) error {
		if !hasCmd("nmcli") {
			j.Logf("Installing NetworkManager")
			if err := aptInstall(ctx, j, "network-manager", "wpasupplicant", "rfkill"); err != nil {
				return err
			}
		}
		j.Logf("Starting NetworkManager")
		if err := hostStream(ctx, j, nil, "systemctl", "enable", "--now", "NetworkManager"); err != nil {
			return err
		}
		time.Sleep(3 * time.Second)
		_, _ = runHost(ctx, 10*time.Second, "rfkill", "unblock", "wifi")
		j.Logf("Ready. Wi-Fi adapters now appear on this page")
		return nil
	})
	n.jobID = job.ID
	return job, nil
}
