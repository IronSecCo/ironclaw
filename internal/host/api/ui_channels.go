// OWNER: T-223 (web-console channels & wiring read-models; extends the AGENT1-owned api package)

package api

import (
	"net/http"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// wiringView is a registry Wiring enriched with the agent-group name so the
// channels page renders wirings readably. Adds no new contract surface.
type wiringView struct {
	registry.Wiring
	AgentGroupName string `json:"agentGroupName,omitempty"`
}

// channelsView is the per-messaging-group projection the channels page renders:
// the messaging group plus its enriched wirings.
type channelsView struct {
	MessagingGroup registry.MessagingGroup `json:"messagingGroup"`
	Wirings        []wiringView            `json:"wirings"`
}

// uiChannelsRoutes registers the channels/wiring read-models. Wired from routes()
// in api.go. Mutations are NOT added here — creating messaging groups, wirings,
// and destinations reuses the existing /v1/registry/* admin endpoints; these are
// the read projections those forms need. Under /v1, so bearer-gated.
func (s *Server) uiChannelsRoutes() {
	s.mux.HandleFunc("GET /v1/ui/channels/{messagingGroupId}", s.handleUIChannel)
	s.mux.HandleFunc("GET /v1/ui/destinations/{agentGroupId}", s.handleUIDestinations)
}

// handleUIChannel returns a messaging group and its wirings, each enriched with
// the target agent-group name.
func (s *Server) handleUIChannel(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	mgID := contract.MessagingGroupID(r.PathValue("messagingGroupId"))
	mg, ok := s.reg.GetMessagingGroup(mgID)
	if !ok {
		http.Error(w, "messaging group not found", http.StatusNotFound)
		return
	}
	wirings, err := s.reg.ListWirings(mgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]wiringView, 0, len(wirings))
	for _, wr := range wirings {
		v := wiringView{Wiring: wr}
		if g, ok := s.reg.GetAgentGroup(wr.AgentGroupID); ok {
			v.AgentGroupName = g.Name
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, channelsView{MessagingGroup: mg, Wirings: views})
}

// handleUIDestinations lists the destinations an agent group may send to. This is
// the read counterpart of the existing POST /v1/registry/destinations, backed by
// the registry's new ListDestinations.
func (s *Server) handleUIDestinations(w http.ResponseWriter, r *http.Request) {
	if !s.regReady(w) {
		return
	}
	agID := contract.AgentGroupID(r.PathValue("agentGroupId"))
	dests := s.reg.ListDestinations(agID)
	if dests == nil {
		dests = []registry.Destination{}
	}
	writeJSON(w, http.StatusOK, dests)
}
