// Command ironctl is the IronClaw admin CLI. It is a thin client of the
// control-plane HTTP API: submit change requests, list pending approvals, and
// record approve/reject decisions.
//
// Usage:
//
//	ironctl [--addr URL] change submit --kind <k> --group <g> --by <user>
//	ironctl [--addr URL] change pending
//	ironctl [--addr URL] change history
//	ironctl [--addr URL] change approve <id> --by <user>
//	ironctl [--addr URL] change reject  <id> --by <user>
//	ironctl [--addr URL] audit [--limit N]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IronSecCo/ironclaw/internal/version"
)

const defaultAddr = "http://127.0.0.1:8787"

// token is the optional bearer token sent with every request. It defaults to the
// IRONCLAW_API_TOKEN env var and can be overridden with the global --token flag.
var token string

// verbose, set by the global -v/--verbose flag, restores the raw "HTTP <code>"
// status line on successful responses. By default success is quiet: callers see
// the response body (or nothing) without protocol noise.
var verbose bool

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ironctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// `ironctl version` / `--version` short-circuits before any flag parsing.
	if len(args) >= 1 && (args[0] == "version" || args[0] == "--version" || args[0] == "-version") {
		fmt.Println("ironctl " + version.String())
		return nil
	}

	// `ironctl help` / `--help` / `-h` is an explicit, friendly first-run banner
	// (not an error). It points newcomers at onboard/doctor/status before the
	// dense `usage` reference, and exits 0.
	if len(args) >= 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		firstRunHelp()
		return nil
	}

	// Global --addr / --token / -v can appear (in any order) before the subcommand.
	addr := defaultAddr
	token = os.Getenv("IRONCLAW_API_TOKEN")
parse:
	for len(args) >= 1 {
		switch args[0] {
		case "--addr":
			if len(args) < 2 {
				return fmt.Errorf("--addr needs a value")
			}
			addr = args[1]
			args = args[2:]
		case "--token":
			if len(args) < 2 {
				return fmt.Errorf("--token needs a value")
			}
			token = args[1]
			args = args[2:]
		case "-v", "--verbose":
			verbose = true
			args = args[1:]
		default:
			break parse
		}
	}
	if len(args) < 1 {
		// Bare `ironctl` (or only global flags) is a first-run user discovering the
		// tool, not a misuse — greet them and exit 0 instead of dumping the full
		// reference and failing.
		firstRunHelp()
		return nil
	}

	// A `-h`/`--help` flag *after* the subcommand (e.g. `change pending --help`)
	// prints the relevant usage block to stdout and exits 0, before the action
	// runs. Top-level `help`/`--help`/`-h` is already handled above; without this
	// guard a subcommand that takes no flags silently ignores the flag and
	// executes, so a user merely probing for options would run the command.
	if wantsHelp(args) {
		helpFor(args, os.Stdout)
		return nil
	}

	// Top-level "audit" command.
	if args[0] == "audit" {
		return cmdAudit(addr, args[1:])
	}

	// Top-level "registry" command (admin CRUD over /v1/registry/*).
	if args[0] == "registry" {
		return cmdRegistry(addr, args[1:])
	}

	// Top-level "onboard" command (guided first-run wizard).
	if args[0] == "onboard" {
		return cmdOnboard(addr, args[1:])
	}

	// Top-level observability commands (read-only over existing endpoints).
	if args[0] == "status" {
		return cmdStatus(addr, args[1:])
	}
	if args[0] == "doctor" {
		return cmdDoctor(addr, args[1:])
	}
	// `usage` / `commands` print the full command reference to stdout and exit 0:
	// a help request is requested output, not an error. The model-call metrics
	// report (which queries /metrics) now lives under `metrics`.
	if args[0] == "usage" || args[0] == "commands" {
		printReference(os.Stdout)
		return nil
	}
	if args[0] == "metrics" {
		return cmdUsage(addr, args[1:])
	}

	// Top-level "skill" command (host-side skills: add/list/remove).
	if args[0] == "skill" {
		return cmdSkill(addr, args[1:])
	}

	// Top-level "mcp" command (host-side MCP servers: list/add/remove/probe/grant).
	if args[0] == "mcp" {
		return cmdMCP(addr, args[1:])
	}

	// Top-level "vault" command (per-group credential policy: list/grant/revoke/set).
	if args[0] == "vault" {
		return cmdVault(addr, args[1:])
	}

	// Top-level "agent" command (friendly one-shot agent create/list/show/templates).
	if args[0] == "agent" {
		return cmdAgent(addr, args[1:])
	}

	// Top-level "tools" command (browse the built-in tool catalog).
	if args[0] == "tools" {
		return cmdTools(addr, args[1:])
	}

	if args[0] != "change" || len(args) < 2 {
		usage()
		return fmt.Errorf("expected: change <submit|pending|history|approve|reject>")
	}
	verb := args[1]
	rest := args[2:]

	switch verb {
	case "submit":
		return cmdSubmit(addr, rest)
	case "pending":
		return cmdPending(addr, rest)
	case "history":
		return cmdHistory(addr, rest)
	case "approve":
		return cmdDecision(addr, "approve", rest)
	case "reject":
		return cmdDecision(addr, "reject", rest)
	default:
		usage()
		return fmt.Errorf("unknown change verb %q", verb)
	}
}

