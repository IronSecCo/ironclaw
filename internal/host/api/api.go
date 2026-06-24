// Package api is the control-plane HTTP API. It binds ONLY to the mesh
// (Tailscale) interface so the control-plane has no public port. It exposes
// endpoints for submitting gateway change requests, listing pending approvals,
// and recording decisions; ironctl is a thin client.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/router"
	"github.com/IronSecCo/ironclaw/internal/host/skills"
)

// HistoryProvider returns the applied/rejected change history. A FileStore
// satisfies it; the in-memory store does not (history endpoint returns empty).
type HistoryProvider interface {
	History() []gateway.HistoryEntry
}

// Server is the control-plane HTTP server. It drives the gateway and, when a
// registry is attached (WithRegistry), the registry admin endpoints.
type Server struct {
	gw         *gateway.Gateway
	history    HistoryProvider
	auditPath  string
	token      string
	reg        registry.Registry
	terminate  SessionTerminator        // host action behind POST /v1/ui/sessions/{id}/terminate
	chatRouter *router.Router           // inbound router for the chat playground
	webchat    *channels.WebchatAdapter // outbound buffer for the chat playground
	skills     *skills.Resolver         // curated, signature-verifying skills resolver; nil = skills disabled
	mcpCatalog *mcp.Catalog             // operator-configured MCP server catalog; nil = MCP disabled
	mcpBroker  *mcp.Broker              // host MCP broker, for probe/discovery; nil = MCP disabled
	vault      VaultPolicyReader        // per-group vault policy, for the read surface; nil = vault read disabled
	mux        *http.ServeMux

	// Hardening (all opt-in; see hardening.go). Zero values disable the feature.
	limiter        *rateLimiter
	maxBodyBytes   int64
	maxHeaderBytes int
	ready          func() error
	metrics        http.Handler
	tlsCert        string
	tlsKey         string
}

// New constructs a Server bound to gw and wires the routes.
func New(gw *gateway.Gateway) *Server {
	s := &Server{gw: gw, mux: http.NewServeMux()}
	s.routes()
	return s
}

// WithHistory attaches a change-history provider (e.g. a *gateway.FileStore) so
// GET /v1/changes/history returns applied + rejected changes. It returns the
// Server for chaining.
func (s *Server) WithHistory(h HistoryProvider) *Server {
	s.history = h
	return s
}

// WithAuditPath attaches the JSONL audit-log path so GET /v1/audit returns recent
// entries. It returns the Server for chaining.
func (s *Server) WithAuditPath(path string) *Server {
	s.auditPath = path
	return s
}

// WithToken enables bearer-token authentication. This is defense-in-depth behind
// the mesh boundary: the API still binds only to the Tailscale interface, but a
// token means a foothold on the tailnet alone is not enough to drive the gateway.
// An empty token (the default) disables the check. Returns the Server for chaining.
func (s *Server) WithToken(token string) *Server {
	s.token = token
	return s
}

// WithRateLimit enables a global token-bucket rate limiter: at most burst
// requests in a spike, refilling at rps requests/second. Probe endpoints
// (/healthz, /readyz) are never throttled. rps <= 0 disables it. Returns the
// Server for chaining.
func (s *Server) WithRateLimit(rps float64, burst int) *Server {
	if rps > 0 {
		s.limiter = newRateLimiter(rps, burst)
	}
	return s
}

// WithLimits caps the request body size (bytes) and the max header size (bytes).
// A non-positive value leaves that limit unset. Returns the Server for chaining.
func (s *Server) WithLimits(maxBodyBytes int64, maxHeaderBytes int) *Server {
	s.maxBodyBytes = maxBodyBytes
	s.maxHeaderBytes = maxHeaderBytes
	return s
}

// WithReadiness attaches a readiness check that gates GET /readyz: a non-nil
// error makes the probe report 503. Nil check (the default) is always ready.
// Returns the Server for chaining.
func (s *Server) WithReadiness(check func() error) *Server {
	s.ready = check
	return s
}

// WithMetrics mounts an injected metrics handler (e.g. from internal/host/
// metrics) at GET /metrics. Decoupled from that package so this server does not
// depend on it. Returns the Server for chaining.
func (s *Server) WithMetrics(h http.Handler) *Server {
	s.metrics = h
	return s
}

// WithTLS makes Run serve HTTPS using the PEM cert/key files. Empty paths (the
// default) serve plain HTTP — the mesh boundary remains the primary control.
// Returns the Server for chaining.
func (s *Server) WithTLS(certFile, keyFile string) *Server {
	s.tlsCert, s.tlsKey = certFile, keyFile
	return s
}

