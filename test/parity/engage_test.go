package parity

import (
	"context"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/host/types"
)

// engageReg builds a registry with one drop-policy wiring of the given engage
// mode/pattern, plus owner access for slack:alice so the access gate passes.
func engageReg(t *testing.T, mode contract.EngageMode, pattern string) (*registry.MemRegistry, contract.MessagingGroupID) {
	t.Helper()
	reg := registry.NewMemRegistry()
	if err := reg.GrantRole(registry.Role{UserID: "slack:alice", Role: registry.RoleOwner}); err != nil {
		t.Fatal(err)
	}
	mg, err := reg.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownPublic)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.PutWiring(registry.Wiring{
		ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "g1",
		EngageMode: mode, EngagePattern: pattern,
		IgnoredMessagePolicy: contract.IgnoreDrop, SessionMode: contract.SessionShared,
	}); err != nil {
		t.Fatal(err)
	}
	return reg, mg.ID
}

func send(t *testing.T, r interface {
	RouteInbound(context.Context, types.InboundEvent) ([]types.RoutingOutcome, error)
}, text string, mentioned bool) types.RoutingOutcome {
	t.Helper()
	out, err := r.RouteInbound(context.Background(), types.InboundEvent{
		ChannelType: "slack", PlatformID: "C1", SenderHandle: "alice", Text: text, Mentioned: mentioned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want exactly one outcome, got %d: %+v", len(out), out)
	}
	return out[0]
}

// TestEngageModes is the behavioral contract for the three engage modes: pattern
// matches text, mention requires an @-mention, and mention-sticky keeps engaging
// follow-ups once a session exists.
func TestEngageModes(t *testing.T) {
	t.Run("pattern", func(t *testing.T) {
		reg, _ := engageReg(t, contract.EngagePattern, "deploy")
		r, waker := newParityRouter(t, reg)

		if o := send(t, r, "please deploy now", false); !o.Engaged {
			t.Fatalf("pattern match should engage: %+v", o)
		}
		// Drop policy: a non-match neither engages nor resolves a session.
		if o := send(t, r, "hello there", false); o.Engaged || o.SessionID != "" {
			t.Fatalf("pattern miss (drop) should not engage or resolve a session: %+v", o)
		}
		if waker.count() != 1 {
			t.Fatalf("only the engaged message wakes: got %d", waker.count())
		}
	})

	t.Run("mention", func(t *testing.T) {
		reg, _ := engageReg(t, contract.EngageMention, "")
		r, waker := newParityRouter(t, reg)

		if o := send(t, r, "hey @agent", true); !o.Engaged {
			t.Fatalf("mention should engage: %+v", o)
		}
		if o := send(t, r, "just chatter", false); o.Engaged || o.SessionID != "" {
			t.Fatalf("non-mention (drop) should not engage: %+v", o)
		}
		if waker.count() != 1 {
			t.Fatalf("only the mention wakes: got %d", waker.count())
		}
	})

	t.Run("mention-sticky", func(t *testing.T) {
		reg, _ := engageReg(t, contract.EngageMentionSticky, "")
		r, waker := newParityRouter(t, reg)

		// Before any session exists, a non-mention is dropped (stickiness needs a
		// prior engaged session).
		if o := send(t, r, "cold chatter", false); o.Engaged || o.SessionID != "" {
			t.Fatalf("sticky must not engage without a prior session: %+v", o)
		}
		// A mention engages and opens the session.
		first := send(t, r, "@agent start", true)
		if !first.Engaged || first.SessionID == "" {
			t.Fatalf("mention should engage and open a session: %+v", first)
		}
		// A follow-up WITHOUT a fresh mention still engages — sticky continuation on
		// the existing session.
		followUp := send(t, r, "more context", false)
		if !followUp.Engaged || followUp.SessionID != first.SessionID {
			t.Fatalf("sticky follow-up should re-engage the same session: %+v", followUp)
		}
		if waker.count() != 2 {
			t.Fatalf("the mention and the sticky follow-up each wake: got %d", waker.count())
		}
	})
}