// wantsHelp reports whether a `-h`/`--help` flag appears anywhere in args. It is
// used to intercept subcommand-level help before the action runs.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// helpFor writes the usage block most relevant to the requested command. Commands
// with their own reference (registry, agent) get it; everything else falls back to
// the full top-level reference.
func helpFor(args []string, w io.Writer) {
	if len(args) > 0 {
		switch args[0] {
		case "registry":
			registryUsage(w)
			return
		case "agent":
			agentUsage(w)
			return
		}
	}
	printReference(w)
}

func cmdSubmit(addr string, args []string) error {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	kind := fs.String("kind", "", "change kind (persona|enabled_tools|packages|wiring|permissions|mounts)")
	group := fs.String("group", "", "agent group id")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *kind == "" || *group == "" || *by == "" {
		return fmt.Errorf("change submit requires --kind, --group, --by")
	}
	body := map[string]any{
		"Kind":         *kind,
		"AgentGroupID": *group,
		"RequestedBy":  *by,
		"CreatedAt":    time.Now().UTC(),
	}
	return postJSON(addr+"/v1/changes", body)
}

// changeRow decodes one /v1/changes/{pending,history} entry. The control-plane
// serializes contract.ChangeRequest with capitalized, untagged keys (ID, Kind,
// AgentGroupID, …); Go's JSON decoder matches them case-insensitively.
type changeRow struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	AgentGroupID string          `json:"agentGroupID"`
	RequestedBy  string          `json:"requestedBy"`
	CreatedAt    time.Time       `json:"createdAt"`
	Before       json.RawMessage `json:"before"`
	After        json.RawMessage `json:"after"`
}

// historyRow decodes one /v1/changes/history entry (a request plus its outcome).
type historyRow struct {
	Request  changeRow `json:"request"`
	Status   string    `json:"status"`
	Decision *struct {
		Outcome   string    `json:"outcome"`
		DecidedBy string    `json:"decidedBy"`
		DecidedAt time.Time `json:"decidedAt"`
	} `json:"decision,omitempty"`
}

// auditRow decodes one /v1/audit entry.
type auditRow struct {
	Time     time.Time `json:"time"`
	Stage    string    `json:"stage"`
	ChangeID string    `json:"changeId"`
	Kind     string    `json:"kind"`
	Detail   string    `json:"detail"`
}

func cmdPending(addr string, args []string) error {
	asJSON, err := parseReadFlags("pending", args)
	if err != nil {
		return err
	}
	body, err := getRawJSON(addr + "/v1/changes/pending")
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(body)
	}
	var rows []changeRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("decode changes: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No changes awaiting approval.")
		return nil
	}
	tw := newTable("ID", "KIND", "GROUP", "REQUESTED-BY", "AGE")
	for _, r := range rows {
		tw.row(r.ID, r.Kind, r.AgentGroupID, r.RequestedBy, humanAge(r.CreatedAt))
	}
	tw.flush()
	fmt.Println(dim("\nApprove with: ironctl change approve <id> --by <you>"))
	return nil
}

