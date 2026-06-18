package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/registry"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Research Bot":     "research-bot",
		"  Support  Bot  ": "support-bot",
		"Já_Vu!! 2":        "j-vu-2",
		"---":              "",
		"ALLCAPS":          "allcaps",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeToolsUnionsAndValidates(t *testing.T) {
	base := []string{"send_message"}
	got, err := mergeTools(base, []string{"web_search", "send_message"})
	if err != nil {
		t.Fatalf("mergeTools: %v", err)
	}
	// send_message deduped; web_search added.
	if len(got) != 2 || got[0] != "send_message" || got[1] != "web_search" {
		t.Fatalf("mergeTools = %v, want [send_message web_search]", got)
	}
	if _, err := mergeTools(nil, []string{"not_a_tool"}); err == nil {
		t.Fatalf("expected error for unknown tool")
	}
}

func TestParseToolSelection(t *testing.T) {
	// Selection indexes into the catalog's display order; assert it maps to real names.
	got, err := parseToolSelection("1,1,2")
	if err != nil {
		t.Fatalf("parseToolSelection: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduped tools, got %v", got)
	}
	if _, err := parseToolSelection("999"); err == nil {
		t.Fatalf("expected out-of-range error")
	}
	if _, err := parseToolSelection("abc"); err == nil {
		t.Fatalf("expected non-numeric error")
	}
}

// TestCmdAgentCreateE2E proves the friendly create PUTs a body that decodes into the
// REAL registry.AgentGroup — the wire contract the server actually accepts — with the
// template's persona/tools applied and a --tool unioned on top.
func TestCmdAgentCreateE2E(t *testing.T) {
	token = ""
	var gotPath, gotMethod string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gotBody)
	}))
	t.Cleanup(srv.Close)

	err := cmdAgentCreate(srv.URL, []string{
		"--name", "Research Bot",
		"--template", "researcher",
		"--tool", "schedule_task",
		"--yes",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/v1/registry/agent-groups/research-bot" {
		t.Fatalf("unexpected request: %s %s", gotMethod, gotPath)
	}
	var g registry.AgentGroup
	if err := json.Unmarshal(gotBody, &g); err != nil {
		t.Fatalf("body does not decode into registry.AgentGroup: %v\n%s", err, gotBody)
	}
	if g.Name != "Research Bot" || g.Folder != "research-bot" {
		t.Fatalf("identity = %+v", g)
	}
	// The researcher template ships structured persona docs (identity/soul/instructions).
	if g.PersonaDocs["soul"] == "" || g.PersonaDocs["identity"] == "" {
		t.Fatalf("expected researcher persona docs to be applied, got %#v", g.PersonaDocs)
	}
	// And the composed system-persona string renders them in order.
	composed := registry.ComposePersona(g)
	if !strings.Contains(composed, "### Identity") || !strings.Contains(composed, "### Soul") {
		t.Fatalf("composed persona missing section headings:\n%s", composed)
	}
	// researcher tools + the unioned schedule_task; restriction is non-empty.
	has := map[string]bool{}
	for _, tool := range g.EnabledTools {
		has[tool] = true
	}
	if !has["web_search"] || !has["schedule_task"] {
		t.Fatalf("enabled tools = %v, want web_search and schedule_task present", g.EnabledTools)
	}
}

// TestCmdAgentCreateAllTools verifies --all-tools clears the restriction (empty
// EnabledTools = every compiled tool).
func TestCmdAgentCreateAllTools(t *testing.T) {
	token = ""
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	if err := cmdAgentCreate(srv.URL, []string{"--name", "Everything", "--template", "researcher", "--all-tools", "--yes"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	var g registry.AgentGroup
	if err := json.Unmarshal(gotBody, &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(g.EnabledTools) != 0 {
		t.Fatalf("--all-tools should clear EnabledTools, got %v", g.EnabledTools)
	}
}

// TestCmdAgentCreateRejectsUnknownTemplate ensures a bad template id fails fast.
func TestCmdAgentCreateRejectsUnknownTemplate(t *testing.T) {
	token = ""
	if err := cmdAgentCreate("http://127.0.0.1:1", []string{"--name", "X", "--template", "nope", "--yes"}); err == nil {
		t.Fatalf("expected error for unknown template")
	}
}

// TestCmdAgentCreatePersonaDirAndFlags proves --persona-dir loads SOUL.md/IDENTITY.md
// and inline --soul overrides it, producing structured PersonaDocs on the wire.
func TestCmdAgentCreatePersonaDirAndFlags(t *testing.T) {
	token = ""
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "IDENTITY.md"), []byte("You are Atlas."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("From the file."), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	err := cmdAgentCreate(srv.URL, []string{
		"--name", "Atlas",
		"--persona-dir", dir,
		"--soul", "Overridden inline.", // beats the file
		"--yes",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var g registry.AgentGroup
	if err := json.Unmarshal(gotBody, &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.PersonaDocs["identity"] != "You are Atlas." {
		t.Errorf("identity from file = %q", g.PersonaDocs["identity"])
	}
	if g.PersonaDocs["soul"] != "Overridden inline." {
		t.Errorf("inline --soul should override the file, got %q", g.PersonaDocs["soul"])
	}
}

// TestCmdAgentCreateRejectsOverlongPersona ensures persona-doc validation runs before
// the PUT (a too-long section fails fast, no request made).
func TestCmdAgentCreateRejectsOverlongPersona(t *testing.T) {
	token = ""
	long := strings.Repeat("x", registry.MaxPersonaDocLen+1)
	err := cmdAgentCreate("http://127.0.0.1:1", []string{"--name", "X", "--soul", long, "--yes"})
	if err == nil {
		t.Fatalf("expected validation error for over-length persona section")
	}
}
