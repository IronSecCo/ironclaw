// Package vaultinjector is IronClaw's minimal in-tree reference credential injector:
// the SEPARATE host-side principal the egress broker forwards a vault:// request TO.
// It is the sole holder of a credential and the only component that attaches it,
// host-side — the broker injects nothing and the sandbox never sees a key (threat
// model §11).
//
// Contract with the broker (internal/host/egress/vault.go):
//   - The broker rewrites a vault://<cred>/<path> request to target this injector,
//     sets X-Ironclaw-Vault-Cred: <cred> (the logical NAME, never a key), strips any
//     sandbox-supplied Authorization, and stamps X-Ironclaw-Correlation for audit.
//   - The injector maps <cred> to its configured upstream + secret, attaches the
//     secret host-side, and reverse-proxies to the real upstream over its own scheme.
//   - An unknown credential is refused 403 (deny-by-default). The secret is read from
//     the host environment, NEVER from the config file, and is never echoed back.
//
// An operator may instead point the broker at an external injector (e.g. OneCLI) that
// honours the same contract; this package is the default, swappable reference.
package vaultinjector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Headers the injector reads, matching the broker's vault forwarding.
const (
	// CredHeader carries the logical credential name from the broker. Mirrors
	// egress.VaultCredHeader (kept local so this package does not import egress).
	CredHeader = "X-Ironclaw-Vault-Cred"
	// CorrelationHeader joins the injector's audit to the broker's. Mirrors
	// egress.CorrelationHeader.
	CorrelationHeader = "X-Ironclaw-Vault-Request-Id"
)

// CredSpec configures one logical credential. The secret itself is NEVER in the file:
// SecretEnv names the host environment variable that holds it.
type CredSpec struct {
	// Upstream is the real API base URL the credential is used against
	// (e.g. "https://api.github.com"). Its host is what the control-plane's vault
	// policy is enforced against.
	Upstream string `json:"upstream"`
	// SecretEnv is the host env var holding the secret value (e.g. "VAULT_GITHUB_TOKEN").
	SecretEnv string `json:"secretEnv"`
	// Header is the request header the secret is attached to. Default "Authorization".
	Header string `json:"header,omitempty"`
	// Scheme is the value prefix (e.g. "Bearer "). Default "Bearer ".
	Scheme string `json:"scheme,omitempty"`
}

// Config is the injector's credential catalog: logical name -> spec.
type Config struct {
	Creds map[string]CredSpec `json:"creds"`
}

// LoadConfig reads and validates a JSON injector config from path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vaultinjector: read config %q: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("vaultinjector: parse config %q: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	for name, spec := range c.Creds {
		if !validCredName(name) {
			return fmt.Errorf("vaultinjector: invalid credential name %q", name)
		}
		u, err := url.Parse(strings.TrimSpace(spec.Upstream))
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("vaultinjector: credential %q has invalid upstream %q", name, spec.Upstream)
		}
		if strings.TrimSpace(spec.SecretEnv) == "" {
			return fmt.Errorf("vaultinjector: credential %q must name a secretEnv", name)
		}
	}
	return nil
}

