package isolation

import "testing"

// TestBuildOCISpecPersona asserts the persona is passed to the sandbox process as a
// --persona arg when set, and omitted (sealed default) when empty.
func TestBuildOCISpecPersona(t *testing.T) {
	// Default: no --persona arg.
	sealed, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	for _, a := range sealed.Process.Args {
		if a == "--persona" {
			t.Fatal("no --persona arg should appear when Persona is empty")
		}
	}

	// Set: --persona <text> appended after /sandbox.
	spec := hardenedTestSpec()
	spec.Persona = "You are a terse on-call assistant."
	withPersona, err := BuildOCISpec(spec)
	if err != nil {
		t.Fatalf("BuildOCISpec (persona): %v", err)
	}
	args := withPersona.Process.Args
	found := -1
	for i, a := range args {
		if a == "--persona" {
			found = i
			break
		}
	}
	if found < 0 || found+1 >= len(args) || args[found+1] != "You are a terse on-call assistant." {
		t.Fatalf("expected --persona with the text, got args=%v", args)
	}
}
