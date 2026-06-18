package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// connectTimeout bounds connecting + handshaking + listing tools for one upstream
// server, so a hung server cannot stall a sandbox's tool registration.
const connectTimeout = 30 * time.Second

// callTimeout bounds a single tools/call end to end.
const callTimeout = 60 * time.Second

// nsSep separates the server name from the tool name in the namespaced tool the agent
// sees (e.g. "github__create_issue"). Server names cannot contain "_" (see
// serverNameRE), so the first occurrence is unambiguously the boundary.
const nsSep = "__"

// Grant is one session's approved access to a server: the server name plus the named
// subset of its tools (empty Tools = all the server's currently-declared tools). It
// mirrors registry.GrantedMCP but is defined here so the broker does not import the
// registry package.
type Grant struct {
	Server string
	Tools  []string
}

// GrantResolver returns the approved grants for a session id. It is the broker's link
// to the gateway's decisions: host-side it is a closure over the registry (session →
// agent group → GrantedMCP), so the per-call surface is always the CURRENTLY-approved
// one — a revoked grant stops working immediately.
type GrantResolver func(session string) []Grant

// AuditRecord is one MCP operation (a tool listing or a tool call), emitted to an
// AuditSink so every MCP interaction is logged through the same governed pipeline as
// the gateway and the egress broker. It never carries argument/result bodies or
// credential values — only metadata.
type AuditRecord struct {
	Time     time.Time     `json:"time"`
	Session  string        `json:"session,omitempty"`
	Server   string        `json:"server,omitempty"`
	Tool     string        `json:"tool,omitempty"`
	Op       string        `json:"op"` // "list" | "call"
	Allowed  bool          `json:"allowed"`
	Status   string        `json:"status"` // "ok" | "denied" | "error"
	Bytes    int           `json:"bytes"`
	Duration time.Duration `json:"durationNanos"`
	Error    string        `json:"error,omitempty"`
}

// AuditSink receives one record per MCP operation. Must be safe for concurrent use.
type AuditSink func(AuditRecord)

// Broker is the host-side MCP choke point. The sandbox never speaks MCP and never
// reaches an MCP server directly: it talks only to a PER-SESSION unix socket the
// broker serves (so the session identity is the host-created socket, not a spoofable
// header). For every request the broker resolves the session's gateway-approved
// grants, enforces them deny-by-default, forwards approved calls to the upstream
// server (a hardened local subprocess or a TLS remote endpoint), and audits the
// outcome. Credentials are expanded host-side and never cross to the sandbox.
type Broker struct {
	baseCtx  context.Context
	catalog  *Catalog
	grants   GrantResolver
	launcher Launcher
	audit    AuditSink
	client   *http.Client

	mu    sync.Mutex
	conns map[string]*conn
	socks map[string]*sessionSock
}

// Option configures a Broker.
type Option func(*Broker)

// WithLauncher sets how local (stdio) servers are run. Default DirectLauncher
// (UNISOLATED) — production should pass a ContainerLauncher.
func WithLauncher(l Launcher) Option {
	return func(b *Broker) {
		if l != nil {
			b.launcher = l
		}
	}
}

// WithAudit sets the sink that receives one record per MCP operation.
func WithAudit(sink AuditSink) Option { return func(b *Broker) { b.audit = sink } }

// WithHTTPClient overrides the HTTP client used for remote servers (tests).
func WithHTTPClient(c *http.Client) Option {
	return func(b *Broker) {
		if c != nil {
			b.client = c
		}
	}
}

