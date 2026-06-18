package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// cmdRegistry dispatches the `registry` resource subcommands, each a thin client
// of the control-plane registry admin API (/v1/registry/*, see internal/host/api).
func cmdRegistry(addr string, args []string) error {
	if len(args) < 1 {
		registryUsage()
		return fmt.Errorf("expected a registry resource (agent-group|messaging-group|wiring|user|role|member|destination|session|access)")
	}
	resource, rest := args[0], args[1:]
	switch resource {
	case "agent-group":
		return cmdRegAgentGroup(addr, rest)
	case "messaging-group":
		return cmdRegMessagingGroup(addr, rest)
	case "wiring":
		return cmdRegWiring(addr, rest)
	case "user":
		return cmdRegUser(addr, rest)
	case "role":
		return cmdRegRole(addr, rest)
	case "member":
		return cmdRegMember(addr, rest)
	case "destination":
		return cmdRegDestination(addr, rest)
	case "session":
		return cmdRegSession(addr, rest)
	case "access":
		return cmdRegAccess(addr, rest)
	default:
		registryUsage()
		return fmt.Errorf("unknown registry resource %q", resource)
	}
}

// reqJSON issues an authenticated request with an optional JSON body and prints
// the response. A nil body sends no payload (used for GET).
func reqJSON(method, url string, body any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	addAuth(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return printBody(resp)
}

// seg path-escapes an id for safe use as a single URL path segment (ids like
// "slack:handle" contain a colon).
func seg(id string) string { return url.PathEscape(id) }

func cmdRegAgentGroup(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry agent-group <put|get> ...")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("agent-group "+verb, flag.ContinueOnError)
	id := fs.String("id", "", "agent group id")
	name := fs.String("name", "", "display name (put)")
	folder := fs.String("folder", "", "workspace folder (put)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("registry agent-group %s requires --id", verb)
	}
	base := addr + "/v1/registry/agent-groups/" + seg(*id)
	switch verb {
	case "put":
		return reqJSON(http.MethodPut, base, map[string]any{"name": *name, "folder": *folder})
	case "get":
		return reqJSON(http.MethodGet, base, nil)
	default:
		return fmt.Errorf("registry agent-group: unknown verb %q (want put|get)", verb)
	}
}

func cmdRegMessagingGroup(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry messaging-group <create|get|wirings> ...")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "create":
		fs := flag.NewFlagSet("messaging-group create", flag.ContinueOnError)
		channel := fs.String("channel", "", "channel type (e.g. slack)")
		platform := fs.String("platform", "", "platform id (chat/channel id)")
		instance := fs.String("instance", "", "adapter instance (defaults to channel)")
		isGroup := fs.Bool("group", false, "the chat is a group/channel (not a DM)")
		policy := fs.String("policy", "", "unknown-sender policy (strict|public)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *channel == "" || *platform == "" {
			return fmt.Errorf("registry messaging-group create requires --channel and --platform")
		}
		return reqJSON(http.MethodPost, addr+"/v1/registry/messaging-groups", map[string]any{
			"channelType": *channel, "platformID": *platform, "instance": *instance,
			"isGroup": *isGroup, "unknownSenderPolicy": *policy,
		})
	case "get", "wirings":
		fs := flag.NewFlagSet("messaging-group "+verb, flag.ContinueOnError)
		id := fs.String("id", "", "messaging group id")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("registry messaging-group %s requires --id", verb)
		}
		path := addr + "/v1/registry/messaging-groups/" + seg(*id)
		if verb == "wirings" {
			path += "/wirings"
		}
		return reqJSON(http.MethodGet, path, nil)
	default:
		return fmt.Errorf("registry messaging-group: unknown verb %q (want create|get|wirings)", verb)
	}
}

func cmdRegWiring(addr string, args []string) error {
	if len(args) < 1 || args[0] != "create" {
		return fmt.Errorf("registry wiring create ... (list lives under messaging-group wirings --id <mg>)")
	}
	fs := flag.NewFlagSet("wiring create", flag.ContinueOnError)
	id := fs.String("id", "", "wiring id (optional; server assigns one when omitted)")
	mg := fs.String("mg", "", "messaging group id")
	agent := fs.String("agent", "", "agent group id")
	engage := fs.String("engage", "", "engage mode (pattern|mention|mention-sticky)")
	pattern := fs.String("pattern", "", "engage pattern (for engage=pattern)")
	scope := fs.String("scope", "", "sender scope (all|known)")
	ignored := fs.String("ignored", "", "ignored-message policy (drop|accumulate)")
	session := fs.String("session", "", "session mode (shared|per-thread|agent-shared)")
	priority := fs.Int("priority", 0, "wiring priority (higher first)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *mg == "" || *agent == "" || *engage == "" {
		return fmt.Errorf("registry wiring create requires --mg, --agent, --engage")
	}
	return reqJSON(http.MethodPost, addr+"/v1/registry/wirings", map[string]any{
		"id": *id, "messagingGroupID": *mg, "agentGroupID": *agent,
		"engageMode": *engage, "engagePattern": *pattern, "senderScope": *scope,
		"ignoredMessagePolicy": *ignored, "sessionMode": *session, "priority": *priority,
	})
}

