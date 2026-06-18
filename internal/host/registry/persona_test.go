package registry

import (
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
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

func TestComposePersonaFallsBackToLegacy(t *testing.T) {
	// No docs → the legacy single-blob persona is used verbatim.
	g := AgentGroup{Persona: "You are terse."}
	if got := ComposePersona(g); got != "You are terse." {
		t.Fatalf("legacy fallback = %q, want the blob", got)
	}
	// Docs present but all empty → still falls back (so a half-filled builder can't
	// silently blank an agent's legacy persona).
	g.PersonaDocs = map[string]string{"soul": "  ", "identity": ""}
	if got := ComposePersona(g); got != "You are terse." {
		t.Fatalf("all-empty docs should fall back, got %q", got)
	}
}

func TestComposePersonaRendersSectionsInOrder(t *testing.T) {
	g := AgentGroup{
		Persona: "ignored when docs are present",
		PersonaDocs: map[string]string{
			"instructions": "Search first.",
			"identity":     "You are Atlas.",
			"soul":         "Curious and precise.",
		},
	}
	got := ComposePersona(g)
	// Canonical order is identity, soul, instructions regardless of map iteration.
	iIdent := strings.Index(got, "Atlas")
	iSoul := strings.Index(got, "Curious")
	iInstr := strings.Index(got, "Search first")
	if !(iIdent >= 0 && iIdent < iSoul && iSoul < iInstr) {
		t.Fatalf("sections out of canonical order:\n%s", got)
	}
	for _, h := range []string{"### Identity", "### Soul", "### Instructions"} {
		if !strings.Contains(got, h) {
			t.Errorf("composed persona missing heading %q:\n%s", h, got)
		}
	}
	if strings.Contains(got, "ignored when docs") {
		t.Error("legacy Persona must be ignored when docs are present")
	}
}

func TestComposePersonaSkipsEmptySections(t *testing.T) {
	g := AgentGroup{PersonaDocs: map[string]string{"identity": "You are Atlas.", "soul": ""}}
	got := ComposePersona(g)
	if strings.Contains(got, "### Soul") {
		t.Errorf("empty section should be skipped:\n%s", got)
	}
	if !strings.Contains(got, "### Identity") {
		t.Errorf("non-empty section should render:\n%s", got)
	}
}

func TestComposePersonaTruncatesOverlongSection(t *testing.T) {
	long := strings.Repeat("x", MaxPersonaDocLen+500)
	g := AgentGroup{PersonaDocs: map[string]string{"soul": long}}
	got := ComposePersona(g)
	// The rendered body must be bounded even if a too-long value slipped past validation.
	if len(got) > MaxPersonaDocLen+len("### Soul\n\n")+8 {
		t.Fatalf("composed section not bounded: %d bytes", len(got))
	}
}

func TestValidatePersonaDocs(t *testing.T) {
	if err := ValidatePersonaDocs(nil); err != nil {
		t.Errorf("nil docs should be valid: %v", err)
	}
	ok := map[string]string{"identity": "a", "soul": "b", "instructions": "c"}
	if err := ValidatePersonaDocs(ok); err != nil {
		t.Errorf("valid docs rejected: %v", err)
	}
	if err := ValidatePersonaDocs(map[string]string{"vibe": "x"}); err == nil {
		t.Error("unknown section key must be rejected")
	}
	if err := ValidatePersonaDocs(map[string]string{"soul": strings.Repeat("y", MaxPersonaDocLen+1)}); err == nil {
		t.Error("over-length section must be rejected")
	}
	if err := ValidatePersonaDocs(map[string]string{"soul": "bad\xff\xfe"}); err == nil {
		t.Error("non-UTF8 section must be rejected")
	}
}
