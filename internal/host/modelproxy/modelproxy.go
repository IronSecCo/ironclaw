// Package modelproxy is the host-side model egress proxy: it listens on a unix
// socket bound into the sandbox and forwards to the model API with a destination
// allowlist. It is the single outbound path — the sandbox has network=none.
//
// The proxy is also the sole authenticator: the sandbox holds no model
// credentials, so the proxy strips any inbound auth header and injects the
// host-held credential for the upstream. The key lives only on the host and
// never enters the sandbox image or its environment.
//
// Production hardening (see hardening.go) layers per-session/token rate caps,
// request/response audit records, and response secret redaction on top of the
// allowlist — all opt-in.
package modelproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Injector authenticates an outbound request to upstreamHost by setting the
// host-held credential headers on req. It runs after any sandbox-supplied auth
// has been stripped, so the proxy is the sole authenticator.
type Injector func(upstreamHost string, req *http.Request)

// Proxy forwards sandbox-originated requests to allowlisted upstream model hosts
// over a unix-domain socket. Any request to a host not on the allowlist is
// rejected with 403.
type Proxy struct {
	mu      sync.RWMutex
	allowed map[string]struct{}
	// transport is used for upstream calls; overridable in tests.
	transport http.RoundTripper
	// inject, if set, stamps the host-held credential onto each forwarded
	// request. nil means forward with no credential (e.g. local/dev upstreams).
	inject Injector

	// Hardening (all opt-in; see hardening.go). Nil/empty disables the feature.
	limiter  *keyedLimiter
	audit    AuditSink
	secrets  []string
	identify func(*http.Request) string
}

// Option configures a Proxy at construction.
type Option func(*Proxy)

// WithInjector sets the credential injector (the host-side authenticator).
func WithInjector(f Injector) Option { return func(p *Proxy) { p.inject = f } }

// WithTransport overrides the upstream RoundTripper (used in tests).
func WithTransport(rt http.RoundTripper) Option {
	return func(p *Proxy) {
		if rt != nil {
			p.transport = rt
		}
	}
}

// WithUpstreamGateway routes every forwarded upstream request through an HTTP
// CONNECT proxy — an operator-vetted credential gateway (e.g. OneCLI) — instead of
// dialing the upstream directly. The gateway injects the real provider credential,
// so the sandbox AND this control-plane stay credential-free for that provider
// (set no Injector for those hosts). proxyURL may embed Basic credentials in its
// userinfo (e.g. http://x:<agent-token>@127.0.0.1:10255); net/http sends them as
// the Proxy-Authorization header on CONNECT. When insecureTLS is set, upstream TLS
// verification is skipped — required when the gateway terminates TLS (MITM) with
// its own CA. Intended for a loopback gateway only.
func WithUpstreamGateway(proxyURL string, insecureTLS bool) Option {
	return func(p *Proxy) {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Host == "" {
			return
		}
		p.transport = &http.Transport{
			Proxy:             http.ProxyURL(u),
			ForceAttemptHTTP2: true,
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: insecureTLS}, //nolint:gosec // loopback credential-gateway MITM CA
		}
	}
}

// New constructs a Proxy whose allowlist is the given set of hostnames (host or
// host:port). Comparison is case-insensitive on the host portion.
func New(allowedHosts []string, opts ...Option) *Proxy {
	m := make(map[string]struct{}, len(allowedHosts))
	for _, h := range allowedHosts {
		m[strings.ToLower(h)] = struct{}{}
	}
	p := &Proxy{allowed: m, transport: http.DefaultTransport}
	for _, o := range opts {
		o(p)
	}
	return p
}

// AnthropicInjector returns an Injector that authenticates requests to the
// Anthropic Messages API with a host-held API key and API version. The key lives
// only on the host (e.g. from ANTHROPIC_API_KEY) and never enters the sandbox.
func AnthropicInjector(apiKey, version string) Injector {
	return func(upstreamHost string, req *http.Request) {
		if !strings.Contains(strings.ToLower(upstreamHost), "anthropic.com") {
			return
		}
		req.Header.Set("x-api-key", apiKey)
		if version != "" {
			req.Header.Set("anthropic-version", version)
		}
	}
}

