package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type hostingTestTransport func(*http.Request) (int, any)

func (fn hostingTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	status, body := fn(r)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}

func cloudflareTestResponse(result any) map[string]any {
	return map[string]any{"success": true, "result": result}
}

func TestHostingPortCheckGuidesHavenPort80Recovery(t *testing.T) {
	check := hostingPortCheck(80, false, true, false, false)
	if check.OK || check.Action != "disable-haven-port-80" || !strings.Contains(check.Info, "Haven is configured") || check.Details == "" || check.Fix == "" {
		t.Fatalf("unexpected Haven port 80 check: %+v", check)
	}
	check = hostingPortCheck(443, false, true, true, false)
	if !check.Recoverable || check.Action != "" {
		t.Fatalf("unexpected partial Coolify port check: %+v", check)
	}
}

func TestHostingDisconnectRequiresNoManagedSites(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, &Docker{})
	connected := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}, Sites: []hostingSite{{ID: "site-one"}}}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: connected.Coolify.URL}
	if err := h.disconnectCoolify(connected, keys); err == nil || !strings.Contains(err.Error(), "Remove every") {
		t.Fatalf("disconnect with a managed site returned %v", err)
	}
	connected.Sites = nil
	if err := h.disconnectCoolify(connected, keys); err != nil {
		t.Fatal(err)
	}
	state, savedKeys, err := h.load()
	if err != nil || state.Coolify.URL != "" || savedKeys.Coolify != "" || savedKeys.CoolifyOrigin != "" {
		t.Fatalf("disconnect did not clear connection and credentials: state=%+v keys=%+v err=%v", state, savedKeys, err)
	}
}

func TestHostingRemoveSiteDeletesOwnedCoolifyService(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := hostingSite{ID: "site-one", Name: "One", Template: "static-html", ServiceID: "service-one", Phase: "installed"}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}, Sites: []hostingSite{site}}
	seenDelete := false
	h := newHosting(store, nil, &Docker{})
	h.http = &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/services/service-one":
			return 200, coolifyService{UUID: site.ServiceID, Description: site.marker(), Apps: []coolifyApplication{{UUID: "app-one", Name: "web"}}}
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/services/service-one":
			for _, key := range []string{"delete_configurations", "delete_volumes", "docker_cleanup", "delete_connected_networks"} {
				if r.URL.Query().Get(key) != "true" {
					t.Fatalf("Coolify deletion did not enable %s", key)
				}
			}
			seenDelete = true
			return 204, nil
		default:
			t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
			return 500, nil
		}
	})}
	if err := h.save(state); err != nil {
		t.Fatal(err)
	}
	if err := h.saveStaticSite(site.ID, "<!doctype html><title>One</title>"); err != nil {
		t.Fatal(err)
	}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	if err := h.removeSite(context.Background(), &Job{}, state, keys, &site); err != nil {
		t.Fatal(err)
	}
	saved, _, err := h.load()
	if err != nil || !seenDelete || len(saved.Sites) != 0 {
		t.Fatalf("site removal incomplete: deleted=%v state=%+v err=%v", seenDelete, saved, err)
	}
	if _, err := os.Stat(filepath.Dir(h.staticSitePath(site.ID))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("saved static source remains after removal: %v", err)
	}
}

func TestHostingRemoveSiteRequiresUnpublish(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, &Docker{})
	site := hostingSite{ID: "site-one", ServiceID: "service-one", Publication: &hostingPublication{Host: "one.example.com"}}
	err = h.removeSite(context.Background(), &Job{}, hostingState{}, hostingSecrets{}, &site)
	if err == nil || !strings.Contains(err.Error(), "Unpublish") {
		t.Fatalf("published site removal returned %v", err)
	}
}

