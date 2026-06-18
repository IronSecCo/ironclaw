package tools

import (
	"sort"
	"testing"
)

func fullTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, tool := range []Tool{
		NewRequestCapabilityChangeTool(),
		NewAskUserQuestionTool(),
		NewScheduleTaskTool(),
		NewHTTPFetchTool(""),
		NewReadPersonaTool(""),
	} {
		if err := r.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}
	return r
}

func TestFilterRegistryEmptyMeansNoRestriction(t *testing.T) {
	full := fullTestRegistry(t)
	got, err := FilterRegistry(full, nil)
	if err != nil {
		t.Fatalf("FilterRegistry: %v", err)
	}
	if len(got.Names()) != len(full.Names()) {
		t.Fatalf("empty enabled must keep all tools: got %d, want %d", len(got.Names()), len(full.Names()))
	}
}

func TestFilterRegistryKeepsEnabledPlusMandatory(t *testing.T) {
	full := fullTestRegistry(t)
	got, err := FilterRegistry(full, []string{"http_fetch", "unknown_tool"})
	if err != nil {
		t.Fatalf("FilterRegistry: %v", err)
	}
	names := got.Names()
	sort.Strings(names)
	// http_fetch (enabled) + the two mandatory tools; unknown_tool ignored; schedule
	// and read_persona dropped.
	want := []string{AskUserQuestionToolName, CapabilityChangeToolName, "http_fetch"}
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("filtered names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("filtered names = %v, want %v", names, want)
		}
	}
	// Mandatory tools survive even though not in the enabled list.
	for _, m := range MandatoryToolNames() {
		if _, ok := got.Get(m); !ok {
			t.Errorf("mandatory tool %q was filtered out", m)
		}
	}
	// A non-enabled, non-mandatory tool is gone.
	if _, ok := got.Get("schedule_task"); ok {
		t.Error("schedule_task should have been filtered out")
	}
}
