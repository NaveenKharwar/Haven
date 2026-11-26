package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func (s *Server) hostingRoutes(mux *http.ServeMux) {
	for _, path := range []string{"", "/preflight", "/catalog", "/sites/{id}/status", "/sites/{id}/logs", "/sites/{id}/recovery"} {
		mux.Handle("GET /api/hosting"+path, s.need(RoleAdmin, s.hostingRead))
	}
	for _, path := range []string{"/connect", "/discover", "/cloudflare", "/cloudflare/check", "/setup", "/sites", "/sites/{id}/retry",
		"/sites/{id}/preview", "/sites/{id}/publish", "/sites/{id}/unpublish", "/sites/{id}/external"} {
		mux.Handle("POST /api/hosting"+path, s.need(RoleAdmin, s.hostingWrite))
	}
}

func (s *Server) hostingRead(w http.ResponseWriter, r *http.Request) {
	h := s.hosting
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	st, keys, err := h.load()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.Pattern == "GET /api/hosting" {
		if st.Sites == nil {
			st.Sites = []hostingSite{}
		}
		writeJSON(w, http.StatusOK, struct {
			hostingState
			Enabled bool   `json:"enabled"`
			Version string `json:"version"`
		}{st, h.enabled, coolifyHostingVersion})
		return
	}
	if !h.enabled {
		jsonError(w, http.StatusServiceUnavailable, "Website hosting is disabled on this server.")
		return
	}
	switch r.Pattern {
	case "GET /api/hosting/preflight":
		writeJSON(w, http.StatusOK, h.setupChecks(ctx))
	case "GET /api/hosting/catalog":
		h.catalogMu.Lock()
		entries, complete := h.catalogEntries(ctx)
		h.catalogMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "complete": complete})
	default:
		site := st.site(r.PathValue("id"))
		if site == nil || site.ServiceID == "" {
			jsonError(w, http.StatusNotFound, "No matching deployed website.")
			return
		}
		if r.Pattern == "GET /api/hosting/sites/{id}/status" {
			writeJSON(w, http.StatusOK, h.live(ctx, st, keys, *site))
			return
		}
		c := h.coolify(st, keys)
		if r.Pattern == "GET /api/hosting/sites/{id}/recovery" {
			w.Header().Set("Content-Disposition", `attachment; filename="haven-website-recovery.json"`)
			writeJSON(w, http.StatusOK, c.recovery(ctx, *site))
			return
		}
		if _, _, err := c.service(ctx, *site); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		var response struct {
			Logs string `json:"logs"`
		}
		path := "/services/" + site.ServiceID + "/logs?lines=100&sub_service_name=" + url.QueryEscape(hostingTemplates[site.Template].Web)
		if err := c.call(ctx, http.MethodGet, path, nil, &response); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error()+" Open Coolify to inspect logs if the token lacks read:sensitive.")
			return
		}
		response.Logs = redactHostingLogs(response.Logs, keys)
		writeJSON(w, http.StatusOK, response)
	}
}

func redactHostingLogs(log string, keys hostingSecrets) string {
	for _, key := range []string{keys.Coolify, keys.Cloudflare} {
		if key != "" {
			log = strings.ReplaceAll(log, key, "[redacted]")
		}
	}
	return log
}

type hostingRequest struct {
	hostingConnection
	Token      string `json:"token"`
	Account    string `json:"account"`
	Zone       string `json:"zone"`
	Domain     string `json:"domain"`
	Name       string `json:"name"`
	Template   string `json:"template"`
	StaticHTML string `json:"staticHtml"`
	Host       string `json:"host"`
	Mode       string `json:"mode"`
	DNS        string `json:"dns"`
	WAN        string `json:"wan"`
	Review     string `json:"review"`
	Confirm    bool   `json:"confirm"`
	Recover    bool   `json:"recover"`
	ProxySafe  bool   `json:"proxySafe"`
}

