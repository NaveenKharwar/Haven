package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	hostingID       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,63}$`)
	cloudflareID    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	tunnelID        = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
	hostingHostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

type hostingPublication struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Mode     string `json:"mode"`
	DNS      string `json:"dns"`
	Address  string `json:"address,omitempty"`
	RecordID string `json:"recordId,omitempty"`
	NextIP   string `json:"nextIp,omitempty"`
	Network  string `json:"network,omitempty"`
	Phase    string `json:"phase"`
	External bool   `json:"external,omitempty"`
	IPError  string `json:"ipError,omitempty"`
}

type hostingSite struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Template    string              `json:"template"`
	ServiceID   string              `json:"serviceId,omitempty"`
	PrivatePort int                 `json:"privatePort"`
	Phase       string              `json:"phase"`
	Deployment  string              `json:"deployment,omitempty"`
	LastError   string              `json:"lastError,omitempty"`
	Publication *hostingPublication `json:"publication,omitempty"`
}

func (s hostingSite) marker() string { return "haven-hosting/" + s.ID }

func (s hostingSite) privateURL() string { return fmt.Sprintf("http://127.0.0.1:%d", s.PrivatePort) }

func (s hostingSite) ingress() hostingIngress {
	spec := hostingTemplates[s.Template]
	return hostingIngress{Host: s.Publication.Host, Service: fmt.Sprintf("http://%s-%s:%d", spec.Web, s.ServiceID, spec.Port)}
}

type hostingState struct {
	Coolify    hostingConnection `json:"coolify"`
	Cloudflare hostingCloudflare `json:"cloudflare"`
	TunnelName string            `json:"tunnelName,omitempty"`
	TunnelID   string            `json:"tunnelId,omitempty"`
	Sites      []hostingSite     `json:"sites"`
}

type hostingSecrets struct {
	Coolify        string `json:"coolify"`
	CoolifyOrigin  string `json:"coolifyOrigin"`
	Cloudflare     string `json:"cloudflare"`
	CloudflareZone string `json:"cloudflareZone"`
}

type hostingPreview struct {
	ID     string             `json:"id"`
	Site   string             `json:"site"`
	Expiry time.Time          `json:"expiry"`
	Plan   hostingPublication `json:"plan"`
}

type Hosting struct {
	dir       string
	docker    *Docker
	store     *Store
	jobs      *Jobs
	http      *http.Client
	cfBase    string
	enabled   bool
	op        sync.Mutex
	previews  map[string]hostingPreview
	catalogMu sync.Mutex
	catalog   []hostingTemplate
}

func newHosting(st *Store, jobs *Jobs, docker *Docker) *Hosting {
	return &Hosting{
		dir: filepath.Join(st.dir, "hosting"), docker: docker, store: st, jobs: jobs,
		http: &http.Client{Timeout: 18 * time.Second, CheckRedirect: noHostingRedirect}, cfBase: cloudflareHostingAPI,
		enabled: os.Getenv("HAVEN_HOSTING_EXPERIMENTAL") == "1", previews: make(map[string]hostingPreview),
	}
}

func noHostingRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func randomHostingID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func (h *Hosting) read(name string, v any) error {
	data, err := os.ReadFile(filepath.Join(h.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || json.Unmarshal(data, v) != nil {
		return errors.New("Haven couldn't read its hosting configuration.")
	}
	return nil
}

func (h *Hosting) write(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return h.writeFile(name, data)
}

func (h *Hosting) writeFile(name string, data []byte) error {
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(h.dir, name), data, 0o600)
}

func (h *Hosting) load() (hostingState, hostingSecrets, error) {
	var st hostingState
	var keys hostingSecrets
	if err := h.read("state.json", &st); err != nil {
		return st, keys, err
	}
	if err := h.read("secrets.json", &keys); err != nil {
		return st, keys, err
	}
	if keys.CoolifyOrigin != st.Coolify.URL {
		keys.Coolify = ""
	}
	if keys.CloudflareZone != st.Cloudflare.Account+"/"+st.Cloudflare.Zone {
		keys.Cloudflare = ""
	}
	return st, keys, nil
}

func (h *Hosting) save(st hostingState) error { return h.write("state.json", st) }

func (h *Hosting) coolify(st hostingState, keys hostingSecrets) coolifyClient {
	return coolifyClient{st.Coolify, keys.Coolify, h.http}
}

func (h *Hosting) cloudflare(st hostingState, keys hostingSecrets) cloudflareHostingClient {
	return cloudflareHostingClient{h.http, h.cfBase, keys.Cloudflare, st.Cloudflare}
}

func (st *hostingState) site(id string) *hostingSite {
	for i := range st.Sites {
		if st.Sites[i].ID == id {
			return &st.Sites[i]
		}
	}
	return nil
}

func (st hostingState) tunnelRoutes(current string) (allowed, desired []hostingIngress) {
	for _, site := range st.Sites {
		p := site.Publication
		if p == nil || p.Mode != "tunnel" {
			continue
		}
		allowed = append(allowed, site.ingress())
		if p.Phase == "configured" || (p.Phase == "publishing" && site.ID == current) {
			desired = append(desired, site.ingress())
		}
	}
	return
}

func normalizeHostingHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if len(host) > 253 || net.ParseIP(host) != nil || !hostingHostname.MatchString(host) {
		return "", errors.New("Enter an exact public hostname, such as example.com or blog.example.com.")
	}
	tld := host[strings.LastIndexByte(host, '.')+1:]
	if tld == "local" || tld == "localhost" || tld == "test" || tld == "invalid" || tld == "example" || tld == "internal" || tld == "onion" {
		return "", errors.New("Choose a hostname under a public DNS domain you control.")
	}
	return host, nil
}

func (h *Hosting) validatePlan(ctx context.Context, st hostingState, keys hostingSecrets, site hostingSite, plan hostingPublication, resume bool) (hostingPublication, error) {
	if site.Phase != "installed" {
		return plan, errors.New("Finish installing the website before publishing it.")
	}
	for _, other := range st.Sites {
		if other.ID != site.ID && other.Publication != nil && other.Publication.Host == plan.Host {
			return plan, errors.New("That hostname is already reserved by another Haven website.")
		}
	}
	dashboard, _ := netHost(st.Coolify.URL)
	if strings.EqualFold(dashboard, plan.Host) {
		return plan, errors.New("The Coolify dashboard hostname cannot be used for a website.")
	}
	c := h.coolify(st, keys)
	version, err := c.version(ctx)
	if err != nil {
		return plan, err
	}
	if version != coolifyHostingVersion {
		return plan, errors.New("Publishing requires the supported Coolify version.")
	}
	_, app, err := c.service(ctx, site)
	if err != nil {
		return plan, err
	}
	if app.domain() != "" && !(resume && plan.Mode == "direct" && sameCoolifyDomain(app.domain(), coolifyPortDomain(site, plan.Host))) {
		return plan, errors.New("The website already has a domain in Coolify. Remove it there before publishing through Haven.")
	}
	network, err := h.inspectSite(ctx, site)
	if err != nil {
		return plan, err
	}
	if plan.Mode == "tunnel" {
		plan.Network = network
	} else {
		if err := h.checkDirectProxy(ctx); err != nil {
			return plan, err
		}
		if err := c.checkDNSAutomation(ctx, plan.Host); err != nil {
			return plan, err
		}
		if !resume || plan.DNS == "cloudflare" {
			address, err := lookupPublic(ctx)
			if err != nil {
				return plan, err
			}
			ip, valid := hostingPublicIP(address.IP)
			if !valid || !ip.Is4() {
				return plan, errors.New("Haven couldn't verify a public IPv4 address for direct publishing.")
			}
			if plan.Address != "" && plan.Address != ip.String() {
				return plan, errors.New("The selected IPv4 differs from the observed public IPv4. Verify the router WAN address or unpublish the pending plan and start again.")
			}
			plan.Address = ip.String()
		}
	}

	switch plan.DNS {
	case "cloudflare":
		if keys.Cloudflare == "" || st.Cloudflare.Zone == "" {
			return plan, errors.New("Connect an active Cloudflare zone before choosing managed DNS.")
		}
		cf := h.cloudflare(st, keys)
		zone, err := cf.zone(ctx)
		if err != nil {
			return plan, err
		}
		if plan.Host != zone && !strings.HasSuffix(plan.Host, "."+zone) {
			return plan, errors.New("The hostname is outside the connected Cloudflare zone.")
		}
		site.Publication = &plan
		if _, err := cf.ownedDNS(ctx, wantedHostingDNS(site, st.TunnelID), false); err != nil {
			return plan, err
		}
	case "manual":
		ips, err := hostingPublicDNS(ctx, h.http, plan.Host)
		if err != nil {
			return plan, err
		}
		for _, ip := range ips {
			if ip.String() != plan.Address {
				return plan, errors.New("The hostname already resolves to another public address. Resolve the DNS conflict before publishing.")
			}
		}
	}
	return plan, nil
}

func (h *Hosting) installSite(ctx context.Context, j *Job, st hostingState, keys hostingSecrets, site *hostingSite) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		site.Deployment = "failed"
		site.LastError = retErr.Error()
		_ = h.save(st)
	}()
	c := h.coolify(st, keys)
	version, err := c.version(ctx)
	if err != nil {
		return err
	}
	if version != coolifyHostingVersion {
		return errors.New("Verify the supported Coolify version before installing a website.")
	}
	dockerVersion, err := h.docker.Version(ctx)
	if err != nil || !dockerLoopbackSafe(dockerVersion) {
		return errors.New("Docker Engine 28 or later is required for loopback-only website ports.")
	}
	if site.ServiceID == "" {
		for !portFree(site.PrivatePort, "tcp") {
			next, err := privateHostingPort(st)
			if err != nil {
				return err
			}
			site.PrivatePort = next
			if err := h.save(st); err != nil {
				return err
			}
		}
		j.Logf("Creating a private Coolify service for %s.", site.Name)
		compose, err := h.siteCompose(*site)
		if err != nil {
			return err
		}
		id, err := c.create(ctx, *site, compose)
		if err != nil {
			return err
		}
		site.ServiceID = id
		if err := h.save(st); err != nil {
			return err
		}
	}
	site.Deployment, site.LastError = "queued", ""
	if err := h.save(st); err != nil {
		return err
	}
	_, app, err := c.service(ctx, *site)
	if err != nil {
		return err
	}
	if app.domain() != "" {
		return errors.New("Coolify assigned a hostname to this service. Remove it in Coolify before retrying the private installation.")
	}
	if err := c.action(ctx, *site, "start"); err != nil {
		return err
	}
	j.Logf("Coolify queued the private deployment. Waiting for the web container to become ready.")
	site.Deployment = "deploying"
	if err := h.save(st); err != nil {
		return err
	}
	if err := h.waitSiteReady(ctx, j, c, *site); err != nil {
		return err
	}
	site.Phase, site.Deployment, site.LastError = "installed", "ready", ""
	j.Logf("The private website is running and ready to check.")
	return h.save(st)
}

func privateHostingPort(st hostingState) (int, error) {
	ports := make(map[int]bool, len(st.Sites))
	for _, site := range st.Sites {
		ports[site.PrivatePort] = true
	}
	for port := 18080; port < 19080; port++ {
		if !ports[port] && portFree(port, "tcp") {
			return port, nil
		}
	}
	return 0, errors.New("No private website port is available.")
}

func (h *Hosting) publish(ctx context.Context, j *Job, st hostingState, keys hostingSecrets, site *hostingSite) error {
	p := site.Publication
	c := h.coolify(st, keys)
	plan, err := h.validatePlan(ctx, st, keys, *site, *p, true)
	if err != nil {
		return err
	}
	*p = plan
	if err := h.save(st); err != nil {
		return err
	}
	_, app, err := c.service(ctx, *site)
	if err != nil {
		return err
	}
	j.Logf("Setting %s as the website’s canonical HTTPS hostname.", p.Host)
	if err := c.setCanonical(ctx, *site, p.Host, site.privateURL()); err != nil {
		return err
	}
	if p.Mode == "direct" {
		if err := c.setDomain(ctx, *site, app, coolifyPortDomain(*site, p.Host)); err != nil {
			return err
		}
	}
	if err := c.action(ctx, *site, "restart"); err != nil {
		return err
	}
	cf := h.cloudflare(st, keys)
	if p.Mode == "tunnel" {
		if st.TunnelName == "" {
			id, err := randomHostingID()
			if err != nil {
				return err
			}
			st.TunnelName = "haven-" + id
			if err := h.save(st); err != nil {
				return err
			}
		}
		if st.TunnelID == "" {
			tunnel, err := cf.ensureTunnel(ctx, st.TunnelName)
			if err != nil {
				return err
			}
			st.TunnelID = tunnel.ID
			if err := h.save(st); err != nil {
				return err
			}
		}
		allowed, desired := st.tunnelRoutes(site.ID)
		if err := cf.enforceIngress(ctx, st.TunnelID, allowed, desired); err != nil {
			return err
		}
		token, err := cf.connectorToken(ctx, st.TunnelID)
		if err != nil {
			return err
		}
		if err := h.ensureConnector(ctx, st.TunnelID, token); err != nil {
			return err
		}
		if err := h.connectorNetwork(ctx, st.TunnelID, p.Network, true); err != nil {
			return err
		}
		j.Logf("Added the exact hostname route to the dedicated Cloudflare Tunnel.")
	}
	if p.Mode == "direct" {
		if err := h.waitDirectRoute(ctx, *site, true); err != nil {
			return err
		}
	}
	if p.DNS == "cloudflare" {
		id, err := cf.ensureDNS(ctx, wantedHostingDNS(*site, st.TunnelID))
		if err != nil {
			return err
		}
		p.RecordID = id
	}
	p.Phase = "configured"
	if err := h.save(st); err != nil {
		return err
	}
	j.Logf("Configuration saved. Use Check website to verify DNS and HTTPS.")
	return nil
}

func (h *Hosting) unpublish(ctx context.Context, j *Job, st hostingState, keys hostingSecrets, site *hostingSite) error {
	p := site.Publication
	c := h.coolify(st, keys)
	cf := h.cloudflare(st, keys)
	if p.Mode == "tunnel" && st.TunnelID != "" {
		allowed, desired := st.tunnelRoutes(site.ID)
		if err := cf.enforceIngress(ctx, st.TunnelID, allowed, desired); err != nil {
			return err
		}
		if p.Network != "" {
			if err := h.connectorNetwork(ctx, st.TunnelID, p.Network, false); err != nil {
				return err
			}
		}
	}
	_, app, err := c.service(ctx, *site)
	if err != nil {
		return err
	}
	if p.Mode == "direct" && app.domain() != "" {
		if !sameCoolifyDomain(app.domain(), coolifyPortDomain(*site, p.Host)) {
			return errors.New("The site’s domain was changed in Coolify. Remove the public route there before retrying.")
		}
		if err := c.setDomain(ctx, *site, app, ""); err != nil {
			return err
		}
	}
	if err := c.setCanonical(ctx, *site, "", site.privateURL()); err != nil {
		return err
	}
	if err := c.action(ctx, *site, "restart"); err != nil {
		return err
	}
	if p.Mode == "direct" {
		if err := h.waitDirectRoute(ctx, *site, false); err != nil {
			return err
		}
	}
	if p.DNS == "cloudflare" && (p.Mode != "tunnel" || st.TunnelID != "") {
		if p.NextIP != "" {
			if err := cf.removeDNS(ctx, wantedHostingDNS(*site, st.TunnelID), p.NextIP); err != nil {
				return err
			}
		} else if err := cf.removeDNS(ctx, wantedHostingDNS(*site, st.TunnelID)); err != nil {
			return err
		}
	}
	site.Publication = nil
	if err := h.save(st); err != nil {
		return err
	}
	j.Logf("The Haven public route was removed. Website files and the database remain in Coolify.")
	return nil
}

type hostingLive struct {
	Deployment  string `json:"deployment,omitempty"`
	Local       string `json:"local,omitempty"`
	Connector   string `json:"connector,omitempty"`
	Route       string `json:"route,omitempty"`
	DNS         string `json:"dns,omitempty"`
	HTTPS       string `json:"https,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	Verified    bool   `json:"verified"`
}