func cmdHistory(addr string, args []string) error {
	asJSON, err := parseReadFlags("history", args)
	if err != nil {
		return err
	}
	body, err := getRawJSON(addr + "/v1/changes/history")
	if err != nil {
		return err
	}
	if asJSON {
		return printJSON(body)
	}
	var rows []historyRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("decode history: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No change history yet.")
		return nil
	}
	tw := newTable("ID", "KIND", "GROUP", "STATUS", "DECIDED-BY", "AGE")
	for _, r := range rows {
		decidedBy := "—"
		if r.Decision != nil && r.Decision.DecidedBy != "" {
			decidedBy = r.Decision.DecidedBy
		}
		tw.row(r.Request.ID, r.Request.Kind, r.Request.AgentGroupID, r.Status, decidedBy, humanAge(r.Request.CreatedAt))
	}
	tw.flush()
	return nil
}

func cmdAudit(addr string, args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	limit := fs.Int("limit", 100, "max recent audit entries to return")
	asJSON := fs.Bool("json", false, "emit raw JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := getRawJSON(fmt.Sprintf("%s/v1/audit?limit=%d", addr, *limit))
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(body)
	}
	var rows []auditRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return fmt.Errorf("decode audit: %w", err)
	}
	if len(rows) == 0 {
		fmt.Println("No audit entries yet.")
		return nil
	}
	tw := newTable("TIME", "STAGE", "CHANGE-ID", "KIND", "DETAIL")
	for _, r := range rows {
		ts := "—"
		if !r.Time.IsZero() {
			ts = r.Time.Local().Format("2006-01-02 15:04:05")
		}
		tw.row(ts, dash(r.Stage), dash(r.ChangeID), dash(r.Kind), dash(r.Detail))
	}
	tw.flush()
	return nil
}

// parseReadFlags parses the shared --json flag for the simple read commands.
func parseReadFlags(name string, args []string) (bool, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit raw JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return false, err
	}
	return *asJSON, nil
}

func cmdDecision(addr, outcome string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("change %s requires <id>", outcome)
	}
	id := args[0]
	fs := flag.NewFlagSet(outcome, flag.ContinueOnError)
	by := fs.String("by", "", "deciding user id (channel:handle)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	body := map[string]any{"outcome": outcome, "decidedBy": *by}
	return postJSON(fmt.Sprintf("%s/v1/changes/%s/decision", addr, id), body)
}

func postJSON(url string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
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
	return printBody(resp)
}

// httpGet issues an authenticated GET.
func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	addAuth(req)
	return http.DefaultClient.Do(req)
}

