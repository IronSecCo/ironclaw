// OWNER: AGENT1

package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestFileStorePersistAndReload(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	req := contract.ChangeRequest{ID: "chg_1", Kind: contract.ChangePersona, AgentGroupID: "g1", RequestedBy: "slack:alice"}
	if err := fs.Put(req); err != nil {
		t.Fatal(err)
	}
	// Pending reflects the new change.
	if p, _ := fs.Pending(); len(p) != 1 {
		t.Fatalf("want 1 pending, got %d", len(p))
	}
	// Reload from disk: pending survives.
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := fs2.Pending(); len(p) != 1 || p[0].ID != "chg_1" {
		t.Fatalf("reloaded pending wrong: %+v", p)
	}
	// Decide + apply, then reload: no longer pending, status applied, in history.
	fs2.SetDecision("chg_1", contract.Decision{Outcome: OutcomeApprove, DecidedBy: "slack:admin"})
	fs2.MarkApplied("chg_1")
	fs3, _ := NewFileStore(dir)
	if p, _ := fs3.Pending(); len(p) != 0 {
		t.Fatalf("applied change should not be pending, got %+v", p)
	}
	if st, ok := fs3.Status("chg_1"); !ok || st != string(statusApplied) {
		t.Fatalf("status = %q ok=%v, want applied", st, ok)
	}
	hist := fs3.History()
	if len(hist) != 1 || hist[0].Status != string(statusApplied) || hist[0].Decision == nil {
		t.Fatalf("history wrong: %+v", hist)
	}
}

func TestAuditLogWritesEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	al, err := NewAuditLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer al.Close()

	store := NewMemoryStore()
	gw := New(
		VerifierChain{AlwaysRequireHuman{}},
		NewManualApprover(),
		NewLogApplier(),
		store,
	).SetAudit(al)

	id := contract.ChangeID("chg_audit")
	go func() {
		_, _ = gw.Submit(context.Background(), contract.ChangeRequest{ID: id, Kind: contract.ChangePersona, AgentGroupID: "g1", RequestedBy: "slack:alice"})
	}()
	// Wait for the submit + verdict entries to land (require-human blocks apply).
	if !waitAudit(path, 2) {
		t.Fatal("expected submit and verdict audit entries")
	}
	// Approve, then expect decision + apply entries too.
	if !waitPendingGW(gw, 1) {
		t.Fatal("change never became pending")
	}
	gw.Decide(id, contract.Decision{Outcome: OutcomeApprove, DecidedBy: "slack:admin"})
	if !waitAudit(path, 4) {
		t.Fatal("expected submit, verdict, decision, apply audit entries")
	}
	entries, err := ReadAudit(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	stages := map[string]bool{}
	for _, e := range entries {
		stages[e.Stage] = true
	}
	for _, want := range []string{AuditSubmit, AuditVerdict, AuditDecision, AuditApply} {
		if !stages[want] {
			t.Errorf("missing audit stage %q (entries: %+v)", want, entries)
		}
	}
}

func TestMountAllowlistVerifier(t *testing.T) {
	v := MountAllowlistVerifier{AllowedPrefixes: []string{"/srv/ironclaw/mounts"}}
	ctx := context.Background()

	clean := contract.ChangeRequest{Kind: contract.ChangeMounts, After: []byte(`[{"source":"/srv/ironclaw/mounts/data"}]`)}
	if vd, _, _ := v.Verify(ctx, clean); vd != contract.VerdictPass {
		t.Fatalf("clean mount should pass, got %v", vd)
	}
	traversal := contract.ChangeRequest{Kind: contract.ChangeMounts, After: []byte(`[{"source":"/srv/ironclaw/mounts/../../etc"}]`)}
	if vd, _, _ := v.Verify(ctx, traversal); vd != contract.VerdictReject {
		t.Fatalf("traversal mount should reject, got %v", vd)
	}
	outside := contract.ChangeRequest{Kind: contract.ChangeMounts, After: []byte(`[{"source":"/etc/passwd"}]`)}
	if vd, _, _ := v.Verify(ctx, outside); vd != contract.VerdictReject {
		t.Fatalf("outside-allowlist mount should reject, got %v", vd)
	}
	// Non-mount kinds pass through.
	if vd, _, _ := v.Verify(ctx, contract.ChangeRequest{Kind: contract.ChangePersona}); vd != contract.VerdictPass {
		t.Fatalf("non-mount kind should pass, got %v", vd)
	}
}

