package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// cmdMCP drives the host-side MCP surface over the control-plane API. Configuring a
// server is operator infrastructure; GRANTING an agent goes through the gateway's
// human-approval floor (kind mcp_access), so `grant` only proposes the change. ironctl
// is a thin client — the daemon (with --mcp-catalog set) owns the broker + verifier.
func cmdMCP(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected: mcp <serve|list|add|remove|probe|grant>")
	}
	switch args[0] {
	case "serve":
		// serve runs IronClaw ITSELF as an MCP server (sandbox_exec tool); it is
		// standalone (no control-plane API), so it ignores addr.
		return cmdMCPServe(args[1:])
	case "list", "ls":
		return cmdMCPList(addr)
	case "add", "put":
		return cmdMCPAdd(addr, args[1:])
	case "remove", "rm":
		return cmdMCPRemove(addr, args[1:])
	case "probe":
		return cmdMCPProbe(addr, args[1:])
	case "grant":
		return cmdMCPGrant(addr, args[1:])
	default:
		return fmt.Errorf("unknown mcp subcommand %q (want serve|list|add|remove|probe|grant)", args[0])
	}
}

// cmdMCPList prints the configured servers (secrets masked by the API).
func cmdMCPList(addr string) error {
	resp, err := httpGet(addr + "/v1/registry/mcp-servers")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

// cmdMCPAdd configures a server:
//
//	ironctl mcp add <name> --command npx --arg -y --arg @scope/server [--image I] [--env K=V] [--env K2=V2]
//	ironctl mcp add <name> --url https://mcp.example.com/rpc [--header "Authorization=Bearer ${TOK}"]
//
// A --url makes it a remote (http) server; otherwise --command makes it a local (stdio)
// server. Secrets should be ${ENV} references; the daemon resolves them host-side.
func cmdMCPAdd(addr string, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: mcp add <name> (--command C [--arg A]... | --url U) [--image I] [--env K=V]... [--header K=V]...")
	}
	name := args[0]
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	command := fs.String("command", "", "local server command (stdio)")
	urlFlag := fs.String("url", "", "remote server URL (http) — https required for non-loopback")
	image := fs.String("image", "", "container image for an isolated local server")
	var argv, envs, headers multiFlag
	fs.Var(&argv, "arg", "a local server argument (repeatable)")
	fs.Var(&envs, "env", "K=V environment entry (repeatable; values may be ${ENV})")
	fs.Var(&headers, "header", "K=V request header (repeatable; values may be ${ENV})")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	cfg := map[string]any{"name": name}
	switch {
	case *urlFlag != "":
		cfg["transport"] = "http"
		cfg["url"] = *urlFlag
		if h := kvPairs(headers); len(h) > 0 {
			cfg["headers"] = h
		}
	case *command != "":
		cfg["transport"] = "stdio"
		cfg["command"] = *command
		if len(argv) > 0 {
			cfg["args"] = []string(argv)
		}
		if *image != "" {
			cfg["image"] = *image
		}
		if e := kvPairs(envs); len(e) > 0 {
			cfg["env"] = e
		}
	default:
		return fmt.Errorf("mcp add needs --url (remote) or --command (local)")
	}
	return putJSON(addr+"/v1/registry/mcp-servers/"+url.PathEscape(name), cfg)
}

// cmdMCPRemove deletes a server from the catalog: `ironctl mcp remove <name>`.
func cmdMCPRemove(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: mcp remove <name>")
	}
	req, err := http.NewRequest(http.MethodDelete, addr+"/v1/registry/mcp-servers/"+url.PathEscape(args[0]), nil)
	if err != nil {
		return err
	}
	addAuth(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

// cmdMCPProbe connects to a server and prints its declared tools: `ironctl mcp probe <name>`.
func cmdMCPProbe(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: mcp probe <name>")
	}
	return postJSON(addr+"/v1/registry/mcp-servers/"+url.PathEscape(args[0])+"/probe", map[string]any{})
}

// cmdMCPGrant proposes an mcp_access change (-> human approval):
//
//	ironctl mcp grant <server> --group <id> [--tools a,b] [--by <user>]
//
// An empty --tools grants all of the server's tools.
func cmdMCPGrant(addr string, args []string) error {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: mcp grant <server> --group <id> [--tools a,b] [--by <user>]")
	}
	server := args[0]
	fs := flag.NewFlagSet("mcp grant", flag.ContinueOnError)
	group := fs.String("group", "", "target agent group id")
	tools := fs.String("tools", "", "comma-separated tool names (empty = all)")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *group == "" {
		return fmt.Errorf("usage: mcp grant <server> --group <id> [--tools a,b] [--by <user>]")
	}
	var toolList []string
	for _, t := range strings.Split(*tools, ",") {
		if t = strings.TrimSpace(t); t != "" {
			toolList = append(toolList, t)
		}
	}
	return postJSON(addr+"/v1/ui/config/change", map[string]any{
		"kind":         "mcp_access",
		"agentGroupID": *group,
		"requestedBy":  *by,
		"after":        map[string]any{"server": server, "tools": toolList},
	})
}

// putJSON issues an authenticated PUT with a JSON body.
func putJSON(u string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(b))
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

// multiFlag (a repeatable string flag) is defined in agent.go and reused here for
// --arg / --env / --header.

// kvPairs parses "K=V" entries into a map (entries without "=" are skipped).
func kvPairs(entries []string) map[string]string {
	out := map[string]string{}
	for _, e := range entries {
		if k, v, ok := strings.Cut(e, "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}
