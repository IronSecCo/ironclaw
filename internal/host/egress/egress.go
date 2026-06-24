// Package egress is the host-side broker for sandbox-originated calls to approved
// EXTERNAL APIs beyond the model host. Like the model proxy, it listens on
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
//     change clears the gateway's human approval (wired by the daemon).
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
	// CorrelationID joins this record to the vault injector's audit for a vaulted
	// request; VaultCredential is the logical credential name (never a key). Both are
	// empty for ordinary egress.
	CorrelationID   string `json:"correlationId,omitempty"`
	VaultCredential string `json:"vaultCredential,omitempty"`
}

// AuditSink receives one AuditRecord per request. Implementations must be safe for
// concurrent use.
type AuditSink func(AuditRecord)

// Broker forwards sandbox-originated requests to allowlisted external hosts over a
// unix-domain socket. A request to a host not on the allowlist is rejected 403.
type Broker struct {
	mu         sync.RWMutex
	allowed    map[string]struct{}
	transport  http.RoundTripper
	audit      AuditSink
	identify   func(*http.Request) string
	vault      *Vault      // optional vault:// routing to a host-local injector
	correlator *Correlator // optional broker<->vault audit correlation
	redactor   *Redactor   // optional broker->sandbox response redaction
	guard      VaultGuard  // optional per-group vault policy enforcement (trusted session)

	socksMu sync.Mutex
	socks   map[string]*sessionSock // per-session sockets (host-trusted session identity)
}

// VaultGuard authorizes a vault-addressed request for a HOST-TRUSTED session, after
// Forward has resolved the logical credential name. It returns the upstream host the
// credential targets (for audit) and whether the session's agent group is granted
// that credential against that host. Deny-by-default: ok is false on any miss
// (unknown session, group, credential, host, or un-granted policy).
//
// Host-side it closes over the session->group registry plus the gateway-approved
// VaultPolicyStore (registry.VaultPolicyStore.Allows), so a revoked grant stops
// working immediately. The session passed in is the one the broker TRUSTS — the
// per-session socket's fixed identity, never the spoofable X-Ironclaw-Session header.
type VaultGuard func(session, credential string) (upstreamHost string, ok bool)

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

// WithVault enables vault:// routing: a vault-addressed request is forwarded by NAME
// to the configured host-local injector (a SEPARATE principal that holds the
// credential), the broker injecting no secret of its own. The injector
// endpoint is still subject to the allowlist, so it must be Allow'd.
func WithVault(v *Vault) Option {
	return func(b *Broker) {
		if v != nil && v.Configured() {
			b.vault = v
		}
	}
}

// WithCorrelator stamps a host-generated correlation id on each broker->vault
// request and records it in the audit, joining the broker and injector audit trails
// . Only meaningful alongside WithVault.
func WithCorrelator(c *Correlator) Option {
	return func(b *Broker) { b.correlator = c }
}

// WithResponseRedactor scrubs configured secrets (and credential-bearing headers)
// from responses on the broker->sandbox hop, so an injected credential can never
// echo back (the model-proxy pattern).
func WithResponseRedactor(rd *Redactor) Option {
	return func(b *Broker) { b.redactor = rd }
}

// WithVaultGuard enables per-group vault policy enforcement. When set, a vault://
// request is permitted only over a PER-SESSION socket (SocketForSession) whose
// identity the host created — never the shared Handler socket, where the session is
// only the spoofable header — and only after the guard authorizes the trusted
// session's group for the credential. Deny-by-default: an un-granted or un-wired
// vault is refused 403. Only meaningful alongside WithVault.
func WithVaultGuard(g VaultGuard) Option {
	return func(b *Broker) {
		if g != nil {
			b.guard = g
		}
	}
}

// SetVaultGuard wires the vault guard after construction (the policy store and the
// session registry are typically assembled after the broker). Idempotent; a nil
// guard is ignored so enforcement is never silently dropped.
func (b *Broker) SetVaultGuard(g VaultGuard) {
	if g != nil {
		b.guard = g
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
		socks:     map[string]*sessionSock{},
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
// tests without a real socket. This is the SHARED socket: the session is read from
// the advisory header, so it is UNTRUSTED — vault enforcement (WithVaultGuard) refuses
// vault addressing here and only honors it on a per-session socket.
func (b *Broker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.serve(w, r, b.identify(r), false)
	})
}

