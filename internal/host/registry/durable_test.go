package registry

import (
	"path/filepath"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func key32(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

// TestSQLRegistryReload seeds one of every entity, closes the backend, reopens it
// with the same key, and verifies the full state — including role precedence and
// session partitioning — survived the round-trip to the encrypted store.
func TestSQLRegistryReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	key := key32(0x11)

	r, err := OpenSQLRegistry(path, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := r.PutAgentGroup(AgentGroup{ID: "ag1", Name: "Alpha", Folder: "/a"}); err != nil {
		t.Fatalf("PutAgentGroup: %v", err)
	}
	mg, err := r.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownStrict)
	if err != nil {
		t.Fatalf("GetOrCreateMessagingGroup: %v", err)
	}
	if err := r.PutWiring(Wiring{ID: "w1", MessagingGroupID: mg.ID, AgentGroupID: "ag1", EngageMode: contract.EngageMention, SessionMode: contract.SessionPerThread, Priority: 5}); err != nil {
		t.Fatalf("PutWiring: %v", err)
	}
	if err := r.PutUser(User{ID: "slack:u1", Kind: "user", DisplayName: "One"}); err != nil {
		t.Fatalf("PutUser: %v", err)
	}
	if err := r.GrantRole(Role{UserID: "slack:u1", Role: RoleOwner}); err != nil { // global owner
		t.Fatalf("GrantRole owner: %v", err)
	}
	// Granting the identical global role again must stay a single row after reload.
	if err := r.GrantRole(Role{UserID: "slack:u1", Role: RoleOwner}); err != nil {
		t.Fatalf("GrantRole owner (dup): %v", err)
	}
	agid := contract.AgentGroupID("ag1")
	if err := r.GrantRole(Role{UserID: "slack:u2", Role: RoleAdmin, AgentGroupID: &agid}); err != nil { // scoped admin
		t.Fatalf("GrantRole scoped: %v", err)
	}
	if err := r.AddMember(Member{UserID: "slack:u3", AgentGroupID: "ag1"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := r.AddDestination("ag1", "slack", "C2"); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}
	sess, err := r.ResolveSession("ag1", mg.ID, ptr("t1"), contract.SessionPerThread)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen with the same key — everything must come back.
	r2, err := OpenSQLRegistry(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	if g, ok := r2.GetAgentGroup("ag1"); !ok || g.Name != "Alpha" || g.Folder != "/a" {
		t.Fatalf("agent group not reloaded: %+v ok=%v", g, ok)
	}
	if got, ok := r2.GetMessagingGroup(mg.ID); !ok || got.ChannelType != "slack" || got.PlatformID != "C1" || !got.IsGroup {
		t.Fatalf("messaging group not reloaded: %+v ok=%v", got, ok)
	}
	// The triple index must be rebuilt: same triple resolves to the same id.
	if again, _ := r2.GetOrCreateMessagingGroup("slack", "C1", "", true, contract.UnknownStrict); again.ID != mg.ID {
		t.Fatalf("triple index not rebuilt: %q != %q", again.ID, mg.ID)
	}
	ws, err := r2.ListWirings(mg.ID)
	if err != nil || len(ws) != 1 || ws[0].ID != "w1" || ws[0].EngageMode != contract.EngageMention || ws[0].Priority != 5 {
		t.Fatalf("wiring not reloaded: %+v err=%v", ws, err)
	}
	if u, ok := r2.GetUser("slack:u1"); !ok || u.DisplayName != "One" {
		t.Fatalf("user not reloaded: %+v ok=%v", u, ok)
	}

	// Role precedence reconstructed from the reloaded rows.
	if ok, reason := r2.CanAccess("slack:u1", "ag1"); !ok || reason != "owner" {
		t.Fatalf("owner access lost: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := r2.CanAccess("slack:u2", "ag1"); !ok || reason != "scoped-admin" {
		t.Fatalf("scoped-admin access lost: ok=%v reason=%q", ok, reason)
	}
	// Scoped admin must NOT leak to another agent group.
	if ok, _ := r2.CanAccess("slack:u2", "agOther"); ok {
		t.Fatalf("scoped admin leaked to another agent group")
	}
	if ok, reason := r2.CanAccess("slack:u3", "ag1"); !ok || reason != "member" {
		t.Fatalf("member access lost: ok=%v reason=%q", ok, reason)
	}
	if !r2.IsAllowedDestination("ag1", "slack", "C2") {
		t.Fatalf("destination not reloaded")
	}

	// Session survived with its thread partition.
	got, ok := r2.GetSession(sess.ID)
	if !ok || got.ThreadID == nil || *got.ThreadID != "t1" || got.AgentGroupID != "ag1" {
		t.Fatalf("session not reloaded: %+v ok=%v", got, ok)
	}
	if found, ok := r2.FindSession("ag1", mg.ID, ptr("t1"), contract.SessionPerThread); !ok || found.ID != sess.ID {
		t.Fatalf("session partition not rebuilt: %+v ok=%v", found, ok)
	}
}

// TestSQLRegistryRevokePersists verifies that revoke/remove are durable.
func TestSQLRegistryRevokePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	key := key32(0x22)

	r, err := OpenSQLRegistry(path, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	agid := contract.AgentGroupID("ag1")
	if err := r.GrantRole(Role{UserID: "u", Role: RoleAdmin, AgentGroupID: &agid}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddMember(Member{UserID: "m", AgentGroupID: "ag1"}); err != nil {
		t.Fatal(err)
	}
	// Revoke / remove them again.
	if err := r.RevokeRole(Role{UserID: "u", Role: RoleAdmin, AgentGroupID: &agid}); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveMember(Member{UserID: "m", AgentGroupID: "ag1"}); err != nil {
		t.Fatal(err)
	}
	r.Close()

	r2, err := OpenSQLRegistry(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	if ok, _ := r2.CanAccess("u", "ag1"); ok {
		t.Fatalf("revoked role still grants access after reload")
	}
	if ok, _ := r2.CanAccess("m", "ag1"); ok {
		t.Fatalf("removed member still grants access after reload")
	}
}

// TestSQLRegistryWrongKey verifies a registry created with one key cannot be
// reopened with another.
func TestSQLRegistryWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	r, err := OpenSQLRegistry(path, key32(0x11))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := r.PutAgentGroup(AgentGroup{ID: "ag1", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	r.Close()

	if bad, err := OpenSQLRegistry(path, key32(0x99)); err == nil {
		bad.Close()
		t.Fatal("opening with the wrong key should fail")
	}
}
