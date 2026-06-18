package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nivardsec/ironclaw/internal/host/catalog"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// cmdAgent is the friendly, one-shot agent surface: define a complete agent (name +
// persona + tools + model) in a single command, instead of a registry PUT followed
// by several gateway changes. It is a thin client of PUT /v1/registry/agent-groups/{id}
// — the same operator-direct write the console uses — and resolves templates/tools
// from the built-in catalog package so the CLI and console can never disagree.
func cmdAgent(addr string, args []string) error {
	if len(args) < 1 {
		agentUsage()
		return fmt.Errorf("expected: agent <create|list|show|templates>")
	}
	switch args[0] {
	case "create", "new":
		return cmdAgentCreate(addr, args[1:])
	case "list", "ls":
		return cmdAgentList(addr)
	case "show", "get":
		return cmdAgentShow(addr, args[1:])
	case "templates":
		return cmdAgentTemplates()
	default:
		agentUsage()
		return fmt.Errorf("unknown agent subcommand %q (want create|list|show|templates)", args[0])
	}
}

// agentGroupBody is the create/edit payload. The lowercase json tags decode (case-
// insensitively) into registry.AgentGroup on the server; omitempty keeps the wire
// minimal so an unset field never clobbers a default.
type agentGroupBody struct {
	Name         string            `json:"name"`
	Folder       string            `json:"folder"`
	Provider     string            `json:"provider,omitempty"`
	Model        string            `json:"model,omitempty"`
	Persona      string            `json:"persona,omitempty"`
	PersonaDocs  map[string]string `json:"personaDocs,omitempty"`
	EnabledTools []string          `json:"enabledTools,omitempty"`
}

