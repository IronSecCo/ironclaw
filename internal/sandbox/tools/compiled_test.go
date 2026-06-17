package tools

import (
	"sort"
	"testing"
)

func TestCompiledToolNamesNoDuplicates(t *testing.T) {
	names := CompiledToolNames()
	if len(names) == 0 {
		t.Fatal("CompiledToolNames is empty")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" {
			t.Error("empty tool name")
		}
		if seen[n] {
			t.Errorf("duplicate tool name %q", n)
		}
		seen[n] = true
	}
}

func TestCompiledToolSetMatchesNames(t *testing.T) {
	set := CompiledToolSet()
	names := CompiledToolNames()
	if len(set) != len(names) {
		t.Fatalf("set size %d != names size %d", len(set), len(names))
	}
	for _, n := range names {
		if !set[n] {
			t.Errorf("set missing %q", n)
		}
	}
}

// TestCompiledToolNamesCoversBuiltRegistry mirrors cmd/sandbox.buildTools and
// asserts the canonical list exactly covers what a real sandbox registers (with
// the egress tool included). This is the drift guard: add a tool to buildTools
// without updating CompiledToolNames and this fails.
func TestCompiledToolNamesCoversBuiltRegistry(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	reg := NewRegistry()
	reg.MustRegisterAll(t, ws.Tools()...)
	reg.MustRegister(t, NewRequestCapabilityChangeTool())
	reg.MustRegister(t, NewScheduleTaskTool())
	reg.MustRegister(t, NewAskUserQuestionTool())
	reg.MustRegister(t, NewReadPersonaTool(""))
	reg.MustRegister(t, NewSendMessageTool(fakeMsgCtx{}))
	reg.MustRegister(t, NewSendFileTool(ws, fakeMsgCtx{}))
	reg.MustRegister(t, NewListDestinationsTool(fakeMsgCtx{}))
	reg.MustRegisterAll(t, TaskManagementTools()...)
	reg.MustRegister(t, NewHTTPFetchTool("/run/ironclaw/egress.sock"))

	got := append([]string(nil), reg.Names()...)
	want := CompiledToolNames()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("registry has %d tools, CompiledToolNames has %d:\n got=%v\nwant=%v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("mismatch at %d: registry=%q canonical=%q\n got=%v\nwant=%v", i, got[i], want[i], got, want)
		}
	}
}

// --- test helpers ---

func (r *Registry) MustRegister(t *testing.T, tool Tool) {
	t.Helper()
	if err := r.Register(tool); err != nil {
		t.Fatalf("register %s: %v", tool.Name(), err)
	}
}

func (r *Registry) MustRegisterAll(t *testing.T, tools ...Tool) {
	t.Helper()
	for _, tool := range tools {
		r.MustRegister(t, tool)
	}
}
