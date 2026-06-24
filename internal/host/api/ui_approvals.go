package api

import (
	"encoding/json"
	"net/http"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
)

// approvalView is the read-model the approvals inbox renders: a pending
// ChangeRequest enriched with the human-readable agent-group and requester names
// resolved from the registry. It adds NO new contract surface — it is a
// presentation projection over the existing gateway + registry, assembled at read
// time. Names are best-effort: an unresolved id leaves the *Name field empty so
// the UI can fall back to the raw id.
type approvalView struct {
	ID              contract.ChangeID     `json:"id"`
	Kind            contract.ChangeKind   `json:"kind"`
	AgentGroupID    contract.AgentGroupID `json:"agentGroupId"`
	AgentGroupName  string                `json:"agentGroupName,omitempty"`
	RequestedBy     contract.UserID       `json:"requestedBy"`
	RequestedByName string                `json:"requestedByName,omitempty"`
	CreatedAt       string                `json:"createdAt"`
	Before          json.RawMessage       `json:"before,omitempty"`
	After           json.RawMessage       `json:"after,omitempty"`
}

// uiApprovalsRoutes registers the approvals read-model endpoint. Wired from
// routes() in api.go. The path lives under /v1 so it stays behind the bearer gate
// (only the static /ui/ shell is auth-exempt).
func (s *Server) uiApprovalsRoutes() {
	s.mux.HandleFunc("GET /v1/ui/approvals", s.handleUIApprovals)
}

// handleUIApprovals returns the pending changes as approvalViews — the same set
// as GET /v1/changes/pending, projected with resolved names so the inbox can
// render group/requester readably without a round-trip per row. The approve/
// reject actions still POST to the existing /v1/changes/{id}/decision endpoint.
func (s *Server) handleUIApprovals(w http.ResponseWriter, r *http.Request) {
	pending, err := s.gw.Pending()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]approvalView, 0, len(pending))
	for _, c := range pending {
		v := approvalView{
			ID:           c.ID,
			Kind:         c.Kind,
			AgentGroupID: c.AgentGroupID,
			RequestedBy:  c.RequestedBy,
			CreatedAt:    c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Before:       c.Before,
			After:        maskApprovalAfter(c.Kind, c.After),
		}
		// Best-effort name resolution; absent registry or unknown ids leave the
		// names empty and the UI shows the raw id.
		if s.reg != nil {
			if g, ok := s.reg.GetAgentGroup(c.AgentGroupID); ok {
				v.AgentGroupName = g.Name
			}
			if u, ok := s.reg.GetUser(c.RequestedBy); ok {
				v.RequestedByName = u.DisplayName
			}
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, views)
}

// maskApprovalAfter masks secret-bearing values in an approval's After payload before
// it crosses to the browser. For a ChangeMCPRegister the payload is a proposed MCP
// ServerConfig that may carry raw credentials in env/headers; the human approver must
// see the FULL endpoint definition (command/args/image/url/headers — the load-bearing
// control), but never a plaintext secret. ServerConfig.Public() preserves keys and
// ${VAR} references while masking raw values. A payload that does not parse is returned
// unchanged (the verifier already rejected a malformed register, so it never reaches an
// approval card; this only guards a best-effort projection). Other kinds pass through.
func maskApprovalAfter(kind contract.ChangeKind, after json.RawMessage) json.RawMessage {
	if kind != contract.ChangeMCPRegister || len(after) == 0 {
		return after
	}
	var cfg mcp.ServerConfig
	if err := json.Unmarshal(after, &cfg); err != nil {
		return after
	}
	masked, err := json.Marshal(cfg.Public())
	if err != nil {
		return after
	}
	return masked
}