func (s *Server) hostingWrite(w http.ResponseWriter, r *http.Request) {
	h := s.hosting
	if !h.enabled && r.Pattern != "POST /api/hosting/sites/{id}/unpublish" {
		jsonError(w, http.StatusServiceUnavailable, "Website hosting is disabled on this server.")
		return
	}
	var req hostingRequest
	if err := readJSON(r, &req); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.op.TryLock() {
		jsonError(w, http.StatusConflict, "Another hosting operation is running. Wait for it to finish and retry.")
		return
	}
	var job *Job
	defer func() {
		if job == nil {
			h.op.Unlock()
		}
	}()
	st, keys, err := h.load()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	startFor := func(action string, deadline time.Duration, fn func(context.Context, *Job) error) {
		job = h.jobs.Start("hosting", "hosting-"+action, deadline, func(ctx context.Context, j *Job) error {
			defer h.op.Unlock()
			return fn(ctx, j)
		})
		s.audit(r, "website hosting: %s", action)
		writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
	}
	start := func(action string, fn func(context.Context, *Job) error) { startFor(action, 15*time.Minute, fn) }
	fail := func(err error) { jsonError(w, http.StatusBadRequest, err.Error()) }

	switch r.Pattern {
	case "POST /api/hosting/setup":
		if !req.Confirm {
			fail(errors.New("Confirm installation of Coolify on this server."))
			return
		}
		startFor("setup", 30*time.Minute, func(ctx context.Context, j *Job) error { return h.installCoolify(ctx, j, req.Recover) })
	case "POST /api/hosting/discover", "POST /api/hosting/connect":
		discover := r.Pattern == "POST /api/hosting/discover"
		result, err := h.connect(ctx, st, keys, req, discover)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	case "POST /api/hosting/cloudflare":
		if err := h.connectCloudflare(ctx, st, keys, req); err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"connected": true})
	case "POST /api/hosting/cloudflare/check":
		config, err := h.checkCloudflare(ctx, st, req)
		if err != nil {
			fail(err)
			return
		}
		writeJSON(w, http.StatusOK, config)
	case "POST /api/hosting/sites":
		if !req.Confirm || st.Coolify.URL == "" || keys.Coolify == "" {
			fail(errors.New("Connect Coolify and confirm a private installation first."))
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if _, ok := hostingTemplates[req.Template]; !ok || len(req.Name) == 0 || len(req.Name) > 80 ||
			strings.ContainsAny(req.Name, "\r\n\t") {
			fail(errors.New("Choose a guided template and enter a website name of 1–80 characters."))
			return
		}
		id, err := randomHostingID()
		if err != nil {
			fail(err)
			return
		}
		port, err := privateHostingPort(st)
		if err != nil {
			fail(err)
			return
		}
		if req.Template == "static-html" {
			if err := h.saveStaticSite(id, req.StaticHTML); err != nil {
				fail(err)
				return
			}
		}
		st.Sites = append(st.Sites, hostingSite{ID: id, Name: req.Name, Template: req.Template, PrivatePort: port, Phase: "installing", Deployment: "queued"})
		if err := h.save(st); err != nil {
			fail(err)
			return
		}
		start("install", func(ctx context.Context, j *Job) error { return h.installSite(ctx, j, st, keys, st.site(id)) })
	default:
		site := st.site(r.PathValue("id"))
		if site == nil {
			jsonError(w, http.StatusNotFound, "No matching website.")
			return
		}
		switch r.Pattern {
		case "POST /api/hosting/sites/{id}/retry":
			if !req.Confirm || site.Phase != "installing" {
				fail(errors.New("Confirm a retry for a pending installation."))
				return
			}
			start("install", func(ctx context.Context, j *Job) error { return h.installSite(ctx, j, st, keys, site) })
		case "POST /api/hosting/sites/{id}/preview":
			preview, err := h.preview(ctx, st, keys, *site, req, r.Host)
			if err != nil {
				fail(err)
				return
			}
			writeJSON(w, http.StatusOK, preview)
		case "POST /api/hosting/sites/{id}/publish":
			preview, ok := h.previews[req.Review]
			if !req.Confirm || !ok || preview.Site != site.ID || time.Now().After(preview.Expiry) {
				fail(errors.New("Review and explicitly confirm this exact website and hostname before publishing."))
				return
			}
			if site.Publication != nil && (site.Publication.Phase != "publishing" || site.Publication.ID != preview.Plan.ID) {
				fail(errors.New("This website already has a publishing configuration. Unpublish it before making a new plan."))
				return
			}
			// Consume all previews for this website, including older browser tabs.
			for id, p := range h.previews {
				if p.Site == site.ID {
					delete(h.previews, id)
				}
			}
			site.Publication = &preview.Plan
			if err := h.save(st); err != nil {
				fail(err)
				return
			}
			start("publish", func(ctx context.Context, j *Job) error { return h.publish(ctx, j, st, keys, site) })
		case "POST /api/hosting/sites/{id}/unpublish":
			if !req.Confirm || site.Publication == nil {
				fail(errors.New("Confirm removal of the website’s public route."))
				return
			}
			site.Publication.Phase = "removing"
			if err := h.save(st); err != nil {
				fail(err)
				return
			}
			start("unpublish", func(ctx context.Context, j *Job) error { return h.unpublish(ctx, j, st, keys, site) })
		case "POST /api/hosting/sites/{id}/external":
			if !req.Confirm || site.Publication == nil || site.Publication.Mode != "direct" || site.Publication.Phase != "configured" {
				fail(errors.New("Confirm that you reached this exact HTTPS website from outside your LAN and Tailscale."))
				return
			}
			site.Publication.External = true
			if err := h.save(st); err != nil {
				fail(err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"confirmed": true})
		}
	}
}

