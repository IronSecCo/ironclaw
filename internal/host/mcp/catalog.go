package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Transport kinds for a configured MCP server.
const (
	TransportStdio = "stdio" // a LOCAL server: a subprocess the host spawns
	TransportHTTP  = "http"  // a REMOTE server: a streamable-HTTP endpoint the host dials
)

// ServerConfig is one operator-configured MCP server. It is HOST-side infrastructure
// (like a messaging-group or a channel adapter), not per-agent: configuring a server
// grants no agent anything — a separate, gateway-approved ChangeMCPAccess does that.
//
// Secrets should be written as ${ENV_VAR} references in Env/Headers, which the broker
// expands from the host environment at connect time, so the catalog file never stores
// a raw credential and the value can be rotated without re-editing the server. A raw
// value is still accepted (and masked from the API), but a reference is preferred.
type ServerConfig struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// Transport is "stdio" (local subprocess) or "http" (remote endpoint).
	Transport string `json:"transport"`

	// stdio (local) fields:
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	Env     map[string]string `json:"env,omitempty"` // extra subprocess env; values may be ${VAR}
	// Image is the container image a local server is isolated in when the daemon runs
	// with container isolation (the default in production). A third-party MCP server is
	// untrusted code, so it runs in a hardened, network=none container — never as a bare
	// host process. Empty uses the daemon's default MCP sandbox image.
	Image string `json:"image,omitempty"`

	// http (remote) fields:
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // request headers; values may be ${VAR}
}

var serverNameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// validateRemoteURL enforces TLS for remote MCP endpoints: https is required so the
// host↔server hop is encrypted. Plain http is allowed ONLY for a loopback host
// (127.0.0.1/::1/localhost), which never leaves the machine — useful for a
// locally-run server and for tests. Anything else is rejected.
func validateRemoteURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("url is not parseable: %v", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("url must be https for a non-loopback host (got %q) — MCP traffic must be encrypted in transit", raw)
	default:
		return fmt.Errorf("url must be http(s), got scheme %q", u.Scheme)
	}
}

// isLoopbackHost reports whether host is a loopback name/address.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// Validate enforces the server config policy and returns a single error listing every
// problem. Fails closed: any violation is an error, never a silent fix.
func (c ServerConfig) Validate() error {
	var problems []string
	if !serverNameRE.MatchString(c.Name) {
		problems = append(problems, fmt.Sprintf("name %q must be a non-empty lowercase label (a-z, 0-9, -; no leading/trailing -)", c.Name))
	}
	switch c.Transport {
	case TransportStdio:
		if strings.TrimSpace(c.Command) == "" {
			problems = append(problems, "stdio transport requires a command")
		}
		if c.URL != "" {
			problems = append(problems, "stdio transport must not set a url")
		}
	case TransportHTTP:
		if strings.TrimSpace(c.URL) == "" {
			problems = append(problems, "http transport requires a url")
		} else if err := validateRemoteURL(c.URL); err != nil {
			problems = append(problems, err.Error())
		}
		if c.Command != "" {
			problems = append(problems, "http transport must not set a command")
		}
	default:
		problems = append(problems, fmt.Sprintf("transport %q must be %q or %q", c.Transport, TransportStdio, TransportHTTP))
	}
	if len(problems) > 0 {
		return fmt.Errorf("mcp: invalid server %q: %s", c.Name, strings.Join(problems, "; "))
	}
	return nil
}

// IsLocal reports whether the server runs as a host subprocess (stdio).
func (c ServerConfig) IsLocal() bool { return c.Transport == TransportStdio }

// Public returns a copy safe to send to the browser: secret-bearing Env/Headers
// VALUES are masked (keys preserved) unless they are a plain ${VAR} reference, which
// is non-secret and kept so the operator can see which env var supplies the value.
func (c ServerConfig) Public() ServerConfig {
	c.Env = maskValues(c.Env)
	c.Headers = maskValues(c.Headers)
	return c
}

// MaskString is the placeholder Public() substitutes for a secret value. The API
// treats an incoming value equal to it as "unchanged", so editing a server without
// re-entering its secrets preserves them.
const MaskString = "••••"

// maskValues masks each value that is not a bare ${VAR} reference.
func maskValues(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if isEnvRef(v) {
			out[k] = v
		} else if v == "" {
			out[k] = ""
		} else {
			out[k] = MaskString
		}
	}
	return out
}

var envRefRE = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

func isEnvRef(v string) bool { return envRefRE.MatchString(strings.TrimSpace(v)) }

// expandEnv resolves ${VAR} references in a value map against the host environment.
// A reference to an unset variable resolves to empty. Non-reference values pass
// through unchanged. Used by the broker at connect time; never persisted.
func expandEnv(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = os.Expand(v, os.Getenv)
	}
	return out
}

// Catalog is the host-side store of configured MCP servers. With a path it persists
// to a 0600 JSON file (load on construct, save on mutate) so servers survive a
// restart; with an empty path it is memory-only. Safe for concurrent use.
type Catalog struct {
	mu     sync.RWMutex
	path   string
	byName map[string]ServerConfig
}

// NewCatalog constructs a catalog. A non-empty path is loaded if it exists (a missing
// file is fine — an empty catalog); a malformed file is an error so corruption is
// loud, not silently dropped.
func NewCatalog(path string) (*Catalog, error) {
	c := &Catalog{path: path, byName: map[string]ServerConfig{}}
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("mcp: read catalog %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return c, nil
	}
	var list []ServerConfig
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("mcp: parse catalog %s: %w", path, err)
	}
	for _, s := range list {
		c.byName[s.Name] = s
	}
	return c, nil
}

// Put validates and stores a server (insert or replace by name), persisting when a
// path is configured.
func (c *Catalog) Put(cfg ServerConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	c.byName[cfg.Name] = cfg
	err := c.save()
	c.mu.Unlock()
	return err
}

// Get returns a server by name.
func (c *Catalog) Get(name string) (ServerConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.byName[name]
	return s, ok
}

// Delete removes a server by name (idempotent), persisting the change.
func (c *Catalog) Delete(name string) error {
	c.mu.Lock()
	delete(c.byName, name)
	err := c.save()
	c.mu.Unlock()
	return err
}

// List returns all servers, ordered by name.
func (c *Catalog) List() []ServerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ServerConfig, 0, len(c.byName))
	for _, s := range c.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// save writes the catalog to its 0600 file atomically (temp + rename). Caller holds
// the write lock. A memory-only catalog (no path) is a no-op.
func (c *Catalog) save() error {
	if c.path == "" {
		return nil
	}
	list := make([]ServerConfig, 0, len(c.byName))
	for _, s := range c.byName {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp: marshal catalog: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("mcp: catalog dir: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("mcp: write catalog: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("mcp: persist catalog: %w", err)
	}
	return nil
}
