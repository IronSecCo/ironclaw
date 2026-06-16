// OWNER: AGENT1

// Package modelproxy is the host-side model egress proxy: it listens on a unix
// socket bound into the sandbox and forwards to the model API with a destination
// allowlist. It is the single outbound path — the sandbox has network=none.
//
// The proxy is also the sole authenticator: the sandbox holds no model
// credentials, so the proxy strips any inbound auth header and injects the
// host-held credential for the upstream. The key lives only on the host and
// never enters the sandbox image or its environment.
//
// Future work: per-token rate caps, request/response logging, secret redaction.
package modelproxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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
		host := r.Host
		if r.URL != nil && r.URL.Host != "" {
			host = r.URL.Host
		}
		if !p.allowedHost(host) {
			http.Error(w, "model-proxy: destination not on allowlist", http.StatusForbidden)
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
			Transport: p.transport,
		}
		rp.ServeHTTP(w, r)
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
