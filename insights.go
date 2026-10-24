package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// Insights: the facts about this server that are useful but awkward to find,
// gathered in one place. Everything here is read-only. Each fact is optional:
// a tool that isn't installed just leaves its part out.

type Hardware struct {
	Vendor      string   `json:"vendor,omitempty"`
	Product     string   `json:"product,omitempty"`
	Board       string   `json:"board,omitempty"`
	Bios        string   `json:"bios,omitempty"`
	Virtual     string   `json:"virtual,omitempty"` // kvm, vmware... empty on real hardware
	CPUModel    string   `json:"cpuModel"`
	CPUCores    int      `json:"cpuCores"` // physical
	CPUThreads  int      `json:"cpuThreads"`
	CPUMaxMHz   float64  `json:"cpuMaxMhz,omitempty"`
	MemTotal    uint64   `json:"memTotal"`
	GPUs        []string `json:"gpus"`
	Drives      []Drive  `json:"drives"`
	Battery     string   `json:"battery,omitempty"`
	Temperature []Temp   `json:"temperature"`
}

type Drive struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
	Size  uint64 `json:"size"`
	Kind  string `json:"kind"` // SSD, hard disk, USB, SD card
	Bus   string `json:"bus,omitempty"`
}

type Temp struct {
	Name string  `json:"name"`
	C    float64 `json:"c"`
}

type OSFacts struct {
	Name      string `json:"name"`
	Kernel    string `json:"kernel"`
	Arch      string `json:"arch"`
	Boot      int64  `json:"boot"`
	Timezone  string `json:"timezone"`
	NTP       string `json:"ntp,omitempty"` // synchronised, not synchronised, or empty if unknown
	Docker    string `json:"docker,omitempty"`
	Hostname  string `json:"hostname"`
	Processes int    `json:"processes,omitempty"`
}

type Iface struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"` // Ethernet, Wi-Fi, Docker, VPN, Virtual
	MAC    string   `json:"mac,omitempty"`
	Up     bool     `json:"up"`
	Addrs  []string `json:"addrs"`
	Speed  int      `json:"speedMbps,omitempty"`
	MTU    int      `json:"mtu"`
	Router bool     `json:"router"` // carries the default route
}

type NetFacts struct {
	Interfaces []Iface  `json:"interfaces"`
	Gateway    string   `json:"gateway,omitempty"`
	DNS        []string `json:"dns"`
}

type ListenPort struct {
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
	Scope   string `json:"scope"` // network (reachable from other devices) or this server only
	Process string `json:"process,omitempty"`
	Address string `json:"address"`
}

type InsightsInfo struct {
	Hardware Hardware     `json:"hardware"`
	System   OSFacts      `json:"system"`
	Network  NetFacts     `json:"network"`
	Ports    []ListenPort `json:"ports"`
}