// addAuth attaches the bearer token if one is configured.
func addAuth(req *http.Request) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func printBody(resp *http.Response) error {
	out := bytes.TrimSpace(mustReadAll(resp.Body))
	if resp.StatusCode >= 400 {
		// Failures always surface the status (on stderr) so they're diagnosable,
		// regardless of -v. The body usually carries the server's reason.
		fmt.Fprintf(os.Stderr, "HTTP %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		if len(out) > 0 {
			fmt.Fprintln(os.Stderr, string(out))
		}
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	// Success is quiet by default: no raw "HTTP 200/202" line. The response body
	// (a JSON list/object) is the useful output; -v restores the status line.
	if verbose {
		fmt.Printf("HTTP %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if len(out) > 0 {
		fmt.Println(string(out))
	}
	return nil
}

func mustReadAll(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

// getRawJSON issues an authenticated GET and returns the response body. On a
// non-2xx status it prints the diagnostic to stderr and returns an error, so the
// primary stdout stream stays clean for the success path.
func getRawJSON(url string) ([]byte, error) {
	resp, err := httpGet(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body := bytes.TrimSpace(mustReadAll(resp.Body))
	if resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "HTTP %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		if len(body) > 0 {
			fmt.Fprintln(os.Stderr, string(body))
		}
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return body, nil
}

// printJSON re-indents a JSON body for the --json path. If the payload is not
// valid JSON it is printed verbatim.
func printJSON(body []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace(body), "", "  "); err != nil {
		fmt.Println(string(bytes.TrimSpace(body)))
		return nil
	}
	fmt.Println(buf.String())
	return nil
}

// humanAge renders a coarse "3m"/"2h"/"5d" age for a timestamp.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// dash returns "—" for an empty string so columns never look truncated.
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// table is a thin tabwriter wrapper that prints a dimmed header row followed by
// tab-aligned data rows.
type table struct {
	w *tabwriter.Writer
}

func newTable(headers ...string) *table {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, dim(strings.Join(headers, "\t")))
	return &table{w: tw}
}

func (t *table) row(cells ...string) {
	fmt.Fprintln(t.w, strings.Join(cells, "\t"))
}

func (t *table) flush() { _ = t.w.Flush() }

// firstRunHelp prints a short, friendly banner for `ironctl` with no command and
// for `ironctl help`. It curates a first-run path (onboard → doctor → status)
// ahead of the full reference so newcomers are not met with the dense `usage`
// block (Hick's Law), and it writes to stdout because it is requested output,
// not an error diagnostic.
func firstRunHelp() {
	fmt.Println(`ironctl ` + version.String() + ` — IronClaw control-plane admin CLI

New here? Start with:
  ironctl onboard      Guided first-run setup (checks deps, writes config)
  ironctl doctor       Diagnose your install (runtime, model creds, channels)
  ironctl status       Control-plane health at a glance

Everyday commands:
  ironctl agent create | list             Create and inspect agents
  ironctl change pending | approve <id>   Review gated capability changes
  ironctl audit                           Tail the append-only audit log

Run ` + "`ironctl usage`" + ` for the full command reference.
  --addr  defaults to ` + defaultAddr + `   ·   --token defaults to $IRONCLAW_API_TOKEN`)
}

// usage writes the full command reference to stderr. It is the error-path
// diagnostic shown when a command is missing or malformed.
func usage() { printReference(os.Stderr) }

// printReference writes the full command reference to w. `ironctl usage`
// (and `ironctl commands`) call it with os.Stdout and exit 0, because asking
// for the reference is requested output — not an error.
func printReference(w io.Writer) {
	fmt.Fprintln(w, `usage:
  ironctl help | usage | commands                                          (this reference; help is the short first-run banner)
  ironctl [--addr URL] [--token T] agent create [--name N] [--template T] [--tool X ...]   (guided in a terminal)
  ironctl [--addr URL] [--token T] agent list | show <id> | templates
  ironctl [--addr URL] [--token T] tools [list]
  ironctl [--addr URL] [--token T] change submit --kind <k> --group <g> --by <user>
  ironctl [--addr URL] [--token T] change pending [--json]
  ironctl [--addr URL] [--token T] change history [--json]
  ironctl [--addr URL] [--token T] change approve <id> --by <user>
  ironctl [--addr URL] [--token T] change reject  <id> --by <user>
  ironctl [--addr URL] [--token T] audit [--limit N] [--json]
  ironctl [--addr URL] [--token T] registry <resource> <verb> ...   (see: registry --help)
  ironctl [--addr URL] onboard [--yes] [--dry-run] [--force] [--config PATH]
  ironctl [--addr URL] [--token T] status [--json]
  ironctl [--addr URL] [--token T] doctor [--runtime BIN] [--model-proxy-socket PATH]
  ironctl [--addr URL] [--token T] metrics [--json]
  ironctl [--addr URL] [--token T] skill add <name>@<version> --group <id> [--by <user>]
  ironctl [--addr URL] [--token T] skill list
  ironctl [--addr URL] [--token T] skill remove <name>[@<version>]
  ironctl mcp serve [--http :ADDR] [--image IMG] [--docker BIN] [--timeout SECONDS]
  ironctl [--addr URL] [--token T] mcp list
  ironctl [--addr URL] [--token T] mcp add <name> (--command C [--arg A]... | --url U) [--image I] [--env K=V]... [--header K=V]...
  ironctl [--addr URL] [--token T] mcp probe <name>
  ironctl [--addr URL] [--token T] mcp grant <server> --group <id> [--tools a,b] [--by <user>]
  ironctl [--addr URL] [--token T] mcp remove <name>
  ironctl [--addr URL] [--token T] vault list --group <id> [--json]
  ironctl [--addr URL] [--token T] vault grant --group <id> --credential C --host H [--host H2]... [--by <user>]
  ironctl [--addr URL] [--token T] vault revoke --group <id> --credential C [--host H]... [--by <user>]
  ironctl [--addr URL] [--token T] vault set --group <id> --rule C=h1,h2 [--rule ...] [--by <user>]

  --addr  defaults to `+defaultAddr+`
  --token defaults to $IRONCLAW_API_TOKEN
  -v      verbose: also print the HTTP status line on success`)
}
