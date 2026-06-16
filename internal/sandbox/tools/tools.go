// OWNER: AGENT2

// Package tools holds the in-sandbox tool implementations.
//
// There are deliberately NO self-edit, install_packages, or add_mcp_server tools:
// capability changes are control-plane mutations and happen only via the host
// gateway. A tool that needs privilege emits a gateway ChangeRequest — it never
// acts directly. The Registry enforces this by refusing to register any tool
// whose name is on the forbidden list.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Tool is an in-sandbox tool the agent may invoke. Inputs and the declared
// schema are JSON (json.RawMessage), keeping the surface typed at the Go level
// without an open interface{} value.
type Tool interface {
	Name() string
	Description() string
	// JSONSchema returns the JSON Schema for the tool's input object.
	JSONSchema() json.RawMessage
	// Invoke runs the tool with the given JSON input and returns its textual
	// result. Implementations must validate input and never reach outside the
	// sandbox's allowed surface.
	Invoke(ctx context.Context, input json.RawMessage) (string, error)
}

// ErrForbiddenTool is returned by Register for any tool whose capability belongs
// to the control plane (it must flow through the host gateway instead).
var ErrForbiddenTool = errors.New("sandbox/tools: forbidden tool — capability changes go through the host gateway")

// ErrDuplicateTool is returned when registering a name that already exists.
var ErrDuplicateTool = errors.New("sandbox/tools: duplicate tool name")

// forbiddenNames are tool names the sandbox must never expose. These would let an
// agent change its own capabilities directly; such changes are control-plane
// mutations and only happen via the host gateway (design-plan §"Agent 2").
var forbiddenNames = map[string]struct{}{
	"install_packages":  {},
	"install_package":   {},
	"add_mcp_server":    {},
	"remove_mcp_server": {},
	"self_edit":         {},
	"edit_self":         {},
	"edit_source":       {},
	"set_persona":       {},
	"set_permissions":   {},
}

// IsForbidden reports whether name is a control-plane capability the sandbox may
// not expose as a direct tool.
func IsForbidden(name string) bool {
	_, ok := forbiddenNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// Registry holds the in-sandbox tools, keyed by name.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. It refuses forbidden names and duplicates.
func (r *Registry) Register(t Tool) error {
	name := t.Name()
	if IsForbidden(name) {
		return fmt.Errorf("%w: %q", ErrForbiddenTool, name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	r.tools[name] = t
	return nil
}

// Get returns the named tool, if registered.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns the registered tools sorted by name.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Names returns the registered tool names, sorted.
func (r *Registry) Names() []string {
	tools := r.List()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}
	return names
}
