package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

const hostingConnectorImage = "cloudflare/cloudflared:2026.11.2"
const hostingConnectorName = "haven-hosting-cloudflared"

type hostingCheck struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Warning     bool   `json:"warning,omitempty"`
	Recoverable bool   `json:"recoverable,omitempty"`
	Info        string `json:"info"`
	Details     string `json:"details,omitempty"`
	Fix         string `json:"fix,omitempty"`
	Action      string `json:"action,omitempty"`
}

type coolifyInstallation struct {
	Status string `json:"status"`
}

func detectCoolifyInstallation(envFile bool, containers []Container) coolifyInstallation {
	installed := envFile
	running := false
	for _, c := range containers {
		installed = installed || c.Name == "coolify" || c.Project == "coolify"
		if c.Name == "coolify" && c.State == "running" {
			running = true
		}
	}
	if running {
		return coolifyInstallation{Status: "running"}
	}
	if installed {
		return coolifyInstallation{Status: "partial"}
	}
	return coolifyInstallation{Status: "not-installed"}
}

func (h *Hosting) coolifyInstallation(ctx context.Context) coolifyInstallation {
	_, err := os.Stat("/data/coolify/source/.env")
	containers, _ := h.docker.ListContainers(ctx)
	return detectCoolifyInstallation(!errors.Is(err, os.ErrNotExist), containers)
}

