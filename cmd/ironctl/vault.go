package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// cmdVault drives the per-group vault credential-management surface over the
// control-plane API. The vault policy — "which agent group may use which logical
// credential against which host" (threat-model §11) — is deny-by-default config,
// never a secret. READING it (`list`) is a plain admin GET; CHANGING it
// (`grant`/`revoke`/`set`) goes through the gateway's human-approval floor (it rides
// a permissions-class change carrying a `vaultPolicy` body), so those verbs only
// PROPOSE the change and print the change id — the grant takes effect after a human
// approves it. ironctl is a thin client; the daemon owns the verifier + applier +
// store.
//
// The actual credential (the key) is never held or rotated here: it lives only in
// the host-side injector (see internal/host/egress/vault.go). Rotating the
// injector's held secret is an injector operation, not a control-plane one.
func cmdVault(addr string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("expected: vault <list|grant|revoke|set>")
	}
	switch args[0] {
	case "list", "ls", "show":
		return cmdVaultList(addr, args[1:])
	case "grant", "add":
		return cmdVaultGrant(addr, args[1:])
	case "revoke", "rm", "remove":
		return cmdVaultRevoke(addr, args[1:])
	case "set":
		return cmdVaultSet(addr, args[1:])
	default:
		return fmt.Errorf("unknown vault subcommand %q (want list|grant|revoke|set)", args[0])
	}
}

// vaultRule mirrors the API read model: a logical credential NAME and the bare
// upstream hosts it may be used against.
type vaultRule struct {
	Credential string   `json:"credential"`
	Hosts      []string `json:"hosts"`
}

// vaultPolicyDoc is the GET /v1/vault/policy/{group} response.
type vaultPolicyDoc struct {
	AgentGroupID  string      `json:"agentGroupId"`
	DenyByDefault bool        `json:"denyByDefault"`
	HasPolicy     bool        `json:"hasPolicy"`
	Rules         []vaultRule `json:"rules"`
}

// cmdVaultList prints a group's deny-by-default state and active grants.
func cmdVaultList(addr string, args []string) error {
	fs := flag.NewFlagSet("vault list", flag.ContinueOnError)
	group := fs.String("group", "", "agent group id")
	asJSON := fs.Bool("json", false, "print the raw policy JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *group == "" {
		return fmt.Errorf("vault list requires --group")
	}
	doc, raw, err := fetchVaultPolicy(addr, *group)
	if err != nil {
		return err
	}
	if *asJSON {
		fmt.Println(strings.TrimSpace(string(raw)))
		return nil
	}
	fmt.Printf("vault policy for group %q (deny-by-default)\n", doc.AgentGroupID)
	if len(doc.Rules) == 0 {
		fmt.Println("  no grants — every vault:// request is denied")
		return nil
	}
	for _, r := range doc.Rules {
		fmt.Printf("  %s -> %s\n", r.Credential, strings.Join(r.Hosts, ", "))
	}
	return nil
}

// cmdVaultGrant proposes adding (credential -> host[,host...]) to a group's policy.
// It reads the current policy, unions the new grant in, and submits the FULL updated
// policy as a gateway change (the store replaces a group's policy wholesale).
//
//	ironctl vault grant --group <id> --credential github --host api.github.com [--host ...] [--by <user>]
func cmdVaultGrant(addr string, args []string) error {
	fs := flag.NewFlagSet("vault grant", flag.ContinueOnError)
	group := fs.String("group", "", "target agent group id")
	cred := fs.String("credential", "", "logical credential name (e.g. github) — never a key")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	var hosts multiFlag
	fs.Var(&hosts, "host", "an upstream host the credential may be used against (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *group == "" || *cred == "" || len(hosts) == 0 {
		return fmt.Errorf("vault grant requires --group, --credential, and at least one --host")
	}
	doc, _, err := fetchVaultPolicy(addr, *group)
	if err != nil {
		return err
	}
	rules := applyVaultGrant(doc.Rules, *cred, hosts)
	return submitVaultPolicy(addr, *group, *by, rules)
}

// cmdVaultRevoke proposes removing a grant from a group's policy. With one or more
// --host, it removes those hosts from the credential (dropping the credential if no
// host remains); with no --host, it removes the whole credential.
//
//	ironctl vault revoke --group <id> --credential github [--host api.github.com ...] [--by <user>]
func cmdVaultRevoke(addr string, args []string) error {
	fs := flag.NewFlagSet("vault revoke", flag.ContinueOnError)
	group := fs.String("group", "", "target agent group id")
	cred := fs.String("credential", "", "logical credential name to revoke")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	var hosts multiFlag
	fs.Var(&hosts, "host", "an upstream host to revoke (repeatable; omit to revoke the whole credential)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *group == "" || *cred == "" {
		return fmt.Errorf("vault revoke requires --group and --credential")
	}
	doc, _, err := fetchVaultPolicy(addr, *group)
	if err != nil {
		return err
	}
	rules := applyVaultRevoke(doc.Rules, *cred, hosts)
	return submitVaultPolicy(addr, *group, *by, rules)
}

