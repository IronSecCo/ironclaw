// OWNER: AGENT1

package delivery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func TestAuthorizeSystemAction(t *testing.T) {
	cases := []struct {
		action     string
		wantKind   contract.ChangeKind
		privileged bool
	}{
		{"set_persona", contract.ChangePersona, true},
		{"install_packages", contract.ChangePackages, true},
		{"script", contract.ChangePermissions, true}, // RCE path is gated, never run
		{"exec", contract.ChangePermissions, true},
		{"totally_unknown", contract.ChangePermissions, true}, // unknown => gated
		{"typing", "", false},
		{"noop", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		kind, priv := authorizeSystemAction(c.action)
		if priv != c.privileged || (priv && kind != c.wantKind) {
			t.Errorf("authorizeSystemAction(%q) = (%q,%v), want (%q,%v)", c.action, kind, priv, c.wantKind, c.privileged)
		}
	}
}

func TestParseSystemAction(t *testing.T) {
	if got := parseSystemAction(`{"action":"install_packages","pkg":"x"}`); got != "install_packages" {
		t.Fatalf("json parse = %q", got)
	}
	if got := parseSystemAction("  typing  "); got != "typing" {
		t.Fatalf("bare parse = %q", got)
	}
}

// newTestDelivery builds a Delivery with a fake adapter, a real registry with one
// session, and a sandbox-side outbound writer over the same shared store the host
// reads. The returned writer lets a test enqueue messages as the sandbox would.
func newTestDelivery(t *testing.T) (*Delivery, *channels.FakeAdapter, registry.Registry, registry.Session, *queue.MemOutbound) {
	t.Helper()
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	sess, _ := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared)

	// One shared store: the host reads it, the test writes it as the sandbox.
	st := queue.NewMemStore()
	hostView := queue.NewMemOutbound(st)
	sandboxWriter := queue.NewMemOutbound(st)

	channelReg := channels.NewRegistry()
	adapter := channels.NewFakeAdapter("fake")
	if err := channelReg.Register(adapter); err != nil {
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
	d := New(channelReg, gw, reg, factory)
	return d, adapter, reg, sess, sandboxWriter
}

func TestNormalDelivery(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	ct, pid := "fake", "C1"
	if err := w.WriteMessageOut(contract.MessageOut{ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adapter.Delivered(); len(got) != 1 || got[0].ID != "o1" {
		t.Fatalf("expected one delivered message, got %+v", got)
	}
}

func TestDeliveryDedup(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	ct, pid := "fake", "C1"
	w.WriteMessageOut(contract.MessageOut{ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: "hi"})
	// Two polls must not double-send.
	d.Poll(context.Background())
	d.Poll(context.Background())
	if got := adapter.Delivered(); len(got) != 1 {
		t.Fatalf("dedup failed: delivered %d times", len(got))
	}
	if d.DeliveredCount() != 1 {
		t.Fatalf("delivered set size = %d, want 1", d.DeliveredCount())
	}
}

func TestSystemActionReauthNotExecuted(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	// A privileged system action: it must NOT be delivered as a normal message and
	// must NOT be auto-applied (the gateway's AlwaysRequireHuman holds it pending).
	w.WriteMessageOut(contract.MessageOut{ID: "sys1", Seq: 1, Kind: contract.KindSystem, Content: `{"action":"install_packages"}`})
	if err := d.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("privileged system action must not be delivered to the channel, got %+v", got)
	}
	// The change is pending a human, never applied. Submit runs in a goroutine, so
	// poll until it lands.
	if !waitPending(d, 1) {
		t.Fatal("expected one pending gateway change")
	}
	pending, _ := d.gw.Pending()
	if pending[0].Kind != contract.ChangePackages {
		t.Fatalf("pending change kind = %q, want packages", pending[0].Kind)
	}
}

// TestCapabilityChangeCarriesStructuredPayload verifies the cross-agent seam:
// the sandbox emits {"action":"<kind>","payload":<obj>,"reason":...} and the host
// must route it to a gateway ChangeRequest whose Kind is correct AND whose After
// is the STRUCTURED payload (so verifiers/approver see the real config, not an
// opaque blob).
func TestCapabilityChangeCarriesStructuredPayload(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	content := `{"action":"packages","payload":{"npm":["left-pad"]},"reason":"need it"}`
	if err := w.WriteMessageOut(contract.MessageOut{ID: "sys-cap", Seq: 1, Kind: contract.KindSystem, Content: content}); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("capability change must not be delivered to a channel, got %+v", got)
	}
	if !waitPending(d, 1) {
		t.Fatal("expected one pending gateway change")
	}
	pending, _ := d.gw.Pending()
	if pending[0].Kind != contract.ChangePackages {
		t.Fatalf("kind = %q, want packages", pending[0].Kind)
	}
	var got struct {
		NPM []string `json:"npm"`
	}
	if err := json.Unmarshal(pending[0].After, &got); err != nil {
		t.Fatalf("After is not the structured payload: %v (After=%s)", err, pending[0].After)
	}
	if len(got.NPM) != 1 || got.NPM[0] != "left-pad" {
		t.Fatalf("payload not threaded into After: %s", pending[0].After)
	}
}

func TestInformationalSystemActionDelivered(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	ct, pid := "fake", "C1"
	w.WriteMessageOut(contract.MessageOut{ID: "sys-typing", Seq: 1, Kind: contract.KindSystem, ChannelType: &ct, PlatformID: &pid, Content: "typing"})
	d.Poll(context.Background())
	if got := adapter.Delivered(); len(got) != 1 {
		t.Fatalf("informational system action should deliver, got %+v", got)
	}
}

func TestDestinationPermissionEnforced(t *testing.T) {
	d, adapter, reg, sess, w := newTestDelivery(t)
	// Target a DIFFERENT chat than the session's origin and don't allow it.
	otherCT, otherPID := "fake", "OTHER"
	w.WriteMessageOut(contract.MessageOut{ID: "o1", Seq: 1, Kind: contract.KindChat, ChannelType: &otherCT, PlatformID: &otherPID, Content: "leak"})
	if err := d.Poll(context.Background()); err == nil {
		t.Fatal("expected a destination-permission error for an unknown destination")
	}
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("disallowed destination must not be delivered, got %+v", got)
	}
	// Now allow the destination and a fresh delivery succeeds.
	reg.AddDestination(sess.AgentGroupID, otherCT, otherPID)
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("expected delivery after allow, got %v", err)
	}
	if got := adapter.Delivered(); len(got) != 1 {
		t.Fatalf("expected one delivered message after allow, got %+v", got)
	}
}

