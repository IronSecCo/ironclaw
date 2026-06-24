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
		usage()
		return fmt.Errorf("expected: change <...> or audit")
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
	if args[0] == "usage" {
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
		return cmdPending(addr)
	case "history":
		return cmdHistory(addr)
	case "approve":
		return cmdDecision(addr, "approve", rest)
	case "reject":
		return cmdDecision(addr, "reject", rest)
	default:
		usage()
		return fmt.Errorf("unknown change verb %q", verb)
	}
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

func cmdPending(addr string) error {
	resp, err := httpGet(addr + "/v1/changes/pending")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

func cmdHistory(addr string) error {
	resp, err := httpGet(addr + "/v1/changes/history")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

func cmdAudit(addr string, args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	limit := fs.Int("limit", 100, "max recent audit entries to return")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := httpGet(fmt.Sprintf("%s/v1/audit?limit=%d", addr, *limit))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
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

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  ironctl [--addr URL] [--token T] agent create [--name N] [--template T] [--tool X ...]   (guided in a terminal)
  ironctl [--addr URL] [--token T] agent list | show <id> | templates
  ironctl [--addr URL] [--token T] tools [list]
  ironctl [--addr URL] [--token T] change submit --kind <k> --group <g> --by <user>
  ironctl [--addr URL] [--token T] change pending
  ironctl [--addr URL] [--token T] change history
  ironctl [--addr URL] [--token T] change approve <id> --by <user>
  ironctl [--addr URL] [--token T] change reject  <id> --by <user>
  ironctl [--addr URL] [--token T] audit [--limit N]
  ironctl [--addr URL] [--token T] registry <resource> <verb> ...   (see: registry --help)
  ironctl [--addr URL] onboard [--yes] [--dry-run] [--force] [--config PATH]
  ironctl [--addr URL] [--token T] status [--json]
  ironctl [--addr URL] [--token T] doctor [--runtime BIN] [--model-proxy-socket PATH]
  ironctl [--addr URL] [--token T] usage [--json]
  ironctl [--addr URL] [--token T] skill add <name>@<version> --group <id> [--by <user>]
  ironctl [--addr URL] [--token T] skill list
  ironctl [--addr URL] [--token T] skill remove <name>[@<version>]
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