// cmdVaultSet proposes replacing a group's ENTIRE policy declaratively. Each --rule
// is "credential=host1,host2". With no --rule it proposes an empty policy (revoke
// all). This is the low-level form behind grant/revoke.
//
//	ironctl vault set --group <id> --rule github=api.github.com --rule stripe=api.stripe.com [--by <user>]
func cmdVaultSet(addr string, args []string) error {
	fs := flag.NewFlagSet("vault set", flag.ContinueOnError)
	group := fs.String("group", "", "target agent group id")
	by := fs.String("by", "", "requesting user id (channel:handle)")
	var ruleArgs multiFlag
	fs.Var(&ruleArgs, "rule", "credential=host1,host2 (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *group == "" {
		return fmt.Errorf("vault set requires --group")
	}
	rules, err := parseVaultRules(ruleArgs)
	if err != nil {
		return err
	}
	return submitVaultPolicy(addr, *group, *by, rules)
}

// fetchVaultPolicy GETs a group's current policy and returns it parsed + raw.
func fetchVaultPolicy(addr, group string) (vaultPolicyDoc, []byte, error) {
	resp, err := httpGet(addr + "/v1/vault/policy/" + url.PathEscape(group))
	if err != nil {
		return vaultPolicyDoc{}, nil, err
	}
	defer resp.Body.Close()
	raw := mustReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return vaultPolicyDoc{}, nil, fmt.Errorf("read vault policy: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var doc vaultPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return vaultPolicyDoc{}, nil, fmt.Errorf("decode vault policy: %w", err)
	}
	return doc, raw, nil
}

// submitVaultPolicy proposes the given full rule set for a group as a gateway change.
// It rides a permissions-class change carrying a `vaultPolicy` body, which the
// daemon's VaultPolicyVerifier validates and AlwaysRequireHuman holds for approval.
func submitVaultPolicy(addr, group, by string, rules []vaultRule) error {
	if rules == nil {
		rules = []vaultRule{}
	}
	return postJSON(addr+"/v1/ui/config/change", map[string]any{
		"kind":         "permissions",
		"agentGroupID": group,
		"requestedBy":  by,
		"after":        map[string]any{"vaultPolicy": map[string]any{"rules": rules}},
	})
}

// applyVaultGrant returns rules with (cred -> hosts) unioned in: it finds or creates
// the credential's rule and adds any hosts not already present. Credential and hosts
// are lowercased/trimmed so the union dedupes the way the server normalizes.
func applyVaultGrant(rules []vaultRule, cred string, hosts []string) []vaultRule {
	cred = strings.ToLower(strings.TrimSpace(cred))
	out := cloneVaultRules(rules)
	idx := -1
	for i := range out {
		if strings.ToLower(strings.TrimSpace(out[i].Credential)) == cred {
			idx = i
			break
		}
	}
	if idx < 0 {
		out = append(out, vaultRule{Credential: cred})
		idx = len(out) - 1
	}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || containsStr(out[idx].Hosts, h) {
			continue
		}
		out[idx].Hosts = append(out[idx].Hosts, h)
	}
	sort.Strings(out[idx].Hosts)
	return out
}

// applyVaultRevoke returns rules with the grant removed. With hosts given, it removes
// those hosts from the credential and drops the credential if none remain; with no
// hosts, it drops the whole credential.
func applyVaultRevoke(rules []vaultRule, cred string, hosts []string) []vaultRule {
	cred = strings.ToLower(strings.TrimSpace(cred))
	drop := map[string]bool{}
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			drop[h] = true
		}
	}
	out := make([]vaultRule, 0, len(rules))
	for _, r := range cloneVaultRules(rules) {
		if strings.ToLower(strings.TrimSpace(r.Credential)) != cred {
			out = append(out, r)
			continue
		}
		if len(drop) == 0 {
			continue // revoke the whole credential
		}
		kept := make([]string, 0, len(r.Hosts))
		for _, h := range r.Hosts {
			if !drop[strings.ToLower(strings.TrimSpace(h))] {
				kept = append(kept, h)
			}
		}
		if len(kept) > 0 {
			out = append(out, vaultRule{Credential: r.Credential, Hosts: kept})
		}
	}
	return out
}

// parseVaultRules parses "credential=host1,host2" entries into rules.
func parseVaultRules(entries []string) ([]vaultRule, error) {
	var out []vaultRule
	for _, e := range entries {
		cred, hostCSV, ok := strings.Cut(e, "=")
		cred = strings.ToLower(strings.TrimSpace(cred))
		if !ok || cred == "" {
			return nil, fmt.Errorf("invalid --rule %q (want credential=host1,host2)", e)
		}
		var hosts []string
		for _, h := range strings.Split(hostCSV, ",") {
			if h = strings.ToLower(strings.TrimSpace(h)); h != "" && !containsStr(hosts, h) {
				hosts = append(hosts, h)
			}
		}
		if len(hosts) == 0 {
			return nil, fmt.Errorf("--rule %q must grant at least one host", e)
		}
		sort.Strings(hosts)
		out = append(out, vaultRule{Credential: cred, Hosts: hosts})
	}
	return out, nil
}

func cloneVaultRules(rules []vaultRule) []vaultRule {
	out := make([]vaultRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, vaultRule{Credential: r.Credential, Hosts: append([]string{}, r.Hosts...)})
	}
	return out
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
