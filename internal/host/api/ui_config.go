// OWNER: T-225 (web-console setup wizard + config editor; extends the AGENT1-owned api package)

package api

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/onboard"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// capabilityKinds are the change kinds the config editor may submit. They are
// exactly the per-group capability mutations — each one MUST route through the
// gateway's human-approval floor. ChangeCreateAgent is deliberately excluded: it
// provisions a new trust principal (RFC-0004) and is not a config-editor edit.
var capabilityKinds = map[contract.ChangeKind]bool{
	contract.ChangePersona:      true,
	contract.ChangeEnabledTools: true,
	contract.ChangePackages:     true,
	contract.ChangePermissions:  true,
	contract.ChangeMounts:       true,
	contract.ChangeWiring:       true,
}

// uiConfigRoutes registers the setup wizard + config-editor read-models and the
// gateway-routed capability-change submit. Wired from routes() in api.go; all
// under /v1 (bearer-gated).
func (s *Server) uiConfigRoutes() {
	s.mux.HandleFunc("GET /v1/ui/onboard", s.handleUIOnboard)
	s.mux.HandleFunc("GET /v1/ui/config/{agentGroupId}", s.handleUIConfigGet)
	s.mux.HandleFunc("POST /v1/ui/config/change", s.handleUIConfigChange)
}

// onboardStepView is one onboarding step surfaced to the browser. It mirrors
// onboard.Step but is an explicit DTO so the wire shape is stable and carries no
// secret (the token is never included).
type onboardStepView struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// handleUIOnboard runs the first-run wizard in DRY-RUN and returns its steps, so
// the browser mirrors `ironctl onboard` without performing any side effect. The
// minted token is never produced in dry-run, so nothing sensitive crosses to the
// client. The Deps are constructed explicitly with inert mutators as a second
// guard against any write.
func (s *Server) handleUIOnboard(w http.ResponseWriter, r *http.Request) {
	deps := onboard.Deps{
		LookPath:  exec.LookPath,
		Getenv:    os.Getenv,
		GenToken:  func() (string, error) { return "", nil }, // unused in dry-run
		ReadFile:  os.ReadFile,
		WriteFile: func(string, []byte, fs.FileMode) error { return nil }, // inert
		MkdirAll:  func(string, fs.FileMode) error { return nil },         // inert
		Stat:      os.Stat,
		// The browser reached this handler, so the control-plane is reachable; the
		// verify step reflects that without a self-request.
		Ping:       func(context.Context, string) error { return nil },
		Stdout:     io.Discard,
		ConfigPath: onboardConfigPath(),
	}
	res, err := deps.Run(r.Context(), onboard.Options{DryRun: true})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	steps := make([]onboardStepView, 0, len(res.Steps))
	for _, st := range res.Steps {
		steps = append(steps, onboardStepView{Name: st.Name, Status: string(st.Status), Detail: st.Detail})
	}
	writeJSON(w, http.StatusOK, map[string]any{"steps": steps, "ok": res.Ok()})
}

// configView is the per-group config the editor renders. Because per-group
// capabilities are not materialized in the registry (the gateway change log is the
// system of record), the "current config" is the agent group's identity plus the
// applied capability changes from history.
type configView struct {
	AgentGroup     registry.AgentGroup    `json:"agentGroup"`
	AppliedChanges []gateway.HistoryEntry `json:"appliedChanges"`
}

// handleUIConfigGet returns an agent group's identity and the capability changes
// recorded against it (from the gateway history). History is empty when no
// history provider is attached (in-memory store).
func (s *Server) handleUIConfigGet(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	id := contract.AgentGroupID(r.PathValue("agentGroupId"))
	g, ok := s.reg.GetAgentGroup(id)
	if !ok {
		http.Error(w, "agent group not found", http.StatusNotFound)
		return
	}
	applied := []gateway.HistoryEntry{}
	if s.history != nil {
		for _, h := range s.history.History() {
			if h.Request.AgentGroupID == id {
				applied = append(applied, h)
			}
		}
	}
	writeJSON(w, http.StatusOK, configView{AgentGroup: g, AppliedChanges: applied})
}

// uiConfigChangeRequest is the config editor's submit body.
type uiConfigChangeRequest struct {
	Kind         contract.ChangeKind   `json:"kind"`
	AgentGroupID contract.AgentGroupID `json:"agentGroupID"`
	RequestedBy  contract.UserID       `json:"requestedBy"`
	After        json.RawMessage       `json:"after"`
}

// handleUIConfigChange validates a proposed capability change and submits it
// THROUGH THE GATEWAY — never a direct registry write. This is the choke point the
// acceptance demands: every capability mutation lands on the human-approval floor
// (it shows up in the Approvals inbox). Submit blocks on the human decision, so it
// runs in a goroutine and the handler returns 202 with the change id immediately,
// mirroring POST /v1/changes.
func (s *Server) handleUIConfigChange(w http.ResponseWriter, r *http.Request) {
	var req uiConfigChangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid change JSON", http.StatusBadRequest)
		return
	}
	if !capabilityKinds[req.Kind] {
		http.Error(w, "kind must be a capability change (persona, enabled_tools, packages, permissions, mounts, wiring)", http.StatusBadRequest)
		return
	}
	if req.AgentGroupID == "" {
		http.Error(w, "agentGroupID is required", http.StatusBadRequest)
		return
	}
	if len(req.After) == 0 || !json.Valid(req.After) {
		http.Error(w, "after must be a non-empty JSON payload for the change kind", http.StatusBadRequest)
		return
	}
	cr := contract.ChangeRequest{
		ID:           newID(),
		Kind:         req.Kind,
		AgentGroupID: req.AgentGroupID,
		RequestedBy:  req.RequestedBy,
		After:        req.After,
		CreatedAt:    time.Now().UTC(),
	}
	go func() {
		// Background context: the submit outlives this request and lives until a
		// human decides (same pattern as handleSubmit).
		_, _ = s.gw.Submit(context.Background(), cr)
	}()
	writeJSON(w, http.StatusAccepted, submitResponse{ID: cr.ID})
}

// onboardConfigPath mirrors the ironctl-side defaultOnboardConfig so the wizard
// reports the same token-file location. It only ever READS this path (dry-run);
// it never writes.
func onboardConfigPath() string {
	if p := os.Getenv("IRONCLAW_CONFIG"); p != "" {
		return p
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "ironclaw", "onboard.env")
	}
	return filepath.Join(os.TempDir(), "ironclaw", "onboard.env")
}
