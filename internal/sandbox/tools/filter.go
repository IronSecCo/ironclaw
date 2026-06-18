package tools

// MandatoryToolNames are always available to the agent regardless of a group's
// enabled-tools restriction. Without request_capability_change the agent could not
// even ASK for a change (its only escape hatch), and without ask_user_question it
// could not reach the operator — so a restriction can never remove them.
func MandatoryToolNames() []string {
	return []string{CapabilityChangeToolName, AskUserQuestionToolName}
}

// FilterRegistry returns a registry restricted to the tools enabled for a group:
// every mandatory tool, plus any tool whose name is in `enabled`.
//
// An EMPTY enabled set means "no restriction" — full is returned unchanged, so a
// group with no enabled-tools configured keeps all compiled tools (preserving the
// existing default). A name in `enabled` that the binary does not implement is
// ignored — a group/skill can only ever ENABLE a tool the sandbox already has, never
// conjure a new one.
func FilterRegistry(full *Registry, enabled []string) (*Registry, error) {
	if len(enabled) == 0 {
		return full, nil
	}
	keep := make(map[string]bool, len(enabled)+2)
	for _, n := range MandatoryToolNames() {
		keep[n] = true
	}
	for _, n := range enabled {
		keep[n] = true
	}
	out := NewRegistry()
	for _, t := range full.List() {
		if keep[t.Name()] {
			if err := out.Register(t); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}
