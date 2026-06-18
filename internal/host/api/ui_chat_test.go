package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/router"
)

func strptr(s string) *string { return &s }

// newChatServer wires a Server with a real router (over a MemRegistry + in-memory
// inbound writers) and a webchat adapter, so the chat send path exercises the
// genuine engage/route/wake flow.
func newChatServer(t *testing.T, token string) (http.Handler, *registry.MemRegistry, *channels.WebchatAdapter, *int) {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	reg := registry.NewMemRegistry()
	if err := reg.PutAgentGroup(registry.AgentGroup{ID: "ag1", Name: "Alpha"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	stores := map[contract.SessionID]*queue.MemInbound{}
	factory := func(id contract.SessionID) (contract.InboundWriter, error) {
		mu.Lock()
		defer mu.Unlock()
		in, ok := stores[id]
		if !ok {
			in = queue.NewMemInbound(queue.NewMemStore())
			stores[id] = in
		}
		return in, nil
	}
	woke := 0
	waker := router.WakerFunc(func(contract.SessionID) error { woke++; return nil })
	r := router.New(reg, factory, waker)
	webchat := channels.NewWebchatAdapter("webchat")
	s := New(gw).WithRegistry(reg).WithChat(r, webchat)
	if token != "" {
		s = s.WithToken(token)
	}
	return s.Handler(), reg, webchat, &woke
}

func TestUIChatSendRoutesThroughRouter(t *testing.T) {
	h, reg, _, woke := newChatServer(t, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/chat/send",
		strings.NewReader(`{"agentGroupID":"ag1","text":"hello agent"}`)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: got %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ConversationID string `json:"conversationId"`
		Engaged        bool   `json:"engaged"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ConversationID != "ag1" || !resp.Engaged {
		t.Errorf("resp = %+v, want conversation ag1 + engaged", resp)
	}
	if *woke == 0 {
		t.Error("router did not wake a session — message never reached the inbound path")
	}
	_ = reg
}

func TestUIChatMessagesDrains(t *testing.T) {
	h, _, webchat, _ := newChatServer(t, "")
	// Simulate an agent reply delivered to the webchat adapter for conversation ag1.
	if _, err := webchat.Deliver(context.Background(), contract.MessageOut{ID: "r1", PlatformID: strptr("ag1"), Content: "hi back"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/chat/ag1/messages", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("messages: got %d, want 200", rec.Code)
	}
	var resp struct {
		ConversationID string                    `json:"conversationId"`
		Messages       []channels.WebchatMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].Content != "hi back" {
		t.Fatalf("messages = %+v, want one 'hi back'", resp.Messages)
	}
}

func TestUIChatValidation(t *testing.T) {
	h, _, _, _ := newChatServer(t, "")
	for _, body := range []string{
		`{"agentGroupID":"","text":"x"}`,
		`{"agentGroupID":"ag1","text":"  "}`,
		`{"agentGroupID":"ghost","text":"hi"}`, // unknown agent group -> 404
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/chat/send", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("body %s: got %d, want 400/404", body, rec.Code)
		}
	}
}

func TestUIChatUnwiredIs503(t *testing.T) {
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	h := New(gw).WithRegistry(registry.NewMemRegistry()).Handler() // no WithChat
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ui/chat/send", strings.NewReader(`{"agentGroupID":"ag1","text":"x"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unwired send: got %d, want 503", rec.Code)
	}
}

func TestUIChatRequiresToken(t *testing.T) {
	h, _, _, _ := newChatServer(t, "s3cret")
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/ui/chat/send"},
		{http.MethodGet, "/v1/ui/chat/ag1/messages"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: got %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}
