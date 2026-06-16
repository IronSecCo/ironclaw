// OWNER: AGENT1

package registry

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func ptr(s string) *string { return &s }

func TestGetOrCreateMessagingGroupIdempotent(t *testing.T) {
	r := NewMemRegistry()
	a, err := r.GetOrCreateMessagingGroup("slack", "C1", "", false, contract.UnknownStrict)
	if err != nil {
		t.Fatal(err)
	}
	if a.Instance != "slack" {
		t.Fatalf("instance default = %q, want slack", a.Instance)
	}
	// Same triple returns the same group.
	b, _ := r.GetOrCreateMessagingGroup("slack", "C1", "slack", false, contract.UnknownStrict)
	if a.ID != b.ID {
		t.Fatalf("expected same id, got %q and %q", a.ID, b.ID)
	}
	// Different instance => different group.
	c, _ := r.GetOrCreateMessagingGroup("slack", "C1", "other", false, contract.UnknownStrict)
	if c.ID == a.ID {
		t.Fatalf("different instance should yield a new group")
	}
}

func TestSessionResolutionShared(t *testing.T) {
	r := NewMemRegistry()
	ag := contract.AgentGroupID("g1")
	mg := contract.MessagingGroupID("m1")
	// Shared: thread is ignored — both resolve to one session.
	s1, _ := r.ResolveSession(ag, mg, ptr("t1"), contract.SessionShared)
	s2, _ := r.ResolveSession(ag, mg, ptr("t2"), contract.SessionShared)
	if s1.ID != s2.ID {
		t.Fatalf("shared mode should collapse threads: %q vs %q", s1.ID, s2.ID)
	}
}

func TestSessionResolutionPerThread(t *testing.T) {
	r := NewMemRegistry()
	ag := contract.AgentGroupID("g1")
	mg := contract.MessagingGroupID("m1")
	s1, _ := r.ResolveSession(ag, mg, ptr("t1"), contract.SessionPerThread)
	s2, _ := r.ResolveSession(ag, mg, ptr("t2"), contract.SessionPerThread)
	if s1.ID == s2.ID {
		t.Fatalf("per-thread mode should split threads")
	}
	// Same thread resolves to the same session.
	s1b, _ := r.ResolveSession(ag, mg, ptr("t1"), contract.SessionPerThread)
	if s1.ID != s1b.ID {
		t.Fatalf("same thread should resolve to same session")
	}
	if s1.ThreadID == nil || *s1.ThreadID != "t1" {
		t.Fatalf("per-thread session should record its thread id")
	}
}

func TestSessionResolutionAgentShared(t *testing.T) {
	r := NewMemRegistry()
	ag := contract.AgentGroupID("g1")
	// agent-shared collapses across messaging groups AND threads.
	s1, _ := r.ResolveSession(ag, "m1", ptr("t1"), contract.SessionAgentShared)
	s2, _ := r.ResolveSession(ag, "m2", ptr("t2"), contract.SessionAgentShared)
	if s1.ID != s2.ID {
		t.Fatalf("agent-shared should collapse across messaging groups: %q vs %q", s1.ID, s2.ID)
	}
	// A different agent group gets a different session.
	s3, _ := r.ResolveSession("g2", "m1", ptr("t1"), contract.SessionAgentShared)
	if s3.ID == s1.ID {
		t.Fatalf("agent-shared should still partition by agent group")
	}
}

func TestFindSessionDoesNotCreate(t *testing.T) {
	r := NewMemRegistry()
	ag := contract.AgentGroupID("g1")
	mg := contract.MessagingGroupID("m1")
	if _, ok := r.FindSession(ag, mg, nil, contract.SessionShared); ok {
		t.Fatal("FindSession should not find a non-existent session")
	}
	r.ResolveSession(ag, mg, nil, contract.SessionShared)
	if _, ok := r.FindSession(ag, mg, nil, contract.SessionShared); !ok {
		t.Fatal("FindSession should find an existing session")
	}
}

