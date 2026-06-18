package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// cmdSkill drives the host-side skills surface over the control-plane API. Install
// is the ONLY trigger and it is host/admin-only — never a sandbox tool: an agent can
// at most ask; only a human approves the resulting gateway ChangeRequest. ironctl is
// a thin client; the daemon (with --skills-dir/--skills-trust-key set) does the
// fetch + signature-verify + tool-validation and submits the change.
func cmdSkill(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected: skill <add|list|remove>")
	}
	switch args[0] {
	case "add":
		return cmdSkillAdd(addr, args[1:])
	case "list", "ls":
		return cmdSkillList(addr)
	case "remove", "rm":
		return cmdSkillRemove(addr, args[1:])
	default:
		return fmt.Errorf("unknown skill subcommand %q (want add|list|remove)", args[0])
	}
}

// cmdSkillAdd fetches+verifies a curated skill and submits its install
// ChangeRequest: `ironctl skill add <name>@<version> --group <id> [--by <user>]`.
func cmdSkillAdd(addr string, args []string) error {
	// The skill ref is the leading positional; flags follow it (Go's flag package
	// stops at the first non-flag arg, so we peel the positional off first).
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: skill add <name>@<version> --group <id> [--by <user>]")
	}
	ref := args[0]
	fs := flag.NewFlagSet("skill add", flag.ContinueOnError)
	group := fs.String("group", "", "target agent group id")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *group == "" {
		return fmt.Errorf("usage: skill add <name>@<version> --group <id> [--by <user>]")
	}
	name, version, err := splitNameVersion(ref)
	if err != nil {
		return err
	}
	return postJSON(addr+"/v1/skills/install", map[string]string{
		"skill":        name,
		"version":      version,
		"agentGroupId": *group,
		"requestedBy":  *by,
	})
}

// cmdSkillList prints the available skills in the curated source (the host catalog).
func cmdSkillList(addr string) error {
	resp, err := httpGet(addr + "/v1/skills")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, strings.TrimSpace(string(body)))
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	var refs []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &refs); err != nil {
		return fmt.Errorf("decode skills: %w", err)
	}
	if len(refs) == 0 {
		fmt.Println("no skills in the catalog")
		return nil
	}
	for _, r := range refs {
		fmt.Printf("%s@%s\n", r.Name, r.Version)
	}
	return nil
}

// cmdSkillRemove un-catalogs a skill: `ironctl skill remove <name>[@<version>]`.
// Without a version it removes every version of the skill.
func cmdSkillRemove(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: skill remove <name>[@<version>]")
	}
	name, version, err := splitNameVersionOptional(args[0])
	if err != nil {
		return err
	}
	u := addr + "/v1/skills/" + url.PathEscape(name)
	if version != "" {
		u += "?version=" + url.QueryEscape(version)
	}
	req, err := http.NewRequest(http.MethodDelete, u, nil)
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

// splitNameVersion parses "name@version" with both parts required.
func splitNameVersion(s string) (name, version string, err error) {
	name, version, ok := strings.Cut(s, "@")
	if !ok || name == "" || version == "" {
		return "", "", fmt.Errorf("expected <name>@<version>, got %q", s)
	}
	return name, version, nil
}

// splitNameVersionOptional parses "name" or "name@version" (version optional).
func splitNameVersionOptional(s string) (name, version string, err error) {
	name, version, ok := strings.Cut(s, "@")
	if name == "" {
		return "", "", fmt.Errorf("empty skill name in %q", s)
	}
	if !ok {
		return name, "", nil
	}
	return name, version, nil
}
