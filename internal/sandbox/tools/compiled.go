// OWNER: T-096 (skills integration — canonical compiled tool registry)

package tools

// CompiledToolNames returns the canonical set of tool names a sandbox can have
// compiled in. It is the authoritative allowlist a skill's requested tools must be
// a SUBSET of: a skill can only ever enable capabilities the binary already
// implements, never introduce a new one (the sealed-runtime invariant — see
// internal/host/skills). Neither the control-plane daemon nor ironctl assembles the
// sandbox tool registry (that happens in cmd/sandbox.buildTools, inside the
// sandbox), so this exported list is how host-side skill validation knows the
// compiled set.
//
// It MUST stay in sync with cmd/sandbox.buildTools. If they drift, the only effect
// is that a skill naming a newly-added tool is conservatively rejected (fail-closed,
// never a capability leak). compiled_test.go guards the param-free entries against
// typos.
//
// http_fetch is included even though it is registered only when the egress broker
// socket is bound: a skill may legitimately want it, and whether the binary
// implements it (it does) is what this list answers — enabling it for a group is a
// separate, gateway-gated egress decision.
func CompiledToolNames() []string {
	// Tools whose constructors need no runtime context — their Name() is read
	// directly so the list can never disagree with the tool itself.
	paramFree := []Tool{
		NewRequestCapabilityChangeTool(),
		NewScheduleTaskTool(),
		NewAskUserQuestionTool(),
		NewReadPersonaTool(""),
		NewHTTPFetchTool(""),
	}
	paramFree = append(paramFree, TaskManagementTools()...)

	names := make([]string, 0, len(paramFree)+6)
	// The workspace file tools and messaging tools need a workspace / message
	// context to construct but expose fixed names; list those explicitly.
	names = append(names,
		"read_file", "write_file", "list_dir",
		"send_message", "send_file", "list_destinations",
	)
	for _, t := range paramFree {
		names = append(names, t.Name())
	}
	return names
}

// CompiledToolSet returns CompiledToolNames as a set, ready to pass as the
// knownTools argument to skills manifest validation / the skills Resolver.
func CompiledToolSet() map[string]bool {
	names := CompiledToolNames()
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}
