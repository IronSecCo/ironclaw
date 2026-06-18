package parity

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

func strptr(s string) *string { return &s }

// TestSessionResolution is the behavioral contract for session partitioning:
// shared collapses threads, per-thread splits by thread, agent-shared collapses
// across chats and threads (still partitioned by agent group), and FindSession
// never creates.
func TestSessionResolution(t *testing.T) {
	const ag = contract.AgentGroupID("g1")
	const mg = contract.MessagingGroupID("m1")

	t.Run("shared ignores thread", func(t *testing.T) {
		r := registry.NewMemRegistry()
		s1, err := r.ResolveSession(ag, mg, strptr("t1"), contract.SessionShared)
		if err != nil {
			t.Fatal(err)
		}
		s2, err := r.ResolveSession(ag, mg, strptr("t2"), contract.SessionShared)
		if err != nil {
			t.Fatal(err)
		}
		if s1.ID != s2.ID {
			t.Fatalf("shared mode must collapse threads to one session: %q vs %q", s1.ID, s2.ID)
		}
	})

	t.Run("per-thread splits by thread", func(t *testing.T) {
		r := registry.NewMemRegistry()
		s1, _ := r.ResolveSession(ag, mg, strptr("t1"), contract.SessionPerThread)
		s2, _ := r.ResolveSession(ag, mg, strptr("t2"), contract.SessionPerThread)
		if s1.ID == s2.ID {
			t.Fatalf("per-thread mode must split distinct threads")
		}
		// The same thread resolves back to the same session.
		s1b, _ := r.ResolveSession(ag, mg, strptr("t1"), contract.SessionPerThread)
		if s1b.ID != s1.ID {
			t.Fatalf("same thread must resolve the same session: %q vs %q", s1b.ID, s1.ID)
		}
		if s1.ThreadID == nil || *s1.ThreadID != "t1" {
			t.Fatalf("per-thread session must record its thread id: %+v", s1.ThreadID)
		}
	})

	t.Run("agent-shared collapses across chats and threads", func(t *testing.T) {
		r := registry.NewMemRegistry()
		s1, _ := r.ResolveSession(ag, "m1", strptr("t1"), contract.SessionAgentShared)
		s2, _ := r.ResolveSession(ag, "m2", strptr("t2"), contract.SessionAgentShared)
		if s1.ID != s2.ID {
			t.Fatalf("agent-shared must collapse across messaging groups + threads: %q vs %q", s1.ID, s2.ID)
		}
		// Still partitioned by agent group.
		s3, _ := r.ResolveSession("g2", "m1", strptr("t1"), contract.SessionAgentShared)
		if s3.ID == s1.ID {
			t.Fatalf("agent-shared must still partition by agent group")
		}
	})

	t.Run("find does not create", func(t *testing.T) {
		r := registry.NewMemRegistry()
		if _, ok := r.FindSession(ag, mg, nil, contract.SessionShared); ok {
			t.Fatal("FindSession must not find a non-existent session")
		}
		if _, err := r.ResolveSession(ag, mg, nil, contract.SessionShared); err != nil {
			t.Fatal(err)
		}
		if _, ok := r.FindSession(ag, mg, nil, contract.SessionShared); !ok {
			t.Fatal("FindSession must find the resolved session")
		}
	})
}
