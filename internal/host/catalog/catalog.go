// Package catalog is the human-facing, host-side description of what an agent can
// be made of: the built-in TOOLS a sandbox already implements (with friendly,
// operator-readable copy and grouping) and a small set of starter TEMPLATES that
// pre-fill a sensible persona + toolset.
//
// It exists so an operator can DEFINE an agent and ADD tools without knowing the
// internal tool names or hand-writing JSON: the web console and ironctl both read
// this one source of truth (the console over GET /v1/ui/{tools,templates}; ironctl
// by importing this package directly), so they can never disagree.
//
// Nothing here widens what an agent can do. The tool set is exactly the compiled
// registry (internal/sandbox/tools.CompiledToolNames) — a curated tool can only
// ever NAME a capability the sandbox binary already has, never introduce one. A
// drift guard in catalog_test.go fails the build if this list and the compiled set
// disagree, so the catalog is always truthful.
package catalog

import "github.com/IronSecCo/ironclaw/internal/sandbox/tools"

// Category groups tools for display. The order of CategoryOrder is the order the
// console and CLI render groups in.
type Category string

const (
	CategoryMessaging Category = "Messaging"
	CategoryFiles     Category = "Workspace files"
	CategoryWeb       Category = "Web & APIs"
	CategorySchedule  Category = "Scheduling"
	CategoryControl   Category = "Control"
)

// CategoryOrder is the canonical display order for tool categories.
func CategoryOrder() []Category {
	return []Category{CategoryMessaging, CategoryFiles, CategoryWeb, CategorySchedule, CategoryControl}
}

// ToolInfo is the friendly, operator-facing description of one built-in tool. Name
// is the exact compiled tool name an agent group enables; the rest is display copy.
type ToolInfo struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    Category `json:"category"`
	// Egress is true for tools that reach OUTSIDE the sandbox. Enabling the tool only
	// makes it visible to the agent; the actual call is still denied by the egress
	// broker until a human approves the specific host (it is a separate, gateway-gated
	// decision). The console badges these so the operator is not surprised by a 403.
	Egress bool `json:"egress"`
	// Mandatory is true for tools that are ALWAYS available regardless of a group's
	// enabled-tools restriction (see tools.MandatoryToolNames). The console shows
	// them ticked and disabled — they can't be turned off.
	Mandatory bool `json:"mandatory"`
}

// toolMeta is the curated display copy keyed by compiled tool name. The Egress and
// Mandatory flags are derived (not stored here) from the tools package so they can
// never drift from the runtime. catalog_test.go asserts the keys here are exactly
// tools.CompiledToolNames().
var toolMeta = map[string]struct {
	Title       string
	Description string
	Category    Category
}{
	// Messaging
	"send_message":      {"Send chat messages", "Reply in the current conversation, or post to a destination you've allowed.", CategoryMessaging},
	"send_file":         {"Send files", "Send a text file from its workspace to the current chat or an allowed destination.", CategoryMessaging},
	"list_destinations": {"List send destinations", "See the chats it's allowed to message — you control this list.", CategoryMessaging},
	// Workspace files
	"read_file":  {"Read files", "Read text files from its own private workspace.", CategoryFiles},
	"write_file": {"Write files", "Create or overwrite text files in its own private workspace.", CategoryFiles},
	"list_dir":   {"Browse folders", "List the contents of directories in its workspace.", CategoryFiles},
	// Web & APIs
	"web_search":         {"Web search", "Look up facts and recent events on the web. Brokered and audited; needs an approved search backend.", CategoryWeb},
	"http_fetch":         {"Call approved APIs", "Make HTTP requests to external APIs. Each host must be approved first, or the call is blocked.", CategoryWeb},
	"request_api_access": {"Request API access", "Let it ask you to approve a new external host to reach. You still approve every one.", CategoryWeb},
	// Scheduling
	"schedule_task": {"Schedule reminders", "Queue a prompt to itself for later, optionally recurring. Runs no code — just re-prompts.", CategorySchedule},
	"list_tasks":    {"List scheduled tasks", "See the reminders it has scheduled and whether each is active or paused.", CategorySchedule},
	"cancel_task":   {"Cancel a scheduled task", "Permanently cancel one of its scheduled reminders.", CategorySchedule},
	"pause_task":    {"Pause a scheduled task", "Pause a scheduled reminder until it's resumed.", CategorySchedule},
	"resume_task":   {"Resume a scheduled task", "Resume a previously paused reminder.", CategorySchedule},
	"update_task":   {"Update a scheduled task", "Change a scheduled reminder's prompt, time, or recurrence.", CategorySchedule},
	// Control
	"read_persona":              {"Read its own persona", "Let it read the persona you configured for it (read-only).", CategoryControl},
	"ask_user_question":         {"Ask you a question", "Pause and ask a human to decide before continuing. Always available.", CategoryControl},
	"request_capability_change": {"Request more access", "Let it ask you to approve more capabilities. This is its only escape hatch, so it's always available.", CategoryControl},
}