func validHostingToken(token string) bool {
	return len(token) >= 12 && len(token) <= 512 && !strings.ContainsAny(token, " \r\n\t")
}

func (h *Hosting) connect(ctx context.Context, st hostingState, keys hostingSecrets, req hostingRequest, discover bool) (any, error) {
	connection := req.hostingConnection
	address, err := validateCoolifyURL(connection.URL)
	if err != nil {
		return nil, err
	}
	connection.URL = address
	if !validHostingToken(req.Token) {
		return nil, errors.New("Enter a valid Coolify API token.")
	}
	if connection.Project != "" && !hostingID.MatchString(connection.Project) {
		return nil, errors.New("Enter a valid Coolify project UUID.")
	}
	c := coolifyClient{connection, req.Token, h.http}
	if discover {
		version, err := c.version(ctx)
		if err != nil {
			return nil, err
		}
		servers, err := c.resources(ctx, "/servers")
		if err != nil {
			return nil, err
		}
		projects, err := c.resources(ctx, "/projects")
		if err != nil {
			return nil, err
		}
		var environments, destinations []hostingResource
		if connection.Project != "" {
			environments, err = c.resources(ctx, "/projects/"+connection.Project+"/environments")
			if err != nil {
				return nil, err
			}
		}
		if hostingID.MatchString(connection.Server) {
			destinations, err = c.resources(ctx, "/servers/"+connection.Server+"/destinations")
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"version": version, "servers": servers, "projects": projects, "environments": environments, "destinations": destinations}, nil
	}
	if !hostingID.MatchString(connection.Server) || !hostingID.MatchString(connection.Project) ||
		!hostingID.MatchString(connection.Environment) || (connection.Destination != "" && !hostingID.MatchString(connection.Destination)) {
		return nil, errors.New("Choose the local server, project, environment, and optional destination from Coolify.")
	}
	if len(st.Sites) > 0 && st.Coolify != connection {
		return nil, errors.New("This Coolify target already owns Haven websites. Only token rotation is available.")
	}
	if err := c.validate(ctx); err != nil {
		return nil, err
	}
	st.Coolify = connection
	keys.Coolify, keys.CoolifyOrigin = req.Token, connection.URL
	if err := h.write("secrets.json", keys); err != nil {
		return nil, err
	}
	return map[string]bool{"connected": true}, h.save(st)
}

