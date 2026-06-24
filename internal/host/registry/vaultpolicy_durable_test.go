package registry

import (
	"database/sql"
	"encoding/hex"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

func vaultKey(b byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = b
	}
	return k
}

// TestDurableVaultPolicyReload is the headline acceptance: an approved grant
// survives a control-plane restart (close + reopen with the same key), and the
// deny-by-default decision is reconstructed identically.
func TestDurableVaultPolicyReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-policies.db")
	key := vaultKey(0x11)

	s, err := OpenDurableVaultPolicyStore(path, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{
		{Credential: "github", Hosts: []string{"api.github.com"}},
		{Credential: "pagerduty", Hosts: []string{"api.pagerduty.com", "events.pagerduty.com"}},
	}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen — a fresh process would see exactly this.
	s2, err := OpenDurableVaultPolicyStore(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if !s2.Allows(grpA, "github", "api.github.com") {
		t.Error("granted credential+host must survive restart")
	}
	if !s2.Allows(grpA, "pagerduty", "events.pagerduty.com") {
		t.Error("second granted host must survive restart")
	}
	// Deny-by-default preserved across the reload.
	if s2.Allows(grpA, "github", "evil.example.com") {
		t.Error("un-granted host must stay denied after restart")
	}
	if s2.Allows(grpA, "stripe", "api.github.com") {
		t.Error("un-granted credential must stay denied after restart")
	}
	if s2.Allows(grpB, "github", "api.github.com") {
		t.Error("a group with no policy must stay denied after restart")
	}
	got, ok := s2.Get(grpA)
	if !ok || len(got.Rules) != 2 {
		t.Errorf("Get after reload = %+v, ok=%v; want 2 rules", got, ok)
	}
}

// TestDurableVaultPolicyDeleteAcrossReopen verifies a deleted policy does not
// resurrect after restart.
func TestDurableVaultPolicyDeleteAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-policies.db")
	key := vaultKey(0x22)

	s, err := OpenDurableVaultPolicyStore(path, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{
		{Credential: "github", Hosts: []string{"api.github.com"}},
	}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.Delete(grpA); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.Allows(grpA, "github", "api.github.com") {
		t.Fatal("deleted policy must deny immediately")
	}
	s.Close()

	s2, err := OpenDurableVaultPolicyStore(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Allows(grpA, "github", "api.github.com") {
		t.Error("deleted policy must not resurrect after restart")
	}
}

// TestDurableVaultPolicyWrongKey verifies opening the persisted DB with the wrong
// key fails loudly rather than silently returning an empty (fully-permissive-by-
// omission) store.
func TestDurableVaultPolicyWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-policies.db")

	s, err := OpenDurableVaultPolicyStore(path, vaultKey(0x33))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Set(VaultPolicy{AgentGroupID: grpA, Rules: []VaultRule{
		{Credential: "github", Hosts: []string{"api.github.com"}},
	}}); err != nil {
		t.Fatalf("set: %v", err)
	}
	s.Close()

	if _, err := OpenDurableVaultPolicyStore(path, vaultKey(0x44)); err == nil {
		t.Fatal("opening with the wrong key must fail, not return an empty store")
	}
}

// TestDurableVaultPolicyRejectsTamperedRow verifies the store fails closed on a
// persisted row that does not validate (e.g. a tampered host), rather than loading
// an unevaluable policy or silently dropping it.
func TestDurableVaultPolicyRejectsTamperedRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-policies.db")
	key := vaultKey(0x55)

	// Create the encrypted DB with the store, then inject a malformed row directly.
	s, err := OpenDurableVaultPolicyStore(path, key)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	s.Close()

	q := url.Values{}
	q.Set("_pragma_key", `x'`+hex.EncodeToString(key[:])+`'`)
	db, err := sql.Open("sqlite3", "file:"+path+"?"+q.Encode())
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	// A wildcard host is rejected by validVaultHost — an unevaluable, dangerous rule.
	if _, err := db.Exec(`INSERT INTO vault_policies (agent_group_id, rules_json) VALUES (?,?)`,
		string(grpA), `[{"Credential":"github","Hosts":["*.github.com"]}]`); err != nil {
		t.Fatalf("inject: %v", err)
	}
	db.Close()

	if _, err := OpenDurableVaultPolicyStore(path, key); err == nil {
		t.Fatal("a malformed persisted policy must fail the open, not be admitted")
	}
}

// TestDurableVaultPolicyEmptyStartDenies verifies a freshly created store denies
// everything (deny-by-default) before any grant is written.
func TestDurableVaultPolicyEmptyStartDenies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-policies.db")
	s, err := OpenDurableVaultPolicyStore(path, vaultKey(0x66))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	var anyGroup contract.AgentGroupID = "unconfigured"
	if s.Allows(anyGroup, "github", "api.github.com") {
		t.Fatal("a brand-new store must deny by default")
	}
}
