package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/skills"
)

// The skills endpoints are the ONLY trigger for a skill install — a host/admin
// action, never a sandbox tool (an agent can at most ask; only a human grants). The
// daemon does the security-critical work (fetch from the curated source, verify the
// signature against the trust root, validate the manifest against the COMPILED tool
// set) inside skills.Resolver/InstallChange, then submits ONE bundled ChangeRequest
// to the gateway — which lands on the AlwaysRequireHuman floor like any other
// capability change. ironctl is a thin client of these.
//
// All three are guarded on a configured resolver (WithSkills); with none set
// (skills not enabled) they return 503, so the default daemon exposes no skills
// surface.

// WithSkills attaches the curated, signature-verifying skills resolver that backs
// the /v1/skills endpoints. nil (the default) leaves skills disabled.
func (s *Server) WithSkills(resolver *skills.Resolver) *Server {
	s.skills = resolver
	return s
}

func (s *Server) skillsRoutes() {
	s.mux.HandleFunc("POST /v1/skills/install", s.handleSkillInstall)
	s.mux.HandleFunc("GET /v1/skills", s.handleSkillList)
	s.mux.HandleFunc("DELETE /v1/skills/{name}", s.handleSkillRemove)
}

// skillInstallRequest is the body of POST /v1/skills/install.
type skillInstallRequest struct {
	Skill        string `json:"skill"`
	Version      string `json:"version"`
	AgentGroupID string `json:"agentGroupId"`
	RequestedBy  string `json:"requestedBy"`
}

// handleSkillInstall resolves + verifies a skill from the curated source and submits
// its install ChangeRequest to the gateway. It returns 202 with the change id; the
// install applies only after a human approves it. A skill that is missing, unsigned,
// untrusted, or out of policy fails here (400) and never reaches the gateway.
func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		http.Error(w, "skills are not enabled on this control-plane", http.StatusServiceUnavailable)
		return
	}
	var req skillInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid skill-install JSON", http.StatusBadRequest)
		return
	}
	if req.Skill == "" || req.Version == "" || req.AgentGroupID == "" {
		http.Error(w, "skill, version, and agentGroupId are required", http.StatusBadRequest)
		return
	}

	cr, err := skills.InstallChange(s.skills, req.Skill, req.Version,
		contract.AgentGroupID(req.AgentGroupID), contract.UserID(req.RequestedBy))
	if err != nil {
		// Resolve/verify/validate failure: the named skill cannot be installed.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cr.ID == "" {
		cr.ID = newID()
	}
	if cr.CreatedAt.IsZero() {
		cr.CreatedAt = time.Now().UTC()
	}
	go func() {
		// Submit blocks until a human decides; it outlives this request (mirrors
		// handleSubmit).
		_, _ = s.gw.Submit(context.Background(), cr)
	}()
	writeJSON(w, http.StatusAccepted, submitResponse{ID: cr.ID})
}

// handleSkillList returns the available skills in the curated source (the host
// catalog). A source that cannot enumerate returns an empty list rather than an
// error.
func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		http.Error(w, "skills are not enabled on this control-plane", http.StatusServiceUnavailable)
		return
	}
	cat, ok := s.skills.Source.(skills.Catalog)
	if !ok {
		writeJSON(w, http.StatusOK, []skills.SkillRef{})
		return
	}
	refs, err := cat.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if refs == nil {
		refs = []skills.SkillRef{}
	}
	writeJSON(w, http.StatusOK, refs)
}

// handleSkillRemove un-catalogs a skill from the curated source. The optional
// ?version= removes one version; absent, it removes every version of the skill.
// Removing from the catalog does not revoke an already-approved install — that is a
// separate gateway change.
func (s *Server) handleSkillRemove(w http.ResponseWriter, r *http.Request) {
	if s.skills == nil {
		http.Error(w, "skills are not enabled on this control-plane", http.StatusServiceUnavailable)
		return
	}
	cat, ok := s.skills.Source.(skills.Catalog)
	if !ok {
		http.Error(w, "the configured skills source does not support removal", http.StatusNotImplemented)
		return
	}
	if err := cat.Remove(r.PathValue("name"), r.URL.Query().Get("version")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