// Tools returns the full catalog of built-in tools with friendly copy, in a stable
// order (by category, then by name within a category). Egress/Mandatory flags are
// derived from the tools package.
func Tools() []ToolInfo {
	egress := map[string]bool{tools.HTTPFetchToolName: true, tools.WebSearchToolName: true}
	mandatory := map[string]bool{}
	for _, n := range tools.MandatoryToolNames() {
		mandatory[n] = true
	}

	byCat := map[Category][]ToolInfo{}
	for _, name := range tools.CompiledToolNames() {
		m, ok := toolMeta[name]
		if !ok {
			// A compiled tool with no curated copy: surface it rather than hide it, so
			// the catalog is never silently incomplete. The drift test prevents this in
			// practice; this is a belt-and-braces fallback.
			m.Title = name
			m.Description = "A built-in tool."
			m.Category = CategoryControl
		}
		byCat[m.Category] = append(byCat[m.Category], ToolInfo{
			Name: name, Title: m.Title, Description: m.Description,
			Category: m.Category, Egress: egress[name], Mandatory: mandatory[name],
		})
	}

	out := make([]ToolInfo, 0, len(toolMeta))
	for _, cat := range CategoryOrder() {
		group := byCat[cat]
		sortByName(group)
		out = append(out, group...)
		delete(byCat, cat)
	}
	// Any category not in CategoryOrder (shouldn't happen) appended last, deterministically.
	for _, cat := range CategoryOrder() {
		delete(byCat, cat)
	}
	return out
}

// sortByName orders a tool slice by Name (insertion sort keeps the dependency-free
// package tiny; the slices are ~6 items).
func sortByName(s []ToolInfo) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].Name > s[j].Name; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// PersonaSection is one document of the OpenClaw-style multi-document persona
// (separation of concerns): WHO the agent is, its PERSONALITY, and HOW it works. Key
// is the stable id stored in AgentGroup.PersonaDocs; the rest is display copy for the
// builder. Filename mirrors the familiar OpenClaw workspace files (IDENTITY.md /
// SOUL.md / AGENTS.md) so the convention is recognizable.
type PersonaSection struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Filename    string `json:"filename"`
	Placeholder string `json:"placeholder"`
	Help        string `json:"help"`
}

// PersonaSections returns the persona documents an agent is composed from, in display
// (and composition) order. The keys mirror registry.PersonaSectionKeys() exactly — a
// drift-guard test enforces it — so the builder, the catalog, and the host composer
// never disagree.
func PersonaSections() []PersonaSection {
	return []PersonaSection{
		{
			Key: "identity", Title: "Identity", Filename: "IDENTITY.md",
			Placeholder: "e.g. You are Atlas, a research assistant for the data team.",
			Help:        "Who it is — name, role, and what it's for. The face users see.",
		},
		{
			Key: "soul", Title: "Soul", Filename: "SOUL.md",
			Placeholder: "e.g. Curious and precise. You cite sources, admit uncertainty, and keep a dry wit.",
			Help:        "Personality — voice, values, and how it should sound. What it embodies.",
		},
		{
			Key: "instructions", Title: "Instructions", Filename: "AGENTS.md",
			Placeholder: "e.g. Search before answering, prefer primary sources, summarize with links.",
			Help:        "How it works — approach, tool use, and rules.",
		},
	}
}

