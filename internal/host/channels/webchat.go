// OWNER: T-226

package channels

import (
	"context"
	"sync"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// defaultWebchatCap bounds the per-conversation reply buffer so a never-polling
// browser cannot grow it without limit.
const defaultWebchatCap = 200

// WebchatAdapter is an in-process channel adapter for the web console's chat
// playground (T-226). Unlike the platform adapters, it does not call out to an
// external service: an agent's outbound message destined for "webchat" is
// buffered in memory, keyed by conversation (MessageOut.PlatformID), and the
// browser polls for it via the API. It is the delivery half of feeding the normal
// router/delivery path; the inbound half is the API handler calling the router.
type WebchatAdapter struct {
	AdapterName string
	cap         int

	mu  sync.Mutex
	buf map[string][]WebchatMessage
}

// WebchatMessage is one buffered agent reply, as the browser renders it.
type WebchatMessage struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversationId"`
	ThreadID       string `json:"threadId,omitempty"`
	Content        string `json:"content"`
	Timestamp      string `json:"timestamp"`
}

// NewWebchatAdapter constructs a WebchatAdapter. name defaults to "webchat".
func NewWebchatAdapter(name string) *WebchatAdapter {
	if name == "" {
		name = "webchat"
	}
	return &WebchatAdapter{AdapterName: name, cap: defaultWebchatCap, buf: make(map[string][]WebchatMessage)}
}

// Name returns the adapter name.
func (a *WebchatAdapter) Name() string { return a.AdapterName }

// Deliver buffers the agent's outbound message for the browser to poll. The
// conversation is MessageOut.PlatformID; a message without one is dropped (there
// is no browser to route it to). The returned platform id is the local message id.
func (a *WebchatAdapter) Deliver(_ context.Context, msg contract.MessageOut) (string, error) {
	conv := ""
	if msg.PlatformID != nil {
		conv = *msg.PlatformID
	}
	if conv == "" {
		return string(msg.ID), nil
	}
	wm := WebchatMessage{
		ID:             string(msg.ID),
		ConversationID: conv,
		Content:        msg.Content,
		Timestamp:      msg.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if msg.ThreadID != nil {
		wm.ThreadID = *msg.ThreadID
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	q := append(a.buf[conv], wm)
	if len(q) > a.cap {
		q = q[len(q)-a.cap:]
	}
	a.buf[conv] = q
	return string(msg.ID), nil
}

// Drain returns and clears the buffered replies for a conversation, so the
// browser polls incrementally. It never returns nil (an empty slice for none).
func (a *WebchatAdapter) Drain(conversationID string) []WebchatMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	q := a.buf[conversationID]
	delete(a.buf, conversationID)
	if q == nil {
		return []WebchatMessage{}
	}
	return q
}
