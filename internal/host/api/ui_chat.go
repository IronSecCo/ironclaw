package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/router"
	"github.com/nivardsec/ironclaw/internal/host/types"
)

// webchatChannel is the channel type for the in-console chat playground. Inbound
// events and the WebchatAdapter both use it, so the delivery loop routes an
// agent's reply back to the adapter the browser polls.
const webchatChannel = "webchat"

// WithChat wires the chat playground: the inbound Router (to feed a
// browser message into the NORMAL routing/delivery path — engage, session
// resolution, inbound queue, wake) and the in-process WebchatAdapter (to buffer
// the agent's outbound replies for polling). Without it the /v1/ui/chat endpoints
// return 503. Returns the Server for chaining.
func (s *Server) WithChat(r *router.Router, w *channels.WebchatAdapter) *Server {
	s.chatRouter = r
	s.webchat = w
	return s
}

// uiChatRoutes registers the chat playground endpoints (bearer-gated, under /v1).
func (s *Server) uiChatRoutes() {
	s.mux.HandleFunc("POST /v1/ui/chat/send", s.handleUIChatSend)
	s.mux.HandleFunc("GET /v1/ui/chat/{agentGroupId}/messages", s.handleUIChatMessages)
}

type uiChatSendRequest struct {
	AgentGroupID contract.AgentGroupID `json:"agentGroupID"`
	Text         string                `json:"text"`
}

// handleUIChatSend feeds a browser message into the agent through the normal
// router: it ensures the webchat messaging group, a wiring to the chosen agent
// group, and console-sender access all exist (idempotent), then calls
// RouteInbound. The agent's reply flows back out through the delivery loop to the
// WebchatAdapter, which the browser polls via /messages. It deliberately does NOT
// shortcut the inbound queue — the playground is a real channel, not a side door.
func (s *Server) handleUIChatSend(w http.ResponseWriter, r *http.Request) {
	if s.chatRouter == nil || s.reg == nil {
		http.Error(w, "chat playground not configured", http.StatusServiceUnavailable)
		return
	}
	var req uiChatSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid chat JSON", http.StatusBadRequest)
		return
	}
	if req.AgentGroupID == "" || strings.TrimSpace(req.Text) == "" {
		http.Error(w, "agentGroupID and text are required", http.StatusBadRequest)
		return
	}
	if _, ok := s.reg.GetAgentGroup(req.AgentGroupID); !ok {
		http.Error(w, "agent group not found", http.StatusNotFound)
		return
	}

	// One playground conversation per agent group; the conversation id is the
	// messaging-group platform id and the key the WebchatAdapter buffers under.
	conv := string(req.AgentGroupID)
	mg, err := s.reg.GetOrCreateMessagingGroup(webchatChannel, conv, webchatChannel, false, contract.UnknownStrict)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Idempotent wiring (deterministic id) so re-sends don't multiply wirings.
	// EngageMention + Mentioned=true below makes every console message engage.
	if err := s.reg.PutWiring(registry.Wiring{
		ID:               "wr_webchat_" + conv,
		MessagingGroupID: mg.ID,
		AgentGroupID:     req.AgentGroupID,
		EngageMode:       contract.EngageMention,
		SessionMode:      contract.SessionShared,
		Priority:         1,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The console operator is already authenticated at the API boundary; grant the
	// namespaced console sender access so the router's CanAccess gate passes.
	// AddMember is idempotent (a duplicate is a no-op), so a non-nil error is a real
	// backend failure — surface it rather than silently routing to an access-denied
	// outcome the caller would misread as "not engaged".
	sender := router.NamespaceUserID(webchatChannel, "console")
	if err := s.reg.AddMember(registry.Member{UserID: sender, AgentGroupID: req.AgentGroupID}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	outcomes, err := s.chatRouter.RouteInbound(r.Context(), types.InboundEvent{
		ChannelType:  webchatChannel,
		PlatformID:   conv,
		Instance:     webchatChannel,
		SenderHandle: "console",
		Text:         req.Text,
		Mentioned:    true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	engaged := false
	for _, o := range outcomes {
		if o.Engaged {
			engaged = true
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"conversationId": conv,
		"engaged":        engaged,
		"outcomes":       outcomes,
	})
}

// handleUIChatMessages drains and returns the buffered agent replies for a
// conversation (the agent group's playground). Polling is incremental: a reply is
// returned once.
func (s *Server) handleUIChatMessages(w http.ResponseWriter, r *http.Request) {
	if s.webchat == nil {
		http.Error(w, "chat playground not configured", http.StatusServiceUnavailable)
		return
	}
	conv := r.PathValue("agentGroupId")
	writeJSON(w, http.StatusOK, map[string]any{
		"conversationId": conv,
		"messages":       s.webchat.Drain(conv),
	})
}
