// OWNER: T-083

package questions

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestStoreRecordListResolve(t *testing.T) {
	s := NewStore()
	if s.Len() != 0 {
		t.Fatalf("new store should be empty, got %d", s.Len())
	}

	p := s.Record("ses1", "ag1", contract.AskUserRequest{
		Question:      "Deploy where?",
		Options:       []string{"staging", "prod"},
		AllowFreeform: true,
	})
	if p.ID == "" || p.SessionID != "ses1" || p.AgentGroupID != "ag1" {
		t.Fatalf("recorded question missing fields: %+v", p)
	}
	if p.Question != "Deploy where?" || len(p.Options) != 2 || !p.AllowFreeform {
		t.Fatalf("recorded question content wrong: %+v", p)
	}
	if p.AskedAt.IsZero() {
		t.Fatal("AskedAt should be set")
	}

	s.Record("ses2", "ag1", contract.AskUserRequest{Question: "Proceed?"})
	if s.Len() != 2 {
		t.Fatalf("expected 2 pending, got %d", s.Len())
	}
	if got := s.List(); len(got) != 2 || got[0].ID != p.ID {
		t.Fatalf("List should be oldest-first starting with %s, got %+v", p.ID, got)
	}

	if _, ok := s.Get(p.ID); !ok {
		t.Fatal("Get should find the recorded question")
	}
	resolved, ok := s.Resolve(p.ID)
	if !ok || resolved.ID != p.ID {
		t.Fatalf("Resolve should return the removed question, got ok=%v %+v", ok, resolved)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 pending after resolve, got %d", s.Len())
	}
	if _, ok := s.Resolve(p.ID); ok {
		t.Fatal("resolving twice should report not-found")
	}
}

// TestStoreOptionsCopied guards against the caller mutating the stored slice.
func TestStoreOptionsCopied(t *testing.T) {
	s := NewStore()
	opts := []string{"a", "b"}
	p := s.Record("ses", "ag", contract.AskUserRequest{Question: "q", Options: opts})
	opts[0] = "mutated"
	got, _ := s.Get(p.ID)
	if got.Options[0] != "a" {
		t.Fatalf("stored options must be a copy, got %v", got.Options)
	}
}
