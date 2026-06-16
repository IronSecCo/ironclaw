// OWNER: AGENT1

package delivery

import (
	"context"
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