func readFirst(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// collectInsights gathers everything. It takes a second or two, so it runs when
// the page is opened rather than in the background.
func (s *Server) collectInsights(ctx context.Context) InsightsInfo {
	var out InsightsInfo

	// Hardware.
	h := &out.Hardware
	h.Vendor = cleanDMI(readFirst("/sys/class/dmi/id/sys_vendor"))
	h.Product = cleanDMI(readFirst("/sys/class/dmi/id/product_name"))
	if h.Product == "" {
		h.Product = cleanDMI(readFirst("/proc/device-tree/model")) // Raspberry Pi and other ARM boards
	}
	h.Board = cleanDMI(strings.TrimSpace(readFirst("/sys/class/dmi/id/board_vendor") + " " + readFirst("/sys/class/dmi/id/board_name")))
	if v := readFirst("/sys/class/dmi/id/bios_version"); v != "" {
		h.Bios = strings.TrimSpace(readFirst("/sys/class/dmi/id/bios_vendor") + " " + v)
		if d := readFirst("/sys/class/dmi/id/bios_date"); d != "" {
			h.Bios += " (" + d + ")"
		}
	}
	if isLinux() && hasCmd("systemd-detect-virt") {
		if v, err := runHost(ctx, 3*time.Second, "systemd-detect-virt"); err == nil && v != "none" {
			h.Virtual = v
		}
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		h.CPUModel = strings.Join(strings.Fields(infos[0].ModelName), " ")
		h.CPUMaxMHz = infos[0].Mhz
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		h.CPUCores = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		h.CPUThreads = n
	}
	if mhz := readFirst("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq"); mhz != "" {
		if k, err := strconv.ParseFloat(mhz, 64); err == nil {
			h.CPUMaxMHz = k / 1000
		}
	}
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		h.MemTotal = m.Total
	}
	h.GPUs = gpuList(ctx)
	h.Drives = driveList(ctx)
	h.Battery = batteryLine()
	h.Temperature = temperatures(ctx)

	// Operating system.
	o := &out.System
	if hi, err := host.InfoWithContext(ctx); err == nil {
		o.Name = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		o.Kernel = hi.KernelVersion
		o.Boot = int64(hi.BootTime)
		o.Hostname = hi.Hostname
		o.Processes = int(hi.Procs)
	}
	if pretty := osPrettyName(); pretty != "" {
		o.Name = pretty
	}
	o.Arch = runtime.GOARCH
	o.Timezone = timezoneName()
	if isLinux() && hasCmd("timedatectl") {
		if v, err := runHost(ctx, 3*time.Second, "timedatectl", "show", "-p", "NTPSynchronized", "--value"); err == nil {
			if v == "yes" {
				o.NTP = "synchronised"
			} else if v == "no" {
				o.NTP = "not synchronised"
			}
		}
	}
	o.Docker = s.sys.Snapshot().Docker

	// Network.
	out.Network = collectNet(ctx)
	out.Ports = listeningPorts(ctx)
	return out
}

func cleanDMI(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "to be filled by o.e.m.", "default string", "system manufacturer", "system product name", "not specified", "none", "unknown":
		return ""
	}
	return s
}

func osPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

func timezoneName() string {
	if tz := readFirst("/etc/timezone"); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	name, _ := time.Now().Zone()
	return name
}

func gpuList(ctx context.Context) []string {
	var out []string
	if isLinux() && hasCmd("lspci") {
		if s, err := runHost(ctx, 3*time.Second, "lspci"); err == nil {
			out = append(out, parseLspciGPUs(s)...)
		}
	}
	return out
}

// parseLspciGPUs picks the display adapters out of `lspci`.
func parseLspciGPUs(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		low := strings.ToLower(l)
		if !strings.Contains(low, "vga compatible") && !strings.Contains(low, "3d controller") && !strings.Contains(low, "display controller") {
			continue
		}
		if i := strings.Index(l, ": "); i >= 0 {
			l = l[i+2:]
		}
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

type lsblkOut struct {
	Blockdevices []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
		Size  uint64 `json:"size"`
		Type  string `json:"type"`
		Rota  any    `json:"rota"`
		Tran  string `json:"tran"`
	} `json:"blockdevices"`
}

func parseLsblk(js string) []Drive {
	var l lsblkOut
	if json.Unmarshal([]byte(js), &l) != nil {
		return nil
	}
	var out []Drive
	for _, d := range l.Blockdevices {
		if d.Type != "disk" || strings.HasPrefix(d.Name, "zram") || strings.HasPrefix(d.Name, "loop") {
			continue
		}
		rota := fmt.Sprint(d.Rota)
		kind := "SSD"
		switch {
		case strings.HasPrefix(d.Name, "mmcblk"):
			kind = "SD card or eMMC"
		case d.Tran == "usb":
			kind = "USB drive"
		case rota == "1" || rota == "true":
			kind = "Hard disk"
		}
		out = append(out, Drive{Name: d.Name, Model: strings.TrimSpace(d.Model), Size: d.Size, Kind: kind, Bus: strings.ToUpper(d.Tran)})
	}
	return out
}

func driveList(ctx context.Context) []Drive {
	if !isLinux() || !hasCmd("lsblk") {
		return nil
	}
	s, err := runHost(ctx, 4*time.Second, "lsblk", "-J", "-b", "-d", "-o", "NAME,MODEL,SIZE,TYPE,ROTA,TRAN")
	if err != nil {
		return nil
	}
	return parseLsblk(s)
}