// Handler exposes the fully-wrapped handler for testing and serving. Middleware
// order (outermost first): rate-limit -> body-size cap -> auth -> routes.
func (s *Server) Handler() http.Handler {
	return s.rateLimit(s.limitBody(s.auth(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("POST /v1/changes", s.handleSubmit)
	s.mux.HandleFunc("GET /v1/changes/pending", s.handlePending)
	s.mux.HandleFunc("GET /v1/changes/history", s.handleHistory)
	s.mux.HandleFunc("POST /v1/changes/{id}/decision", s.handleDecision)
	s.mux.HandleFunc("GET /v1/audit", s.handleAudit)
	s.registryRoutes()
	s.uiRoutes()          // embedded web console at GET /ui/ (see ui.go)
	s.uiApprovalsRoutes() // approvals read-model at GET /v1/ui/approvals (see ui_approvals.go)
	s.uiSessionsRoutes()  // sessions read-model + terminate at /v1/ui/sessions (see ui_sessions.go)
	s.uiAuditRoutes()     // audit read-model at GET /v1/ui/audit (see ui_audit.go)
	s.uiChannelsRoutes()  // channels/wiring read-models at /v1/ui/channels|destinations (see ui_channels.go)
	s.uiConfigRoutes()    // setup wizard + config editor at /v1/ui/onboard|config (see ui_config.go)
	s.uiChatRoutes()      // chat playground at /v1/ui/chat (see ui_chat.go)
	s.uiAgentsRoutes()    // agents picker + builder read-model at GET /v1/ui/agents (see ui_agents.go)
	s.uiCatalogRoutes()   // built-in tool catalog + starter templates at /v1/ui/{tools,templates} (see ui_catalog.go)
	s.skillsRoutes()      // skills install/list/remove at /v1/skills (see skills.go)
	s.mcpRoutes()         // MCP server CRUD/probe + read-model at /v1/registry/mcp-servers|/v1/ui/mcp-servers (see mcp.go)
	s.vaultRoutes()       // per-group vault policy read surface at /v1/vault/policy (see vault.go)
}

// auth wraps h with optional bearer-token authentication. With no token set, the
// API relies solely on the mesh (Tailscale) network boundary. The /healthz and
// /readyz probes are always exempt so liveness/readiness checks need no
// credential; the static web-console shell under /ui/ is likewise exempt
// (uiAuthExempt) because a browser cannot header a navigation — every data read
// and action it makes still goes through the gated /v1 API. The token comparison
// is constant-time.
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" || probeExempt(r.URL.Path) || uiAuthExempt(r.URL.Path) {
			h.ServeHTTP(w, r)
			return
		}
		const prefix = "Bearer "
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(got, prefix)), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// handleHealthz is an unauthenticated liveness probe.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleHistory returns the applied + rejected change history. When no history
// provider is attached (in-memory store), it returns an empty list.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		writeJSON(w, http.StatusOK, []gateway.HistoryEntry{})
		return
	}
	hist := s.history.History()
	if hist == nil {
		hist = []gateway.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, hist)
}

// handleAudit returns recent audit entries. The optional ?limit= query caps the
// count (default 100). When no audit path is attached, it returns an empty list.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if s.auditPath == "" {
		writeJSON(w, http.StatusOK, []gateway.AuditEntry{})
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := gateway.ReadAudit(s.auditPath, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []gateway.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// submitResponse is returned from POST /v1/changes.
type submitResponse struct {
	ID contract.ChangeID `json:"id"`
}

// handleSubmit decodes a ChangeRequest and submits it to the gateway. Submit
// blocks on human approval, so it runs in a goroutine; the handler returns 202
// with the assigned change id immediately.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	var req contract.ChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid change request JSON", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		req.ID = newID()
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	go func() {
		// Background context: the submit outlives this HTTP request and lives until
		// a human decides. A real server would tie this to the server lifetime.
		_, _ = s.gw.Submit(context.Background(), req)
	}()
	writeJSON(w, http.StatusAccepted, submitResponse{ID: req.ID})
}

// handlePending returns the changes awaiting a decision.
func (s *Server) handlePending(w http.ResponseWriter, r *http.Request) {
	pending, err := s.gw.Pending()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pending == nil {
		pending = []contract.ChangeRequest{}
	}
	writeJSON(w, http.StatusOK, pending)
}

// decisionRequest is the body of POST /v1/changes/{id}/decision.
type decisionRequest struct {
	Outcome   string          `json:"outcome"`
	DecidedBy contract.UserID `json:"decidedBy"`
}

// handleDecision records a human decision for a pending change.
func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	id := contract.ChangeID(r.PathValue("id"))
	if id == "" {
		http.Error(w, "missing change id", http.StatusBadRequest)
		return
	}
	var dr decisionRequest
	if err := json.NewDecoder(r.Body).Decode(&dr); err != nil {
		http.Error(w, "invalid decision JSON", http.StatusBadRequest)
		return
	}
	if dr.Outcome != gateway.OutcomeApprove && dr.Outcome != gateway.OutcomeReject {
		http.Error(w, "outcome must be approve or reject", http.StatusBadRequest)
		return
	}
	d := contract.Decision{Outcome: dr.Outcome, DecidedBy: dr.DecidedBy, DecidedAt: time.Now().UTC()}
	if err := s.gw.Decide(id, d); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// Run binds the API to addr (expected to be the Tailscale interface address — the
// control-plane must have NO public port) and serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler()}
	if s.maxHeaderBytes > 0 {
		srv.MaxHeaderBytes = s.maxHeaderBytes
	}
	errCh := make(chan error, 1)
	go func() {
		if s.tlsCert != "" && s.tlsKey != "" {
			errCh <- srv.ServeTLS(ln, s.tlsCert, s.tlsKey)
			return
		}
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// newID generates an API-side change id when the client omits one. It mirrors the
// gateway's format closely enough for logs; the gateway treats a non-empty id as
// authoritative.
func newID() contract.ChangeID {
	return contract.ChangeID("chg_" + time.Now().UTC().Format("20060102150405.000000000"))
}