func (h *Hosting) connectCloudflare(ctx context.Context, st hostingState, keys hostingSecrets, req hostingRequest) error {
	config, err := h.checkCloudflare(ctx, st, req)
	if err != nil {
		return err
	}
	st.Cloudflare = config
	keys.Cloudflare, keys.CloudflareZone = req.Token, config.Account+"/"+config.Zone
	if err := h.write("secrets.json", keys); err != nil {
		return err
	}
	return h.save(st)
}

func (h *Hosting) checkCloudflare(ctx context.Context, st hostingState, req hostingRequest) (hostingCloudflare, error) {
	if !validHostingToken(req.Token) {
		return hostingCloudflare{}, errors.New("Enter a valid Cloudflare API token.")
	}
	locked := st.TunnelName != ""
	for _, site := range st.Sites {
		locked = locked || (site.Publication != nil && site.Publication.DNS == "cloudflare")
	}
	c := cloudflareHostingClient{h.http, h.cfBase, req.Token, hostingCloudflare{}}
	var config hostingCloudflare
	var err error
	if locked {
		config = st.Cloudflare
		if req.Domain != "" && !strings.EqualFold(strings.TrimSpace(req.Domain), config.Name) {
			return hostingCloudflare{}, errors.New("Existing Haven routes use the connected zone. Enter the same domain to rotate its token.")
		}
		c.config = config
		_, err = c.zone(ctx)
	} else if strings.TrimSpace(req.Domain) != "" {
		config, err = c.discoverZone(ctx, strings.TrimSpace(req.Domain))
	} else if cloudflareID.MatchString(req.Account) && cloudflareID.MatchString(req.Zone) {
		config = hostingCloudflare{Account: req.Account, Zone: req.Zone}
		c.config = config
		config.Name, err = c.zone(ctx)
	} else {
		return hostingCloudflare{}, errors.New("Enter the root domain and an API token scoped to that zone and account.")
	}
	if err != nil {
		return hostingCloudflare{}, err
	}
	if config.Name == "" {
		c.config = config
		config.Name, err = c.zone(ctx)
		if err != nil {
			return hostingCloudflare{}, err
		}
	}
	c.config = config
	if err := c.checkPublishingAccess(ctx); err != nil {
		return hostingCloudflare{}, err
	}
	return config, nil
}

func (h *Hosting) preview(ctx context.Context, st hostingState, keys hostingSecrets, site hostingSite, req hostingRequest, dashboardHost string) (hostingPreview, error) {
	var result hostingPreview
	var err error
	plan := hostingPublication{Phase: "publishing"}
	resume := site.Publication != nil
	if resume {
		if site.Publication.Phase != "publishing" {
			return result, errors.New("Unpublish the current hostname before selecting another.")
		}
		plan = *site.Publication
	} else {
		plan.Host, err = normalizeHostingHost(req.Host)
		if err != nil {
			return result, err
		}
		plan.Mode, plan.DNS = req.Mode, req.DNS
		if (plan.Mode != "direct" && plan.Mode != "tunnel") ||
			(plan.DNS != "cloudflare" && plan.DNS != "manual") ||
			(plan.Mode == "tunnel" && plan.DNS != "cloudflare") {
			return result, errors.New("Choose Tunnel with Cloudflare DNS, or Direct with Cloudflare or manual DNS.")
		}
		if plan.Mode == "direct" {
			if !req.ProxySafe {
				return result, errors.New("Review what Coolify’s proxy can expose and confirm its routes before proceeding.")
			}
			if raw := strings.TrimSpace(req.WAN); raw != "" {
				ip, valid := hostingPublicIP(raw)
				if !valid || !ip.Is4() {
					return result, errors.New("The router address is not a public IPv4. Use Tunnel or confirm your internet provider supports inbound traffic.")
				}
				plan.Address = ip.String()
			}
		}
		plan.ID, err = randomHostingID()
		if err != nil {
			return result, err
		}
	}
	if dashboard, err := url.Parse("//" + dashboardHost); err == nil && strings.EqualFold(dashboard.Hostname(), plan.Host) {
		return result, errors.New("The hostname used to open Haven cannot be published as a website here.")
	}
	plan, err = h.validatePlan(ctx, st, keys, site, plan, resume)
	if err != nil {
		return result, err
	}
	nonce, err := randomHostingID()
	if err != nil {
		return result, err
	}
	for id, old := range h.previews {
		if old.Site == site.ID || time.Now().After(old.Expiry) {
			delete(h.previews, id)
		}
	}
	result = hostingPreview{nonce, site.ID, time.Now().Add(5 * time.Minute), plan}
	h.previews[nonce] = result
	return result, nil
}

