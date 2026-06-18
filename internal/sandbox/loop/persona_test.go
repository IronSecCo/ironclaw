package loop

import (
	"strings"
	"testing"
)

func TestSystemPromptWithEmpty(t *testing.T) {
	if got := SystemPromptWith(""); got != DefaultSystemPrompt {
		t.Error("empty persona must return the base prompt unchanged")
	}
	if got := SystemPromptWith("   \n  "); got != DefaultSystemPrompt {
		t.Error("whitespace-only persona must return the base prompt unchanged")
	}
}

func TestSystemPromptWithPersona(t *testing.T) {
	got := SystemPromptWith("You are a terse on-call assistant.")
	if !strings.HasPrefix(got, DefaultSystemPrompt) {
		t.Error("the security framing must come FIRST (persona is appended after)")
	}
	if !strings.Contains(got, "You are a terse on-call assistant.") {
		t.Error("persona text must be present")
	}
	// The base prompt's boundary rules must still be intact.
	if !strings.Contains(got, "request_capability_change") {
		t.Error("base security framing must be preserved")
	}
}
