package gateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// SetEnabledToolsFunc stores a group's approved enabled-tools set. Satisfied
// host-side in cmd/controlplane; a seam so the gateway stays decoupled from the
// registry package.
type SetEnabledToolsFunc func(id contract.AgentGroupID, tools []string) error

// GetEnabledToolsFunc reads a group's current enabled-tools set (nil/empty = the
// permissive default, i.e. every compiled tool). Optional; required only to apply the
// additive payload form.
type GetEnabledToolsFunc func(id contract.AgentGroupID) []string

// EnabledToolsApplier materializes an approved ChangeEnabledTools and stores the result
// on the target group, so the next sandbox launch reflects it (the mandatory request/
// ask tools are always kept — see tools.FilterRegistry). Every other kind passes
// through. It accepts three payload shapes:
//
//	["a","b"]            REPLACE — the full set (the web config editor, ui_config.go)
//	{"tools":["a","b"]}  REPLACE — same, named form
//	{"add":["web_search"]} ADD   — union into the group's current set (an agent asking
//	                              for ONE more tool without knowing — or clobbering —
//	                              the rest of its set)
//
// The ADD form is the one an agent uses via request_capability_change: requesting
// {"tools":["web_search"]} on a permissive group would REPLACE "all tools" with just
// web_search, silently removing everything else. ADD avoids that footgun, and on a
// permissive group (empty current set) it is a no-op — the group already has every
// tool, so it stays permissive rather than collapsing to a one-tool restriction.
type EnabledToolsApplier struct {
	set  SetEnabledToolsFunc
	get  GetEnabledToolsFunc // optional; needed only for the additive ({"add":[...]}) form
	next contract.Applier
}

// NewEnabledToolsApplier wraps next. set may be nil (a ChangeEnabledTools then errors
// rather than silently dropping); next may be nil.
func NewEnabledToolsApplier(set SetEnabledToolsFunc, next contract.Applier) *EnabledToolsApplier {
	return &EnabledToolsApplier{set: set, next: next}
}

// WithCurrentTools supplies a reader for a group's current enabled set, enabling the
// additive ({"add":[...]}) payload form. Without it an additive payload is rejected
// (the replace forms still work). Returns the receiver for chaining.
func (a *EnabledToolsApplier) WithCurrentTools(get GetEnabledToolsFunc) *EnabledToolsApplier {
	a.get = get
	return a
}

// Apply stores an approved enabled-tools set, then delegates.
func (a *EnabledToolsApplier) Apply(ctx context.Context, req contract.ChangeRequest, d contract.Decision) error {
	if req.Kind == contract.ChangeEnabledTools {
		if a.set == nil {
			return fmt.Errorf("enabled_tools apply: no enabled-tools setter wired")
		}
		tools, skip, err := a.resolveTools(req.AgentGroupID, req.After)
		if err != nil {
			return err
		}
		if !skip {
			if err := a.set(req.AgentGroupID, tools); err != nil {
				return fmt.Errorf("enabled_tools apply: %w", err)
			}
		}
	}
	if a.next != nil {
		return a.next.Apply(ctx, req, d)
	}
	return nil
}

// resolveTools maps the payload to the final enabled set. skip=true means "leave the
// group's set unchanged" (an additive request against a permissive group).
func (a *EnabledToolsApplier) resolveTools(id contract.AgentGroupID, payload json.RawMessage) (final []string, skip bool, err error) {
	// Bare array -> replace.
	var arr []string
	if json.Unmarshal(payload, &arr) == nil {
		return arr, false, nil
	}
	var p struct {
		Tools []string `json:"tools"`
		Add   []string `json:"add"`
	}
	if e := json.Unmarshal(payload, &p); e != nil {
		return nil, false, fmt.Errorf("enabled_tools apply: parse payload: %w", e)
	}
	if len(p.Add) > 0 {
		if a.get == nil {
			return nil, false, fmt.Errorf("enabled_tools apply: additive payload needs a current-tools reader")
		}
		current := a.get(id)
		if len(current) == 0 {
			// Permissive (empty = every compiled tool): adding one is a no-op; restricting
			// to {add} would strip all the others. Leave the group permissive.
			return nil, true, nil
		}
		return unionTools(current, p.Add), false, nil
	}
	return p.Tools, false, nil
}

// unionTools returns the de-duplicated union of two tool-name lists, preserving the
// order of the first then any new names from the second.
func unionTools(current, add []string) []string {
	seen := make(map[string]struct{}, len(current)+len(add))
	out := make([]string, 0, len(current)+len(add))
	for _, src := range [][]string{current, add} {
		for _, t := range src {
			if t == "" {
				continue
			}
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}