func batteryLine() string {
	matches, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	for _, m := range matches {
		pct := readFirst(m + "/capacity")
		st := readFirst(m + "/status")
		if pct != "" {
			return strings.TrimSpace(pct + "% " + strings.ToLower(st))
		}
	}
	return ""
}

func temperatures(ctx context.Context) []Temp {
	var out []Temp
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	sort.Strings(zones)
	for _, z := range zones {
		t, err := strconv.ParseFloat(readFirst(z+"/temp"), 64)
		if err != nil || t <= 0 {
			continue
		}
		name := readFirst(z + "/type")
		if name == "" {
			name = filepath.Base(z)
		}
		out = append(out, Temp{Name: name, C: t / 1000})
	}
	return out
}

// --- network facts ------------------------------------------------------------------

func ifaceKind(name string) string {
	switch {
	case strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth"):
		return "Docker"
	case strings.HasPrefix(name, "tailscale") || strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "wg") || strings.HasPrefix(name, "tun"):
		return "VPN"
	case strings.HasPrefix(name, "wl") || strings.HasPrefix(name, "wlan"):
		return "Wi-Fi"
	case strings.HasPrefix(name, "virbr") || strings.HasPrefix(name, "vmnet") || strings.HasPrefix(name, "bridge"):
		return "Virtual"
	case strings.HasPrefix(name, "en") || strings.HasPrefix(name, "eth"):
		return "Ethernet"
	}
	return "Other"
}

// parseDefaultRoute reads /proc/net/route and returns the gateway and interface.
func parseDefaultRoute(table string) (gw, iface string) {
	for i, l := range strings.Split(table, "\n") {
		f := strings.Fields(l)
		if i == 0 || len(f) < 3 || f[1] != "00000000" {
			continue
		}
		raw, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil {
			continue
		}
		return net.IPv4(byte(raw), byte(raw>>8), byte(raw>>16), byte(raw>>24)).String(), f[0]
	}
	return "", ""
}

func parseResolvConf(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}

// defaultGateway returns the router's address and the interface that reaches it.
func defaultGateway(ctx context.Context) (gw, iface string) {
	if isLinux() {
		return parseDefaultRoute(readFirst("/proc/net/route"))
	}
	if hasCmd("route") {
		if s, err := runHost(ctx, 3*time.Second, "route", "-n", "get", "default"); err == nil {
			for _, l := range strings.Split(s, "\n") {
				l = strings.TrimSpace(l)
				if v, ok := strings.CutPrefix(l, "gateway: "); ok {
					gw = v
				}
				if v, ok := strings.CutPrefix(l, "interface: "); ok {
					iface = v
				}
			}
		}
	}
	return gw, iface
}

