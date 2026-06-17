package registry

import (
	"strings"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestValidatePersona(t *testing.T) {
	if err := ValidatePersona(""); err != nil {
		t.Errorf("empty persona should be valid (clears it): %v", err)
	}
	if err := ValidatePersona("a terse on-call assistant"); err != nil {
		t.Errorf("normal persona rejected: %v", err)
	}
	if err := ValidatePersona(strings.Repeat("x", MaxPersonaLen+1)); err == nil {
		t.Error("over-length persona must be rejected")
	}
	if err := ValidatePersona("bad\xff\xfeutf8"); err == nil {
		t.Error("non-UTF8 persona must be rejected")
	}
}

func TestSetPersona(t *testing.T) {
	r := NewMemRegistry()
	const id contract.AgentGroupID = "grp-1"
	if err := r.PutAgentGroup(AgentGroup{ID: id, Name: "Triage", Folder: "triage"}); err != nil {
		t.Fatal(err)
	}

	if err := SetPersona(r, id, "You are terse.\n"); err != nil {
		t.Fatalf("SetPersona: %v", err)
	}
	g, _ := r.GetAgentGroup(id)
	if g.Persona != "You are terse." { // trailing newline trimmed
		t.Fatalf("persona = %q, want trimmed", g.Persona)
	}
	// Other fields are preserved.
	if g.Name != "Triage" || g.Folder != "triage" {
		t.Errorf("SetPersona clobbered other fields: %+v", g)
	}

	// Unknown group errors.
	if err := SetPersona(r, "ghost", "x"); err == nil {
		t.Error("SetPersona on a missing group should error")
	}
	// Over-length rejected and the stored persona is unchanged.
	if err := SetPersona(r, id, strings.Repeat("y", MaxPersonaLen+1)); err == nil {
		t.Error("over-length persona must be rejected")
	}
	g, _ = r.GetAgentGroup(id)
	if g.Persona != "You are terse." {
		t.Errorf("rejected SetPersona must leave persona unchanged, got %q", g.Persona)
	}
}