// multiFlag collects a repeatable string flag (e.g. --tool web_search --tool http_fetch).
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func cmdAgentCreate(addr string, args []string) error {
	fs := flag.NewFlagSet("agent create", flag.ContinueOnError)
	name := fs.String("name", "", "display name (e.g. \"Research Bot\")")
	id := fs.String("id", "", "agent id (defaults to a slug of the name)")
	template := fs.String("template", "", "starter template id (see: ironctl agent templates)")
	persona := fs.String("persona", "", "legacy single-blob persona (prefer --identity/--soul/--instructions)")
	identity := fs.String("identity", "", "IDENTITY.md — who it is (overrides the template's)")
	soul := fs.String("soul", "", "SOUL.md — personality/voice (overrides the template's)")
	instructions := fs.String("instructions", "", "AGENTS.md — how it works (overrides the template's)")
	personaDir := fs.String("persona-dir", "", "load IDENTITY.md/SOUL.md/AGENTS.md from a directory")
	model := fs.String("model", "", "model id (blank = the control-plane default)")
	provider := fs.String("provider", "", "model provider (blank = the default backend)")
	allTools := fs.Bool("all-tools", false, "enable every built-in tool (no restriction)")
	yes := fs.Bool("yes", false, "don't prompt; create with the given flags")
	var toolFlags multiFlag
	fs.Var(&toolFlags, "tool", "enable a built-in tool by name (repeatable; see: ironctl tools)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	interactive := !*yes && *name == "" && stdinIsTerminal()
	if interactive {
		return agentCreateInteractive(addr)
	}

	// Non-interactive: --name (or --id) is required.
	if *name == "" && *id == "" {
		return fmt.Errorf("agent create requires --name (or run it in a terminal for the guided flow)")
	}
	display := *name
	if display == "" {
		display = *id
	}
	agentID := *id
	if agentID == "" {
		agentID = slugify(*name)
	}
	if agentID == "" {
		return fmt.Errorf("could not derive an id from %q — pass --id", *name)
	}

	// Resolve the template, then layer explicit flags on top.
	body := agentGroupBody{Name: display, Folder: agentID}
	if *template != "" {
		tpl, ok := catalog.TemplateByID(*template)
		if !ok {
			return fmt.Errorf("unknown template %q (see: ironctl agent templates)", *template)
		}
		body.PersonaDocs = copyDocs(tpl.PersonaDocs)
		body.Model = tpl.Model
		body.EnabledTools = append([]string(nil), tpl.Tools...)
	}
	// Persona documents: template defaults, overridden by --persona-dir files, then by
	// the inline --identity/--soul/--instructions flags.
	docs, err := resolvePersonaDocs(body.PersonaDocs, *personaDir, *identity, *soul, *instructions)
	if err != nil {
		return err
	}
	body.PersonaDocs = docs
	if *persona != "" {
		body.Persona = *persona // legacy single blob; ComposePersona prefers docs when set
	}
	if *model != "" {
		body.Model = *model
	}
	if *provider != "" {
		body.Provider = *provider
	}
	// --tool flags union into the enabled set; unknown names are a hard error so a
	// typo can't silently leave a tool out.
	if len(toolFlags) > 0 {
		merged, err := mergeTools(body.EnabledTools, toolFlags)
		if err != nil {
			return err
		}
		body.EnabledTools = merged
	}
	if *allTools {
		body.EnabledTools = nil // empty = all tools (no restriction)
	}

	return putAgent(addr, agentID, body, false)
}

// agentCreateInteractive runs the guided, OpenClaw-style flow: name → template →
// model → tools → confirm. It only runs on a TTY (see cmdAgentCreate).
func agentCreateInteractive(addr string) error {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("Let's create an agent. Press Enter to accept the [default].")
	fmt.Println()

	name := ask(in, "Name", "")
	for strings.TrimSpace(name) == "" {
		fmt.Println("  A name is required.")
		name = ask(in, "Name", "")
	}
	agentID := slugify(name)
	if agentID == "" {
		return fmt.Errorf("could not derive an id from %q", name)
	}
	fmt.Printf("  → id: %s\n\n", agentID)

	// Template pick.
	tpls := catalog.Templates()
	fmt.Println("Start from a template:")
	for i, t := range tpls {
		fmt.Printf("  %d) %-22s %s\n", i+1, t.Name, t.Description)
	}
	choice := ask(in, fmt.Sprintf("Template [1-%d]", len(tpls)), "1")
	ti, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || ti < 1 || ti > len(tpls) {
		ti = 1
	}
	tpl := tpls[ti-1]
	fmt.Printf("  → %s\n\n", tpl.Name)

	body := agentGroupBody{
		Name:         name,
		Folder:       agentID,
		PersonaDocs:  copyDocs(tpl.PersonaDocs),
		Model:        tpl.Model,
		EnabledTools: append([]string(nil), tpl.Tools...),
	}

	// Persona — three short documents (OpenClaw-style). Enter keeps the template's text.
	fmt.Println("Persona — describe the agent in three short sections (Enter to keep the template's):")
	if body.PersonaDocs == nil {
		body.PersonaDocs = map[string]string{}
	}
	for _, sec := range catalog.PersonaSections() {
		fmt.Printf("  %s (%s) — %s\n", sec.Title, sec.Filename, sec.Help)
		v := strings.TrimSpace(ask(in, "  "+sec.Title, body.PersonaDocs[sec.Key]))
		if v != "" {
			body.PersonaDocs[sec.Key] = v
		} else {
			delete(body.PersonaDocs, sec.Key)
		}
	}
	if len(body.PersonaDocs) == 0 {
		body.PersonaDocs = nil
	}
	if err := registry.ValidatePersonaDocs(body.PersonaDocs); err != nil {
		return err
	}
	fmt.Println()

	// Model.
	body.Model = strings.TrimSpace(ask(in, "Model (blank = control-plane default)", body.Model))
	fmt.Println()

	// Tools.
	fmt.Println("Tools to enable:")
	printToolChecklist(body.EnabledTools)
	fmt.Println("  Enter to keep, 'all' for every tool, or numbers to choose (e.g. 1,4,7).")
	sel := strings.TrimSpace(ask(in, "Tools", ""))
	switch {
	case sel == "":
		// keep template tools
	case strings.EqualFold(sel, "all"):
		body.EnabledTools = nil
	default:
		picked, perr := parseToolSelection(sel)
		if perr != nil {
			return perr
		}
		body.EnabledTools = picked
	}
	fmt.Println()

	// Confirm.
	fmt.Println("About to create:")
	printAgentSummary(body, agentID)
	if !askYesNo(in, "Create it?", true) {
		fmt.Println("Cancelled.")
		return nil
	}
	return putAgent(addr, agentID, body, true)
}

// putAgent writes the agent group and prints a friendly result. quiet=false prints a
// next-steps hint (used by the guided flow and one-shot create alike).
func putAgent(addr, id string, body agentGroupBody, _ bool) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, addr+"/v1/registry/agent-groups/"+seg(id), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	addAuth(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, strings.TrimSpace(string(out)))
		return fmt.Errorf("create failed with status %d", resp.StatusCode)
	}
	fmt.Printf("\n✓ Created agent %q (%s)\n", body.Name, id)
	toolsLabel := "all built-in tools"
	if len(body.EnabledTools) > 0 {
		toolsLabel = fmt.Sprintf("%s (%d)", strings.Join(body.EnabledTools, ", "), len(body.EnabledTools))
	}
	modelLabel := body.Model
	if modelLabel == "" {
		modelLabel = "(control-plane default)"
	}
	fmt.Printf("  model:  %s\n", modelLabel)
	fmt.Printf("  tools:  %s\n", toolsLabel)
	if hasEgressTool(body.EnabledTools) {
		fmt.Printf("  note:   web/API tools still need an approved host before they work\n")
	}
	fmt.Printf("  next:   ironctl agent show %s   ·   wire a channel in the console (/ui/)\n", id)
	return nil
}

