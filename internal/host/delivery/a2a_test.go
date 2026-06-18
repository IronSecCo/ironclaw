package delivery

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// newA2ATestDelivery builds a Delivery with a SENDER session (group "g1") and a
// TARGET session (group "g2"), an inbound writer over a per-session store the test
// reads back, and the sandbox-side outbound writer for the sender.
func newA2ATestDelivery(t *testing.T) (d *Delivery, reg registry.Registry, sender, target registry.Session, senderOut *queue.MemOutbound, targetInbound *queue.MemInbound) {
	t.Helper()
	reg = registry.NewMemRegistry()
	mgA, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	mgB, _ := reg.GetOrCreateMessagingGroup("fake", "C2", "", true, contract.UnknownPublic)
	sender, _ = reg.ResolveSession("g1", mgA.ID, nil, contract.SessionShared)
	target, _ = reg.ResolveSession("g2", mgB.ID, nil, contract.SessionShared)

	outStore := queue.NewMemStore()
	hostOut := queue.NewMemOutbound(outStore)
	senderOut = queue.NewMemOutbound(outStore)

	inStore := queue.NewMemStore()
	hostInbound := queue.NewMemInbound(inStore)
	targetInbound = queue.NewMemInbound(inStore)

	channelReg := channels.NewRegistry()
	adapter := channels.NewFakeAdapter("fake")
	if err := channelReg.Register(adapter); err != nil {
		t.Fatal(err)
	}
	gw := gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())

	d = New(channelReg, gw, reg, func(id contract.SessionID) (contract.OutboundReader, error) {
		if id == sender.ID {
			return hostOut, nil
		}
		return queue.NewMemOutbound(queue.NewMemStore()), nil
	})
	d.WithInboundWriter(func(id contract.SessionID) (contract.InboundWriter, error) {
		if id == target.ID {
			return hostInbound, nil
		}
		return queue.NewMemInbound(queue.NewMemStore()), nil
	})
	return d, reg, sender, target, senderOut, targetInbound
}

// a2aMsg builds a sandbox outbound addressed to the agent sentinel channel. seq is
// odd (sandbox parity) and must be unique within a store.
func a2aMsg(id string, seq int64, targetGroup contract.AgentGroupID, content string) contract.MessageOut {
	ct := agentChannel
	pid := string(targetGroup)
	return contract.MessageOut{ID: contract.MessageID(id), Seq: seq, Kind: contract.KindChat, ChannelType: &ct, PlatformID: &pid, Content: content}
}