// Template is a starter preset: a structured persona (identity/soul/instructions) plus
// a recommended toolset, so an operator can stand up a useful agent in one click
// without writing a system prompt or picking tools from scratch. Tools is the
// enabled-tools subset (empty means ALL compiled tools — the default). Model is left
// empty so the agent uses the control-plane's default backend unless the operator
// picks one. PersonaDocs keys are PersonaSections() keys.
type Template struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	PersonaDocs map[string]string `json:"personaDocs,omitempty"`
	Tools       []string          `json:"tools"`
	Model       string            `json:"model,omitempty"`
}

// Templates returns the built-in starter presets, in display order. Every tool named
// here is a compiled tool (catalog_test.go enforces this). Mandatory tools are
// omitted from the lists because they're always added at launch anyway.
func Templates() []Template {
	return []Template{
		{
			ID:          "blank",
			Name:        "Blank agent",
			Description: "Start from scratch — full toolset, no persona. Configure it yourself.",
			PersonaDocs: nil,
			Tools:       nil, // empty → all tools (the default, no restriction)
		},
		{
			ID:          "assistant",
			Name:        "General assistant",
			Description: "A friendly helper that chats, works with files, and sets reminders.",
			PersonaDocs: map[string]string{
				"identity":     "You are a general-purpose assistant helping one person get things done.",
				"soul":         "Friendly, concise, and practical. You get to the point, ask before assuming, and say when you're unsure.",
				"instructions": "Understand the request first. Use your tools when they help — send messages, work with files, set reminders. Confirm anything you scheduled or changed.",
			},
			Tools: []string{
				"send_message", "send_file", "list_destinations",
				"read_file", "write_file", "list_dir",
				"schedule_task", "list_tasks", "cancel_task", "pause_task", "resume_task", "update_task",
			},
		},
		{
			ID:          "researcher",
			Name:        "Research assistant",
			Description: "Looks things up on the web and summarizes with sources. Needs web access approved.",
			PersonaDocs: map[string]string{
				"identity":     "You are a research assistant who finds, verifies, and summarizes information.",
				"soul":         "Curious, rigorous, and honest. You cite sources by URL, separate fact from inference, and flag what you couldn't verify.",
				"instructions": "Search the web before answering questions you're unsure about. Prefer primary sources. Summarize clearly with links, and note gaps or conflicting evidence. Web/API tools need an approved host to work.",
			},
			Tools: []string{
				"web_search", "http_fetch", "request_api_access",
				"send_message", "send_file",
				"read_file", "write_file", "list_dir",
			},
		},
		{
			ID:          "reminders",
			Name:        "Reminders & check-ins",
			Description: "Schedules and manages recurring reminders and check-ins.",
			PersonaDocs: map[string]string{
				"identity":     "You are a reminder assistant that schedules and manages recurring check-ins.",
				"soul":         "Reliable and brief. You confirm exactly what you scheduled and when, and never nag.",
				"instructions": "When asked to remember or schedule something, create the task and confirm the what and when. Use list/update/cancel to keep the schedule tidy.",
			},
			Tools: []string{
				"schedule_task", "list_tasks", "cancel_task", "pause_task", "resume_task", "update_task",
				"send_message",
			},
		},
		{
			ID:          "triage",
			Name:        "On-call triage",
			Description: "Triages alerts from approved APIs and posts status updates. Needs API access approved.",
			PersonaDocs: map[string]string{
				"identity":     "You are an on-call triage assistant for an engineering team.",
				"soul":         "Terse, calm, and action-oriented. You cite alert IDs and propose concrete next steps.",
				"instructions": "Pull alert details from approved APIs, summarize impact, and recommend next actions. Post status updates to the right channel. API access needs an approved host.",
			},
			Tools: []string{
				"http_fetch", "request_api_access",
				"send_message", "send_file",
				"schedule_task", "list_tasks",
				"read_file", "write_file",
			},
		},
	}
}

// Template returns the template with the given id, and ok=false if none matches.
func TemplateByID(id string) (Template, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}