func cmdAgentList(addr string) error {
	resp, err := httpGet(addr + "/v1/ui/agents")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	var views []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Model        string `json:"model"`
		Sessions     int    `json:"sessions"`
		Destinations int    `json:"destinations"`
	}
	if err := json.Unmarshal(body, &views); err != nil {
		return fmt.Errorf("decode agents: %w", err)
	}
	if len(views) == 0 {
		fmt.Println("No agents yet. Create one with: ironctl agent create")
		return nil
	}
	fmt.Printf("%-24s %-22s %-16s %8s %8s\n", "ID", "NAME", "MODEL", "SESSIONS", "CHANNELS")
	for _, v := range views {
		model := v.Model
		if model == "" {
			model = "(default)"
		}
		fmt.Printf("%-24s %-22s %-16s %8d %8d\n", v.ID, truncate(v.Name, 22), truncate(model, 16), v.Sessions, v.Destinations)
	}
	return nil
}

func cmdAgentShow(addr string, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: agent show <id>")
	}
	resp, err := httpGet(addr + "/v1/registry/agent-groups/" + seg(args[0]))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	var g struct {
		ID              string            `json:"id"`
		Name            string            `json:"name"`
		Folder          string            `json:"folder"`
		Provider        string            `json:"provider"`
		Model           string            `json:"model"`
		Persona         string            `json:"persona"`
		PersonaDocs     map[string]string `json:"personaDocs"`
		EnabledTools    []string          `json:"enabledTools"`
		InstalledSkills []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"installedSkills"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		return fmt.Errorf("decode agent: %w", err)
	}
	fmt.Printf("%s (%s)\n", g.Name, g.ID)
	if g.Model != "" || g.Provider != "" {
		fmt.Printf("  model:    %s %s\n", g.Model, dim(g.Provider))
	} else {
		fmt.Printf("  model:    (control-plane default)\n")
	}
	if len(g.EnabledTools) == 0 {
		fmt.Printf("  tools:    all built-in tools\n")
	} else {
		fmt.Printf("  tools:    %s\n", strings.Join(g.EnabledTools, ", "))
	}
	if len(g.InstalledSkills) > 0 {
		names := make([]string, 0, len(g.InstalledSkills))
		for _, s := range g.InstalledSkills {
			names = append(names, s.Name+"@"+s.Version)
		}
		fmt.Printf("  skills:   %s\n", strings.Join(names, ", "))
	}
	if len(g.PersonaDocs) > 0 {
		fmt.Printf("  persona:\n")
		for _, sec := range catalog.PersonaSections() {
			if v := strings.TrimSpace(g.PersonaDocs[sec.Key]); v != "" {
				fmt.Printf("    %s (%s):\n      %s\n", sec.Title, sec.Filename, strings.ReplaceAll(v, "\n", "\n      "))
			}
		}
	} else if g.Persona != "" {
		fmt.Printf("  persona:  %s\n", g.Persona)
	}
	return nil
}