// UpstreamHost returns the bare upstream host a credential targets, for the
// control-plane's vault-policy enforcement (the host dimension of
// VaultPolicyStore.Allows). This is the one config fact the broker side shares with
// the injector: the policy maps (group, cred) -> approved upstream host.
func (c *Config) UpstreamHost(cred string) (string, bool) {
	spec, ok := c.Creds[strings.ToLower(strings.TrimSpace(cred))]
	if !ok {
		return "", false
	}
	u, err := url.Parse(spec.Upstream)
	if err != nil || u.Host == "" {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

// CredHosts returns the cred -> upstream-host map, a convenience for wiring the
// broker's VaultGuard host resolution.
func (c *Config) CredHosts() map[string]string {
	out := make(map[string]string, len(c.Creds))
	for name := range c.Creds {
		if h, ok := c.UpstreamHost(name); ok {
			out[strings.ToLower(name)] = h
		}
	}
	return out
}

// AuditRecord is one injection or rotation decision, emitted to an AuditSink. It
// carries the credential NAME and correlation id — never the secret value.
type AuditRecord struct {
	Time          time.Time     `json:"time"`
	Action        string        `json:"action,omitempty"` // "inject" (default) or "rotate"
	Credential    string        `json:"credential"`
	CorrelationID string        `json:"correlationId,omitempty"`
	Upstream      string        `json:"upstream,omitempty"`
	Path          string        `json:"path"`
	Status        int           `json:"status"`
	Allowed       bool          `json:"allowed"`
	Duration      time.Duration `json:"durationNanos"`
}

// AuditSink receives one record per injection. Must be safe for concurrent use.
type AuditSink func(AuditRecord)

// resolved is one credential ready to attach: upstream + the secret value pulled from
// the environment at construction. secretEnv is retained so Rotate can re-resolve the
// secret from the same host env var the credential was configured against.
type resolved struct {
	upstream  *url.URL
	secret    string
	secretEnv string
	header    string
	scheme    string
}

// Injector attaches host-held credentials to broker-forwarded requests. It holds the
// secret values resolved from the environment; the config file never does. The held
// secrets are guarded by mu so a Rotate can swap one atomically while Handler reads it.
type Injector struct {
	mu        sync.RWMutex
	creds     map[string]*resolved
	lookupEnv func(string) (string, bool)
	transport http.RoundTripper
	audit     AuditSink
}

// Option configures an Injector.
type Option func(*Injector)

// WithTransport overrides the upstream RoundTripper (tests). WithAudit sets the sink.
func WithTransport(rt http.RoundTripper) Option {
	return func(i *Injector) {
		if rt != nil {
			i.transport = rt
		}
	}
}
func WithAudit(sink AuditSink) Option { return func(i *Injector) { i.audit = sink } }

// New builds an Injector from cfg, resolving each credential's secret via lookupEnv
// (default os.LookupEnv). A credential whose secret env var is unset is a
// configuration error: the injector fails closed rather than serving a credential it
// cannot attach.
func New(cfg *Config, lookupEnv func(string) (string, bool), opts ...Option) (*Injector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("vaultinjector: nil config")
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	i := &Injector{creds: make(map[string]*resolved, len(cfg.Creds)), lookupEnv: lookupEnv, transport: http.DefaultTransport}
	for name, spec := range cfg.Creds {
		u, err := url.Parse(strings.TrimSpace(spec.Upstream))
		if err != nil {
			return nil, fmt.Errorf("vaultinjector: credential %q upstream: %w", name, err)
		}
		secret, ok := lookupEnv(spec.SecretEnv)
		if !ok || secret == "" {
			return nil, fmt.Errorf("vaultinjector: credential %q secret env %q is unset", name, spec.SecretEnv)
		}
		header := spec.Header
		if strings.TrimSpace(header) == "" {
			header = "Authorization"
		}
		scheme := spec.Scheme
		if scheme == "" {
			scheme = "Bearer "
		}
		i.creds[strings.ToLower(name)] = &resolved{upstream: u, secret: secret, secretEnv: spec.SecretEnv, header: header, scheme: scheme}
	}
	for _, o := range opts {
		o(i)
	}
	return i, nil
}

// Creds returns the configured credential names (sorted), for diagnostics. Never
// returns secrets.
func (i *Injector) Creds() []string {
	out := make([]string, 0, len(i.creds))
	for name := range i.creds {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Rotate re-resolves a credential's secret from its configured secretEnv via the
// injector's env lookup and atomically swaps the held value. This is the injector's
// half of the rotation contract (IRO-144): the control plane signals a rotation only
// AFTER a human approves it, and the new secret is read from the host environment
// here — it NEVER travels through the control plane, the change body, or this call.
// An unknown credential, or a secret env that is now unset/empty, returns an error
// and KEEPS the old secret, so a botched rotation fails closed rather than blanking a
// live credential. The secret value is never returned or logged.
func (i *Injector) Rotate(cred string) error {
	cred = strings.ToLower(strings.TrimSpace(cred))
	i.mu.RLock()
	res, ok := i.creds[cred]
	env := ""
	if ok {
		env = res.secretEnv
	}
	i.mu.RUnlock()
	if cred == "" || !ok {
		return fmt.Errorf("vaultinjector: unknown credential %q", cred)
	}
	secret, found := i.lookupEnv(env)
	if !found || secret == "" {
		return fmt.Errorf("vaultinjector: credential %q secret env %q is unset", cred, env)
	}
	i.mu.Lock()
	i.creds[cred].secret = secret
	i.mu.Unlock()
	return nil
}

// RotateHandler serves the injector's CONTROL surface: a request that re-resolves a
// credential's held secret from the host environment. It is SEPARATE from Handler
// (the broker-facing proxy) and MUST be bound to a channel only the control plane can
// reach — its own loopback addr / unix socket, never the broker's — so the sandbox can
// never trigger a rotation. The request carries the credential NAME in CredHeader
// (never a secret) and returns no secret: 200 on a successful rotation, 403 for an
// unknown credential, 502 when the new secret cannot be resolved. Every outcome is
// audited (name + correlation only).
func (i *Injector) RotateHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cred := strings.ToLower(strings.TrimSpace(r.Header.Get(CredHeader)))
		corr := r.Header.Get(CorrelationHeader)
		err := i.Rotate(cred)
		status := http.StatusOK
		switch {
		case err == nil:
		case strings.Contains(err.Error(), "unknown credential"):
			status = http.StatusForbidden
		default:
			status = http.StatusBadGateway
		}
		i.emit(AuditRecord{Time: start, Action: "rotate", Credential: cred, CorrelationID: corr, Path: "/rotate", Status: status, Allowed: err == nil, Duration: time.Since(start)})
		if err != nil {
			http.Error(w, "vaultinjector: rotate failed", status)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rotated\n"))
	})
}

// Handler serves the injector: it reads the credential name, attaches the host-held
// secret, and reverse-proxies to the real upstream. Deny-by-default: an unknown or
// missing credential is refused 403. The secret is attached only on the upstream hop
// and never written to the response.
func (i *Injector) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		cred := strings.ToLower(strings.TrimSpace(r.Header.Get(CredHeader)))
		corr := r.Header.Get(CorrelationHeader)
		path := "/"
		if r.URL != nil {
			path = r.URL.Path
		}
		// Snapshot the credential under the read lock so a concurrent Rotate cannot
		// swap the secret mid-request; the proxy below uses these copies.
		i.mu.RLock()
		res, ok := i.creds[cred]
		var target url.URL
		var secret, header, scheme string
		if ok {
			target = *res.upstream
			secret = res.secret
			header = res.header
			scheme = res.scheme
		}
		i.mu.RUnlock()
		if cred == "" || !ok {
			http.Error(w, "vaultinjector: unknown credential", http.StatusForbidden)
			i.emit(AuditRecord{Time: start, Action: "inject", Credential: cred, CorrelationID: corr, Path: path, Status: http.StatusForbidden, Allowed: false, Duration: time.Since(start)})
			return
		}
		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
				// Preserve the upstream's own base path if it has one, then the request path.
				req.URL.Path = singleJoin(target.Path, path)
				// The credential name + correlation id are host-internal; do not leak
				// them upstream.
				req.Header.Del(CredHeader)
				req.Header.Del(CorrelationHeader)
				// Attach the host-held credential — the ONE place a real key is added.
				req.Header.Set(header, scheme+secret)
			},
			Transport: i.transport,
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		rp.ServeHTTP(rec, r)
		i.emit(AuditRecord{Time: start, Action: "inject", Credential: cred, CorrelationID: corr, Upstream: target.Host, Path: path, Status: rec.status, Allowed: true, Duration: time.Since(start)})
	})
}

func (i *Injector) emit(rec AuditRecord) {
	if i.audit != nil {
		i.audit(rec)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// singleJoin joins a base path and a request path with exactly one slash.
func singleJoin(base, p string) string {
	base = strings.TrimSuffix(base, "/")
	if p == "" {
		if base == "" {
			return "/"
		}
		return base
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// validCredName mirrors the egress broker's credential-name rule.
func validCredName(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