func (h *Hosting) setupChecks(ctx context.Context) []hostingCheck {
	v, err := h.docker.Version(ctx)
	_, composeErr := h.docker.ComposeVersion(ctx)
	memory, _ := mem.VirtualMemoryWithContext(ctx)
	swap, _ := mem.SwapMemoryWithContext(ctx)
	loadAvg, _ := load.AvgWithContext(ctx)
	storage, _ := disk.UsageWithContext(ctx, "/")
	checks := []hostingCheck{
		{Name: "Host", OK: isLinux() && os.Geteuid() == 0 && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"), Info: "Requires a Linux amd64 or arm64 Haven server running as root.", Details: "The guided installer changes host services and supports only 64-bit Linux on Intel/AMD or ARM.", Fix: "Run Haven as root on a supported Linux server. On another platform, install Coolify separately and use Connect an existing installation."},
		{Name: "CPU", OK: runtime.NumCPU() >= 2, Info: fmt.Sprintf("%d CPU cores detected; Coolify needs at least two.", runtime.NumCPU()), Details: "Coolify uses parallel containers for its control plane, proxy, builds and deployed applications.", Fix: "Increase the VM or server allocation to at least 2 CPU cores; 4 or more is recommended for builds."},
		{Name: "Docker and Compose", OK: err == nil && composeErr == nil && dockerLoopbackSafe(v), Info: "Requires a working Docker Engine 28 or later and the Compose plugin.", Details: "Haven needs Docker 28+ for safe loopback-only website ports, and Coolify uses the Compose plugin to manage its services.", Fix: "Install or upgrade Docker Engine to version 28 or later, install the Docker Compose plugin, start Docker, then rerun the checks."},
		{Name: "Memory", OK: memory != nil && memory.Total >= 2_000_000_000, Info: "2 GB is the upstream minimum. Haven recommends at least 4 GB plus memory for every website and database.", Details: "The minimum covers Coolify’s control services only; builds, websites and databases need additional memory.", Fix: "Increase the server or VM memory to at least 2 GB before installation. Use 4 GB or more for a practical deployment."},
		{Name: "Disk", OK: storage != nil && storage.Free >= 10_000_000_000, Info: "At least 10 GB must be free on the root filesystem.", Details: "Coolify stores its control data under /data, while Docker images, build layers, databases and backups consume additional space.", Fix: "Free at least 10 GB on the root filesystem or enlarge the disk. Keep extra capacity for each website and its backups."},
		{Name: "SSH", OK: hasCmd("sshd") && !portFree(22, "tcp"), Info: "Requires a running SSH server. Coolify needs SSH access to manage its local server.", Details: "Even on the same machine, Coolify connects through SSH to configure and deploy to its local server.", Fix: "Install and start OpenSSH Server, confirm TCP 22 is listening, then rerun the checks."},
	}
	if memory != nil {
		low := memory.Available < 1_500_000_000
		checks = append(checks, hostingCheck{Name: "Available memory", OK: true, Warning: low,
			Info:    fmt.Sprintf("%.1f GB is available now. Free at least 1.5 GB before installing; more is recommended for builds.", float64(memory.Available)/1e9),
			Details: "Total memory may meet the minimum while running services leave too little memory for a reliable installation.",
			Fix:     "Stop memory-heavy workloads or increase the server memory, then rerun the checks when at least 1.5 GB is available."})
	}
	if swap != nil {
		high := swap.Total > 0 && swap.UsedPercent >= 50
		checks = append(checks, hostingCheck{Name: "Swap pressure", OK: true, Warning: high,
			Info:    fmt.Sprintf("Swap is %.0f%% used. Heavy swap can make Coolify setup and image pulls appear stuck.", swap.UsedPercent),
			Details: "High swap use indicates memory pressure and can make container extraction, builds and database startup extremely slow.",
			Fix:     "Stop memory-heavy workloads and wait for pressure to fall, or add memory. Do not disable swap only to clear this warning."})
	}
	if loadAvg != nil {
		high := loadAvg.Load1 > float64(runtime.NumCPU()*2)
		checks = append(checks, hostingCheck{Name: "Current system load", OK: true, Warning: high,
			Info:    fmt.Sprintf("One-minute load is %.1f across %d CPU cores. Wait for load to fall below %.0f before installation.", loadAvg.Load1, runtime.NumCPU(), float64(runtime.NumCPU()*2)),
			Details: "A busy host can cause installer timeouts and make the first Coolify services fail their health checks.",
			Fix:     "Wait for current jobs to finish or stop CPU-heavy workloads, then rerun the checks."})
	}
	_, installErr := os.Stat("/data/coolify/source/.env")
	containers, _ := h.docker.ListContainers(ctx)
	installation := detectCoolifyInstallation(!errors.Is(installErr, os.ErrNotExist), containers)
	installed := installation.Status != "not-installed"
	running := installation.Status == "running"
	existingInfo := "No existing Coolify installation was detected."
	existingDetails := "A second Coolify installation would conflict with the existing containers, networks and ports."
	existingFix := "Use Connect an existing installation if Coolify is running."
	if installed && !running {
		existingInfo = "A partial Coolify installation was found. Review it, then use Resume installation to rerun the pinned installer safely."
		existingFix = "Review the other checks, then choose Review installation recovery to resume without deleting Coolify data."
	} else if running {
		existingInfo = "Coolify is installed and running. Connect it to Haven instead of installing another copy."
		existingDetails = "The running Coolify stack is ready for account setup, API access and connection to Haven."
		existingFix = "Open the Coolify dashboard, finish its setup, create an API token, then connect it to Haven."
	}
	checks = append(checks, hostingCheck{Name: "Existing Coolify", OK: !installed || running, Recoverable: installed && !running, Info: existingInfo, Details: existingDetails, Fix: existingFix})
	havenPort80 := h.store.Settings().Port80
	for _, port := range []int{80, 443, 6001, 6002, 8000} {
		free := portFree(port, "tcp")
		coolifyOwnsPort := false
		for _, container := range containers {
			if (container.Project == "coolify" || container.Name == "coolify") && strings.Contains(container.Ports, fmt.Sprintf(":%d->", port)) {
				coolifyOwnsPort = true
				break
			}
		}
		checks = append(checks, hostingPortCheck(port, free, havenPort80, installed && !running && coolifyOwnsPort, running && coolifyOwnsPort))
	}
	return checks
}

func hostingPortCheck(port int, free, havenPort80, coolifyRecoverable, coolifyRunning bool) hostingCheck {
	check := hostingCheck{
		Name: fmt.Sprintf("TCP %d", port), OK: free, Info: "Must be available for a new Coolify installation.",
		Details: fmt.Sprintf("Coolify needs exclusive access to TCP %d for its proxy or control services.", port),
		Fix:     fmt.Sprintf("Find the listener with `sudo ss -ltnp '( sport = :%d )'`, then stop or reconfigure that service before rerunning the checks.", port),
	}
	if free {
		return check
	}
	if coolifyRunning {
		check.OK = true
		check.Info = "This port belongs to the running Coolify installation."
		check.Details = "The existing Coolify stack is expected to own this port; it is not an installation problem."
		check.Fix = ""
		return check
	}
	if port == 80 && havenPort80 {
		check.Info = "Haven is configured to answer on port 80. Continue on Haven’s main port, then turn off the extra listener for Coolify."
		check.Details = "Coolify’s public proxy must own port 80. Turning off Haven’s extra listener does not stop Haven on its main port."
		check.Fix = "Continue to Haven’s main port using the action below, rerun the checks there, then turn off Haven on port 80."
		check.Action = "disable-haven-port-80"
		return check
	}
	if coolifyRecoverable {
		check.Recoverable = true
		check.Info = "This port belongs to the partial Coolify stack and can be reused during recovery."
		check.Details = "The existing partial installation already owns this port, so it does not block resuming that same installation."
		check.Fix = "Use Review installation recovery. Do not stop the partial Coolify containers unless recovery fails and you have reviewed their data."
	}
	return check
}

func dockerLoopbackSafe(version string) bool {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(major)
	return err == nil && n >= 28
}

func (h *Hosting) installCoolify(ctx context.Context, j *Job, recover bool) error {
	if h.coolifyInstallation(ctx).Status == "running" {
		return errors.New("Coolify is already installed and running. Connect it to Haven instead of installing another copy.")
	}
	for _, check := range h.setupChecks(ctx) {
		if !check.OK && !(recover && check.Recoverable) {
			return fmt.Errorf("%s preflight failed. %s", check.Name, check.Info)
		}
	}
	// The upstream installer also reads this argument to pin the platform image.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://raw.githubusercontent.com/coollabsio/coolify/fb4bf6a3671e423e02e93591cd339b80f008d4f3/scripts/install.sh", nil)
	resp, err := h.http.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return errors.New("Couldn't download the pinned Coolify installer.")
	}
	defer resp.Body.Close()
	path := filepath.Join(h.dir, "coolify-install.sh")
	content, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(content) > 1<<20 || !strings.HasPrefix(string(content), "#!") {
		return errors.New("The pinned Coolify installer could not be read.")
	}
	if err := h.writeFile("coolify-install.sh", content); err != nil {
		return err
	}
	j.Logf("Running the Coolify %s installer. Keep ports 8000, 6001 and 6002 private.", coolifyHostingVersion)
	if recover {
		j.Logf("Resuming the partial installation with the same pinned Coolify release.")
	}
	if err := hostStream(ctx, j, []string{"AUTOUPDATE=false"}, "bash", path, coolifyHostingVersion); err != nil {
		return errors.New("The Coolify installer failed. Inspect the install on the server before retrying, or connect it if setup finished.")
	}
	j.Logf("Finish account setup from the LAN or an SSH port forward, enable the Coolify API, then connect it here.")
	return nil
}

