package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// These tests are compiled on every test run but touch Cloudflare only when a
// protected workflow explicitly opts in. They verify the provider control
// plane, not connector connectivity or public HTTPS.
type cloudflareLiveState struct {
	Domain      string `json:"domain"`
	Account     string `json:"account"`
	Zone        string `json:"zone"`
	Host        string `json:"host"`
	Site        string `json:"site"`
	Publication string `json:"publication"`
	TunnelName  string `json:"tunnelName"`
	TunnelID    string `json:"tunnelId,omitempty"`
	DNSRecord   string `json:"dnsRecord,omitempty"`
}

var cloudflareLiveSuffix = regexp.MustCompile(`^[0-9a-f]{16}$`)

func cloudflareLiveSettings(t *testing.T, mode string) (token, domain, statePath string) {
	t.Helper()
	if os.Getenv("HAVEN_CLOUDFLARE_LIVE") != mode {
		t.Skip("live Cloudflare integration is disabled")
	}
	token = os.Getenv("CLOUDFLARE_API_TOKEN")
	domain = os.Getenv("CLOUDFLARE_TEST_DOMAIN")
	statePath = os.Getenv("HAVEN_CLOUDFLARE_STATE")
	if !validHostingToken(token) || domain == "" || statePath == "" {
		t.Fatal("the protected Cloudflare environment is incomplete")
	}
	return
}

