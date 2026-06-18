package tools

import (
	"context"
	"testing"
)

func TestReadPersonaTool(t *testing.T) {
	tool := NewReadPersonaTool("You are a terse on-call assistant.")
	if tool.Name() != ReadPersonaToolName {
		t.Fatalf("name = %q, want %q", tool.Name(), ReadPersonaToolName)
	}
	out, err := tool.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if out != "You are a terse on-call assistant." {
		t.Fatalf("Invoke = %q", out)
	}

	// Empty persona returns a clear message, never an empty string.
	empty := NewReadPersonaTool("")
	out, err = empty.Invoke(context.Background(), nil)
	if err != nil || out == "" {
		t.Fatalf("empty persona Invoke = %q, %v", out, err)
	}
}