func (h *Hosting) staticSitePath(id string) string {
	return filepath.Join(h.dir, "sites", id, "index.html")
}

func (h *Hosting) saveStaticSite(id, content string) error {
	if len(content) == 0 || len(content) > 256<<10 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return errors.New("Static HTML must be valid UTF-8 and no larger than 256 KB.")
	}
	dir := filepath.Dir(h.staticSitePath(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(h.staticSitePath(id), []byte(content), 0o600)
}

func (h *Hosting) siteCompose(site hostingSite) (string, error) {
	template, ok := hostingTemplates[site.Template]
	if !ok {
		return "", errors.New("The selected hosting template is unavailable.")
	}
	if !template.Static {
		return strings.ReplaceAll(template.Compose, "HAVEN_PRIVATE_PORT", strconv.Itoa(site.PrivatePort)), nil
	}
	content, err := os.ReadFile(h.staticSitePath(site.ID))
	if err != nil || len(content) == 0 || len(content) > 256<<10 || !utf8.Valid(content) {
		return "", errors.New("Haven couldn't read the saved static homepage. Create the site again or restore its source file.")
	}
	indented := "      " + strings.ReplaceAll(string(content), "\n", "\n      ")
	return fmt.Sprintf(`services:
  web:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports: ["127.0.0.1:%d:80"]
    networks: [default, hosting]
    configs:
      - source: homepage
        target: /usr/share/nginx/html/index.html
networks:
  hosting:
configs:
  homepage:
    content: |
%s
`, site.PrivatePort, indented), nil
}

func (h *Hosting) waitSiteReady(ctx context.Context, j *Job, c coolifyClient, site hostingSite) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	last := ""
	for {
		service, app, err := c.service(ctx, site)
		status := app.Status
		if status == "" {
			status = service.Status
		}
		if status != "" && status != last {
			j.Logf("Coolify deployment status: %s", status)
			last = status
		}
		if err == nil {
			if _, inspectErr := h.inspectSite(ctx, site); inspectErr == nil {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, site.privateURL(), nil)
				if resp, requestErr := h.http.Do(req); requestErr == nil {
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 500 {
						return nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("The deployment did not become ready before the job deadline. Open Coolify for the latest logs, then retry in Haven.")
		case <-ticker.C:
		}
	}
}

type hostingDockerContainer struct {
	Name   string `json:"Name"`
	Config struct {
		Labels map[string]string `json:"Labels"`
		Image  string            `json:"Image"`
	} `json:"Config"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		Networks map[string]struct{ NetworkID string } `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (h *Hosting) inspectContainer(ctx context.Context, name string) (hostingDockerContainer, error) {
	var container hostingDockerContainer
	out, err := h.docker.run(ctx, "container", "inspect", "--format", "{{json .}}", name)
	if err != nil || json.Unmarshal(out, &container) != nil {
		return container, errors.New("The expected local Docker container could not be inspected.")
	}
	return container, nil
}

func (h *Hosting) inspectSite(ctx context.Context, site hostingSite) (string, error) {
	spec := hostingTemplates[site.Template]
	name := spec.Web + "-" + site.ServiceID
	web, err := h.inspectContainer(ctx, name)
	if err != nil || !web.State.Running || web.Config.Labels["com.docker.compose.project"] != site.ServiceID ||
		web.Config.Labels["com.docker.compose.service"] != spec.Web || strings.TrimPrefix(web.Name, "/") != name {
		return "", errors.New("Wait for the expected Coolify web container to run on this Haven server.")
	}
	found := false
	for port, bindings := range web.NetworkSettings.Ports {
		for _, binding := range bindings {
			if port != strconv.Itoa(spec.Port)+"/tcp" || binding.HostIP != "127.0.0.1" || binding.HostPort != strconv.Itoa(site.PrivatePort) || found {
				return "", errors.New("The website has unexpected published ports. Review its bindings in Coolify.")
			}
			found = true
		}
	}
	if !found {
		return "", errors.New("The website’s private port could not be verified.")
	}
	for networkName, network := range web.NetworkSettings.Networks {
		out, err := h.docker.run(ctx, "network", "inspect", "--format", "{{json .}}", network.NetworkID)
		var info struct {
			Labels     map[string]string
			Containers map[string]struct{ Name string }
		}
		if err != nil || json.Unmarshal(out, &info) != nil || info.Labels["com.docker.compose.project"] != site.ServiceID ||
			info.Labels["com.docker.compose.network"] != "hosting" {
			continue
		}
		webConnected := false
		for _, member := range info.Containers {
			if member.Name != name && member.Name != hostingConnectorName {
				return "", errors.New("The connector network contains a container other than this website or cloudflared.")
			}
			webConnected = webConnected || member.Name == name
		}
		if webConnected {
			return networkName, nil
		}
	}
	return "", errors.New("The website’s dedicated connector network could not be verified.")
}

func (h *Hosting) checkDirectProxy(ctx context.Context) error {
	if h.store.Settings().Port80 {
		return errors.New("Turn off Haven’s extra port 80 listener before using Coolify’s public proxy.")
	}
	proxy, err := h.inspectContainer(ctx, "coolify-proxy")
	if err != nil || !proxy.State.Running || !strings.Contains(proxy.Config.Image, "traefik") {
		return errors.New("Start the local Coolify Traefik proxy and verify its public routes in Coolify.")
	}
	for _, port := range []string{"80", "443"} {
		if !hasHostingProxyBinding(proxy, port) {
			return errors.New("The local Coolify proxy must own TCP ports 80 and 443.")
		}
	}
	return nil
}

func hasHostingProxyBinding(proxy hostingDockerContainer, port string) bool {
	for _, b := range proxy.NetworkSettings.Ports[port+"/tcp"] {
		if (b.HostIP == "0.0.0.0" || b.HostIP == "::") && b.HostPort == port {
			return true
		}
	}
	return false
}

func (h *Hosting) ensureConnector(ctx context.Context, id, token string) error {
	secretDir := filepath.Join(h.dir, "connector")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(secretDir, "token")
	existing, _ := os.ReadFile(path)
	if string(existing) != token {
		if err := writeFileAtomic(path, []byte(token), 0o600); err != nil {
			return err
		}
	}
	if err := os.Chown(path, 65532, 65532); err != nil {
		return err
	}
	label := "io.haven.hosting.tunnel"
	// Use a dedicated directory mount so an atomic token rotation is visible to cloudflared on restart.
	// The host directory remains private and only the connector's UID may read the file.
	if err := os.Chmod(secretDir, 0o711); err != nil {
		return err
	}
	containers, err := h.docker.ListContainers(ctx)
	if err != nil {
		return errors.New("Couldn't list local Docker containers for the tunnel connector.")
	}
	for _, c := range containers {
		if c.Name != hostingConnectorName {
			continue
		}
		connector, err := h.inspectContainer(ctx, hostingConnectorName)
		if err != nil || connector.Config.Labels[label] != id || connector.Config.Image != hostingConnectorImage {
			return errors.New("The reserved connector container name is already in use.")
		}
		if string(existing) != token {
			_, err = h.docker.run(ctx, "restart", hostingConnectorName)
		} else if !connector.State.Running {
			_, err = h.docker.run(ctx, "start", hostingConnectorName)
		}
		if err != nil {
			return errors.New("Couldn't start the dedicated cloudflared connector.")
		}
		return nil
	}
	_, err = h.docker.run(ctx, "run", "-d", "--name", hostingConnectorName, "--restart", "unless-stopped",
		"--network", "bridge", "--user", "65532:65532", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--label", label+"="+id, "--mount", "type=bind,src="+secretDir+",dst=/haven-token,readonly",
		hostingConnectorImage, "tunnel", "--no-autoupdate", "run", "--token-file", "/haven-token/token")
	if err != nil {
		return errors.New("Couldn't create cloudflared. Check Docker registry access and the connector in Docker.")
	}
	return nil
}

func (h *Hosting) connectorNetwork(ctx context.Context, id, network string, attach bool) error {
	connector, err := h.inspectContainer(ctx, hostingConnectorName)
	if err != nil {
		if attach {
			return err
		}
		all, listErr := h.docker.ListContainers(ctx)
		if listErr != nil {
			return err
		}
		for _, c := range all {
			if c.Name == hostingConnectorName {
				return err
			}
		}
		return nil
	}
	if connector.Config.Labels["io.haven.hosting.tunnel"] != id {
		return errors.New("The local connector does not belong to this Haven tunnel.")
	}
	_, attached := connector.NetworkSettings.Networks[network]
	if attach == attached {
		return nil
	}
	action := "connect"
	if !attach {
		action = "disconnect"
	}
	if _, err := h.docker.run(ctx, "network", action, network, hostingConnectorName); err != nil {
		return errors.New("Couldn't update the connector’s dedicated website network.")
	}
	return nil
}

var hostingSpecialRanges = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2002::/16"),
}

func hostingPublicIP(raw string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip)) {
		return ip, false
	}
	for _, prefix := range hostingSpecialRanges {
		if prefix.Contains(ip) {
			return ip, false
		}
	}
	if ip.Is4() && ip.As4()[0] == 0 {
		return ip, false
	}
	return ip, true
}

