package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const coolifyHostingVersion = "4.7.9"

type hostingConnection struct {
	URL         string `json:"url"`
	Server      string `json:"server"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
	Destination string `json:"destination,omitempty"`
}

type hostingResource struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	IP   string `json:"ip,omitempty"`
}

type coolifyClient struct {
	connection hostingConnection
	token      string
	client     *http.Client
}

type hostingProviderError struct {
	provider string
	status   int
}

func (e hostingProviderError) Error() string {
	switch e.status {
	case 401, 403:
		return e.provider + " denied the request. Check the API token and its permissions."
	case 409:
		return e.provider + " reported a conflict. Review the existing resource before retrying."
	default:
		return fmt.Sprintf("%s returned HTTP %d.", e.provider, e.status)
	}
}

func hostingProviderStatus(err error, status int) bool {
	var providerErr hostingProviderError
	return errors.As(err, &providerErr) && providerErr.status == status
}

func hostingJSON(ctx context.Context, client *http.Client, provider, method, endpoint, token string, body, result any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return errors.New(provider + " request is invalid.")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("Couldn't reach " + provider + ". Check connectivity and TLS.")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hostingProviderError{provider, resp.StatusCode}
	}
	if result != nil {
		reader := io.LimitReader(resp.Body, 12<<20)
		if value, ok := result.(*string); ok {
			data, err := io.ReadAll(io.LimitReader(reader, 256))
			if err != nil || len(data) >= 256 {
				return errors.New(provider + " returned an unexpected response.")
			}
			if json.Unmarshal(data, value) != nil {
				*value = strings.TrimSpace(string(data))
			}
		} else if err := json.NewDecoder(reader).Decode(result); err != nil {
			return errors.New(provider + " returned an unexpected response.")
		}
	}
	return nil
}

func validateCoolifyURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("Enter the Coolify dashboard origin, without a path, query, or credentials.")
	}
	ip, _ := netip.ParseAddr(u.Hostname())
	if u.Scheme == "http" && !ip.IsLoopback() {
		return "", errors.New("Use HTTPS for Coolify, or HTTP with a loopback IP on this server.")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (c coolifyClient) call(ctx context.Context, method, path string, body, result any) error {
	return hostingJSON(ctx, c.client, "Coolify", method, c.connection.URL+"/api/v1"+path, c.token, body, result)
}

func (c coolifyClient) version(ctx context.Context) (string, error) {
	var version string
	err := c.call(ctx, http.MethodGet, "/version", nil, &version)
	return strings.TrimPrefix(strings.TrimSpace(version), "v"), err
}

func (c coolifyClient) resources(ctx context.Context, path string) ([]hostingResource, error) {
	var result []hostingResource
	err := c.call(ctx, http.MethodGet, path, nil, &result)
	return result, err
}

func (c coolifyClient) validate(ctx context.Context) error {
	version, err := c.version(ctx)
	if err != nil {
		return err
	}
	if version != coolifyHostingVersion {
		return errors.New("This integration currently targets Coolify " + coolifyHostingVersion + ".")
	}
	for _, lookup := range []struct{ path, value, label string }{
		{"/servers", c.connection.Server, "server"},
		{"/projects", c.connection.Project, "project"},
		{"/projects/" + c.connection.Project + "/environments", c.connection.Environment, "environment"},
		{"/servers/" + c.connection.Server + "/destinations", c.connection.Destination, "destination"},
	} {
		if lookup.value == "" {
			continue
		}
		resources, err := c.resources(ctx, lookup.path)
		if err != nil {
			return err
		}
		found := false
		for _, resource := range resources {
			if resource.UUID != lookup.value {
				continue
			}
			found = true
			if lookup.label == "server" {
				ip, _ := netip.ParseAddr(resource.IP)
				if !(ip.IsLoopback() || resource.IP == "host.docker.internal" || resource.IP == "localhost") {
					return errors.New("Choose Coolify’s local server on the same Docker host as Haven.")
				}
			}
			break
		}
		if !found {
			return fmt.Errorf("The selected %s is unavailable to this Coolify token.", lookup.label)
		}
	}
	return nil
}

type coolifyService struct {
	UUID        string               `json:"uuid"`
	Description string               `json:"description"`
	Status      string               `json:"status"`
	Apps        []coolifyApplication `json:"applications"`
	Databases   []coolifyApplication `json:"databases"`
}

type coolifyApplication struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	FQDN   string `json:"fqdn"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

func (a coolifyApplication) domain() string {
	if a.URL != "" {
		return a.URL
	}
	return a.FQDN
}

func (c coolifyClient) checkDNSAutomation(ctx context.Context, host string) error {
	var zones []struct {
		Name        string `json:"zone"`
		Integration string `json:"integration_token_uuid"`
	}
	if err := c.call(ctx, http.MethodGet, "/dns/zones", nil, &zones); err != nil {
		return errors.New("Couldn't verify Coolify DNS ownership. Use a Coolify team administrator token for direct publishing.")
	}
	for _, zone := range zones {
		if host != zone.Name && !strings.HasSuffix(host, "."+zone.Name) {
			continue
		}
		if !hostingID.MatchString(zone.Integration) {
			return errors.New("Couldn't verify the matching DNS integration in Coolify.")
		}
		var integration struct {
			Automatic *bool `json:"automatic_dns"`
		}
		if err := c.call(ctx, http.MethodGet, "/security/integration-tokens/"+zone.Integration, nil, &integration); err != nil {
			return err
		}
		if integration.Automatic == nil || *integration.Automatic {
			return errors.New("Disable automatic DNS for this zone in Coolify before Haven configures a direct route.")
		}
	}
	return nil
}

func (c coolifyClient) service(ctx context.Context, site hostingSite) (coolifyService, coolifyApplication, error) {
	var service coolifyService
	err := c.call(ctx, http.MethodGet, "/services/"+site.ServiceID, nil, &service)
	if err != nil {
		return service, coolifyApplication{}, err
	}
	if service.UUID != site.ServiceID || service.Description != site.marker() || len(service.Apps) != 1 ||
		service.Apps[0].Name != hostingTemplates[site.Template].Web || !hostingID.MatchString(service.Apps[0].UUID) {
		return service, coolifyApplication{}, errors.New("The Coolify service no longer matches this Haven website. Review its configuration in Coolify.")
	}
	return service, service.Apps[0], nil
}

func (c coolifyClient) create(ctx context.Context, site hostingSite, compose string) (string, error) {
	// A retry recovers a service created before a timeout or restart.
	var all []coolifyService
	if err := c.call(ctx, http.MethodGet, "/services", nil, &all); err != nil {
		return "", err
	}
	var existing string
	for _, service := range all {
		if service.Description != site.marker() {
			continue
		}
		if existing != "" || !hostingID.MatchString(service.UUID) {
			return "", errors.New("More than one Coolify resource may match this install. Review your Coolify services.")
		}
		existing = service.UUID
	}
	if existing != "" {
		return existing, nil
	}
	body := map[string]any{
		"name": site.Name, "description": site.marker(),
		"server_uuid": c.connection.Server, "project_uuid": c.connection.Project, "environment_uuid": c.connection.Environment,
		"instant_deploy": false, "connect_to_docker_network": false, "redact_all_env_values_in_logs": true,
		"docker_compose_raw": base64.StdEncoding.EncodeToString([]byte(compose)),
	}
	if c.connection.Destination != "" {
		body["destination_uuid"] = c.connection.Destination
	}
	var response hostingResource
	if err := c.call(ctx, http.MethodPost, "/services", body, &response); err != nil {
		return "", err
	}
	if !hostingID.MatchString(response.UUID) {
		return "", errors.New("Coolify did not return a valid service identifier.")
	}
	return response.UUID, nil
}

func (c coolifyClient) setDomain(ctx context.Context, site hostingSite, app coolifyApplication, domain string) error {
	if sameCoolifyDomain(app.domain(), domain) {
		return nil
	}
	body := map[string]any{"url": nil}
	if domain != "" {
		body = map[string]any{"url": domain, "is_force_https_enabled": true, "traefik_certificate_resolver": "letsencrypt"}
	}
	return c.call(ctx, http.MethodPatch, "/services/"+site.ServiceID+"/applications/"+app.UUID, body, nil)
}

func (c coolifyClient) setCanonical(ctx context.Context, site hostingSite, host, privateURL string) error {
	value := privateURL
	if host != "" {
		value = "https://" + host
	}
	return c.call(ctx, http.MethodPatch, "/services/"+site.ServiceID+"/envs/bulk", map[string]any{
		"data": []map[string]any{{"key": "HAVEN_SITE_URL", "value": value, "is_literal": true}},
	}, nil)
}

func (c coolifyClient) action(ctx context.Context, site hostingSite, path string) error {
	return c.call(ctx, http.MethodPost, "/services/"+site.ServiceID+"/"+path, nil, nil)
}

func (c coolifyClient) remove(ctx context.Context, site hostingSite) error {
	if site.ServiceID == "" {
		var services []coolifyService
		if err := c.call(ctx, http.MethodGet, "/services", nil, &services); err != nil {
			return err
		}
		for _, candidate := range services {
			if candidate.Description != site.marker() {
				continue
			}
			if site.ServiceID != "" || !hostingID.MatchString(candidate.UUID) {
				return errors.New("More than one Coolify resource may match this website. Review it in Coolify before removal.")
			}
			site.ServiceID = candidate.UUID
		}
		if site.ServiceID == "" {
			return nil
		}
	}
	if _, _, err := c.service(ctx, site); err != nil {
		if hostingProviderStatus(err, http.StatusNotFound) {
			return nil
		}
		return err
	}
	err := c.call(ctx, http.MethodDelete, "/services/"+site.ServiceID+"?delete_configurations=true&delete_volumes=true&docker_cleanup=true&delete_connected_networks=true", nil, nil)
	if hostingProviderStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

func (c coolifyClient) resourceCount(ctx context.Context) (int, error) {
	var resources []json.RawMessage
	if err := c.call(ctx, http.MethodGet, "/resources", nil, &resources); err != nil {
		return 0, err
	}
	return len(resources), nil
}

type hostingTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Guided      bool   `json:"guided"`
	Web         string `json:"-"`
	Port        int    `json:"-"`
	Compose     string `json:"-"`
	Static      bool   `json:"-"`
}

var hostingTemplates = map[string]hostingTemplate{
	"static-html": {
		ID: "static-html", Name: "Static HTML", Description: "Publish a fast single-page site with Nginx. Paste an HTML document now; no database is required.",
		Guided: true, Web: "web", Port: 80, Static: true,
	},
	"wordpress-with-mariadb": {
		ID: "wordpress-with-mariadb", Name: "WordPress + MariaDB", Description: "Build a site or blog with themes, plugins and a dedicated MariaDB database.",
		Guided: true, Web: "wordpress", Port: 80,
		Compose: `services:
  wordpress:
    image: wordpress:php8.3-apache
    restart: unless-stopped
    ports: ["127.0.0.1:HAVEN_PRIVATE_PORT:80"]
    networks: [default, hosting]
    environment:
      WORDPRESS_DB_HOST: mariadb
      WORDPRESS_DB_USER: wordpress
      WORDPRESS_DB_NAME: wordpress
      WORDPRESS_DB_PASSWORD: ${SERVICE_PASSWORD_DB}
      HAVEN_SITE_URL: ${HAVEN_SITE_URL:-http://127.0.0.1:HAVEN_PRIVATE_PORT}
      WORDPRESS_CONFIG_EXTRA: |
        define('WP_HOME', getenv('HAVEN_SITE_URL'));
        define('WP_SITEURL', getenv('HAVEN_SITE_URL'));
    volumes:
      - wordpress-data:/var/www/html
    depends_on:
      mariadb:
        condition: service_healthy
  mariadb:
    image: mariadb:11.4
    restart: unless-stopped
    environment:
      MARIADB_USER: wordpress
      MARIADB_DATABASE: wordpress
      MARIADB_PASSWORD: ${SERVICE_PASSWORD_DB}
      MARIADB_ROOT_PASSWORD: ${SERVICE_PASSWORD_ROOT}
    volumes:
      - mariadb-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      interval: 10s
      timeout: 5s
      retries: 12
networks:
  hosting:
volumes:
  wordpress-data:
  mariadb-data:
`,
	},
	"ghost": {
		ID: "ghost", Name: "Ghost", Description: "Publish a blog or newsletter with a dedicated MySQL database.",
		Guided: true, Web: "ghost", Port: 2368,
		Compose: `services:
  ghost:
    image: ghost:6-alpine
    restart: unless-stopped
    ports: ["127.0.0.1:HAVEN_PRIVATE_PORT:2368"]
    networks: [default, hosting]
    environment:
      url: ${HAVEN_SITE_URL:-http://127.0.0.1:HAVEN_PRIVATE_PORT}
      database__client: mysql
      database__connection__host: mysql
      database__connection__user: ghost
      database__connection__password: ${SERVICE_PASSWORD_DB}
      database__connection__database: ghost
    volumes:
      - ghost-content:/var/lib/ghost/content
    depends_on:
      mysql:
        condition: service_healthy
  mysql:
    image: mysql:8.4
    restart: unless-stopped
    environment:
      MYSQL_USER: ghost
      MYSQL_DATABASE: ghost
      MYSQL_PASSWORD: ${SERVICE_PASSWORD_DB}
      MYSQL_ROOT_PASSWORD: ${SERVICE_PASSWORD_ROOT}
    volumes:
      - mysql-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1", "--silent"]
      interval: 10s
      timeout: 5s
      retries: 12
networks:
  hosting:
volumes:
  ghost-content:
  mysql-data:
`,
	},
}

func coolifyPortDomain(site hostingSite, host string) string {
	return "https://" + host + ":" + strconv.Itoa(hostingTemplates[site.Template].Port)
}

func sameCoolifyDomain(value, desired string) bool {
	return value == desired || (strings.HasSuffix(desired, ":80") && value == strings.TrimSuffix(desired, ":80"))
}

type hostingRecoveryStorage struct {
	Name         string `json:"name"`
	Mount        string `json:"mount_path"`
	ResourceType string `json:"resource_type"`
}

func (c coolifyClient) recovery(ctx context.Context, site hostingSite) map[string]any {
	data := map[string]any{
		"website": site.Name, "template": site.Template, "coolifyVersion": coolifyHostingVersion,
		"service": site.ServiceID, "project": c.connection.Project, "environment": c.connection.Environment,
		"backupStatus": "Check the database and storage backup results in Coolify.",
	}
	if p := site.Publication; p != nil {
		data["publication"] = map[string]string{"host": p.Host, "method": p.Mode, "dns": p.DNS}
	}
	var volumes struct {
		Storage []hostingRecoveryStorage `json:"persistent_storages"`
	}
	service, app, err := c.service(ctx, site)
	if err != nil {
		data["platformStatus"] = err.Error()
		return data
	}
	images := map[string]string{app.Name: app.Image}
	for _, database := range service.Databases {
		images[database.Name] = database.Image
	}
	data["images"] = images
	if err := c.call(ctx, http.MethodGet, "/services/"+site.ServiceID+"/storages", nil, &volumes); err != nil {
		data["storageStatus"] = err.Error()
	} else {
		data["storage"] = volumes.Storage
	}
	return data
}