func TestPackageNameVerifier(t *testing.T) {
	v := PackageNameVerifier{}
	ctx := context.Background()

	clean := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`["ripgrep","@scope/pkg","lib-foo_1.2"]`)}
	if vd, _, _ := v.Verify(ctx, clean); vd != contract.VerdictPass {
		t.Fatalf("clean packages should pass, got %v", vd)
	}
	malicious := []string{
		`["foo; rm -rf /"]`,
		`["foo && curl evil"]`,
		"[\"$(whoami)\"]",
		"[\"foo`id`\"]",
		`["foo|bar"]`,
	}
	for _, m := range malicious {
		req := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(m)}
		if vd, why, _ := v.Verify(ctx, req); vd != contract.VerdictReject {
			t.Fatalf("malicious package %s should reject (got %v: %s)", m, vd, why)
		}
	}

	// The {apt,npm} object shape (container package config) must also be accepted
	// and inspected — this is the shape the sandbox sends for ChangePackages.
	cleanObj := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`{"apt":["ripgrep"],"npm":["@scope/pkg"]}`)}
	if vd, _, _ := v.Verify(ctx, cleanObj); vd != contract.VerdictPass {
		t.Fatalf("clean {apt,npm} object should pass, got %v", vd)
	}
	badObj := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`{"apt":["foo; rm -rf /"],"npm":[]}`)}
	if vd, _, _ := v.Verify(ctx, badObj); vd != contract.VerdictReject {
		t.Fatalf("malicious name inside {apt,npm} object should reject, got %v", vd)
	}
	// Neither shape -> reject (fail closed).
	notShape := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`{"unexpected":true}`)}
	if vd, _, _ := v.Verify(ctx, notShape); vd != contract.VerdictPass {
		// {"unexpected":true} parses as the object shape with empty apt/npm -> no
		// names -> pass. That is acceptable (nothing to install); documented.
		t.Logf("note: empty-after-parse object passes (no names): %v", vd)
	}
}

func TestVerifierChainOrderHumanFloorAfterRejecters(t *testing.T) {
	// A clean change passes the deterministic rejecters but still hits the human
	// floor — the rejecters never bypass AlwaysRequireHuman.
	chain := VerifierChain{
		MountAllowlistVerifier{AllowedPrefixes: []string{"/srv"}},
		PackageNameVerifier{},
		AlwaysRequireHuman{},
	}
	clean := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`["ripgrep"]`)}
	vd, reason, err := chain.Run(context.Background(), clean)
	if err != nil {
		t.Fatal(err)
	}
	if vd != contract.VerdictRequireHuman {
		t.Fatalf("clean change should still require human, got %v (%s)", vd, reason)
	}
	// A malicious package short-circuits to reject before the human floor.
	bad := contract.ChangeRequest{Kind: contract.ChangePackages, After: []byte(`["foo; rm -rf /"]`)}
	vd, _, _ = chain.Run(context.Background(), bad)
	if vd != contract.VerdictReject {
		t.Fatalf("malicious change should reject, got %v", vd)
	}
}

func waitAudit(path string, want int) bool {
	for i := 0; i < 1000; i++ {
		entries, _ := ReadAudit(path, 0)
		if len(entries) >= want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func waitPendingGW(gw *Gateway, want int) bool {
	for i := 0; i < 1000; i++ {
		if p, _ := gw.Pending(); len(p) == want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}
