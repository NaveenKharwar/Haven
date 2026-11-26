package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const cloudflareHostingAPI = "https://api.cloudflare.com/client/v4"

type hostingCloudflare struct {
	Account string `json:"account"`
	Zone    string `json:"zone"`
	Name    string `json:"name"`
}

type cloudflareHostingClient struct {
	client *http.Client
	base   string
	token  string
	config hostingCloudflare
}

func (c cloudflareHostingClient) call(ctx context.Context, method, path string, body, result any) error {
	var response struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	if err := hostingJSON(ctx, c.client, "Cloudflare", method, c.base+path, c.token, body, &response); err != nil {
		return err
	}
	if !response.Success {
		return errors.New("Cloudflare rejected the request. Check the token scope and existing resources.")
	}
	if result != nil && (len(response.Result) == 0 || string(response.Result) == "null" || json.Unmarshal(response.Result, result) != nil) {
		return errors.New("Cloudflare returned an unexpected result.")
	}
	return nil
}

func (c cloudflareHostingClient) zone(ctx context.Context) (string, error) {
	var zone struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	if err := c.call(ctx, http.MethodGet, "/zones/"+c.config.Zone, nil, &zone); err != nil {
		return "", err
	}
	if zone.Status != "active" || zone.Account.ID != c.config.Account || !hostingHostname.MatchString(zone.Name) {
		return "", errors.New("Choose an active Cloudflare zone in the specified account.")
	}
	return zone.Name, nil
}

func (c cloudflareHostingClient) discoverZone(ctx context.Context, domain string) (hostingCloudflare, error) {
	domain, err := normalizeHostingHost(domain)
	if err != nil {
		return hostingCloudflare{}, errors.New("Enter the root domain already active in Cloudflare, such as example.com.")
	}
	var zones []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Account struct {
			ID string `json:"id"`
		} `json:"account"`
	}
	path := "/zones?per_page=50&status=active&name=" + url.QueryEscape(domain)
	if err := c.call(ctx, http.MethodGet, path, nil, &zones); err != nil {
		return hostingCloudflare{}, err
	}
	if len(zones) != 1 || zones[0].Name != domain || zones[0].Status != "active" ||
		!cloudflareID.MatchString(zones[0].ID) || !cloudflareID.MatchString(zones[0].Account.ID) {
		return hostingCloudflare{}, errors.New("Cloudflare did not return one active zone for that exact domain. Check the domain and token scope.")
	}
	return hostingCloudflare{Account: zones[0].Account.ID, Zone: zones[0].ID, Name: zones[0].Name}, nil
}

func (c cloudflareHostingClient) checkPublishingAccess(ctx context.Context) error {
	var tunnels []hostingTunnel
	if err := c.call(ctx, http.MethodGet, c.tunnelPath("")+"?is_deleted=false&per_page=1", nil, &tunnels); err != nil {
		return errors.New("Cloudflare could not verify Tunnel access. Give the token Cloudflare Tunnel Edit for the discovered account.")
	}
	if _, err := c.dnsRecords(ctx, c.config.Name); err != nil {
		return errors.New("Cloudflare could not verify DNS access. Give the token DNS Edit for the discovered zone.")
	}
	return nil
}