func hostingPublicDNS(ctx context.Context, client *http.Client, host string) ([]netip.Addr, error) {
	var result []netip.Addr
	for _, q := range []string{"A", "AAAA"} {
		var dns struct {
			Status int
			Answer []struct {
				Type int
				Data string
			}
		}
		if err := hostingJSON(ctx, client, "Public DNS", http.MethodGet, "https://dns.google/resolve?name="+host+"&type="+q, "", nil, &dns); err != nil {
			return nil, err
		}
		if dns.Status != 0 && dns.Status != 3 {
			return nil, errors.New("The public DNS resolver could not verify this hostname.")
		}
		for _, answer := range dns.Answer {
			if answer.Type != 1 && answer.Type != 28 {
				continue
			}
			ip, public := hostingPublicIP(answer.Data)
			if !public {
				return nil, errors.New("The hostname resolves to a non-public address.")
			}
			result = append(result, ip)
		}
	}
	return result, nil
}

func hostingHTTPS(ctx context.Context, host string, ips []netip.Addr) (time.Time, error) {
	if len(ips) == 0 {
		return time.Time{}, errors.New("Waiting for public DNS.")
	}
	// Dial only already-validated public IPs. Use the original hostname for SNI,
	// certificate verification and Host, and never follow arbitrary redirects.
	for _, ip := range ips {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := net.Dialer{Timeout: 3 * time.Second}
				return dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), "443"))
			},
		}
		client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: noHostingRedirect}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
		resp, err := client.Do(req)
		transport.CloseIdleConnections()
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			if resp.StatusCode >= 300 {
				location, err := resp.Location()
				if err != nil || location.Hostname() != host || location.Scheme != "https" ||
					(location.Port() != "" && location.Port() != "443") {
					continue
				}
			}
			if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
				return resp.TLS.PeerCertificates[0].NotAfter, nil
			}
		}
	}
	return time.Time{}, errors.New("Haven couldn't verify a trusted certificate and HTTPS response. Check DNS, ACME or Cloudflare, and test from outside your LAN.")
}