func TestHostingRemoveSiteRejectsChangedCoolifyService(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := hostingSite{ID: "site-one", Name: "One", Template: "static-html", ServiceID: "service-one", Phase: "installed"}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}, Sites: []hostingSite{site}}
	deleted := false
	h := newHosting(store, nil, &Docker{})
	h.http = &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
		if r.Method == http.MethodDelete {
			deleted = true
		}
		return 200, coolifyService{UUID: site.ServiceID, Description: "changed", Apps: []coolifyApplication{{UUID: "app-one", Name: "web"}}}
	})}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	err = h.removeSite(context.Background(), &Job{}, state, keys, &site)
	if err == nil || !strings.Contains(err.Error(), "no longer matches") || deleted {
		t.Fatalf("changed service removal was not blocked: err=%v deleted=%v", err, deleted)
	}
}

func TestHostingRemovePendingSiteWithoutCoolifyService(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := hostingSite{ID: "pending-site", Name: "Pending", Template: "static-html", Phase: "installing"}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}, Sites: []hostingSite{site}}
	h := newHosting(store, nil, &Docker{})
	h.http = &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/services" {
			t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
		}
		return 200, []coolifyService{}
	})}
	if err := h.save(state); err != nil {
		t.Fatal(err)
	}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	if err := h.removeSite(context.Background(), &Job{}, state, keys, &site); err != nil {
		t.Fatal(err)
	}
	saved, _, err := h.load()
	if err != nil || len(saved.Sites) != 0 {
		t.Fatalf("pending site record remains: state=%+v err=%v", saved, err)
	}
}

func TestHostingRemoveSiteForgetsMissingCoolifyService(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, &Docker{})
	site := hostingSite{ID: "missing-site", Name: "Missing site", Template: "static-html", ServiceID: "missing-service"}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}, Sites: []hostingSite{site}}
	if err := h.save(state); err != nil {
		t.Fatal(err)
	}
	h.http = &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/services/missing-service" {
			t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
		}
		return http.StatusNotFound, nil
	})}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	if err := h.removeSite(context.Background(), &Job{}, state, keys, &site); err != nil {
		t.Fatal(err)
	}
	saved, _, err := h.load()
	if err != nil || len(saved.Sites) != 0 {
		t.Fatalf("missing Coolify service left a Haven record: state=%+v err=%v", saved, err)
	}
}

func TestHostingUninstallChecksResourcesAndRemovesOnlyChosenRoot(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, &Docker{})
	h.http = &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/resources" {
			t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
		}
		return 200, []any{}
	})}
	root := filepath.Join(t.TempDir(), "coolify")
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", "docker-compose.yml", "docker-compose.prod.yml", "docker-compose.custom.yml"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	var commands [][]string
	run := func(_ context.Context, _ *Job, args ...string) error {
		commands = append(commands, append([]string(nil), args...))
		return nil
	}
	if err := h.uninstallCoolify(context.Background(), &Job{}, state, keys, root, run); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("uninstall ran %d commands, want 2: %v", len(commands), commands)
	}
	joined := strings.Join(commands[0], " ")
	for _, want := range []string{"docker compose", "--env-file " + filepath.Join(source, ".env"), "-f " + filepath.Join(source, "docker-compose.custom.yml"), "down --volumes --remove-orphans"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("uninstall command %q missing %q", joined, want)
		}
	}
	if got := strings.Join(commands[1], " "); got != "docker network rm coolify" {
		t.Fatalf("network cleanup command = %q", got)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Coolify root remains after uninstall: %v", err)
	}
}

func TestHostingUninstallRefusesRemainingResources(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, &Docker{})
	h.http = &http.Client{Transport: hostingTestTransport(func(*http.Request) (int, any) { return 200, []any{map[string]string{"uuid": "other"}} })}
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}}
	keys := hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}
	called := false
	err = h.uninstallCoolify(context.Background(), &Job{}, state, keys, t.TempDir(), func(context.Context, *Job, ...string) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "still manages 1 resource") || called {
		t.Fatalf("unsafe uninstall result: err=%v runnerCalled=%v", err, called)
	}
}

func TestCoolifyComposeArgsRejectsUnsafeRoot(t *testing.T) {
	if _, err := coolifyComposeArgs(t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe data path") {
		t.Fatalf("unsafe uninstall root returned %v", err)
	}
}

