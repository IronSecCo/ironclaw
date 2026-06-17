// OWNER: AGENT1

// Package egress is the host-side broker for sandbox-originated calls to approved
// EXTERNAL APIs beyond the model host (T-111). Like the model proxy, it listens on
// a unix socket bound into the sandbox and forwards over HTTPS — but its allowlist
// is a deny-by-default set of arbitrary external hosts an operator has explicitly
// approved.
//
// The sandbox stays network=none: it never gets a NIC. Its ONLY egress paths are
// these two host-mediated unix sockets (model proxy + egress broker), each with
// its own allowlist and per-request audit. Opening egress therefore widens what
// an agent can reach, but it does NOT give the sandbox a network stack — every
// byte still crosses a host choke point.
//
// Threat model (see docs/threat-model.md "Egress broker"):
//   - Deny by default. An empty allowlist forwards nothing; a host must be
//     explicitly approved. In production the allowlist is mutated only after the
//     change clears the gateway's human approval (wired by the daemon, T-120).
//   - Every request — allowed or denied — is audited (host, path, status, bytes).
//   - HTTPS only. The broker always dials the upstream over TLS so traffic to an
//     external host is never sent in cleartext from the host.
//   - NOT a credential vault (an explicit IronClaw non-goal): the broker injects
//     no host-held secrets and forwards the request's own headers, so it cannot
//     launder access to a credential the sandbox does not already hold.
package egress

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// AuditRecord is one egress decision/forward, emitted to an AuditSink. It mirrors
// the model proxy's audit shape so a single log pipeline can consume both.
type AuditRecord struct {
	Time          time.Time     `json:"time"`
	Session       string        `json:"session,omitempty"`
	Method        string        `json:"method"`
	Host          string        `json:"host"`
	Path          string        `json:"path"`
	Status        int           `json:"status"`
	Allowed       bool          `json:"allowed"`
	RequestBytes  int64         `json:"requestBytes"`
	ResponseBytes int64         `json:"responseBytes"`
	Duration      time.Duration `json:"durationNanos"`
}

// AuditSink receives one AuditRecord per request. Implementations must be safe for
// concurrent use.
type AuditSink func(AuditRecord)

// Broker forwards sandbox-originated requests to allowlisted external hosts over a
// unix-domain socket. A request to a host not on the allowlist is rejected 403.
type Broker struct {
	mu        sync.RWMutex
	allowed   map[string]struct{}
	transport http.RoundTripper
	audit     AuditSink
	identify  func(*http.Request) string
}

// Option configures a Broker at construction.
type Option func(*Broker)

// WithTransport overrides the upstream RoundTripper (used in tests).
func WithTransport(rt http.RoundTripper) Option {
	return func(b *Broker) {
		if rt != nil {
			b.transport = rt
		}
	}
}

// WithAudit sets the audit sink that receives one record per request.
func WithAudit(sink AuditSink) Option {
	return func(b *Broker) { b.audit = sink }
}

// WithSessionIdentifier sets how a request is mapped to a session id for audit
// (e.g. reading a header). The default reads the X-Ironclaw-Session header.
func WithSessionIdentifier(f func(*http.Request) string) Option {
	return func(b *Broker) {
		if f != nil {
			b.identify = f
		}
	}
}

// sessionHeader is the request header the sandbox may set to attribute an egress
// call to its session in the audit log. It is advisory (audit only) and never a
// security control.
const sessionHeader = "X-Ironclaw-Session"

// New constructs a deny-by-default Broker seeded with the given approved hosts
// (host or host:port, case-insensitive). Pass nil/empty for a fully-sealed broker
// whose allowlist is populated later via Allow (e.g. after gateway approval).
func New(approvedHosts []string, opts ...Option) *Broker {
	m := make(map[string]struct{}, len(approvedHosts))
	for _, h := range approvedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			m[h] = struct{}{}
		}
	}
	b := &Broker{
		allowed:   m,
		transport: http.DefaultTransport,
		identify:  func(r *http.Request) string { return r.Header.Get(sessionHeader) },
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// Allow adds host to the allowlist (idempotent). In production this is called by
// the daemon AFTER the change has cleared the gateway's human approval. The
// addition is logged so the allowlist's evolution is itself auditable.
func (b *Broker) Allow(host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	b.mu.Lock()
	b.allowed[host] = struct{}{}
	b.mu.Unlock()
	log.Printf("host/egress: allowlist + %q", host)
}

// Deny removes host from the allowlist (idempotent). Logged.
func (b *Broker) Deny(host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	b.mu.Lock()
	delete(b.allowed, host)
	b.mu.Unlock()
	log.Printf("host/egress: allowlist - %q", host)
}

// Allowed reports whether host (as it appears in a request, possibly host:port) is
// approved. A bare-host allowlist entry matches a host:port request.
func (b *Broker) Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if _, ok := b.allowed[host]; ok {
		return true
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		if _, ok := b.allowed[h]; ok {
			return true
		}
	}
	return false
}

// Handler returns the http.Handler that enforces the allowlist and reverse-proxies
// allowed requests to their upstream over HTTPS. Exported so it can be mounted in
// tests without a real socket.
func (b *Broker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		session := b.identify(r)

		host := r.Host
		if r.URL != nil && r.URL.Host != "" {
			host = r.URL.Host
		}
		path := ""
		if r.URL != nil {
			path = r.URL.Path
		}

		if !b.Allowed(host) {
			http.Error(w, "egress: destination not on allowlist", http.StatusForbidden)
			b.emitAudit(AuditRecord{
				Time: start, Session: session, Method: r.Method, Host: host, Path: path,
				Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
				Duration: time.Since(start),
			})
			return
		}

		target := &url.URL{Scheme: "https", Host: hostOnly(host)}
		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = target.Scheme
				req.URL.Host = target.Host
				req.Host = target.Host
				// The audit-only session header is host-internal; never forward it
				// to the external API.
				req.Header.Del(sessionHeader)
			},
			Transport: b.transport,
		}

		rec := &countingRW{ResponseWriter: w}
		rp.ServeHTTP(rec, r)
		b.emitAudit(AuditRecord{
			Time: start, Session: session, Method: r.Method, Host: host, Path: path,
			Status: rec.status, Allowed: true, RequestBytes: r.ContentLength,
			ResponseBytes: rec.n, Duration: time.Since(start),
		})
	})
}

// emitAudit delivers a record to the sink if one is configured.
func (b *Broker) emitAudit(rec AuditRecord) {
	if b.audit != nil {
		b.audit(rec)
	}
}

// hostOnly strips a :port from host if present (the https scheme implies 443).
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// countingRW wraps a ResponseWriter to capture the status code and response byte
// count for the audit record.
type countingRW struct {
	http.ResponseWriter
	status int
	n      int64
}

func (c *countingRW) WriteHeader(status int) {
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *countingRW) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.ResponseWriter.Write(p)
	c.n += int64(n)
	return n, err
}

// Serve listens on socketPath (a unix-domain socket bound into the sandbox) and
// serves the allowlist-enforcing handler until ctx is cancelled. The socket file
// is removed on start (stale cleanup) and on stop.
func (b *Broker) Serve(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		return errors.New("host/egress: empty socket path")
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: b.Handler()}

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
