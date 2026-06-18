package catalog

import (
	"testing"

	"github.com/nivardsec/ironclaw/internal/host/registry"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
)

// TestToolsCoverCompiledSetExactly is the drift guard: the friendly catalog must
// describe EXACTLY the compiled tool set — no missing tool (the console would hide a
// real capability) and no extra tool (the console would offer one the binary can't
// honor). If cmd/sandbox.buildTools gains or drops a tool, update toolMeta to match.
func TestToolsCoverCompiledSetExactly(t *testing.T) {
	compiled := map[string]bool{}
	for _, n := range tools.CompiledToolNames() {
		compiled[n] = true
	}
	got := map[string]bool{}
	for _, ti := range Tools() {
		if got[ti.Name] {
			t.Errorf("tool %q listed twice in the catalog", ti.Name)
		}
		got[ti.Name] = true
	}
	for n := range compiled {
		if !got[n] {
			t.Errorf("compiled tool %q missing from the catalog (add it to toolMeta)", n)
		}
	}
	for n := range got {
		if !compiled[n] {
			t.Errorf("catalog tool %q is not a compiled tool (remove it from toolMeta)", n)
		}
	}
}

// TestEveryToolHasCopy ensures no tool renders with the empty-fallback copy.
func TestEveryToolHasCopy(t *testing.T) {
	for _, ti := range Tools() {
		if ti.Title == "" || ti.Title == ti.Name {
			t.Errorf("tool %q has no friendly Title", ti.Name)
		}
		if ti.Description == "" || ti.Description == "A built-in tool." {
			t.Errorf("tool %q has no friendly Description", ti.Name)
		}
		if ti.Category == "" {
			t.Errorf("tool %q has no Category", ti.Name)
		}
	}
}

// TestMandatoryFlag asserts the catalog marks exactly the tools the runtime keeps
// regardless of restriction, so the console disables the right checkboxes.
func TestMandatoryFlag(t *testing.T) {
	want := map[string]bool{}
	for _, n := range tools.MandatoryToolNames() {
		want[n] = true
	}
	for _, ti := range Tools() {
		if ti.Mandatory != want[ti.Name] {
			t.Errorf("tool %q Mandatory=%v, want %v", ti.Name, ti.Mandatory, want[ti.Name])
		}
	}
}

// TestEgressFlag asserts only the outbound tools are badged as egress.
func TestEgressFlag(t *testing.T) {
	want := map[string]bool{tools.HTTPFetchToolName: true, tools.WebSearchToolName: true}
	for _, ti := range Tools() {
		if ti.Egress != want[ti.Name] {
			t.Errorf("tool %q Egress=%v, want %v", ti.Name, ti.Egress, want[ti.Name])
		}
	}
}

// TestToolsGroupedByCategoryOrder asserts the catalog is returned grouped in the
// canonical category order, so clients can render groups without re-sorting.
func TestToolsGroupedByCategoryOrder(t *testing.T) {
	order := map[Category]int{}
	for i, c := range CategoryOrder() {
		order[c] = i
	}
	last := -1
	for _, ti := range Tools() {
		idx, ok := order[ti.Category]
		if !ok {
			t.Fatalf("tool %q has category %q not in CategoryOrder", ti.Name, ti.Category)
		}
		if idx < last {
			t.Fatalf("tool %q (category %q) is out of category order", ti.Name, ti.Category)
		}
		last = idx
	}
}

// TestTemplatesAreValid asserts every template's tools are compiled tools, ids are
// unique and non-empty, and names/descriptions are present.
func TestTemplatesAreValid(t *testing.T) {
	compiled := map[string]bool{}
	for _, n := range tools.CompiledToolNames() {
		compiled[n] = true
	}
	seen := map[string]bool{}
	for _, tpl := range Templates() {
		if tpl.ID == "" {
			t.Errorf("template %q has empty id", tpl.Name)
		}
		if seen[tpl.ID] {
			t.Errorf("duplicate template id %q", tpl.ID)
		}
		seen[tpl.ID] = true
		if tpl.Name == "" || tpl.Description == "" {
			t.Errorf("template %q missing name or description", tpl.ID)
		}
		for _, tool := range tpl.Tools {
			if !compiled[tool] {
				t.Errorf("template %q references unknown tool %q", tpl.ID, tool)
			}
		}
		// Persona doc keys must be valid sections, and each must pass registry validation.
		if err := registry.ValidatePersonaDocs(tpl.PersonaDocs); err != nil {
			t.Errorf("template %q has invalid persona docs: %v", tpl.ID, err)
		}
	}
	if !seen["blank"] {
		t.Errorf("expected a 'blank' template to exist")
	}
}

// TestPersonaSectionsMatchRegistry is the drift guard: the catalog's persona sections
// (UI copy) must describe exactly the keys the host composer renders, in the same
// order — else the builder would show a field the composer drops, or vice versa.
func TestPersonaSectionsMatchRegistry(t *testing.T) {
	want := registry.PersonaSectionKeys()
	got := PersonaSections()
	if len(got) != len(want) {
		t.Fatalf("PersonaSections has %d entries, registry has %d", len(got), len(want))
	}
	for i, sec := range got {
		if sec.Key != want[i] {
			t.Errorf("section %d key = %q, want %q (order must match registry)", i, sec.Key, want[i])
		}
		if sec.Title == "" || sec.Filename == "" || sec.Placeholder == "" || sec.Help == "" {
			t.Errorf("section %q is missing display copy: %+v", sec.Key, sec)
		}
	}
}

// TestTemplateByID round-trips known and unknown ids.
func TestTemplateByID(t *testing.T) {
	if _, ok := TemplateByID("researcher"); !ok {
		t.Errorf("expected researcher template to resolve")
	}
	if _, ok := TemplateByID("does-not-exist"); ok {
		t.Errorf("unexpected resolve for unknown template id")
	}
}
