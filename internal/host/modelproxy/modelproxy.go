// OWNER: AGENT1

// Package modelproxy is the host-side model egress proxy: it listens on a unix
// socket bound into the sandbox and forwards to the model API with a destination
// allowlist. It is the single outbound path — the sandbox has network=none.
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

// Proxy forwards sandbox-originated requests to allowlisted upstream model hosts
// over a unix-domain socket. Any request to a host not on the allowlist is
// rejected with 403.
type Proxy struct {
	mu      sync.RWMutex
	allowed map[string]struct{}
	// transport is used for upstream calls; overridable in tests.
	transport http.RoundTripper
}

// New constructs a Proxy whose allowlist is the given set of hostnames (host or
// host:port). Comparison is case-insensitive on the host portion.
func New(allowedHosts []string) *Proxy {
	m := make(map[string]struct{}, len(allowedHosts))
	for _, h := range allowedHosts {
		m[strings.ToLower(h)] = struct{}{}
	}
	return &Proxy{allowed: m, transport: http.DefaultTransport}
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