func (h *Hosting) live(ctx context.Context, st hostingState, keys hostingSecrets, site hostingSite) hostingLive {
	var result hostingLive
	service, app, err := h.coolify(st, keys).service(ctx, site)
	if err != nil {
		result.Deployment = err.Error()
	} else {
		result.Deployment = app.Status
		if result.Deployment == "" {
			result.Deployment = service.Status
		}
		if result.Deployment == "" {
			result.Deployment = "Check deployment in Coolify."
		}
	}
	_, localErr := h.inspectSite(ctx, site)
	if localErr != nil {
		result.Local = localErr.Error()
	} else {
		result.Local = "Private web container running"
	}
	p := site.Publication
	if p == nil {
		return result
	}

	routeOK := false
	cf := h.cloudflare(st, keys)
	if p.Mode == "tunnel" {
		if st.TunnelID == "" {
			result.Connector = "Waiting for the dedicated tunnel."
		} else {
			var tunnel hostingTunnel
			if err := cf.call(ctx, http.MethodGet, cf.tunnelPath("/"+st.TunnelID), nil, &tunnel); err != nil {
				result.Connector = err.Error()
			} else {
				result.Connector = tunnel.Status
			}
			routes, err := cf.ingress(ctx, st.TunnelID)
			allowed, _ := st.tunnelRoutes("")
			routeOK = err == nil && len(routes) > 1 && routes[len(routes)-1] == hostingDenyIngress &&
				containsIngress(routes, site.ingress()) && validateHostingIngress(routes, allowed) == nil
		}
	} else {
		_, secure, _ := h.directRoute(ctx, site)
		routeOK = secure && sameCoolifyDomain(app.domain(), coolifyPortDomain(site, p.Host))
	}
	result.Route = "Waiting for the exact public route."
	if routeOK {
		result.Route = "Exact hostname route configured"
	}

	dnsOK := p.DNS == "manual"
	if p.DNS == "cloudflare" && (p.Mode != "tunnel" || st.TunnelID != "") {
		if record, err := cf.ownedDNS(ctx, wantedHostingDNS(site, st.TunnelID), false); err != nil {
			result.DNS = err.Error()
		} else {
			dnsOK = record != nil
		}
	}
	ips, err := hostingPublicDNS(ctx, h.http, p.Host)
	if result.DNS == "" {
		result.DNS = "Waiting for matching public DNS."
	}
	if err != nil {
		result.DNS = err.Error()
		return result
	}
	if p.Mode == "direct" {
		dnsOK = dnsOK && slices.ContainsFunc(ips, func(ip netip.Addr) bool { return ip.String() == p.Address })
		for _, ip := range ips {
			dnsOK = dnsOK && ip.String() == p.Address
		}
	}
	dnsOK = dnsOK && len(ips) > 0
	if dnsOK {
		result.DNS = "Public DNS resolves"
	}
	result.HTTPS = "Waiting for a trusted HTTPS response."
	if dnsOK && routeOK {
		if expires, err := hostingHTTPS(ctx, p.Host, ips); err != nil {
			result.HTTPS = err.Error()
		} else {
			result.HTTPS = "Trusted HTTPS response verified from Haven"
			result.Certificate = "Valid through " + expires.UTC().Format("2006-01-02") + " UTC"
			if time.Until(expires) < 14*24*time.Hour {
				result.Certificate += ". Check certificate renewal with your provider."
			}
			tunnelOnline := p.Mode == "tunnel" && (result.Connector == "healthy" || result.Connector == "degraded")
			result.Verified = p.Phase == "configured" && localErr == nil && (tunnelOnline || p.External)
		}
	}
	return result
}

func (h *Hosting) catalogEntries(ctx context.Context) ([]hostingTemplate, bool) {
	if h.catalog != nil {
		return h.catalog, true
	}
	var upstream map[string]struct {
		Slogan string `json:"slogan"`
	}
	endpoint := "https://raw.githubusercontent.com/coollabsio/coolify/v" + coolifyHostingVersion + "/templates/service-templates.json"
	err := hostingJSON(ctx, h.http, "Coolify catalog", http.MethodGet, endpoint, "", nil, &upstream)
	entries := slices.Collect(maps.Values(hostingTemplates))
	if err == nil {
		for id, template := range upstream {
			if _, guided := hostingTemplates[id]; !guided {
				entries = append(entries, hostingTemplate{ID: id, Name: strings.ReplaceAll(id, "-", " "), Description: template.Slogan})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Guided != entries[j].Guided {
			return entries[i].Guided
		}
		return entries[i].ID < entries[j].ID
	})
	if err == nil {
		h.catalog = entries
	}
	return entries, err == nil
}

func netHost(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}