// serve enforces vault policy + the allowlist for one request. session is the id the
// broker attributes the call to; trusted reports whether that id is host-established
// (a per-session socket) rather than the advisory header. Vault addressing is honored
// only on a trusted session when a guard is wired (the spoof defense).
func (b *Broker) serve(w http.ResponseWriter, r *http.Request, session string, trusted bool) {
	start := time.Now()

	// Vault routing: a vault://<cred>/<path> request is rewritten to the
	// configured host-local injector — a SEPARATE principal that holds the
	// credential. Forward injects no secret (it strips Authorization and tags only
	// the credential NAME); a correlation id joins this hop to the injector's own
	// audit. After this, the request targets the injector, which is itself subject
	// to the allowlist below (deny-by-default like any host).
	var correlationID, vaultCred string
	if b.vault != nil {
		scheme := ""
		if r.URL != nil {
			scheme = r.URL.Scheme
		}
		if IsVaultAddressed(r.Host, scheme) {
			// Enforcement requires a host-trusted session. On the shared (untrusted)
			// socket a vault address is refused outright: a compromised sandbox cannot
			// reach a credential by spoofing X-Ironclaw-Session — vault is reachable
			// only over the per-session socket whose identity the host created.
			if b.guard != nil && !trusted {
				http.Error(w, "egress: vault requires a per-session binding", http.StatusForbidden)
				b.emitAudit(AuditRecord{
					Time: start, Session: session, Method: r.Method, Host: hostLabel(r.Host), Path: pathOf(r),
					Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
					Duration: time.Since(start),
				})
				return
			}
			cred, err := b.vault.Forward(r)
			if err != nil {
				http.Error(w, "egress: "+err.Error(), http.StatusForbidden)
				b.emitAudit(AuditRecord{
					Time: start, Session: session, Method: r.Method, Host: hostLabel(r.Host), Path: pathOf(r),
					Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
					Duration: time.Since(start),
				})
				return
			}
			vaultCred = cred
			// Per-group policy, keyed on the host-TRUSTED session->group mapping: the
			// group must be granted this credential against its upstream host. Deny-by-
			// default — an un-granted credential is refused 403, audited with the cred
			// NAME (never a key).
			if b.guard != nil {
				upstreamHost, ok := b.guard(session, cred)
				if !ok {
					http.Error(w, "egress: vault credential not granted for this agent", http.StatusForbidden)
					b.emitAudit(AuditRecord{
						Time: start, Session: session, Method: r.Method, Host: vaultHostFallback(upstreamHost, r.Host), Path: pathOf(r),
						Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
						Duration: time.Since(start), VaultCredential: vaultCred,
					})
					return
				}
			}
			if b.correlator != nil {
				if id, e := b.correlator.Stamp(r); e == nil {
					correlationID = id
				}
			}
		}
	}

	host := r.Host
	if r.URL != nil && r.URL.Host != "" {
		host = r.URL.Host
	}
	path := pathOf(r)

	if !b.Allowed(host) {
		http.Error(w, "egress: destination not on allowlist", http.StatusForbidden)
		b.emitAudit(AuditRecord{
			Time: start, Session: session, Method: r.Method, Host: host, Path: path,
			Status: http.StatusForbidden, Allowed: false, RequestBytes: r.ContentLength,
			Duration: time.Since(start), CorrelationID: correlationID, VaultCredential: vaultCred,
		})
		return
	}

	// Upstream target: external hosts are dialed over HTTPS with the port implied
	// (the allowlist is bare-host). The host-local vault injector keeps the exact
	// scheme AND host:port Forward set on it (it may be plain http on a loopback
	// port).
	scheme, targetHost := "https", hostOnly(host)
	if vaultCred != "" && r.URL != nil {
		scheme, targetHost = r.URL.Scheme, r.URL.Host
	}
	target := &url.URL{Scheme: scheme, Host: targetHost}
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
	if b.redactor != nil {
		// Scrub configured secrets + credential headers from the response before
		// it reaches the sandbox.
		rp.ModifyResponse = b.redactor.Redact
	}

	rec := &countingRW{ResponseWriter: w}
	rp.ServeHTTP(rec, r)
	b.emitAudit(AuditRecord{
		Time: start, Session: session, Method: r.Method, Host: host, Path: path,
		Status: rec.status, Allowed: true, RequestBytes: r.ContentLength,
		ResponseBytes: rec.n, Duration: time.Since(start),
		CorrelationID: correlationID, VaultCredential: vaultCred,
	})
}

// vaultHostFallback returns the upstream host for audit on a vault denial: the guard's
// resolved host when known, else the request's labelled host (e.g. "vault").
func vaultHostFallback(upstreamHost, reqHost string) string {
	if upstreamHost != "" {
		return upstreamHost
	}
	return hostLabel(reqHost)
}

// pathOf safely reads the request path.
func pathOf(r *http.Request) string {
	if r.URL != nil {
		return r.URL.Path
	}
	return ""
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