func cmdRegUser(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry user <put|get> ...")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("user "+verb, flag.ContinueOnError)
	id := fs.String("id", "", "user id (channel:handle)")
	kind := fs.String("kind", "", "user kind (put)")
	name := fs.String("name", "", "display name (put)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *id == "" {
		return fmt.Errorf("registry user %s requires --id", verb)
	}
	base := addr + "/v1/registry/users/" + seg(*id)
	switch verb {
	case "put":
		return reqJSON(http.MethodPut, base, map[string]any{"kind": *kind, "displayName": *name})
	case "get":
		return reqJSON(http.MethodGet, base, nil)
	default:
		return fmt.Errorf("registry user: unknown verb %q (want put|get)", verb)
	}
}

func cmdRegRole(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry role <grant|revoke> ...")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("role "+verb, flag.ContinueOnError)
	user := fs.String("user", "", "user id (channel:handle)")
	role := fs.String("role", "", "role (owner|admin)")
	agent := fs.String("agent", "", "agent group id to scope to (omit for a global role)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *user == "" || *role == "" {
		return fmt.Errorf("registry role %s requires --user and --role", verb)
	}
	body := map[string]any{"userID": *user, "role": *role}
	if *agent != "" {
		body["agentGroupID"] = *agent
	}
	switch verb {
	case "grant":
		return reqJSON(http.MethodPost, addr+"/v1/registry/roles", body)
	case "revoke":
		return reqJSON(http.MethodPost, addr+"/v1/registry/roles/revoke", body)
	default:
		return fmt.Errorf("registry role: unknown verb %q (want grant|revoke)", verb)
	}
}

func cmdRegMember(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry member <add|remove> ...")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("member "+verb, flag.ContinueOnError)
	user := fs.String("user", "", "user id (channel:handle)")
	agent := fs.String("agent", "", "agent group id")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *user == "" || *agent == "" {
		return fmt.Errorf("registry member %s requires --user and --agent", verb)
	}
	body := map[string]any{"userID": *user, "agentGroupID": *agent}
	switch verb {
	case "add":
		return reqJSON(http.MethodPost, addr+"/v1/registry/members", body)
	case "remove":
		return reqJSON(http.MethodPost, addr+"/v1/registry/members/remove", body)
	default:
		return fmt.Errorf("registry member: unknown verb %q (want add|remove)", verb)
	}
}

func cmdRegDestination(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry destination <add|check> ...")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("destination "+verb, flag.ContinueOnError)
	agent := fs.String("agent", "", "agent group id")
	channel := fs.String("channel", "", "channel type")
	platform := fs.String("platform", "", "platform id")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *agent == "" || *channel == "" || *platform == "" {
		return fmt.Errorf("registry destination %s requires --agent, --channel, --platform", verb)
	}
	switch verb {
	case "add":
		return reqJSON(http.MethodPost, addr+"/v1/registry/destinations", map[string]any{
			"agentGroupID": *agent, "channelType": *channel, "platformID": *platform,
		})
	case "check":
		q := url.Values{}
		q.Set("agentGroupID", *agent)
		q.Set("channelType", *channel)
		q.Set("platformID", *platform)
		return reqJSON(http.MethodGet, addr+"/v1/registry/destinations/check?"+q.Encode(), nil)
	default:
		return fmt.Errorf("registry destination: unknown verb %q (want add|check)", verb)
	}
}

func cmdRegSession(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("registry session <list|get> ...")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return reqJSON(http.MethodGet, addr+"/v1/registry/sessions", nil)
	case "get":
		fs := flag.NewFlagSet("session get", flag.ContinueOnError)
		id := fs.String("id", "", "session id")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *id == "" {
			return fmt.Errorf("registry session get requires --id")
		}
		return reqJSON(http.MethodGet, addr+"/v1/registry/sessions/"+seg(*id), nil)
	default:
		return fmt.Errorf("registry session: unknown verb %q (want list|get)", verb)
	}
}

func cmdRegAccess(addr string, args []string) error {
	fs := flag.NewFlagSet("access", flag.ContinueOnError)
	user := fs.String("user", "", "user id (channel:handle)")
	agent := fs.String("agent", "", "agent group id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *user == "" || *agent == "" {
		return fmt.Errorf("registry access requires --user and --agent")
	}
	q := url.Values{}
	q.Set("userID", *user)
	q.Set("agentGroupID", *agent)
	return reqJSON(http.MethodGet, addr+"/v1/registry/access?"+q.Encode(), nil)
}

func registryUsage() {
	fmt.Fprintln(os.Stderr, `registry subcommands (thin clients of /v1/registry/*):
  registry agent-group     put --id <id> [--name N] [--folder F] | get --id <id>
  registry messaging-group create --channel C --platform P [--instance I] [--group] [--policy strict|public]
  registry messaging-group get --id <id> | wirings --id <id>
  registry wiring          create --mg <id> --agent <id> --engage <mode> [--pattern P] [--scope all|known]
                                  [--ignored drop|accumulate] [--session shared|per-thread|agent-shared] [--priority N]
  registry user            put --id <id> [--kind K] [--name N] | get --id <id>
  registry role            grant|revoke --user <id> --role owner|admin [--agent <id>]
  registry member          add|remove --user <id> --agent <id>
  registry destination     add|check --agent <id> --channel C --platform P
  registry session         list | get --id <id>
  registry access          --user <id> --agent <id>`)
}