func TestA2ARoutesToTargetInbound(t *testing.T) {
	d, reg, sender, target, w, targetInbound := newA2ATestDelivery(t)
	// Grant the sender an agent-destination to the target (deny-by-default).
	if err := reg.AddDestination(sender.AgentGroupID, agentChannel, string(target.AgentGroupID)); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteMessageOut(a2aMsg("a1", 1, target.AgentGroupID, "hello peer")); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	msgs, _ := targetInbound.PendingMessages(true)
	if len(msgs) != 1 {
		t.Fatalf("target inbound = %d messages, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Content != "hello peer" {
		t.Fatalf("content = %q", m.Content)
	}
	if m.SourceSessionID == nil || *m.SourceSessionID != string(sender.ID) {
		t.Fatalf("provenance SourceSessionID = %v, want %q", m.SourceSessionID, sender.ID)
	}
	if m.Trigger != 1 {
		t.Fatalf("a2a message must engage the target (trigger=1), got %d", m.Trigger)
	}
	if m.Seq%2 != 0 {
		t.Fatalf("host-written inbound seq must be even, got %d", m.Seq)
	}
}

func TestA2ADeniedWithoutGrant(t *testing.T) {
	d, _, _, target, w, targetInbound := newA2ATestDelivery(t)
	// No AddDestination grant: a2a must be refused.
	if err := w.WriteMessageOut(a2aMsg("a1", 1, target.AgentGroupID, "sneak")); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err == nil {
		t.Fatal("expected a2a without a grant to be refused")
	}
	if msgs, _ := targetInbound.PendingMessages(true); len(msgs) != 0 {
		t.Fatalf("denied a2a must not enqueue to the target, got %d", len(msgs))
	}
}

func TestA2ARefusedWithoutInboundWriter(t *testing.T) {
	// Build a delivery WITHOUT an inbound writer; a2a must be refused, not dropped.
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("fake", "C1", "", true, contract.UnknownPublic)
	sender, _ := reg.ResolveSession("g1", mg.ID, nil, contract.SessionShared)
	_ = reg.AddDestination(sender.AgentGroupID, agentChannel, "g2")

	outStore := queue.NewMemStore()
	hostOut := queue.NewMemOutbound(outStore)
	w := queue.NewMemOutbound(outStore)
	channelReg := channels.NewRegistry()
	_ = channelReg.Register(channels.NewFakeAdapter("fake"))
	gw := gateway.New(gateway.VerifierChain{gateway.AlwaysRequireHuman{}}, gateway.NewManualApprover(), gateway.NewLogApplier(), gateway.NewMemoryStore())
	d := New(channelReg, gw, reg, func(id contract.SessionID) (contract.OutboundReader, error) {
		if id == sender.ID {
			return hostOut, nil
		}
		return queue.NewMemOutbound(queue.NewMemStore()), nil
	})

	_ = w.WriteMessageOut(a2aMsg("a1", 1, "g2", "x"))
	if err := d.Poll(context.Background()); err == nil {
		t.Fatal("expected a2a to be refused without an inbound writer")
	}
}

func TestA2AHopLimitDrops(t *testing.T) {
	d, reg, sender, target, w, targetInbound := newA2ATestDelivery(t)
	_ = reg.AddDestination(sender.AgentGroupID, agentChannel, string(target.AgentGroupID))
	// Put the sender at the hop limit: a further a2a hop must be dropped (silently —
	// amplification safety must not stall the loop).
	d.mu.Lock()
	d.a2aHops[sender.ID] = d.a2aHopLimit
	d.mu.Unlock()

	if err := w.WriteMessageOut(a2aMsg("a1", 1, target.AgentGroupID, "loop?")); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("hop-limit drop must not error: %v", err)
	}
	if msgs, _ := targetInbound.PendingMessages(true); len(msgs) != 0 {
		t.Fatalf("a2a beyond hop limit must be dropped, got %d", len(msgs))
	}
}

func TestA2AQuotaDrops(t *testing.T) {
	d, reg, sender, target, w, targetInbound := newA2ATestDelivery(t)
	d.WithA2ALimits(5, 2) // 2 sends/min
	_ = reg.AddDestination(sender.AgentGroupID, agentChannel, string(target.AgentGroupID))

	for i, id := range []string{"a1", "a2", "a3"} {
		if err := w.WriteMessageOut(a2aMsg(id, int64(2*i+1), target.AgentGroupID, "msg")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	// Only the first 2 (within quota) reach the target; the 3rd is dropped.
	if msgs, _ := targetInbound.PendingMessages(true); len(msgs) != 2 {
		t.Fatalf("quota=2: target inbound = %d, want 2 (3rd dropped)", len(msgs))
	}
}

func TestCreateAgentRoutedToGateway(t *testing.T) {
	d, adapter, _, _, w := newTestDelivery(t)
	if err := w.WriteMessageOut(contract.MessageOut{
		ID: "ca1", Seq: 1, Kind: contract.KindSystem,
		Content: `{"action":"create_agent","payload":{"name":"helper"},"reason":"need help"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adapter.Delivered(); len(got) != 0 {
		t.Fatalf("create_agent must not deliver to a channel, got %+v", got)
	}
	if !waitPending(d, 1) {
		t.Fatal("expected one pending gateway change for create_agent")
	}
	pending, _ := d.gw.Pending()
	if pending[0].Kind != contract.ChangeCreateAgent {
		t.Fatalf("pending kind = %q, want create_agent", pending[0].Kind)
	}
}

func TestCreateAgentIsPrivileged(t *testing.T) {
	kind, priv := authorizeSystemAction("create_agent")
	if !priv || kind != contract.ChangeCreateAgent {
		t.Fatalf("authorizeSystemAction(create_agent) = (%q,%v), want (create_agent,true)", kind, priv)
	}
}
