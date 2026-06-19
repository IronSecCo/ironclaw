package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/channels"
	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

// newTelegramDelivery wires a Delivery whose channel registry holds a REAL
// TelegramAdapter pointed at the given httptest base URL, with one registry
// session whose origin messaging group is telegram/C1 (so destinationAllowed
// permits the origin chat). It returns the host-side Delivery plus a sandbox-side
// outbound writer over the same shared store, mirroring newTestDelivery but with a
// production adapter rather than the FakeAdapter. This exercises the full host
// delivery path: outbound queue -> Delivery.Poll -> gateway re-auth -> adapter ->
// platform HTTP API.
func newTelegramDelivery(t *testing.T, baseURL string) (*Delivery, registry.Session, *queue.MemOutbound) {
	t.Helper()
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("telegram", "C1", "", true, contract.UnknownPublic)
	sess, _ := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared)

	st := queue.NewMemStore()
	hostView := queue.NewMemOutbound(st)
	sandboxWriter := queue.NewMemOutbound(st)

	channelReg := channels.NewRegistry()
	tg := channels.NewTelegramAdapter("telegram", "TESTTOKEN")
	tg.BaseURL = baseURL
	if err := channelReg.Register(tg); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	factory := func(id contract.SessionID) (contract.OutboundReader, error) {
		if id == sess.ID {
			return hostView, nil
		}
		return queue.NewMemOutbound(queue.NewMemStore()), nil
	}
	return New(channelReg, gw, reg, factory), sess, sandboxWriter
}

// TestTelegramDeliveryHappyPath drives an agent's outbound chat all the way to the
// Telegram Bot API through the real Delivery loop and the real TelegramAdapter,
// asserting the platform received a well-formed sendMessage and Delivery recorded
// the send exactly once.
func TestTelegramDeliveryHappyPath(t *testing.T) {
	var hits int32
	var gotPath, gotCT string
	var gotBody struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":4242}}`)
	}))
	defer srv.Close()

	d, _, w := newTelegramDelivery(t, srv.URL)
	ct, pid := "telegram", "C1"
	if err := w.WriteMessageOut(contract.MessageOut{
		ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "hello world",
	}); err != nil {
		t.Fatal(err)
	}

	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("telegram API hit %d times, want 1", got)
	}
	if gotPath != "/botTESTTOKEN/sendMessage" {
		t.Fatalf("request path = %q, want /botTESTTOKEN/sendMessage", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody.ChatID != "C1" || gotBody.Text != "hello world" {
		t.Errorf("upstream body = %+v", gotBody)
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("delivered set size = %d, want 1", d.DeliveredCount())
	}

	// A second poll must not re-send (dedup), even though the message is still due.
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("second Poll: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("telegram API hit %d times after re-poll, want 1 (dedup)", got)
	}
}

// TestTelegramDeliveryFailurePathRetries asserts the failure path is durable: when
// the platform API rejects the send, Delivery.Poll surfaces the error and does NOT
// mark the message delivered, so a later poll re-attempts it. This proves a
// transient platform outage cannot silently drop an agent reply.
func TestTelegramDeliveryFailurePathRetries(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if fail.Load() {
			// Telegram signals logical failure with ok:false (and often a non-2xx).
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"bad gateway"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":7}}`)
	}))
	defer srv.Close()

	d, _, w := newTelegramDelivery(t, srv.URL)
	ct, pid := "telegram", "C1"
	if err := w.WriteMessageOut(contract.MessageOut{
		ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "retry me",
	}); err != nil {
		t.Fatal(err)
	}

	// First poll: the platform fails. Poll must report the error and leave the
	// message undelivered (not in the dedup set) so it can be retried.
	err := d.Poll(context.Background())
	if err == nil {
		t.Fatal("expected Poll to surface the platform delivery error")
	}
	if !strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("error should carry the platform description, got %v", err)
	}
	if d.DeliveredCount() != 0 {
		t.Fatalf("failed delivery must not be marked delivered, set size = %d", d.DeliveredCount())
	}

	// Platform recovers; a subsequent poll delivers the same message.
	fail.Store(false)
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("Poll after recovery: %v", err)
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("message must be delivered after recovery, set size = %d", d.DeliveredCount())
	}
	// 1 failed attempt + 1 successful attempt = 2 hits exactly (no double-send).
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("telegram API hit %d times, want 2 (one failure, one success)", got)
	}
}