func collectNet(ctx context.Context) NetFacts {
	var nf NetFacts
	gw, gwIf := defaultGateway(ctx)
	nf.Gateway = gw
	nf.DNS = parseResolvConf(readFirst("/etc/resolv.conf"))
	// systemd-resolved lists 127.0.0.53 here; the real servers are in resolvectl.
	if isLinux() && len(nf.DNS) == 1 && strings.HasPrefix(nf.DNS[0], "127.0.0.") && hasCmd("resolvectl") {
		if s, err := runHost(ctx, 3*time.Second, "resolvectl", "dns"); err == nil {
			var real []string
			for _, l := range strings.Split(s, "\n") {
				if i := strings.Index(l, ":"); i >= 0 && !strings.Contains(l[:i], "docker") {
					for _, ip := range strings.Fields(l[i+1:]) {
						if net.ParseIP(ip) != nil && !contains(real, ip) {
							real = append(real, ip)
						}
					}
				}
			}
			if len(real) > 0 {
				nf.DNS = real
			}
		}
	}
	if nf.DNS == nil {
		nf.DNS = []string{}
	}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		i := Iface{Name: ifc.Name, Kind: ifaceKind(ifc.Name), MAC: ifc.HardwareAddr.String(), Up: ifc.Flags&net.FlagUp != 0, MTU: ifc.MTU, Router: ifc.Name == gwIf, Addrs: []string{}}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() {
				i.Addrs = append(i.Addrs, ipn.String())
			}
		}
		if sp, err := strconv.Atoi(readFirst("/sys/class/net/" + ifc.Name + "/speed")); err == nil && sp > 0 {
			i.Speed = sp
		}
		if len(i.Addrs) == 0 {
			continue // nothing to show for an interface with no address
		}
		nf.Interfaces = append(nf.Interfaces, i)
	}
	sort.SliceStable(nf.Interfaces, func(a, b int) bool {
		if nf.Interfaces[a].Router != nf.Interfaces[b].Router {
			return nf.Interfaces[a].Router
		}
		return nf.Interfaces[a].Up && !nf.Interfaces[b].Up
	})
	if nf.Interfaces == nil {
		nf.Interfaces = []Iface{}
	}
	return nf
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// parseSS reads `ss -H -tulnp`: one line per listening socket.
func parseSS(out string) []ListenPort {
	var ports []ListenPort
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		proto := f[0]
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		addr, ps := strings.Trim(local[:i], "[]"), local[i+1:]
		port, err := strconv.Atoi(ps)
		if err != nil {
			continue
		}
		scope := "network"
		if ip := net.ParseIP(strings.SplitN(addr, "%", 2)[0]); ip != nil && ip.IsLoopback() {
			scope = "this server only"
		}
		proc := ""
		if j := strings.Index(l, `users:(("`); j >= 0 {
			rest := l[j+len(`users:(("`):]
			if k := strings.Index(rest, `"`); k >= 0 {
				proc = rest[:k]
			}
		}
		key := fmt.Sprintf("%s/%d/%s", proto, port, scope)
		if seen[key] {
			continue // IPv4 and IPv6 listeners are one thing to a reader
		}
		seen[key] = true
		ports = append(ports, ListenPort{Port: port, Proto: strings.ToUpper(proto), Scope: scope, Process: proc, Address: addr})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Scope != ports[j].Scope {
			return ports[i].Scope == "network"
		}
		return ports[i].Port < ports[j].Port
	})
	return ports
}

func listeningPorts(ctx context.Context) []ListenPort {
	if !isLinux() || !hasCmd("ss") {
		return []ListenPort{}
	}
	s, err := runHost(ctx, 4*time.Second, "ss", "-H", "-tulnp")
	if err != nil {
		return []ListenPort{}
	}
	p := parseSS(s)
	if p == nil {
		return []ListenPort{}
	}
	return p
}

// --- public address ------------------------------------------------------------------

type PublicInfo struct {
	IP       string    `json:"ip"`
	City     string    `json:"city"`
	Region   string    `json:"region"`
	Country  string    `json:"country"`
	ISP      string    `json:"isp"`
	Timezone string    `json:"timezone"`
	Checked  time.Time `json:"checked"`
}

var (
	publicMu    sync.Mutex
	publicCache *PublicInfo
)

// lookupPublic asks ipwho.is where the server's internet connection appears to be.
// The location is the provider's guess for the address, usually a city near the
// exchange rather than the building. It only runs when the person asks.
func lookupPublic(ctx context.Context) (*PublicInfo, error) {
	publicMu.Lock()
	defer publicMu.Unlock()
	if publicCache != nil && time.Since(publicCache.Checked) < time.Hour {
		return publicCache, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://ipwho.is/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the lookup service. Is the server online?")
	}
	defer resp.Body.Close()
	var r struct {
		Success    bool   `json:"success"`
		IP         string `json:"ip"`
		City       string `json:"city"`
		Region     string `json:"region"`
		Country    string `json:"country"`
		Connection struct {
			ISP string `json:"isp"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || !r.Success {
		return nil, fmt.Errorf("the lookup service didn't answer properly")
	}
	publicCache = &PublicInfo{IP: r.IP, City: r.City, Region: r.Region, Country: r.Country, ISP: r.Connection.ISP, Timezone: r.Timezone.ID, Checked: time.Now()}
	return publicCache, nil
}

// --- HTTP ------------------------------------------------------------------------------

func (s *Server) getInsights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.collectInsights(ctx))
}

func (s *Server) getPublicAddress(w http.ResponseWriter, r *http.Request) {
	p, err := lookupPublic(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}