func TestHostingCleanupWorksWhileExperimentalFeatureIsOff(t *testing.T) {
	t.Setenv("HAVEN_HOSTING_EXPERIMENTAL", "")
	server, clients, store := roleServer(t)
	h := newHosting(store, nil, nil)
	state := hostingState{Coolify: hostingConnection{URL: "https://coolify.example.com"}}
	if err := h.save(state); err != nil {
		t.Fatal(err)
	}
	if err := h.write("secrets.json", hostingSecrets{Coolify: "coolify-token", CoolifyOrigin: state.Coolify.URL}); err != nil {
		t.Fatal(err)
	}
	if got := call(t, clients["boss"], http.MethodPost, server.URL+"/api/hosting/disconnect", `{"confirm":true,"revoked":true}`); got != http.StatusOK {
		t.Fatalf("cleanup while Hosting is off returned %d", got)
	}
	saved, keys, err := h.load()
	if err != nil || saved.Coolify.URL != "" || keys.Coolify != "" {
		t.Fatalf("cleanup did not clear connection: state=%+v keys=%+v err=%v", saved, keys, err)
	}
}

func TestDetectCoolifyInstallationStates(t *testing.T) {
	for _, tc := range []struct {
		name       string
		envFile    bool
		containers []Container
		want       string
	}{
		{name: "new host", want: "not-installed"},
		{name: "files only", envFile: true, want: "partial"},
		{name: "stopped stack", containers: []Container{{Name: "coolify", State: "exited", Project: "coolify"}}, want: "partial"},
		{name: "running stack", containers: []Container{{Name: "coolify", State: "running", Project: "coolify"}}, want: "running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectCoolifyInstallation(tc.envFile, tc.containers).Status; got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}

	runningPort := hostingPortCheck(8000, false, false, false, true)
	if !runningPort.OK || !strings.Contains(runningPort.Info, "running Coolify") || runningPort.Fix != "" {
		t.Fatalf("running Coolify port was treated as blocked: %+v", runningPort)
	}
}

func TestHostingPrivateCoolifyInstallContract(t *testing.T) {
	for _, template := range hostingTemplates {
		t.Run(template.ID, func(t *testing.T) {
			site := hostingSite{ID: "saved-site", Name: "Private site", Template: template.ID, PrivatePort: 18080}
			store, err := LoadStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			hosting := newHosting(store, nil, nil)
			if template.Static {
				if err := hosting.saveStaticSite(site.ID, "<!doctype html><title>Private site</title><h1>Ready</h1>"); err != nil {
					t.Fatal(err)
				}
			}
			composeInput, err := hosting.siteCompose(site)
			if err != nil {
				t.Fatal(err)
			}
			var created bool
			client := coolifyClient{
				connection: hostingConnection{URL: "https://coolify.example.com", Server: "local-coolify", Project: "project", Environment: "environment"},
				token:      "test-coolify-api-token",
				client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
					if r.URL.Path != "/api/v1/services" || r.Header.Get("Authorization") != "Bearer test-coolify-api-token" {
						t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
					}
					if r.Method == http.MethodGet {
						if created {
							return 200, []coolifyService{{UUID: "service123", Description: site.marker()}}
						}
						return 200, []coolifyService{}
					}
					if r.Method != http.MethodPost || created {
						t.Fatal("a retry must recover the saved service instead of creating a second one")
					}
					created = true
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body["instant_deploy"] != false || body["urls"] != nil || body["type"] != nil || body["description"] != site.marker() || body["connect_to_docker_network"] != false {
						t.Fatalf("site must be created without a deployment, domain or shared network: %+v", body)
					}
					raw, err := base64.StdEncoding.DecodeString(body["docker_compose_raw"].(string))
					if err != nil {
						t.Fatal(err)
					}
					var compose struct {
						Services map[string]struct {
							Ports    []string
							Networks []string
							Volumes  []string
						}
					}
					if err := yaml.Unmarshal(raw, &compose); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(raw), "SERVICE_FQDN_") {
						t.Fatal("guided templates must not request a generated hostname")
					}
					if template.Static && len(compose.Services) != 1 {
						t.Fatal("the static template should contain one web service")
					}
					if !template.Static && len(compose.Services) != 2 {
						t.Fatal("database-backed templates need one web service and one database")
					}
					for service, spec := range compose.Services {
						if !template.Static && len(spec.Volumes) != 1 {
							t.Errorf("%s has no persistent volume", service)
						}
						if service == template.Web {
							if len(spec.Ports) != 1 || !strings.HasPrefix(spec.Ports[0], "127.0.0.1:18080:") ||
								!reflect.DeepEqual(spec.Networks, []string{"default", "hosting"}) {
								t.Errorf("unsafe web service: %+v", spec)
							}
						} else if len(spec.Ports) != 0 || len(spec.Networks) != 0 {
							t.Errorf("database must stay on the stack's default network: %+v", spec)
						}
					}
					return 201, hostingResource{UUID: "service123"}
				})},
			}
			for range 2 {
				if id, err := client.create(context.Background(), site, composeInput); id != "service123" || err != nil {
					t.Fatalf("create returned %q, %v", id, err)
				}
			}
		})
	}
}