func writeCloudflareLiveState(path string, state cloudflareLiveState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

func readCloudflareLiveState(path string) (cloudflareLiveState, error) {
	var state cloudflareLiveState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if json.Unmarshal(data, &state) != nil {
		return state, errors.New("the Cloudflare live-test state is invalid")
	}
	return state, nil
}

func TestCloudflareLiveLifecycle(t *testing.T) {
	token, rawDomain, statePath := cloudflareLiveSettings(t, "run")
	domain, err := normalizeHostingHost(rawDomain)
	if err != nil {
		t.Fatal("CLOUDFLARE_TEST_DOMAIN must be an active root domain")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client := cloudflareHostingClient{client: &http.Client{Timeout: 18 * time.Second, CheckRedirect: noHostingRedirect}, base: cloudflareHostingAPI, token: token}
	config, err := client.discoverZone(ctx, domain)
	if err != nil {
		t.Fatal(err)
	}
	client.config = config

	random, err := randomHostingID()
	if err != nil {
		t.Fatal(err)
	}
	suffix := random[:16]
	state := cloudflareLiveState{
		Domain: domain, Account: config.Account, Zone: config.Zone,
		Host: "haven-ci-" + suffix + "." + domain,
		Site: "ci-" + suffix, Publication: random,
		TunnelName: "haven-ci-" + suffix,
	}
	// Persist ownership markers before the first mutation so the unconditional
	// cleanup step can reconcile a process interrupted immediately after a call.
	if err := writeCloudflareLiveState(statePath, state); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cleanupCancel()
		if err := cleanupCloudflareLive(cleanupCtx, token, domain, statePath); err != nil {
			t.Errorf("Cloudflare cleanup failed: %v", err)
		}
	}()

	tunnel, err := client.ensureTunnel(ctx, state.TunnelName)
	if err != nil {
		t.Fatal(err)
	}
	state.TunnelID = tunnel.ID
	if err := writeCloudflareLiveState(statePath, state); err != nil {
		t.Fatal(err)
	}
	route := hostingIngress{Host: state.Host, Service: "http_status:204"}
	if err := client.enforceIngress(ctx, tunnel.ID, nil, []hostingIngress{route}); err != nil {
		t.Fatal(err)
	}
	routes, err := client.ingress(ctx, tunnel.ID)
	if err != nil || len(routes) != 2 || routes[0] != route || routes[1] != hostingDenyIngress {
		t.Fatalf("Cloudflare did not preserve the exact route and 404 fallback: %v", err)
	}
	if token, err := client.connectorToken(ctx, tunnel.ID); err != nil || len(token) < 20 {
		t.Fatalf("Cloudflare connector-token check failed: %v", err)
	}

	site := state.cloudflareSite()
	desired := wantedHostingDNS(site, tunnel.ID)
	recordID, err := client.ensureDNS(ctx, desired)
	if err != nil {
		t.Fatal(err)
	}
	state.DNSRecord = recordID
	if err := writeCloudflareLiveState(statePath, state); err != nil {
		t.Fatal(err)
	}
	site.Publication.RecordID = recordID
	desired = wantedHostingDNS(site, tunnel.ID)
	if record, err := client.ownedDNS(ctx, desired, false); err != nil || record == nil || record.ID != recordID {
		t.Fatalf("Cloudflare did not return the Haven-owned DNS record: %v", err)
	}
	if recovered, err := client.ensureDNS(ctx, desired); err != nil || recovered != recordID {
		t.Fatalf("Cloudflare DNS retry was not idempotent: %v", err)
	}
	foreign := desired
	foreign.Comment = "not-owned-by-haven"
	if _, err := client.ownedDNS(ctx, foreign, false); err == nil {
		t.Fatal("Cloudflare conflict protection accepted a foreign ownership marker")
	}
}

func TestCloudflareLiveCleanup(t *testing.T) {
	token, domain, statePath := cloudflareLiveSettings(t, "cleanup")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := cleanupCloudflareLive(ctx, token, domain, statePath); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareLiveStateValidation(t *testing.T) {
	valid := cloudflareLiveState{
		Domain: "example.com", Account: strings.Repeat("a", 32), Zone: strings.Repeat("b", 32),
		Host: "haven-ci-1234567890abcdef.example.com", Site: "ci-1234567890abcdef", Publication: "1234567890abcdef" + strings.Repeat("c", 16),
		TunnelName: "haven-ci-1234567890abcdef", TunnelID: "12345678-1234-1234-1234-123456789abc", DNSRecord: strings.Repeat("d", 32),
	}
	if !validCloudflareLiveState(valid) {
		t.Fatal("valid live-test ownership state was rejected")
	}
	for name, mutate := range map[string]func(*cloudflareLiveState){
		"foreign host":    func(s *cloudflareLiveState) { s.Host = "www.example.com" },
		"foreign tunnel":  func(s *cloudflareLiveState) { s.TunnelName = "production" },
		"other domain":    func(s *cloudflareLiveState) { s.Host = "haven-ci-1234567890abcdef.other.example" },
		"mismatched site": func(s *cloudflareLiveState) { s.Site = "ci-fedcba0987654321" },
		"mismatched run":  func(s *cloudflareLiveState) { s.Publication = strings.Repeat("e", 32) },
		"invalid record":  func(s *cloudflareLiveState) { s.DNSRecord = "not-an-id" },
	} {
		t.Run(name, func(t *testing.T) {
			state := valid
			mutate(&state)
			if validCloudflareLiveState(state) {
				t.Fatal("unsafe live-test state was accepted")
			}
		})
	}
}

func (state cloudflareLiveState) cloudflareSite() hostingSite {
	return hostingSite{ID: state.Site, Publication: &hostingPublication{
		ID: state.Publication, Host: state.Host, Mode: "tunnel", DNS: "cloudflare", RecordID: state.DNSRecord,
	}}
}

func validCloudflareLiveState(state cloudflareLiveState) bool {
	domain, err := normalizeHostingHost(state.Domain)
	suffix := strings.TrimPrefix(state.TunnelName, "haven-ci-")
	return err == nil && domain == state.Domain && cloudflareID.MatchString(state.Account) && cloudflareID.MatchString(state.Zone) &&
		cloudflareLiveSuffix.MatchString(suffix) && state.Site == "ci-"+suffix && cloudflareID.MatchString(state.Publication) &&
		strings.HasPrefix(state.Publication, suffix) && state.Host == state.TunnelName+"."+state.Domain &&
		(state.TunnelID == "" || tunnelID.MatchString(state.TunnelID)) && (state.DNSRecord == "" || cloudflareID.MatchString(state.DNSRecord))
}

func cleanupCloudflareLive(ctx context.Context, token, expectedDomain, statePath string) error {
	state, err := readCloudflareLiveState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !validCloudflareLiveState(state) {
		return errors.New("refusing to clean invalid Cloudflare live-test state")
	}
	domain, err := normalizeHostingHost(expectedDomain)
	if err != nil || domain != state.Domain {
		return errors.New("refusing to clean Cloudflare state outside the configured test domain")
	}
	client := cloudflareHostingClient{
		client: &http.Client{Timeout: 18 * time.Second, CheckRedirect: noHostingRedirect}, base: cloudflareHostingAPI, token: token,
	}
	config, err := client.discoverZone(ctx, domain)
	if err != nil || config.Account != state.Account || config.Zone != state.Zone {
		return errors.New("refusing to clean Cloudflare state outside the discovered account and zone")
	}
	client.config = config
	var cleanupErrors []error
	site := state.cloudflareSite()
	if err := client.removeDNS(ctx, wantedHostingDNS(site, state.TunnelID)); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("DNS cleanup: %w", err))
	}

	tunnel, tunnelErr := findCloudflareLiveTunnel(ctx, client, state)
	if tunnelErr != nil {
		cleanupErrors = append(cleanupErrors, tunnelErr)
	} else if tunnel.ID != "" {
		if err := client.call(ctx, http.MethodDelete, client.tunnelPath("/"+tunnel.ID), nil, nil); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("Tunnel cleanup: %w", err))
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.Join(cleanupErrors...)
	}
	return os.Remove(statePath)
}

func findCloudflareLiveTunnel(ctx context.Context, client cloudflareHostingClient, state cloudflareLiveState) (hostingTunnel, error) {
	var tunnels []hostingTunnel
	path := client.tunnelPath("") + "?is_deleted=false&name=" + url.QueryEscape(state.TunnelName)
	if err := client.call(ctx, http.MethodGet, path, nil, &tunnels); err != nil {
		return hostingTunnel{}, err
	}
	var found hostingTunnel
	for _, tunnel := range tunnels {
		if tunnel.Name != state.TunnelName {
			continue
		}
		if found.ID != "" || tunnel.Source != "cloudflare" || !tunnelID.MatchString(tunnel.ID) ||
			(state.TunnelID != "" && tunnel.ID != state.TunnelID) {
			return hostingTunnel{}, errors.New("refusing to delete an ambiguous Cloudflare live-test tunnel")
		}
		found = tunnel
	}
	return found, nil
}
