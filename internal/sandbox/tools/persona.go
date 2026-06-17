// OWNER: T-234 (persona — read-only persona tool)

package tools

import (
	"context"
	"encoding/json"
)

// ReadPersonaToolName is the read-only persona tool's name.
const ReadPersonaToolName = "read_persona"

// ReadPersonaTool lets the agent READ its configured persona — the system-persona
// the operator set via the approval gateway, injected host-side at launch. It is
// deliberately read-only: there is no edit/write counterpart, so an agent can never
// change its own persona. Changing it must go through request_capability_change →
// gateway → human approval, like any other capability change.
type ReadPersonaTool struct {
	persona string
}

// NewReadPersonaTool builds the tool over the launch-time persona text (may be empty).
func NewReadPersonaTool(persona string) *ReadPersonaTool {
	return &ReadPersonaTool{persona: persona}
}

func (t *ReadPersonaTool) Name() string { return ReadPersonaToolName }

func (t *ReadPersonaTool) Description() string {
	return "Read your configured persona (set by the operator via the approval gateway). Read-only — to change it, call request_capability_change."
}

func (t *ReadPersonaTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

func (t *ReadPersonaTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) {
	if t.persona == "" {
		return "(no persona configured for this agent group)", nil
	}
	return t.persona, nil
}