type hostingTunnel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"config_src"`
	Status string `json:"status"`
}

func (c cloudflareHostingClient) tunnelPath(id string) string {
	return "/accounts/" + c.config.Account + "/cfd_tunnel" + id
}

func (c cloudflareHostingClient) ensureTunnel(ctx context.Context, name string) (hostingTunnel, error) {
	var tunnels []hostingTunnel
	if err := c.call(ctx, http.MethodGet, c.tunnelPath("")+"?is_deleted=false&name="+url.QueryEscape(name), nil, &tunnels); err != nil {
		return hostingTunnel{}, err
	}
	var existing hostingTunnel
	for _, tunnel := range tunnels {
		if tunnel.Name != name {
			continue
		}
		if existing.ID != "" || !tunnelID.MatchString(tunnel.ID) || tunnel.Source != "cloudflare" {
			return hostingTunnel{}, errors.New("The dedicated Cloudflare Tunnel name is already in use. Review the tunnel in Cloudflare.")
		}
		existing = tunnel
	}
	if existing.ID != "" {
		return existing, nil
	}
	err := c.call(ctx, http.MethodPost, c.tunnelPath(""), map[string]string{"name": name, "config_src": "cloudflare"}, &existing)
	if err == nil && (!tunnelID.MatchString(existing.ID) || existing.Name != name || existing.Source != "cloudflare") {
		err = errors.New("Cloudflare did not return a valid remotely managed tunnel.")
	}
	return existing, err
}

func (c cloudflareHostingClient) connectorToken(ctx context.Context, id string) (string, error) {
	var token string
	err := c.call(ctx, http.MethodGet, c.tunnelPath("/"+id+"/token"), nil, &token)
	if err == nil && (len(token) < 20 || strings.ContainsAny(token, " \r\n\t")) {
		err = errors.New("Cloudflare did not return a valid connector token.")
	}
	return token, err
}

type hostingIngress struct {
	Host    string `json:"hostname,omitempty"`
	Service string `json:"service"`
}

var hostingDenyIngress = hostingIngress{Service: "http_status:404"}

func (c cloudflareHostingClient) ingress(ctx context.Context, tunnel string) ([]hostingIngress, error) {
	var response struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	path := c.tunnelPath("/" + tunnel + "/configurations")
	if err := c.call(ctx, http.MethodGet, path, nil, &response); err != nil {
		var providerErr hostingProviderError
		if errors.As(err, &providerErr) && providerErr.status == 404 {
			return nil, nil
		}
		return nil, err
	}
	var rawRoutes []json.RawMessage
	for key, raw := range response.Config {
		switch key {
		case "ingress":
			if json.Unmarshal(raw, &rawRoutes) != nil {
				return nil, errors.New("The Cloudflare Tunnel contains invalid ingress configuration.")
			}
		case "originRequest", "warp-routing":
			if string(raw) == "{}" || string(raw) == "null" || (key == "warp-routing" && string(raw) == `{"enabled":false}`) {
				continue
			}
			return nil, errors.New("The Cloudflare Tunnel has customized origin or private-network settings.")
		default:
			return nil, errors.New("The Cloudflare Tunnel has unmanaged configuration.")
		}
	}
	var routes []hostingIngress
	for _, raw := range rawRoutes {
		var route hostingIngress
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &route) != nil || json.Unmarshal(raw, &fields) != nil {
			return nil, errors.New("The Cloudflare Tunnel contains an unsupported route.")
		}
		for field, value := range fields {
			if field != "hostname" && field != "service" && !(field == "originRequest" && string(value) == "{}") &&
				!(field == "path" && string(value) == `""`) {
				return nil, errors.New("The Cloudflare Tunnel contains an unmanaged route option.")
			}
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (c cloudflareHostingClient) enforceIngress(ctx context.Context, tunnel string, allowed, desired []hostingIngress) error {
	current, err := c.ingress(ctx, tunnel)
	if err != nil {
		return err
	}
	if err := validateHostingIngress(current, allowed); err != nil {
		return err
	}
	desired = append(desired, hostingDenyIngress)
	return c.call(ctx, http.MethodPut, c.tunnelPath("/"+tunnel+"/configurations"),
		map[string]any{"config": map[string]any{"ingress": desired}}, nil)
}

func validateHostingIngress(current, allowed []hostingIngress) error {
	seen := map[string]bool{}
	for i, route := range current {
		if route == hostingDenyIngress && i == len(current)-1 {
			continue
		}
		if route.Host == "" || seen[route.Host] || !containsIngress(allowed, route) {
			return errors.New("Haven found unmanaged Cloudflare Tunnel routes. Review them in Cloudflare before continuing.")
		}
		seen[route.Host] = true
	}
	return nil
}

func containsIngress(list []hostingIngress, route hostingIngress) bool {
	for _, entry := range list {
		if entry == route {
			return true
		}
	}
	return false
}

type hostingDNSRecord struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Comment string `json:"comment,omitempty"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