func TestCanAccessPrecedence(t *testing.T) {
	r := NewMemRegistry()
	g := contract.AgentGroupID("g1")
	other := contract.AgentGroupID("g2")

	owner := contract.UserID("slack:owner")
	gadmin := contract.UserID("slack:gadmin")
	sadmin := contract.UserID("slack:sadmin")
	member := contract.UserID("slack:member")
	stranger := contract.UserID("slack:stranger")

	gscope := g
	otherScope := other
	if err := r.GrantRole(Role{UserID: owner, Role: RoleOwner}); err != nil {
		t.Fatal(err)
	}
	r.GrantRole(Role{UserID: gadmin, Role: RoleAdmin})                              // global admin
	r.GrantRole(Role{UserID: sadmin, Role: RoleAdmin, AgentGroupID: &gscope})       // scoped to g
	r.GrantRole(Role{UserID: stranger, Role: RoleAdmin, AgentGroupID: &otherScope}) // scoped to other
	r.AddMember(Member{UserID: member, AgentGroupID: g})

	cases := []struct {
		user       contract.UserID
		wantOK     bool
		wantReason string
	}{
		{owner, true, "owner"},
		{gadmin, true, "global-admin"},
		{sadmin, true, "scoped-admin"},
		{member, true, "member"},
		{stranger, false, "no-access"}, // scoped to a different group
		{contract.UserID("slack:nobody"), false, "no-access"},
	}
	for _, c := range cases {
		ok, reason := r.CanAccess(c.user, g)
		if ok != c.wantOK || reason != c.wantReason {
			t.Errorf("CanAccess(%q) = (%v,%q), want (%v,%q)", c.user, ok, reason, c.wantOK, c.wantReason)
		}
	}
}

func TestCanAccessOwnerIsGlobal(t *testing.T) {
	r := NewMemRegistry()
	owner := contract.UserID("slack:owner")
	r.GrantRole(Role{UserID: owner, Role: RoleOwner})
	// Owner can access any agent group, including ones never seen.
	if ok, reason := r.CanAccess(owner, "any-group"); !ok || reason != "owner" {
		t.Fatalf("owner should access any group, got (%v,%q)", ok, reason)
	}
}

func TestIsKnownSender(t *testing.T) {
	r := NewMemRegistry()
	g := contract.AgentGroupID("g1")
	member := contract.UserID("slack:member")
	r.AddMember(Member{UserID: member, AgentGroupID: g})
	if !r.IsKnownSender(member, g) {
		t.Fatal("member should be a known sender")
	}
	if r.IsKnownSender("slack:nobody", g) {
		t.Fatal("stranger should not be a known sender")
	}
}

func TestRevokeAndRemove(t *testing.T) {
	r := NewMemRegistry()
	g := contract.AgentGroupID("g1")
	u := contract.UserID("slack:u")
	r.GrantRole(Role{UserID: u, Role: RoleAdmin})
	if ok, _ := r.CanAccess(u, g); !ok {
		t.Fatal("expected access after grant")
	}
	r.RevokeRole(Role{UserID: u, Role: RoleAdmin})
	if ok, _ := r.CanAccess(u, g); ok {
		t.Fatal("expected no access after revoke")
	}
	r.AddMember(Member{UserID: u, AgentGroupID: g})
	if ok, _ := r.CanAccess(u, g); !ok {
		t.Fatal("expected access after add member")
	}
	r.RemoveMember(Member{UserID: u, AgentGroupID: g})
	if ok, _ := r.CanAccess(u, g); ok {
		t.Fatal("expected no access after remove member")
	}
}

func TestListWiringsPriorityOrder(t *testing.T) {
	r := NewMemRegistry()
	mg := contract.MessagingGroupID("m1")
	r.PutWiring(Wiring{ID: "w-low", MessagingGroupID: mg, AgentGroupID: "g1", Priority: 1})
	r.PutWiring(Wiring{ID: "w-high", MessagingGroupID: mg, AgentGroupID: "g2", Priority: 10})
	r.PutWiring(Wiring{ID: "w-other", MessagingGroupID: "m2", AgentGroupID: "g3", Priority: 99})
	ws, _ := r.ListWirings(mg)
	if len(ws) != 2 {
		t.Fatalf("want 2 wirings for m1, got %d", len(ws))
	}
	if ws[0].ID != "w-high" || ws[1].ID != "w-low" {
		t.Fatalf("wirings not in descending priority order: %v", ws)
	}
}