func (h *Hosting) directRoute(ctx context.Context, site hostingSite) (present, secure bool, err error) {
	spec := hostingTemplates[site.Template]
	web, err := h.inspectContainer(ctx, spec.Web+"-"+site.ServiceID)
	if err != nil {
		return false, false, err
	}
	if web.Config.Labels["com.docker.compose.project"] != site.ServiceID ||
		web.Config.Labels["com.docker.compose.service"] != spec.Web {
		return false, false, errors.New("The expected Coolify web container no longer matches this website.")
	}
	present, secure = hostedTraefikRoute(web.Config.Labels, site.Publication.Host, spec.Port)
	return present, secure && web.State.Running, nil
}

func hostedTraefikRoute(labels map[string]string, host string, port int) (present, secure bool) {
	tickHost := "`" + host + "`"
	quoteHost := strconv.Quote(host)
	for key, value := range labels {
		if !strings.HasPrefix(key, "traefik.http.routers.") || !strings.HasSuffix(key, ".rule") ||
			(!strings.Contains(value, tickHost) && !strings.Contains(value, quoteHost)) {
			continue
		}
		present = true
		router := strings.TrimSuffix(key, ".rule")
		service := labels[router+".service"]
		fullRule := value == "Host("+tickHost+") && PathPrefix(`/`)" || value == "Host("+quoteHost+")"
		if fullRule && labels["traefik.enable"] == "true" && labels[router+".tls"] == "true" &&
			labels[router+".tls.certresolver"] != "" &&
			labels["traefik.http.services."+service+".loadbalancer.server.port"] == strconv.Itoa(port) {
			secure = true
		}
	}
	return present, secure
}

func (h *Hosting) waitDirectRoute(ctx context.Context, site hostingSite, want bool) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		present, secure, err := h.directRoute(ctx, site)
		if err == nil && ((want && secure) || (!want && !present)) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("Couldn't verify the updated Coolify proxy route. Check the deployment in Coolify, then retry.")
		case <-ticker.C:
		}
	}
}