func TestCoolifyClientRequiresSupportedVersion(t *testing.T) {
	version := "v" + coolifyHostingVersion
	client := coolifyClient{
		connection: hostingConnection{URL: "https://coolify.example.com"},
		token:      "test-coolify-api-token",
		client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
			if r.URL.Path != "/api/v1/version" {
				t.Fatalf("unexpected Coolify request: %s %s", r.Method, r.URL.Path)
			}
			return http.StatusOK, version
		})},
	}
	if err := client.validate(context.Background()); err != nil {
		t.Fatalf("target Coolify version was rejected: %v", err)
	}
	version = "v4.7.6"
	if err := client.validate(context.Background()); err == nil || !strings.Contains(err.Error(), coolifyHostingVersion) {
		t.Fatalf("older Coolify version was not rejected clearly: %v", err)
	}
}

func TestHostingPublishNeedsAdminAndMatchingReview(t *testing.T) {
	t.Setenv("HAVEN_HOSTING_EXPERIMENTAL", "1")
	server, clients, store := roleServer(t)
	h := newHosting(store, nil, nil)
	state := hostingState{Sites: []hostingSite{{ID: "persisted-site", Name: "Test website", Phase: "installed"}}}
	if err := h.save(state); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user, route, body string
		want              int
	}{
		{"mia", "", "", http.StatusForbidden},
		{"mia", "/sites", `{"confirm":true}`, http.StatusForbidden},
		{"mia", "/sites/persisted-site/publish", `{"confirm":true}`, http.StatusForbidden},
		{"mia", "/disconnect", `{"confirm":true,"revoked":true}`, http.StatusForbidden},
		{"boss", "/sites/persisted-site/publish", `{"confirm":true,"host":"example.com","review":"anything"}`, http.StatusBadRequest},
		{"boss", "/sites/persisted-site/publish", `{"confirm":false}`, http.StatusBadRequest},
		{"boss", "/sites/persisted-site/remove", `{"confirm":false}`, http.StatusBadRequest},
		{"boss", "/disconnect", `{"confirm":false}`, http.StatusBadRequest},
		{"boss", "/disconnect", `{"confirm":true,"revoked":false}`, http.StatusBadRequest},
		{"boss", "/disconnect", `{"confirm":true,"revoked":true}`, http.StatusBadRequest},
		{"boss", "/uninstall", `{"confirm":true,"phrase":"wrong"}`, http.StatusBadRequest},
		{"boss", "/uninstall", `{"confirm":true,"phrase":"UNINSTALL COOLIFY"}`, http.StatusBadRequest},
	} {
		method := http.MethodPost
		if tc.body == "" {
			method = http.MethodGet
		}
		if got := call(t, clients[tc.user], method, server.URL+"/api/hosting"+tc.route, tc.body); got != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.user, tc.route, got, tc.want)
		}
	}
	saved, _, err := h.load()
	if err != nil || saved.Sites[0].Publication != nil {
		t.Fatal("unauthorized or unreviewed publishing wrote public state")
	}
}

