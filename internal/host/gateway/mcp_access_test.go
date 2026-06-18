package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestMCPAccessApplier_RecordsGrant(t *testing.T) {
	var gotID contract.AgentGroupID
	var gotServer string
	var gotTools []string
	set := func(id contract.AgentGroupID, server string, tools []string) error {
		gotID, gotServer, gotTools = id, server, tools
		return nil
	}
	a := NewMCPAccessApplier(set, nil)

	req := contract.ChangeRequest{
		Kind:         contract.ChangeMCPAccess,
		AgentGroupID: "team-a",
		After:        json.RawMessage(`{"server":"github","tools":["create_issue","list_issues"]}`),
	}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if gotID != "team-a" || gotServer != "github" || len(gotTools) != 2 {
		t.Fatalf("grant recorded as id=%q server=%q tools=%v", gotID, gotServer, gotTools)
	}

	// A non-MCP change passes through without touching the setter.
	gotServer = ""
	other := contract.ChangeRequest{Kind: contract.ChangePersona, After: json.RawMessage(`{"instructions":"x"}`)}
	if err := a.Apply(context.Background(), other, contract.Decision{}); err != nil {
		t.Fatalf("Apply persona: %v", err)
	}
	if gotServer != "" {
		t.Fatal("persona change should not invoke the MCP grant setter")
	}
}

func TestMCPAccessApplier_NilSetterErrors(t *testing.T) {
	a := NewMCPAccessApplier(nil, nil)
	req := contract.ChangeRequest{Kind: contract.ChangeMCPAccess, After: json.RawMessage(`{"server":"github"}`)}
	if err := a.Apply(context.Background(), req, contract.Decision{}); err == nil {
		t.Fatal("expected an error when no grant setter is wired")
	}
}

func TestMCPServerVerifier(t *testing.T) {
	known := func(server string) bool { return server == "github" }
	v := NewMCPServerVerifier(known)

	cases := []struct {
		name string
		req  contract.ChangeRequest
		want contract.Verdict
	}{
		{"known server passes", contract.ChangeRequest{Kind: contract.ChangeMCPAccess, After: json.RawMessage(`{"server":"github"}`)}, contract.VerdictPass},
		{"unknown server rejected", contract.ChangeRequest{Kind: contract.ChangeMCPAccess, After: json.RawMessage(`{"server":"evil"}`)}, contract.VerdictReject},
		{"empty server rejected", contract.ChangeRequest{Kind: contract.ChangeMCPAccess, After: json.RawMessage(`{"tools":["x"]}`)}, contract.VerdictReject},
		{"unparseable rejected", contract.ChangeRequest{Kind: contract.ChangeMCPAccess, After: json.RawMessage(`not json`)}, contract.VerdictReject},
		{"other kind passes", contract.ChangeRequest{Kind: contract.ChangePersona, After: json.RawMessage(`{}`)}, contract.VerdictPass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := v.Verify(context.Background(), c.req)
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if got != c.want {
				t.Fatalf("verdict = %v, want %v", got, c.want)
			}
		})
	}
}
