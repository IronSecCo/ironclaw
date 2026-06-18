package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxFileBytes caps a single read or write so a tool result cannot blow up the
// model context or the workspace.
const maxFileBytes = 1 << 20 // 1 MiB

// Workspace exposes file operations jailed to a single root directory (e.g.
// /workspace). Every path is resolved relative to the root and rejected if it
// escapes it. This is a normal in-sandbox capability: the sandbox is the trust
// boundary (gVisor confines it), so file access here cannot touch the host.
type Workspace struct {
	root string
}

// NewWorkspace constructs a Workspace rooted at dir. The root is resolved to an
// absolute, symlink-evaluated path so containment checks are robust.
func NewWorkspace(dir string) (*Workspace, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("sandbox/tools: workspace root: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return &Workspace{root: abs}, nil
}

// safeJoin resolves rel against the workspace root and rejects absolute paths and
// any path that would escape the root.
func (w *Workspace) safeJoin(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative to the workspace: %q", rel)
	}
	clean := filepath.Clean("/" + rel) // anchor at root, collapsing any ".."
	joined := filepath.Join(w.root, clean)
	relCheck, err := filepath.Rel(w.root, joined)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the workspace: %q", rel)
	}
	return joined, nil
}

// Tools returns the three workspace tools as a slice for registration.
func (w *Workspace) Tools() []Tool {
	return []Tool{
		&readFileTool{w: w},
		&writeFileTool{w: w},
		&listDirTool{w: w},
	}
}

// --- read_file ---

type readFileInput struct {
	Path string `json:"path"`
}

type readFileTool struct{ w *Workspace }

func (t *readFileTool) Name() string        { return "read_file" }
func (t *readFileTool) Description() string { return "Read a UTF-8 text file from the workspace." }
func (t *readFileTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace root."}},"required":["path"],"additionalProperties":false}`)
}

func (t *readFileTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in readFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("read_file: invalid input: %w", err)
	}
	full, err := t.w.safeJoin(in.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file: %q is a directory", in.Path)
	}
	if info.Size() > maxFileBytes {
		return "", fmt.Errorf("read_file: %q is %d bytes, exceeds %d-byte limit", in.Path, info.Size(), maxFileBytes)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}
	return string(data), nil
}

// --- write_file ---

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeFileTool struct{ w *Workspace }

func (t *writeFileTool) Name() string { return "write_file" }
func (t *writeFileTool) Description() string {
	return "Create or overwrite a text file in the workspace."
}
func (t *writeFileTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path relative to the workspace root."},"content":{"type":"string","description":"File contents."}},"required":["path","content"],"additionalProperties":false}`)
}

func (t *writeFileTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in writeFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("write_file: invalid input: %w", err)
	}
	if len(in.Content) > maxFileBytes {
		return "", fmt.Errorf("write_file: content is %d bytes, exceeds %d-byte limit", len(in.Content), maxFileBytes)
	}
	full, err := t.w.safeJoin(in.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	if err := os.WriteFile(full, []byte(in.Content), 0o644); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), nil
}

// --- list_dir ---

type listDirInput struct {
	Path string `json:"path"`
}

type dirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

type listDirTool struct{ w *Workspace }

func (t *listDirTool) Name() string { return "list_dir" }
func (t *listDirTool) Description() string {
	return "List the entries of a directory in the workspace."
}
func (t *listDirTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path relative to the workspace root; empty for the root."}},"additionalProperties":false}`)
}

func (t *listDirTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in listDirInput
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return "", fmt.Errorf("list_dir: invalid input: %w", err)
		}
	}
	full, err := t.w.safeJoin(in.Path)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	out := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, dirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("list_dir: %w", err)
	}
	return string(b), nil
}