func (c cloudflareHostingClient) dnsRecords(ctx context.Context, host string) ([]hostingDNSRecord, error) {
	var records []hostingDNSRecord
	path := "/zones/" + c.config.Zone + "/dns_records?per_page=100&name.exact=" + url.QueryEscape(host)
	err := c.call(ctx, http.MethodGet, path, nil, &records)
	if err == nil && len(records) >= 100 {
		err = errors.New("Too many matching DNS records to verify ownership safely.")
	}
	return records, err
}

func wantedHostingDNS(site hostingSite, tunnel string) hostingDNSRecord {
	p := site.Publication
	record := hostingDNSRecord{ID: p.RecordID, Name: p.Host, Type: "A", Comment: site.marker() + "/" + p.ID, Content: p.Address, TTL: 1}
	if p.Mode == "tunnel" {
		record.Type = "CNAME"
		record.Content = tunnel + ".cfargotunnel.com"
		record.Proxied = true
	}
	return record
}

func (c cloudflareHostingClient) ownedDNS(ctx context.Context, wanted hostingDNSRecord, deleting bool, alternate ...string) (*hostingDNSRecord, error) {
	records, err := c.dnsRecords(ctx, wanted.Name)
	if err != nil {
		return nil, err
	}
	var owned *hostingDNSRecord
	for _, record := range records {
		if !strings.EqualFold(record.Name, wanted.Name) || (record.Type != "A" && record.Type != "AAAA" && record.Type != "CNAME" && record.Type != "NS") {
			continue
		}
		if record.Comment != wanted.Comment || record.Type != wanted.Type {
			if deleting {
				continue
			}
			return nil, fmt.Errorf("An existing DNS record conflicts with %s. Resolve it in Cloudflare.", wanted.Name)
		}
		if owned != nil || !cloudflareID.MatchString(record.ID) || (wanted.ID != "" && wanted.ID != record.ID) || record.Proxied != wanted.Proxied ||
			!matchingHostingDNSContent(strings.TrimSuffix(record.Content, "."), wanted.Content, alternate) {
			return nil, errors.New("A Haven DNS record has changed outside Haven. Review the record in Cloudflare.")
		}
		owned = &record
	}
	return owned, nil
}

func matchingHostingDNSContent(actual, desired string, alternate []string) bool {
	return actual == desired || (len(alternate) == 1 && actual == alternate[0])
}

func (c cloudflareHostingClient) ensureDNS(ctx context.Context, desired hostingDNSRecord) (string, error) {
	owned, err := c.ownedDNS(ctx, desired, false)
	if err != nil {
		return "", err
	}
	if owned != nil {
		return owned.ID, nil
	}
	if desired.ID != "" {
		return "", errors.New("The previously recorded DNS record is missing. Review DNS in Cloudflare.")
	}
	var record hostingDNSRecord
	if err := c.call(ctx, http.MethodPost, "/zones/"+c.config.Zone+"/dns_records", desired, &record); err != nil {
		return "", err
	}
	if !cloudflareID.MatchString(record.ID) {
		return "", errors.New("Cloudflare did not return a valid DNS record identifier. Retry to reconcile the record.")
	}
	return record.ID, nil
}

func (c cloudflareHostingClient) removeDNS(ctx context.Context, desired hostingDNSRecord, alternate ...string) error {
	owned, err := c.ownedDNS(ctx, desired, true, alternate...)
	if err != nil || owned == nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, "/zones/"+c.config.Zone+"/dns_records/"+owned.ID, nil, nil)
}