func TestHostingSecretsStayBoundAndOutsideFileExplorer(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, nil)
	if err := h.save(hostingState{Coolify: hostingConnection{URL: "https://old.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.write("secrets.json", hostingSecrets{Coolify: "new-coolify-token", CoolifyOrigin: "https://new.example.com"}); err != nil {
		t.Fatal(err)
	}
	_, keys, err := h.load()
	if err != nil || keys.Coolify != "" {
		t.Fatal("credentials from an interrupted rebind must not be used against the old origin")
	}
	info, err := os.Stat(filepath.Join(h.dir, "secrets.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("hosting secrets must be owner-readable only")
	}
	files := NewFiles(store, store.dir).As(true)
	if err := store.UpdateSettings(func(s *HostSettings) { s.FilesFullDisk = true }); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Resolve(filepath.Join(h.dir, "secrets.json")); err == nil {
		t.Fatal("the file explorer must not open hosting credentials, even with full-disk access")
	}
	link := filepath.Join(store.dir, "outside")
	if err := os.Symlink(filepath.Join(h.dir, "secrets.json"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Resolve(link); err == nil {
		t.Fatal("a symlink must not expose hosting credentials")
	}
}

func TestHostingCloudflareDiscoversExactZone(t *testing.T) {
	client := cloudflareHostingClient{
		token: "scoped-cloudflare-token",
		client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
			if r.Method != http.MethodGet || r.URL.Path != "/zones" || r.URL.Query().Get("name") != "example.com" ||
				r.Header.Get("Authorization") != "Bearer scoped-cloudflare-token" {
				t.Fatalf("unexpected discovery request: %s %s", r.Method, r.URL.String())
			}
			return 200, cloudflareTestResponse([]map[string]any{{
				"id": "11111111111111111111111111111111", "name": "example.com", "status": "active", "account": map[string]string{"id": "22222222222222222222222222222222"},
			}})
		})},
		base: "https://api.cloudflare.test",
	}
	config, err := client.discoverZone(context.Background(), "Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if config.Name != "example.com" || config.Zone != "11111111111111111111111111111111" || config.Account != "22222222222222222222222222222222" {
		t.Fatalf("unexpected discovered zone: %+v", config)
	}
}

func TestHostingCloudflareChecksPublishingAccess(t *testing.T) {
	seen := map[string]bool{}
	client := cloudflareHostingClient{
		token: "scoped-cloudflare-token",
		config: hostingCloudflare{
			Account: "22222222222222222222222222222222",
			Zone:    "11111111111111111111111111111111",
			Name:    "example.com",
		},
		client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
			seen[r.URL.Path] = true
			switch r.URL.Path {
			case "/accounts/22222222222222222222222222222222/cfd_tunnel":
				if r.URL.Query().Get("is_deleted") != "false" || r.URL.Query().Get("per_page") != "1" {
					t.Fatalf("unexpected tunnel access query: %s", r.URL.RawQuery)
				}
			case "/zones/11111111111111111111111111111111/dns_records":
				if r.URL.Query().Get("name.exact") != "example.com" {
					t.Fatalf("unexpected DNS access query: %s", r.URL.RawQuery)
				}
			default:
				t.Fatalf("unexpected access request: %s", r.URL.String())
			}
			return 200, cloudflareTestResponse([]any{})
		})},
		base: "https://api.cloudflare.test",
	}
	if err := client.checkPublishingAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("expected Tunnel and DNS access checks, saw %v", seen)
	}
}

func TestHostingStaticSourceValidation(t *testing.T) {
	store, err := LoadStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := newHosting(store, nil, nil)
	if err := h.saveStaticSite("site", ""); err == nil {
		t.Fatal("empty static source should be rejected")
	}
	if err := h.saveStaticSite("site", "<!doctype html><title>Safe</title><h1>Hello</h1>"); err != nil {
		t.Fatal(err)
	}
	compose, err := h.siteCompose(hostingSite{ID: "site", Template: "static-html", PrivatePort: 18080})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compose, `127.0.0.1:18080:80`) || !strings.Contains(compose, "<h1>Hello</h1>") ||
		!strings.Contains(compose, "networks: [default, hosting]") {
		t.Fatalf("static compose lost its private binding, content or isolated network: %s", compose)
	}
}

