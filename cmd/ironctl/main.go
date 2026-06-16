// OWNER: AGENT1

// Command ironctl is the IronClaw admin CLI. It is a thin client of the
// control-plane HTTP API: submit change requests, list pending approvals, and
// record approve/reject decisions.
//
// Usage:
//
//	ironctl [--addr URL] change submit --kind <k> --group <g> --by <user>
//	ironctl [--addr URL] change pending
//	ironctl [--addr URL] change approve <id> --by <user>
//	ironctl [--addr URL] change reject  <id> --by <user>
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
)

const defaultAddr = "http://127.0.0.1:8787"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ironctl:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Global --addr can appear before the subcommand.
	addr := defaultAddr
	for len(args) >= 2 && args[0] == "--addr" {
		addr = args[1]
		args = args[2:]
	}
	if len(args) < 2 || args[0] != "change" {
		usage()
		return fmt.Errorf("expected: change <submit|pending|approve|reject>")
	}
	verb := args[1]
	rest := args[2:]

	switch verb {
	case "submit":
		return cmdSubmit(addr, rest)
	case "pending":
		return cmdPending(addr)
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
	resp, err := http.Get(addr + "/v1/changes/pending")
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
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

func printBody(resp *http.Response) error {
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	if len(out) > 0 {
		fmt.Println(string(bytes.TrimSpace(out)))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  ironctl [--addr URL] change submit --kind <k> --group <g> --by <user>
  ironctl [--addr URL] change pending
  ironctl [--addr URL] change approve <id> --by <user>
  ironctl [--addr URL] change reject  <id> --by <user>

  --addr defaults to `+defaultAddr)
}