// OpenAIInjector returns an Injector that authenticates requests to the OpenAI
// API with a host-held API key via the Bearer scheme. Like AnthropicInjector the
// key lives only on the host (e.g. from OPENAI_API_KEY) and never enters the
// sandbox. It self-guards on the upstream host so it no-ops for any other
// provider — safe to compose through MultiInjector.
func OpenAIInjector(apiKey string) Injector {
	return func(upstreamHost string, req *http.Request) {
		if !strings.Contains(strings.ToLower(upstreamHost), "openai.com") {
			return
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// OpenRouterInjector returns an Injector that authenticates requests to the
// OpenRouter API — an OpenAI-compatible multi-model gateway — with a host-held
// API key via the Bearer scheme (e.g. from OPENROUTER_API_KEY). It self-guards on
// the upstream host so it no-ops for any other provider.
func OpenRouterInjector(apiKey string) Injector {
	return func(upstreamHost string, req *http.Request) {
		if !strings.Contains(strings.ToLower(upstreamHost), "openrouter.ai") {
			return
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// MultiInjector composes several provider injectors into one. Each injector
// self-guards on the upstream host, so for any given request exactly the matching
// provider's credential is stamped and the rest no-op. This is how the proxy
// authenticates a multi-provider allowlist (per-agent-group provider selection)
// with a single Injector. nil injectors are skipped.
func MultiInjector(injectors ...Injector) Injector {
	return func(upstreamHost string, req *http.Request) {
		for _, in := range injectors {
			if in != nil {
				in(upstreamHost, req)
			}
		}
	}
}

// allowed reports whether host (as it appears in a request, possibly host:port)
// is on the allowlist. The bare hostname is also checked so an allowlist entry of
// "api.anthropic.com" matches "api.anthropic.com:443".
func (p *Proxy) allowedHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if _, ok := p.allowed[host]; ok {
		return true
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		if _, ok := p.allowed[h]; ok {
			return true
		}
	}
	return false
}

// Handler returns the http.Handler that enforces the allowlist and reverse-proxies
// allowed requests to their upstream over HTTPS. It is exported so it can be
// mounted in tests without a real socket.
func (p *Proxy) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		session := p.sessionKey(r)

		host := r.Host
		if r.URL != nil && r.URL.Host != "" {
			host = r.URL.Host
		}
		path := ""
		if r.URL != nil {
			path = r.URL.Path
		}

		if !p.allowedHost(host) {
			http.Error(w, "model-proxy: destination not on allowlist", http.StatusForbidden)
			p.emitAudit(AuditRecord{
				Time: start, Session: session, Method: r.Method, Host: host, Path: path,
				Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
				Duration: time.Since(start),
			})
			return
		}

		if p.limiter != nil && !p.limiter.allow(session) {
			http.Error(w, "model-proxy: egress rate limit exceeded", http.StatusTooManyRequests)
			p.emitAudit(AuditRecord{
				Time: start, Session: session, Method: r.Method, Host: host, Path: path,
				Status: http.StatusTooManyRequests, Allowed: true, RateLimited: true,
				RequestBytes: r.ContentLength, Duration: time.Since(start),
			})
			return
		}

		target := &url.URL{Scheme: "https", Host: hostOnly(host)}
		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
				// The sandbox holds no model credentials and must not be able to
				// present its own. Strip any inbound auth, then inject the
				// host-held credential for this upstream.
				req.Header.Del("Authorization")
				req.Header.Del("X-Api-Key")
				if p.inject != nil {
					p.inject(target.Host, req)
				}
			},
			Transport:      p.transport,
			ModifyResponse: p.modifyResponse,
		}

		rec := &countingRW{ResponseWriter: w}
		rp.ServeHTTP(rec, r)
		p.emitAudit(AuditRecord{
			Time: start, Session: session, Method: r.Method, Host: host, Path: path,
			Status: rec.status, Allowed: true, RequestBytes: r.ContentLength,
			ResponseBytes: rec.n, Duration: time.Since(start),
		})
	})
}

// hostOnly strips a :port from host if present (the scheme implies the port).
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// Serve listens on socketPath (a unix-domain socket bound into the sandbox) and
// serves the allowlist-enforcing handler until ctx is cancelled. The socket file
// is removed on start (stale cleanup) and on stop.
func (p *Proxy) Serve(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		return errors.New("host/modelproxy: empty socket path")
	}
	_ = os.Remove(socketPath)
	// Ensure the socket's parent dir exists before binding. In production the
	// installer/systemd provisions it; in --dev it usually does not, so create it
	// here (0700) rather than failing the whole control-plane with a bind error.
	if dir := filepath.Dir(socketPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: p.Handler()}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = os.Remove(socketPath)
		return ctx.Err()
	case err := <-errCh:
		_ = os.Remove(socketPath)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
