// OWNER: T-013a

package parity

import (
	"context"
	"sync"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/queue"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/router"
	"github.com/nivardsec/ironclaw/internal/host/types"
)

// parityWaker is the fake-sandbox wake seam the routing/engage specs observe in
// place of a real isolation launch.
type parityWaker struct {
	mu    sync.Mutex
	woken []contract.SessionID
}

func (w *parityWaker) Wake(id contract.SessionID) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.woken = append(w.woken, id)
	return nil
}

func (w *parityWaker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.woken)
}

// newParityRouter wires a real Router over a MemRegistry with per-session
// in-memory inbound writers and a fake waker — the black-box seam the routing and
// engage parity specs drive.
func newParityRouter(t *testing.T, reg registry.Registry) (*router.Router, *parityWaker) {
	t.Helper()
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
	waker := &parityWaker{}
	return router.New(reg, factory, waker), waker
}

// TestRoutingFanOut is the behavioral contract: one inbound platform message fans
// out to every wired agent group, in descending wiring priority, each into its own
// session, waking each engaged sandbox once.
func TestRoutingFanOut(t *testing.T) {
	reg := registry.NewMemRegistry()
	if err := reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	mg, err := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	if err != nil {
		t.Fatal(err)
	}
	// Two wirings on the same chat, different agent groups + priorities.
	if err := reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared, Priority: 2}); err != nil {
		t.Fatal(err)
	}
	if err := reg.PutWiring(registry.Wiring{ID: "w2", MessagingGroupID: mg.ID, AgentGroupID: "g2", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared, Priority: 1}); err != nil {
		t.Fatal(err)
	}

	r, waker := newParityRouter(t, reg)
	outcomes, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("want one outcome per wiring, got %d: %+v", len(outcomes), outcomes)
	}
	// Descending priority order.
	if outcomes[0].AgentGroupID != "g1" || outcomes[1].AgentGroupID != "g2" {
		t.Fatalf("fan-out not in priority order: %+v", outcomes)
	}
	for _, o := range outcomes {
		if !o.Engaged || o.SessionID == "" {
			t.Fatalf("each wired agent should engage with its own session: %+v", o)
		}
	}
	if outcomes[0].SessionID == outcomes[1].SessionID {
		t.Fatalf("distinct agent groups must resolve distinct sessions: %+v", outcomes)
	}
	if waker.count() != 2 {
		t.Fatalf("each engaged sandbox should wake once: got %d wakes", waker.count())
	}
}

// TestRoutingIdentityNamespacing is the behavioral contract: the sender identity
// is namespaced channelType + ":" + handle, and a handle is never trusted to carry
// its own colon — a colon-stuffed handle cannot assume a different (channel,
// handle) identity.
func TestRoutingIdentityNamespacing(t *testing.T) {
	reg := registry.NewMemRegistry()
	// A privileged principal exists under the literal id "evil:owner".
	if err := reg.GrantRole(registry.Role{UserID: "evil:owner", Role: registry.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	if err := reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	mg, err := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.PutWiring(registry.Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1", EngageMode: contract.EngagePattern, EngagePattern: ".", SessionMode: contract.SessionShared}); err != nil {
		t.Fatal(err)
	}
	r, _ := newParityRouter(t, reg)

	// Spoof attempt: handle "evil:owner" on channel "slack" namespaces to
	// "slack:evil" (everything from the first colon is stripped), NOT the
	// privileged "evil:owner" — so access is denied and the message does not engage.
	spoof, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "evil:owner", Text: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(spoof) != 1 || spoof[0].Engaged || spoof[0].SessionID != "" {
		t.Fatalf("colon-stuffed handle must not assume another identity: %+v", spoof)
	}

	// The legitimate handle resolves to "slack:alice" and engages.
	ok, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ok) != 1 || !ok[0].Engaged {
		t.Fatalf("known namespaced sender should engage: %+v", ok)
	}
}
