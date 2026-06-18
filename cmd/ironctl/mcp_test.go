package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordedJSON struct {
	method string
	path   string
	body   map[string]any
}

func mcpMock(t *testing.T, status int, respBody string) (*httptest.Server, *recordedJSON) {
	t.Helper()
	rec := &recordedJSON{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &rec.body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestCmdMCPAddRemote(t *testing.T) {
	srv, rec := mcpMock(t, http.StatusOK, `{"name":"github"}`)
	err := cmdMCPAdd(srv.URL, []string{"github", "--url", "https://mcp.example.com/rpc", "--header", "Authorization=Bearer ${TOK}"})
	if err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if rec.method != http.MethodPut || rec.path != "/v1/registry/mcp-servers/github" {
		t.Fatalf("unexpected request: %s %s", rec.method, rec.path)
	}
	if rec.body["transport"] != "http" || rec.body["url"] != "https://mcp.example.com/rpc" {
		t.Fatalf("unexpected body: %v", rec.body)
	}
	headers, _ := rec.body["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer ${TOK}" {
		t.Fatalf("header not sent: %v", rec.body["headers"])
	}
}

func TestCmdMCPAddLocal(t *testing.T) {
	srv, rec := mcpMock(t, http.StatusOK, `{}`)
	err := cmdMCPAdd(srv.URL, []string{"files", "--command", "npx", "--arg", "-y", "--arg", "@scope/server", "--image", "img:1", "--env", "TOKEN=${T}"})
	if err != nil {
		t.Fatalf("add local: %v", err)
	}
	if rec.body["transport"] != "stdio" || rec.body["command"] != "npx" || rec.body["image"] != "img:1" {
		t.Fatalf("unexpected body: %v", rec.body)
	}
	args, _ := rec.body["args"].([]any)
	if len(args) != 2 || args[0] != "-y" || args[1] != "@scope/server" {
		t.Fatalf("args not sent: %v", rec.body["args"])
	}
	env, _ := rec.body["env"].(map[string]any)
	if env["TOKEN"] != "${T}" {
		t.Fatalf("env not sent: %v", rec.body["env"])
	}
}

func TestCmdMCPAddNeedsTransport(t *testing.T) {
	srv, _ := mcpMock(t, http.StatusOK, "{}")
	if err := cmdMCPAdd(srv.URL, []string{"x"}); err == nil {
		t.Error("add without --url or --command should error")
	}
}

func TestCmdMCPGrant(t *testing.T) {
	srv, rec := mcpMock(t, http.StatusAccepted, `{"id":"chg_1"}`)
	err := cmdMCPGrant(srv.URL, []string{"github", "--group", "team-a", "--tools", "create_issue, list_issues", "--by", "cli:admin"})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/v1/ui/config/change" {
		t.Fatalf("unexpected request: %s %s", rec.method, rec.path)
	}
	if rec.body["kind"] != "mcp_access" || rec.body["agentGroupID"] != "team-a" {
		t.Fatalf("unexpected body: %v", rec.body)
	}
	after, _ := rec.body["after"].(map[string]any)
	if after["server"] != "github" {
		t.Fatalf("after.server wrong: %v", after)
	}
	tools, _ := after["tools"].([]any)
	if len(tools) != 2 || tools[0] != "create_issue" || tools[1] != "list_issues" {
		t.Fatalf("after.tools wrong: %v", after["tools"])
	}
}

func TestCmdMCPGrantNeedsGroup(t *testing.T) {
	srv, _ := mcpMock(t, http.StatusAccepted, "{}")
	if err := cmdMCPGrant(srv.URL, []string{"github"}); err == nil {
		t.Error("grant without --group should error")
	}
}

func TestCmdMCPProbeAndRemove(t *testing.T) {
	srv, rec := mcpMock(t, http.StatusOK, `{"tools":[]}`)
	if err := cmdMCPProbe(srv.URL, []string{"github"}); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/v1/registry/mcp-servers/github/probe" {
		t.Fatalf("unexpected probe request: %s %s", rec.method, rec.path)
	}

	srv2, rec2 := mcpMock(t, http.StatusNoContent, "")
	if err := cmdMCPRemove(srv2.URL, []string{"github"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rec2.method != http.MethodDelete || rec2.path != "/v1/registry/mcp-servers/github" {
		t.Fatalf("unexpected remove request: %s %s", rec2.method, rec2.path)
	}
}

func TestMCPDispatchUnknown(t *testing.T) {
	if err := cmdMCP("http://x", []string{"frobnicate"}); err == nil {
		t.Error("unknown mcp subcommand should error")
	}
	if err := cmdMCP("http://x", nil); err == nil {
		t.Error("no subcommand should error")
	}
}

func TestKVPairs(t *testing.T) {
	got := kvPairs([]string{"A=1", "B=2", "noeq", "=v"})
	if got["A"] != "1" || got["B"] != "2" || len(got) != 2 {
		t.Fatalf("kvPairs = %v, want A=1 B=2", got)
	}
}
