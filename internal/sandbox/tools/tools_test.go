package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubTool is a minimal Tool for registry tests.
type stubTool struct{ name string }

func (s stubTool) Name() string                { return s.name }
func (s stubTool) Description() string         { return "stub" }
func (s stubTool) JSONSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Invoke(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

func TestRegistryRejectsForbidden(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"install_packages", "add_mcp_server", "self_edit", "set_permissions"} {
		if err := r.Register(stubTool{name: name}); !errors.Is(err, ErrForbiddenTool) {
			t.Fatalf("Register(%q) err = %v, want ErrForbiddenTool", name, err)
		}
	}
}

func TestRegistryRegisterGetListDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stubTool{name: "beta"}); err != nil {
		t.Fatalf("Register beta: %v", err)
	}
	if err := r.Register(stubTool{name: "alpha"}); err != nil {
		t.Fatalf("Register alpha: %v", err)
	}
	if err := r.Register(stubTool{name: "alpha"}); !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("duplicate Register err = %v, want ErrDuplicateTool", err)
	}
	if _, ok := r.Get("alpha"); !ok {
		t.Fatal("Get(alpha) not found")
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("Get(missing) unexpectedly found")
	}
	names := r.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("Names() = %v, want sorted [alpha beta]", names)
	}
}

func TestWorkspaceReadWriteList(t *testing.T) {
	dir := t.TempDir()
	ws, err := NewWorkspace(dir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	r := NewRegistry()
	for _, tool := range ws.Tools() {
		if err := r.Register(tool); err != nil {
			t.Fatalf("register %s: %v", tool.Name(), err)
		}
	}
	ctx := context.Background()

	write, _ := r.Get("write_file")
	out, err := write.Invoke(ctx, json.RawMessage(`{"path":"sub/note.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(out, "wrote 5 bytes") {
		t.Fatalf("write_file out = %q", out)
	}

	read, _ := r.Get("read_file")
	got, err := read.Invoke(ctx, json.RawMessage(`{"path":"sub/note.txt"}`))
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if got != "hello" {
		t.Fatalf("read_file = %q, want %q", got, "hello")
	}

	list, _ := r.Get("list_dir")
	listOut, err := list.Invoke(ctx, json.RawMessage(`{"path":"sub"}`))
	if err != nil {
		t.Fatalf("list_dir: %v", err)
	}
	var entries []dirEntry
	if err := json.Unmarshal([]byte(listOut), &entries); err != nil {
		t.Fatalf("list_dir output not JSON: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "note.txt" || entries[0].IsDir {
		t.Fatalf("list_dir entries = %+v", entries)
	}
}

func TestWorkspaceRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	// Place a secret outside the workspace to ensure traversal cannot reach it.
	outside := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(outside, []byte("top secret"), 0o600); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	wsDir := filepath.Join(dir, "ws")
	if err := os.Mkdir(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	ws, err := NewWorkspace(wsDir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	read := &readFileTool{w: ws}
	ctx := context.Background()

	for _, bad := range []string{`{"path":"../secret.txt"}`, `{"path":"../../etc/passwd"}`} {
		if _, err := read.Invoke(ctx, json.RawMessage(bad)); err == nil {
			t.Fatalf("read_file(%s) should have been rejected", bad)
		}
	}
	// Absolute paths are rejected too.
	if _, err := read.Invoke(ctx, json.RawMessage(`{"path":"`+outside+`"}`)); err == nil {
		t.Fatal("read_file with absolute path should be rejected")
	}
}

func TestCapabilityChangeTool(t *testing.T) {
	tool := NewRequestCapabilityChangeTool()
	if IsForbidden(tool.Name()) {
		t.Fatal("request_capability_change must not be on the forbidden list")
	}
	ctx := context.Background()

	out, err := tool.Invoke(ctx, json.RawMessage(`{"kind":"packages","payload":{"add":["jq"]},"reason":"need jq"}`))
	if err != nil {
		t.Fatalf("Invoke valid: %v", err)
	}
	var env CapabilityChange
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope not JSON: %v", err)
	}
	if env.Kind != "packages" || env.Reason != "need jq" {
		t.Fatalf("envelope = %+v", env)
	}

	if _, err := tool.Invoke(ctx, json.RawMessage(`{"kind":"bogus","payload":{}}`)); err == nil {
		t.Fatal("unknown kind should error")
	}
	if _, err := tool.Invoke(ctx, json.RawMessage(`{"kind":"packages"}`)); err == nil {
		t.Fatal("missing payload should error")
	}
}

// TestSystemActionJSON asserts the capability change renders into the host's
// system-action wire format (keyed on "action"), with the action name equal to
// the ChangeKind string the host maps back to a gateway ChangeKind.
func TestSystemActionJSON(t *testing.T) {
	cc := CapabilityChange{Kind: "packages", Payload: json.RawMessage(`{"add":["jq"]}`), Reason: "need jq"}
	s, err := cc.SystemActionJSON()
	if err != nil {
		t.Fatalf("SystemActionJSON: %v", err)
	}
	var obj struct {
		Action  string          `json:"action"`
		Payload json.RawMessage `json:"payload"`
		Reason  string          `json:"reason"`
	}
	if err := json.Unmarshal([]byte(s), &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if obj.Action != "packages" {
		t.Fatalf("action = %q, want packages", obj.Action)
	}
	if obj.Reason != "need jq" || len(obj.Payload) == 0 {
		t.Fatalf("payload/reason not preserved: %+v", obj)
	}
}
