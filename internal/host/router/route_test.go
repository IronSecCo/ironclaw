// OWNER: AGENT1

package router

import (
	"context"
	"sync"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/types"
)

// fakeWaker records the sessions it was asked to wake.
type fakeWaker struct {
	mu    sync.Mutex
	woken []contract.SessionID
}

func (f *fakeWaker) Wake(id contract.SessionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.woken = append(f.woken, id)
	return nil
}

func (f *fakeWaker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.woken)
}

// newTestRouter wires a Router over a MemRegistry and a per-session in-memory
// inbound writer (one shared memStore per session ID). It returns the router, the
// registry, the waker, and a function to read the messages written to a session.
func newTestRouter(t *testing.T, reg registry.Registry) (*Router, *fakeWaker, func(contract.SessionID) []contract.MessageIn) {
	t.Helper()
	stores := map[contract.SessionID]*queue.MemInbound{}
	var mu sync.Mutex
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
	waker := &fakeWaker{}
	r := New(reg, factory, waker)
	read := func(id contract.SessionID) []contract.MessageIn {
		mu.Lock()
		defer mu.Unlock()
		in, ok := stores[id]
		if !ok {
			return nil
		}
		msgs, _ := in.PendingMessages(true)
		return msgs
	}
	return r, waker, read
}

func TestRouteFanOutToMultipleAgents(t *testing.T) {
	reg := registry.NewMemRegistry()
	// Two agent groups wired to the same messaging group, both pattern match-all,
	// both sender-all, and both with an owner so access passes.
	reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner})

	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared, Priority: 2})
	reg.PutWiring(registry.Wiring{ID: "w2", MessagingGroupID: mg.ID, AgentGroupID: "g2", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared, Priority: 1})

	r, waker, read := newTestRouter(t, reg)
	outcomes, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("want 2 outcomes (one per wiring), got %d: %+v", len(outcomes), outcomes)
	}
	// Highest priority first.
	if outcomes[0].AgentGroupID != "g1" || outcomes[1].AgentGroupID != "g2" {
		t.Fatalf("fan-out not in priority order: %+v", outcomes)
	}
	for _, o := range outcomes {
		if !o.Engaged {
			t.Fatalf("expected engaged outcome, got %+v", o)
		}
		if msgs := read(o.SessionID); len(msgs) != 1 || msgs[0].Trigger != 1 {
			t.Fatalf("expected one trigger=1 message for %s, got %+v", o.SessionID, msgs)
		}
	}
	if waker.count() != 2 {
		t.Fatalf("expected 2 wakes, got %d", waker.count())
	}
}

func TestRouteAccessDeny(t *testing.T) {
	reg := registry.NewMemRegistry()
	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownStrict)
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared})

	r, waker, _ := newTestRouter(t, reg)
	// Sender has no role/membership — access denied.
	outcomes, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "stranger", Text: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Engaged {
		t.Fatalf("expected one non-engaged (denied) outcome, got %+v", outcomes)
	}
	if outcomes[0].SessionID != "" {
		t.Fatalf("denied outcome should not have a session, got %q", outcomes[0].SessionID)
	}
	if waker.count() != 0 {
		t.Fatalf("denied message should not wake, got %d", waker.count())
	}
}

func TestRouteAccumulatePath(t *testing.T) {
	reg := registry.NewMemRegistry()
	reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner})
	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	// Mention mode: a non-mention message does NOT engage, but accumulate policy
	// still records it with trigger=0.
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngageMention, IgnoredMessagePolicy: contract.IgnoreAccumulate, SessionMode: contract.SessionShared})

	r, waker, read := newTestRouter(t, reg)
	outcomes, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "chatter", Mentioned: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Engaged {
		t.Fatalf("expected one accumulated (non-engaged) outcome, got %+v", outcomes)
	}
	if outcomes[0].SessionID == "" {
		t.Fatal("accumulate should resolve a session")
	}
	msgs := read(outcomes[0].SessionID)
	if len(msgs) != 1 || msgs[0].Trigger != 0 {
		t.Fatalf("expected one trigger=0 message, got %+v", msgs)
	}
	if waker.count() != 0 {
		t.Fatalf("accumulate must not wake, got %d", waker.count())
	}
}

func TestRouteDropPath(t *testing.T) {
	reg := registry.NewMemRegistry()
	reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner})
	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	// Mention mode + drop policy: non-mention is dropped, no session, no message.
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngageMention, IgnoredMessagePolicy: contract.IgnoreDrop, SessionMode: contract.SessionShared})

	r, _, read := newTestRouter(t, reg)
	outcomes, _ := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "chatter",
	})
	if len(outcomes) != 1 || outcomes[0].SessionID != "" {
		t.Fatalf("drop should not resolve a session: %+v", outcomes)
	}
	if msgs := read(outcomes[0].SessionID); len(msgs) != 0 {
		t.Fatalf("drop should write no message, got %+v", msgs)
	}
}

func TestRoutePerThreadSessionCreation(t *testing.T) {
	reg := registry.NewMemRegistry()
	reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner})
	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionPerThread})

	r, _, _ := newTestRouter(t, reg)
	t1, t2 := "thread-1", "thread-2"
	o1, _ := r.RouteInbound(context.Background(), types.InboundEvent{ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "a", ThreadID: &t1})
	o2, _ := r.RouteInbound(context.Background(), types.InboundEvent{ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "b", ThreadID: &t2})
	o1b, _ := r.RouteInbound(context.Background(), types.InboundEvent{ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "c", ThreadID: &t1})

	if o1[0].SessionID == o2[0].SessionID {
		t.Fatal("different threads should get different sessions")
	}
	if o1[0].SessionID != o1b[0].SessionID {
		t.Fatal("same thread should reuse the session")
	}
}

func TestRouteSenderScopeKnownGate(t *testing.T) {
	reg := registry.NewMemRegistry()
	// Known sender (member) and unknown sender; wiring is known-only.
	reg.AddMember(registry.Member{UserID: "slack:known", AgentGroupID: "g1"})
	mg, _ := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SenderScope: contract.SenderKnown, SessionMode: contract.SessionShared})

	r, waker, _ := newTestRouter(t, reg)
	// Known sender engages.
	ok, _ := r.RouteInbound(context.Background(), types.InboundEvent{ChannelType: "slack", PlatformID: "C1", SenderHandle: "known", Text: "hi"})
	if len(ok) != 1 || !ok[0].Engaged {
		t.Fatalf("known sender should engage: %+v", ok)
	}
	// Unknown sender is denied by access (no role/member) before sender-scope even
	// matters — assert it does not engage and does not wake.
	un, _ := r.RouteInbound(context.Background(), types.InboundEvent{ChannelType: "slack", PlatformID: "C1", SenderHandle: "ghost", Text: "hi"})
	if len(un) != 1 || un[0].Engaged {
		t.Fatalf("unknown sender should not engage: %+v", un)
	}
	if waker.count() != 1 {
		t.Fatalf("only the known sender should wake, got %d", waker.count())
	}
}