func (h *Hosting) finishHostingIP(ctx context.Context, cf cloudflareHostingClient, site hostingSite) error {
	p := site.Publication
	old := wantedHostingDNS(site, "")
	next := old
	next.Content = p.NextIP
	records, err := cf.dnsRecords(ctx, p.Host)
	if err != nil {
		return err
	}
	var owned *hostingDNSRecord
	for _, record := range records {
		if record.Name != old.Name || (record.Type != "A" && record.Type != "AAAA" && record.Type != "CNAME" && record.Type != "NS") {
			continue
		}
		if owned != nil || record.Type != old.Type || record.Comment != old.Comment || record.Proxied ||
			(old.ID != "" && old.ID != record.ID) ||
			!cloudflareID.MatchString(record.ID) || (record.Content != old.Content && record.Content != next.Content) {
			return errors.New("DNS changed outside Haven. The IP update is paused.")
		}
		owned = &record
	}
	if owned == nil {
		return errors.New("The Haven-owned DNS record is missing. No DNS update was made.")
	}
	if owned.Content == next.Content {
		return nil
	}
	path := fmt.Sprintf("/zones/%s/dns_records/%s", cf.config.Zone, owned.ID)
	return cf.call(ctx, http.MethodPatch, path, map[string]string{"content": next.Content}, nil)
}

func (h *Hosting) run(ctx context.Context) {
	if !h.enabled {
		return
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if h.op.TryLock() {
				checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				h.refreshHostingIPs(checkCtx)
				cancel()
				h.op.Unlock()
			}
		}
	}
}

func (h *Hosting) refreshHostingIPs(ctx context.Context) {
	st, keys, err := h.load()
	if err != nil {
		return
	}
	cf := h.cloudflare(st, keys)
	for i := range st.Sites {
		site := &st.Sites[i]
		p := site.Publication
		if p == nil || p.Mode != "direct" || p.DNS != "cloudflare" || p.Phase != "configured" || !p.External {
			continue
		}
		err := h.refreshHostingIP(ctx, st, cf, *site)
		if err != nil {
			p.IPError = err.Error()
		} else {
			p.IPError = ""
		}
		if h.save(st) != nil {
			return
		}
	}
}

func (h *Hosting) refreshHostingIP(ctx context.Context, st hostingState, cf cloudflareHostingClient, site hostingSite) error {
	p := site.Publication
	observed, err := lookupPublic(ctx)
	if err != nil {
		return err
	}
	ip, valid := hostingPublicIP(observed.IP)
	if !valid || !ip.Is4() {
		return errors.New("Haven could not verify the new public IPv4. DNS was unchanged.")
	}
	if p.NextIP != "" && p.NextIP != ip.String() {
		return errors.New("The observed IPv4 shifted during a DNS update. Review the record before retrying.")
	}
	if p.Address == ip.String() && p.NextIP == "" {
		return nil
	}
	if err := h.checkDirectProxy(ctx); err != nil {
		return err
	}
	if _, secure, err := h.directRoute(ctx, site); err != nil || !secure {
		return errors.New("Haven couldn't verify the Coolify proxy route. DNS was unchanged.")
	}
	if _, err := hostingHTTPS(ctx, p.Host, []netip.Addr{ip}); err != nil {
		return errors.New("The new IPv4 did not return the website’s trusted HTTPS response. DNS was unchanged.")
	}
	p.NextIP = ip.String()
	if err := h.save(st); err != nil {
		return err
	}
	if err := h.finishHostingIP(ctx, cf, site); err != nil {
		return err
	}
	p.Address, p.NextIP = p.NextIP, ""
	return nil
}