func TestScheduleTaskEnqueuesFutureInbound(t *testing.T) {
	d, adapter, _, sess, w := newTestDelivery(t)

	// Wire an inbound writer over a fresh shared store so we can read back what the
	// schedule_task action enqueued.
	inStore := queue.NewMemStore()
	hostInbound := queue.NewMemInbound(inStore)
	readView := queue.NewMemInbound(inStore)
	d.WithInboundWriter(func(id contract.SessionID) (contract.InboundWriter, error) {
		if id == sess.ID {
			return hostInbound, nil
		}
		return queue.NewMemInbound(queue.NewMemStore()), nil
	})

	runAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	body := `{"action":"schedule_task","prompt":"run the daily report","run_at":"` + runAt + `","recurrence":"daily"}`
	if err := w.WriteMessageOut(contract.MessageOut{ID: "s1", Seq: 1, Kind: contract.KindSystem, Content: body}); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	// schedule_task must NOT have delivered anything to a channel.
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("schedule_task must not deliver to a channel, got %+v", got)
	}
	// It must have enqueued exactly one future inbound message carrying the prompt.
	msgs, _ := readView.PendingMessages(true)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 enqueued inbound message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Content != "run the daily report" {
		t.Fatalf("enqueued prompt = %q", m.Content)
	}
	if m.ProcessAfter == nil {
		t.Fatal("enqueued message must have a ProcessAfter time")
	}
	if m.Recurrence == nil || *m.Recurrence != "daily" {
		t.Fatalf("enqueued message recurrence = %v, want daily", m.Recurrence)
	}
	if m.Seq%2 != 0 {
		t.Fatalf("enqueued message seq must be even (host parity), got %d", m.Seq)
	}
}

func TestScheduleTaskRejectsEmptyPrompt(t *testing.T) {
	d, _, _, sess, w := newTestDelivery(t)
	inStore := queue.NewMemStore()
	hostInbound := queue.NewMemInbound(inStore)
	d.WithInboundWriter(func(id contract.SessionID) (contract.InboundWriter, error) {
		_ = sess
		return hostInbound, nil
	})
	w.WriteMessageOut(contract.MessageOut{ID: "s1", Seq: 1, Kind: contract.KindSystem, Content: `{"action":"schedule_task","prompt":""}`})
	if err := d.Poll(context.Background()); err == nil {
		t.Fatal("expected schedule_task with empty prompt to error")
	}
}

func TestScheduleTaskRefusedWithoutInboundWriter(t *testing.T) {
	d, _, _, _, w := newTestDelivery(t)
	// No WithInboundWriter call: schedule_task must be refused, not silently dropped.
	w.WriteMessageOut(contract.MessageOut{ID: "s1", Seq: 1, Kind: contract.KindSystem, Content: `{"action":"schedule_task","prompt":"x"}`})
	if err := d.Poll(context.Background()); err == nil {
		t.Fatal("expected schedule_task to be refused without an inbound writer")
	}
}

func TestScheduleTaskIsNonPrivileged(t *testing.T) {
	// schedule_task must authorize as non-privileged (it only enqueues a prompt).
	if _, priv := authorizeSystemAction("schedule_task"); priv {
		t.Fatal("schedule_task must be non-privileged")
	}
}

// waitPending polls the gateway's pending list until it reaches want or times out.
func waitPending(d *Delivery, want int) bool {
	for i := 0; i < 500; i++ {
		if p, _ := d.gw.Pending(); len(p) == want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