func cmdAgentTemplates() error {
	fmt.Println("Starter templates (use with: ironctl agent create --template <id>):")
	fmt.Println()
	for _, t := range catalog.Templates() {
		tools := "all built-in tools"
		if len(t.Tools) > 0 {
			tools = fmt.Sprintf("%d tools", len(t.Tools))
		}
		fmt.Printf("  %-12s %s\n", t.ID, t.Name)
		fmt.Printf("  %-12s %s\n", "", dim(t.Description))
		fmt.Printf("  %-12s %s\n\n", "", dim(tools))
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

// mergeTools unions extra into base, validating every extra name against the
// catalog so a typo fails loudly. base is assumed already-valid (from a template).
func mergeTools(base, extra []string) ([]string, error) {
	known := map[string]bool{}
	for _, ti := range catalog.Tools() {
		known[ti.Name] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, t := range base {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range extra {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !known[t] {
			return nil, fmt.Errorf("unknown tool %q (see: ironctl tools)", t)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// parseToolSelection turns "1,4,7" into the corresponding tool names from the
// catalog's display order (the same order printToolChecklist prints).
func parseToolSelection(sel string) ([]string, error) {
	all := catalog.Tools()
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(all) {
			return nil, fmt.Errorf("invalid selection %q (pick 1-%d, comma-separated)", part, len(all))
		}
		name := all[n-1].Name
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// printToolChecklist prints the catalog with a [x] for tools in `enabled` (empty
// `enabled` means all, so every box is checked). Mandatory tools show as always-on.
func printToolChecklist(enabled []string) {
	on := map[string]bool{}
	for _, t := range enabled {
		on[t] = true
	}
	all := len(enabled) == 0
	for i, ti := range catalog.Tools() {
		mark := " "
		if all || on[ti.Name] || ti.Mandatory {
			mark = "x"
		}
		badge := ""
		if ti.Mandatory {
			badge = dim(" (always on)")
		} else if ti.Egress {
			badge = dim(" (needs approval)")
		}
		fmt.Printf("  [%s] %2d) %-26s %s%s\n", mark, i+1, ti.Name, ti.Title, badge)
	}
}

func printAgentSummary(body agentGroupBody, id string) {
	fmt.Printf("  name:    %s (%s)\n", body.Name, id)
	model := body.Model
	if model == "" {
		model = "(control-plane default)"
	}
	fmt.Printf("  model:   %s\n", model)
	if len(body.EnabledTools) == 0 {
		fmt.Printf("  tools:   all built-in tools\n")
	} else {
		fmt.Printf("  tools:   %s\n", strings.Join(body.EnabledTools, ", "))
	}
	if len(body.PersonaDocs) > 0 {
		for _, sec := range catalog.PersonaSections() {
			if v := strings.TrimSpace(body.PersonaDocs[sec.Key]); v != "" {
				fmt.Printf("  %-8s %s\n", strings.ToLower(sec.Title)+":", truncate(v, 64))
			}
		}
	} else if body.Persona != "" {
		fmt.Printf("  persona: %s\n", truncate(body.Persona, 64))
	}
}

func hasEgressTool(enabled []string) bool {
	if len(enabled) == 0 {
		return true // all tools includes the egress ones
	}
	eg := map[string]bool{}
	for _, ti := range catalog.Tools() {
		if ti.Egress {
			eg[ti.Name] = true
		}
	}
	for _, t := range enabled {
		if eg[t] {
			return true
		}
	}
	return false
}

// resolvePersonaDocs layers persona documents: a template's defaults (base), then any
// IDENTITY.md/SOUL.md/AGENTS.md found under dir, then the inline overrides. Empty
// sections are dropped; the result is validated against the known section schema, and
// nil is returned when nothing is set (so the agent keeps the default/legacy persona).
func resolvePersonaDocs(base map[string]string, dir, identity, soul, instructions string) (map[string]string, error) {
	docs := copyDocs(base)
	if docs == nil {
		docs = map[string]string{}
	}
	if dir != "" {
		for _, sec := range catalog.PersonaSections() {
			b, err := os.ReadFile(filepath.Join(dir, sec.Filename))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("read %s from %s: %w", sec.Filename, dir, err)
			}
			docs[sec.Key] = strings.TrimSpace(string(b))
		}
	}
	if identity != "" {
		docs[registry.PersonaIdentity] = identity
	}
	if soul != "" {
		docs[registry.PersonaSoul] = soul
	}
	if instructions != "" {
		docs[registry.PersonaInstructions] = instructions
	}
	for k, v := range docs {
		if strings.TrimSpace(v) == "" {
			delete(docs, k)
		}
	}
	if len(docs) == 0 {
		return nil, nil
	}
	if err := registry.ValidatePersonaDocs(docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// copyDocs returns a shallow copy of a persona-docs map (nil for an empty input).
func copyDocs(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// slugify turns a friendly name into a stable id ("Research Bot" → "research-bot").
// It mirrors the web console's slugify so the two surfaces derive the same id.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// dim wraps text in a faint ANSI style when stdout is a terminal, else returns it
// plain (so piped output stays clean).
func dim(s string) string {
	if s == "" {
		return ""
	}
	if !stdoutIsTerminal() {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}

// ask prompts for a single line, returning def when the input is empty.
func ask(in *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, _ := in.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return def
	}
	return line
}

func askYesNo(in *bufio.Reader, label string, def bool) bool {
	d := "y/N"
	if def {
		d = "Y/n"
	}
	fmt.Printf("%s [%s]: ", label, d)
	line, _ := in.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	if line == "" {
		return def
	}
	return line == "y" || line == "yes"
}

// stdinIsTerminal reports whether stdin is an interactive terminal (a char device),
// using only the standard library so ironctl pulls in no new dependency.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func agentUsage() {
	fmt.Fprintln(os.Stderr, `agent subcommands:
  agent create [--name N] [--template T] [--model M] [--persona TEXT] [--tool X ...] [--all-tools] [--yes]
               run with no flags in a terminal for a guided wizard
  agent list
  agent show <id>
  agent templates`)
}