// New constructs a Broker. baseCtx governs spawned local servers' lifetime (cancel it,
// or call Close, to tear them down).
func New(baseCtx context.Context, catalog *Catalog, grants GrantResolver, opts ...Option) *Broker {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	b := &Broker{
		baseCtx:  baseCtx,
		catalog:  catalog,
		grants:   grants,
		launcher: DirectLauncher{},
		client:   &http.Client{Timeout: callTimeout},
		conns:    map[string]*conn{},
		socks:    map[string]*sessionSock{},
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// conn is a lazily-established upstream connection to one server, shared across
// sessions (per-session access is gated separately). tools is the server's declared
// tool list, cached at connect for the membership checks and the /tools surface.
type conn struct {
	cfg   ServerConfig
	mu    sync.Mutex
	cli   *Client
	tools []Tool
	ready bool
}

// sessionSock is a per-session serving socket and its listener.
type sessionSock struct {
	path string
	ln   net.Listener
	srv  *http.Server
}

// emit delivers an audit record if a sink is configured.
func (b *Broker) emit(r AuditRecord) {
	if b.audit != nil {
		if r.Time.IsZero() {
			r.Time = time.Now().UTC()
		}
		b.audit(r)
	}
}

// ensure returns a connected upstream client for server, connecting + handshaking +
// listing tools on first use. A connection failure is returned (not cached as
// permanent) so a later call can retry once the server is reachable.
func (b *Broker) ensure(server string) (*conn, error) {
	b.mu.Lock()
	c := b.conns[server]
	if c == nil {
		cfg, found := b.catalog.Get(server)
		if !found {
			b.mu.Unlock()
			return nil, fmt.Errorf("server %q is not configured", server)
		}
		c = &conn{cfg: cfg}
		b.conns[server] = c
	}
	b.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ready {
		return c, nil
	}
	cli, err := b.dial(c.cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(b.baseCtx, connectTimeout)
	defer cancel()
	if _, err := cli.Initialize(ctx); err != nil {
		cli.Close()
		return nil, err
	}
	tools, err := cli.ListTools(ctx)
	if err != nil {
		cli.Close()
		return nil, err
	}
	c.cli, c.tools, c.ready = cli, tools, true
	return c, nil
}

// dial opens the right transport for a server config (secrets expanded host-side).
func (b *Broker) dial(cfg ServerConfig) (*Client, error) {
	switch cfg.Transport {
	case TransportStdio:
		return DialStdio(b.baseCtx, b.launcher, cfg)
	case TransportHTTP:
		return DialHTTP(httpConfig{URL: cfg.URL, Headers: expandEnv(cfg.Headers), Client: b.client})
	default:
		return nil, fmt.Errorf("server %q has unknown transport %q", cfg.Name, cfg.Transport)
	}
}

// Invalidate drops the cached connection for a server so its next use reconnects with
// fresh config. The API calls it after a server is edited or removed.
func (b *Broker) Invalidate(server string) {
	b.mu.Lock()
	c := b.conns[server]
	delete(b.conns, server)
	b.mu.Unlock()
	if c != nil {
		c.mu.Lock()
		if c.cli != nil {
			_ = c.cli.Close()
		}
		c.ready = false
		c.mu.Unlock()
	}
}

// Probe connects to a server and returns its declared tools, for the console's
// "test connection / discover tools" action. It does NOT consult grants — it is an
// operator action on configured infrastructure, not an agent call.
func (b *Broker) Probe(ctx context.Context, server string) ([]Tool, error) {
	// Always reconnect so a probe reflects the latest saved config.
	b.Invalidate(server)
	c, err := b.ensure(server)
	if err != nil {
		return nil, err
	}
	out := make([]Tool, len(c.tools))
	copy(out, c.tools)
	return out, nil
}

// --- per-session sandbox surface ---

// toolDescriptor is one namespaced tool the sandbox sees over GET /tools.
type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// callRequest / callResponse are the POST /call wire shapes.
type callRequest struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type callResponse struct {
	Content string `json:"content"`
	IsError bool   `json:"isError"`
}

// toolsForSession returns the namespaced, gateway-approved tool surface for a session.
// A server that fails to connect is skipped (audited) rather than failing the whole
// listing, so one broken server does not deny the agent its other tools.
func (b *Broker) toolsForSession(ctx context.Context, session string) []toolDescriptor {
	var out []toolDescriptor
	for _, g := range b.grants(session) {
		start := time.Now()
		c, err := b.ensure(g.Server)
		if err != nil {
			b.emit(AuditRecord{Session: session, Server: g.Server, Op: "list", Status: "error", Duration: time.Since(start), Error: errText(err)})
			continue
		}
		allow := allowSet(g.Tools)
		n := 0
		for _, t := range c.tools {
			if allow != nil && !allow[t.Name] {
				continue
			}
			out = append(out, toolDescriptor{
				Name:        namespaced(g.Server, t.Name),
				Description: describeTool(g.Server, t),
				InputSchema: t.InputSchema,
			})
			n++
		}
		b.emit(AuditRecord{Session: session, Server: g.Server, Op: "list", Allowed: true, Status: "ok", Bytes: n, Duration: time.Since(start)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// call enforces the session's grant deny-by-default, then forwards an approved call to
// the upstream server. A policy denial comes back as an IsError result (so the agent
// gets actionable text), not an HTTP error; both outcomes are audited.
func (b *Broker) call(ctx context.Context, session string, req callRequest) callResponse {
	start := time.Now()
	server, tool, ok := splitNamespaced(req.Name)
	if !ok {
		b.emit(AuditRecord{Session: session, Tool: req.Name, Op: "call", Status: "denied", Duration: time.Since(start), Error: "malformed tool name"})
		return errorResult(fmt.Sprintf("MCP call rejected: %q is not a valid <server>__<tool> name", req.Name))
	}

	grant, granted := findGrant(b.grants(session), server)
	if !granted {
		b.emit(AuditRecord{Session: session, Server: server, Tool: tool, Op: "call", Status: "denied", Duration: time.Since(start), Error: "server not granted"})
		return errorResult(fmt.Sprintf("MCP access denied: this agent is not granted server %q", server))
	}

	c, err := b.ensure(server)
	if err != nil {
		b.emit(AuditRecord{Session: session, Server: server, Tool: tool, Op: "call", Status: "error", Duration: time.Since(start), Error: errText(err)})
		return errorResult(fmt.Sprintf("MCP server %q is unavailable: %v", server, err))
	}

	if !grantAllowsTool(grant, c.tools, tool) {
		b.emit(AuditRecord{Session: session, Server: server, Tool: tool, Op: "call", Status: "denied", Duration: time.Since(start), Error: "tool not granted"})
		return errorResult(fmt.Sprintf("MCP access denied: this agent is not granted tool %q on server %q", tool, server))
	}

	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := c.cli.CallTool(cctx, tool, req.Input)
	if err != nil {
		b.emit(AuditRecord{Session: session, Server: server, Tool: tool, Op: "call", Allowed: true, Status: "error", Duration: time.Since(start), Error: errText(err)})
		return errorResult(fmt.Sprintf("MCP call %s failed: %v", req.Name, err))
	}
	text := res.Text()
	b.emit(AuditRecord{Session: session, Server: server, Tool: tool, Op: "call", Allowed: true, Status: "ok", Bytes: len(text), Duration: time.Since(start)})
	return callResponse{Content: text, IsError: res.IsError}
}

// SocketForSession creates (idempotently) and serves a per-session unix socket and
// returns its path, for the launch layer to bind into that one sandbox. The socket
// only ever serves THIS session's approved surface.
func (b *Broker) SocketForSession(session, dir string) (string, error) {
	if session == "" {
		return "", errors.New("mcp: SocketForSession needs a session id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s, ok := b.socks[session]; ok {
		return s.path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mcp: socket dir: %w", err)
	}
	path := sessionSocketPath(dir, session)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", fmt.Errorf("mcp: listen %s: %w", path, err)
	}
	srv := &http.Server{Handler: b.sessionHandler(session)}
	go func() { _ = srv.Serve(ln) }()
	b.socks[session] = &sessionSock{path: path, ln: ln, srv: srv}
	return path, nil
}

// CloseSession stops serving and removes a session's socket (idempotent).
func (b *Broker) CloseSession(session string) {
	b.mu.Lock()
	s := b.socks[session]
	delete(b.socks, session)
	b.mu.Unlock()
	if s != nil {
		shutdownSock(s)
	}
}

// Close stops every session socket and tears down every upstream connection (killing
// spawned local servers). Best-effort.
func (b *Broker) Close() {
	b.mu.Lock()
	socks := b.socks
	conns := b.conns
	b.socks = map[string]*sessionSock{}
	b.conns = map[string]*conn{}
	b.mu.Unlock()
	for _, s := range socks {
		shutdownSock(s)
	}
	for _, c := range conns {
		c.mu.Lock()
		if c.cli != nil {
			_ = c.cli.Close()
		}
		c.mu.Unlock()
	}
}

// sessionHandler serves GET /tools and POST /call for one fixed session id.
func (b *Broker) sessionHandler(session string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"tools": b.toolsForSession(r.Context(), session)})
	})
	mux.HandleFunc("POST /call", func(w http.ResponseWriter, r *http.Request) {
		var req callRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid call JSON", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, b.call(r.Context(), session, req))
	})
	return mux
}

// --- helpers ---

func shutdownSock(s *sessionSock) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = os.Remove(s.path)
}

func sessionSocketPath(dir, session string) string {
	// Keep the filename short and filesystem-safe; the unix socket path length is
	// bounded (~104 bytes), so use a sanitized session id.
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, session)
	return dir + "/" + safe + ".sock"
}

func namespaced(server, tool string) string { return server + nsSep + tool }

func splitNamespaced(name string) (server, tool string, ok bool) {
	i := strings.Index(name, nsSep)
	if i <= 0 || i+len(nsSep) >= len(name) {
		return "", "", false
	}
	return name[:i], name[i+len(nsSep):], true
}

func describeTool(server string, t Tool) string {
	d := strings.TrimSpace(t.Description)
	if d == "" {
		d = "(no description provided by the server)"
	}
	return fmt.Sprintf("[MCP server %q] %s", server, d)
}

func allowSet(tools []string) map[string]bool {
	if len(tools) == 0 {
		return nil // nil = all declared tools
	}
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t] = true
	}
	return m
}

func findGrant(grants []Grant, server string) (Grant, bool) {
	for _, g := range grants {
		if g.Server == server {
			return g, true
		}
	}
	return Grant{}, false
}

// grantAllowsTool reports whether the grant permits calling tool on the server. The
// tool must be DECLARED by the server (so a grant can never reach a method the server
// does not expose), and either the grant lists it or the grant is "all declared".
func grantAllowsTool(g Grant, declared []Tool, tool string) bool {
	found := false
	for _, t := range declared {
		if t.Name == tool {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if len(g.Tools) == 0 {
		return true
	}
	for _, t := range g.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

func errorResult(msg string) callResponse { return callResponse{Content: msg, IsError: true} }

// errText returns an error's text, trimmed, for audit (never includes secrets — the
// transports keep credential values out of their error strings).
func errText(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