func TestHostingCloudflareTunnelPreservesExactRoutes(t *testing.T) {
	first := hostingIngress{"one.example.com", "http://wordpress-abc:80"}
	second := hostingIngress{"two.example.com", "http://ghost-def:2368"}
	for _, tc := range []struct {
		name     string
		current  []hostingIngress
		expected []hostingIngress
	}{
		{"add second", []hostingIngress{first, hostingDenyIngress}, []hostingIngress{first, second, hostingDenyIngress}},
		{"reject wildcard", []hostingIngress{{"*.example.com", "http://coolify:8000"}, hostingDenyIngress}, nil},
		{"reject wrong origin", []hostingIngress{{first.Host, "http://haven:8484"}, hostingDenyIngress}, nil},
		{"reject duplicate", []hostingIngress{first, first, hostingDenyIngress}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []hostingIngress
			c := cloudflareHostingClient{
				base: "https://api.example.com", config: hostingCloudflare{Account: "account"},
				client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
					switch r.Method {
					case http.MethodGet:
						return 200, cloudflareTestResponse(map[string]any{"config": map[string]any{"ingress": tc.current}})
					case http.MethodPut:
						var update struct {
							Config struct{ Ingress []hostingIngress }
						}
						if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
							t.Fatal(err)
						}
						sent = update.Config.Ingress
						return 200, cloudflareTestResponse(nil)
					default:
						t.Fatalf("unexpected method: %s", r.Method)
						return 500, nil
					}
				})},
			}
			err := c.enforceIngress(context.Background(), "dedicated", []hostingIngress{first, second}, []hostingIngress{first, second})
			if (err == nil) != (tc.expected != nil) || !reflect.DeepEqual(sent, tc.expected) {
				t.Errorf("sent %+v and error %v, want %+v", sent, err, tc.expected)
			}
		})
	}
}

func TestHostingOwnedDNSConflictAndRecovery(t *testing.T) {
	ctx := context.Background()
	site := hostingSite{ID: "site1", Publication: &hostingPublication{
		ID: "publication1", Host: "blog.example.com", Mode: "direct", Address: "8.8.8.8",
	}}
	wanted := wantedHostingDNS(site, "")
	foreign := hostingDNSRecord{ID: strings.Repeat("f", 32), Name: wanted.Name, Type: "A", Content: wanted.Content}
	records := []hostingDNSRecord{foreign}
	var changed []string
	c := cloudflareHostingClient{
		base: "https://api.example.com", config: hostingCloudflare{Zone: "zone"},
		client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
			switch r.Method {
			case http.MethodGet:
				if r.URL.Query().Get("name.exact") != wanted.Name {
					t.Fatal("DNS ownership lookup did not filter by exact hostname")
				}
				return 200, cloudflareTestResponse(records)
			case http.MethodPost:
				var newRecord hostingDNSRecord
				if err := json.NewDecoder(r.Body).Decode(&newRecord); err != nil {
					t.Fatal(err)
				}
				newRecord.ID = strings.Repeat("a", 32)
				records = append(records, newRecord)
				changed = append(changed, r.Method)
				return 200, cloudflareTestResponse(newRecord)
			case http.MethodPatch, http.MethodDelete:
				if !strings.HasSuffix(r.URL.Path, "/"+strings.Repeat("a", 32)) {
					t.Fatalf("attempt to mutate an unrelated record: %s", r.URL.Path)
				}
				if r.Method == http.MethodPatch {
					records[0].Content = site.Publication.NextIP
				}
				changed = append(changed, r.Method)
				return 200, cloudflareTestResponse(nil)
			}
			t.Fatalf("unexpected method: %s", r.Method)
			return 500, nil
		})},
	}
	if _, err := c.ensureDNS(ctx, wanted); err == nil {
		t.Fatal("matching content is still a conflict when Haven does not own the record")
	}
	if err := c.removeDNS(ctx, wanted); err != nil || len(changed) != 0 {
		t.Fatal("unpublish must preserve the unrelated record")
	}
	records = []hostingDNSRecord{}
	id, err := c.ensureDNS(ctx, wanted)
	if err != nil || id != strings.Repeat("a", 32) {
		t.Fatalf("record creation: %q, %v", id, err)
	}
	if recovered, err := c.ensureDNS(ctx, wanted); err != nil || recovered != id || len(changed) != 1 {
		t.Fatal("retry must recover the existing record after a failed state write")
	}
	site.Publication.RecordID, site.Publication.NextIP = id, "1.1.1.1"
	h := &Hosting{}
	for range 2 {
		if err := h.finishHostingIP(ctx, c, site); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.removeDNS(ctx, wantedHostingDNS(site, ""), site.Publication.NextIP); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"POST", "PATCH", "DELETE"}) {
		t.Errorf("unexpected DNS writes: %v", changed)
	}
}

