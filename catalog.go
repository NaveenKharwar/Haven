package main

import (
	"crypto/rand"
	"embed"
	"fmt"
	"math/big"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed catalog/*.yaml
var catalogFS embed.FS

type Setting struct {
	Key         string   `yaml:"key" json:"key"`
	Label       string   `yaml:"label" json:"label"`
	Type        string   `yaml:"type" json:"type"` // string, port, path, password, number, select
	Default     string   `yaml:"default" json:"default"`
	Description string   `yaml:"description" json:"description,omitempty"`
	Required    bool     `yaml:"required" json:"required"`
	Generate    bool     `yaml:"generate" json:"generate,omitempty"`   // password: create a random one if left empty
	Protocols   []string `yaml:"protocols" json:"protocols,omitempty"` // port: tcp (default), udp
	Options     []string `yaml:"options" json:"options,omitempty"`
	Prefill     string   `yaml:"prefill" json:"prefill,omitempty"` // "host": the UI fills in the server address
}

type WebSpec struct {
	Port   string `yaml:"port" json:"port"` // name of a port setting, or a fixed number
	Path   string `yaml:"path" json:"path,omitempty"`
	Scheme string `yaml:"scheme" json:"scheme,omitempty"`
}

type CatalogApp struct {
	ID           string    `yaml:"id" json:"id"`
	Name         string    `yaml:"name" json:"name"`
	Category     string    `yaml:"category" json:"category"`
	Tagline      string    `yaml:"tagline" json:"tagline"`
	Description  string    `yaml:"description" json:"description"`
	Icon         string    `yaml:"icon" json:"icon"`
	Website      string    `yaml:"website" json:"website,omitempty"`
	Version      string    `yaml:"version" json:"version"`
	Archs        []string  `yaml:"archs" json:"archs"`
	MinRAMMB     int       `yaml:"min_ram_mb" json:"minRamMb,omitempty"`
	StartTimeout int       `yaml:"start_timeout" json:"startTimeout,omitempty"` // seconds
	Notes        string    `yaml:"notes" json:"notes,omitempty"`
	HostPorts    []string  `yaml:"host_ports" json:"hostPorts,omitempty"`    // fixed ports like "22000/udp"
	Screenshots  []string  `yaml:"screenshots" json:"screenshots,omitempty"` // https image links from the project
	Web          *WebSpec  `yaml:"web" json:"web,omitempty"`
	Settings     []Setting `yaml:"settings" json:"settings"`
	Compose      string    `yaml:"compose" json:"compose"`
}

// Variables Haven always provides to every app.
var builtinVars = map[string]bool{"APP_DATA": true, "PUID": true, "PGID": true, "TZ": true}

var (
	appIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)
	settingKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	composeVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}`)
	validTypes   = map[string]bool{"string": true, "port": true, "path": true, "password": true, "number": true, "select": true}
)

type Catalog struct {
	apps []CatalogApp
	byID map[string]*CatalogApp
}

func LoadCatalog() (*Catalog, error) {
	entries, err := catalogFS.ReadDir("catalog")
	if err != nil {
		return nil, err
	}
	c := &Catalog{byID: map[string]*CatalogApp{}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := catalogFS.ReadFile(path.Join("catalog", e.Name()))
		if err != nil {
			return nil, err
		}
		var a CatalogApp
		if err := yaml.Unmarshal(b, &a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := validateCatalogApp(&a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, dup := c.byID[a.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate app id %q", e.Name(), a.ID)
		}
		c.apps = append(c.apps, a)
		c.byID[a.ID] = nil
	}
	sort.Slice(c.apps, func(i, j int) bool { return strings.ToLower(c.apps[i].Name) < strings.ToLower(c.apps[j].Name) })
	for i := range c.apps {
		c.byID[c.apps[i].ID] = &c.apps[i]
	}
	return c, nil
}

func (c *Catalog) All() []CatalogApp { return c.apps }

func (c *Catalog) Get(id string) (*CatalogApp, bool) {
	a, ok := c.byID[id]
	return a, ok && a != nil
}

func (a *CatalogApp) Setting(key string) *Setting {
	for i := range a.Settings {
		if a.Settings[i].Key == key {
			return &a.Settings[i]
		}
	}
	return nil
}

// validateCatalogApp catches broken catalog entries when Haven starts (and
// in tests), instead of when a user clicks Install.
func validateCatalogApp(a *CatalogApp) error {
	if !appIDRe.MatchString(a.ID) {
		return fmt.Errorf("invalid id %q", a.ID)
	}
	if a.Name == "" || a.Category == "" || a.Tagline == "" || a.Version == "" {
		return fmt.Errorf("name, category, tagline and version are required")
	}
	if len(a.Archs) == 0 {
		return fmt.Errorf("archs is required")
	}
	for _, arch := range a.Archs {
		if arch != "amd64" && arch != "arm64" {
			return fmt.Errorf("unknown arch %q", arch)
		}
	}
	keys := map[string]bool{}
	for _, s := range a.Settings {
		if !settingKeyRe.MatchString(s.Key) {
			return fmt.Errorf("setting key %q must be UPPER_SNAKE_CASE", s.Key)
		}
		if builtinVars[s.Key] {
			return fmt.Errorf("setting key %q is reserved", s.Key)
		}
		if keys[s.Key] {
			return fmt.Errorf("duplicate setting %q", s.Key)
		}
		keys[s.Key] = true
		if !validTypes[s.Type] {
			return fmt.Errorf("setting %q has unknown type %q", s.Key, s.Type)
		}
		if s.Label == "" {
			return fmt.Errorf("setting %q needs a label", s.Key)
		}
		if s.Default != "" {
			if err := validateValue(&s, s.Default); err != nil {
				return fmt.Errorf("setting %q default: %w", s.Key, err)
			}
		}
		if s.Type == "select" && len(s.Options) == 0 {
			return fmt.Errorf("select setting %q needs options", s.Key)
		}
		for _, p := range s.Protocols {
			if p != "tcp" && p != "udp" {
				return fmt.Errorf("setting %q: unknown protocol %q", s.Key, p)
			}
		}
	}
	if len(a.Screenshots) > 6 {
		return fmt.Errorf("at most 6 screenshots")
	}
	for _, u := range a.Screenshots {
		if err := validFeedURL(u); err != nil {
			return fmt.Errorf("screenshot %q: %w", u, err)
		}
	}
	for _, hp := range a.HostPorts {
		if _, _, err := parseHostPort(hp); err != nil {
			return err
		}
	}
	if a.Web != nil {
		if _, err := strconv.Atoi(a.Web.Port); err != nil {
			s := a.Setting(a.Web.Port)
			if s == nil || s.Type != "port" {
				return fmt.Errorf("web.port %q must be a port setting or a number", a.Web.Port)
			}
		}
	}

	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(a.Compose), &doc); err != nil {
		return fmt.Errorf("compose is not valid YAML: %w", err)
	}
	if len(doc.Services) == 0 {
		return fmt.Errorf("compose has no services")
	}
	for name, svc := range doc.Services {
		img, _ := svc["image"].(string)
		if img == "" {
			return fmt.Errorf("service %q has no image", name)
		}
		if strings.HasSuffix(img, ":latest") || !strings.Contains(path.Base(img), ":") {
			// Floating tags are allowed but must be explicit, never implied.
			if !strings.Contains(path.Base(img), ":") {
				return fmt.Errorf("service %q image %q must have an explicit tag", name, img)
			}
		}
		if _, ok := svc["container_name"]; ok {
			return fmt.Errorf("service %q must not set container_name (it causes name clashes)", name)
		}
		if _, ok := svc["restart"]; !ok {
			return fmt.Errorf("service %q must set a restart policy", name)
		}
	}
	for _, m := range composeVarRe.FindAllStringSubmatch(strings.ReplaceAll(a.Compose, "$$", ""), -1) {
		if !builtinVars[m[1]] && !keys[m[1]] {
			return fmt.Errorf("compose uses ${%s} but no setting defines it", m[1])
		}
	}
	return nil
}

// parseHostPort reads "8123" or "21027/udp".
func parseHostPort(s string) (int, string, error) {
	num, proto, _ := strings.Cut(s, "/")
	if proto == "" {
		proto = "tcp"
	}
	n, err := strconv.Atoi(num)
	if err != nil || n < 1 || n > 65535 || (proto != "tcp" && proto != "udp") {
		return 0, "", fmt.Errorf("invalid host port %q", s)
	}
	return n, proto, nil
}

// bindDirRe finds folders under ${APP_DATA} that the compose file mounts.
var bindDirRe = regexp.MustCompile(`\$\{APP_DATA\}/([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*)\s*:`)

func appDataSubdirs(compose string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range bindDirRe.FindAllStringSubmatch(compose, -1) {
		if !seen[m[1]] && !strings.Contains(m[1], "..") {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func validateValue(s *Setting, v string) error {
	if strings.ContainsAny(v, "\n\r'") {
		return fmt.Errorf("must not contain quotes or line breaks")
	}
	if len(v) > 1024 {
		return fmt.Errorf("is too long")
	}
	switch s.Type {
	case "port":
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("must be a port number between 1 and 65535")
		}
	case "number":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("must be a number")
		}
	case "path":
		if !strings.HasPrefix(v, "/") {
			return fmt.Errorf("must be an absolute path (start with /)")
		}
		for _, part := range strings.Split(v, "/") {
			if part == ".." {
				return fmt.Errorf("must not contain ..")
			}
		}
		if strings.ContainsAny(v, ":$") {
			return fmt.Errorf("must not contain : or $")
		}
	case "password":
		if strings.Contains(v, "$") {
			return fmt.Errorf("must not contain $")
		}
	case "select":
		for _, o := range s.Options {
			if o == v {
				return nil
			}
		}
		return fmt.Errorf("must be one of: %s", strings.Join(s.Options, ", "))
	}
	return nil
}

func randomPassword(n int) string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	for i := range b {
		x, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[x.Int64()]
	}
	return string(b)
}