func TestHostingHostnameAddressesAndDirectRoute(t *testing.T) {
	for _, host := range []string{"*.example.com", "http://example.com", "home.example.com:443", "127.0.0.1", "server.local"} {
		if _, err := normalizeHostingHost(host); err == nil {
			t.Errorf("accepted invalid hosting name %q", host)
		}
	}
	if name, err := normalizeHostingHost(" Blog.Example.com. "); name != "blog.example.com" || err != nil {
		t.Fatalf("normalize hostname: %q %v", name, err)
	}
	for _, address := range []string{"192.168.1.2", "100.64.0.1", "203.0.113.1", "::ffff:127.0.0.1", "fe80::1%en0"} {
		if _, valid := hostingPublicIP(address); valid {
			t.Errorf("accepted nonpublic address %q", address)
		}
	}
	labels := map[string]string{
		"traefik.enable":                                      "true",
		"traefik.http.routers.site.rule":                      "Host(`blog.example.com`) && PathPrefix(`/`)",
		"traefik.http.routers.site.tls":                       "true",
		"traefik.http.routers.site.tls.certresolver":          "letsencrypt",
		"traefik.http.routers.site.service":                   "site",
		"traefik.http.services.site.loadbalancer.server.port": "2368",
	}
	if present, secure := hostedTraefikRoute(labels, "blog.example.com", 2368); !present || !secure {
		t.Fatal("expected a valid Coolify-style HTTPS route")
	}
	if present, secure := hostedTraefikRoute(labels, "example.com", 2368); present || secure {
		t.Fatal("a subdomain must not match its parent")
	}
	if present, secure := hostedTraefikRoute(labels, "blog.example.com", 80); !present || secure {
		t.Fatal("the HTTPS route must target the selected site's actual web port")
	}
}

func TestHostingDirectGuardsCoolifyAutomaticDNS(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		response   map[string]any
		wantError  bool
	}{
		{"unrelated zone", "elsewhere.example", map[string]any{"automatic_dns": true}, false},
		{"automatic DNS off", "example.com", map[string]any{"automatic_dns": false}, false},
		{"automatic DNS on", "example.com", map[string]any{"automatic_dns": true}, true},
		{"unknown settings", "example.com", map[string]any{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := coolifyClient{
				connection: hostingConnection{URL: "https://coolify.example.com"},
				client: &http.Client{Transport: hostingTestTransport(func(r *http.Request) (int, any) {
					if r.Method != http.MethodGet {
						t.Fatal("DNS preflight must be read-only")
					}
					switch r.URL.Path {
					case "/api/v1/dns/zones":
						return 200, []map[string]string{{"zone": tc.zone, "integration_token_uuid": "integration123"}}
					case "/api/v1/security/integration-tokens/integration123":
						return 200, tc.response
					default:
						t.Fatalf("unexpected request %s", r.URL.Path)
						return 500, nil
					}
				})},
			}
			if err := client.checkDNSAutomation(context.Background(), "blog.example.com"); (err != nil) != tc.wantError {
				t.Fatalf("unexpected automatic DNS result: %v", err)
			}
		})
	}
}
