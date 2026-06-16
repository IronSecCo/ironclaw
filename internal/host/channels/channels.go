// OWNER: AGENT1

// Package channels is the channel-adapter registry plus adapters. It ships a
// FakeAdapter (for tests) and a reference WebhookAdapter (HTTP POST delivery).
// Platform-specific adapters (Slack, Discord, ...) follow the WebhookAdapter
// shape and register the same way.
package channels

import (
	"context"
	"fmt"
	"sync"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Adapter delivers an outbound message to a concrete platform and returns the
// platform-assigned message ID.
type Adapter interface {
	Name() string
	Deliver(ctx context.Context, msg contract.MessageOut) (string, error)
}

// Registry holds the available channel adapters keyed by Name. It is
// mutex-guarded and safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds an adapter. It errors on a nil adapter, an empty name, or a
// duplicate name.
func (r *Registry) Register(a Adapter) error {
	if a == nil {
		return fmt.Errorf("host/channels: nil adapter")
	}
	name := a.Name()
	if name == "" {
		return fmt.Errorf("host/channels: adapter has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[name]; ok {
		return fmt.Errorf("host/channels: adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}

// Get returns the adapter registered under name. The bool is false if none.
func (r *Registry) Get(name string) (Adapter, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	return a, ok
}

// List returns the names of all registered adapters.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		out = append(out, name)
	}
	return out
}

// FakeAdapter records the messages it is asked to deliver. It is exported for use
// in tests across the host tree.
type FakeAdapter struct {
	AdapterName string
	mu          sync.Mutex
	delivered   []contract.MessageOut
	counter     int
}

// NewFakeAdapter constructs a FakeAdapter with the given name (defaults to
// "fake").
func NewFakeAdapter(name string) *FakeAdapter {
	if name == "" {
		name = "fake"
	}
	return &FakeAdapter{AdapterName: name}
}

// Name returns the adapter name.
func (f *FakeAdapter) Name() string { return f.AdapterName }

// Deliver records msg and returns a synthetic platform message ID.
func (f *FakeAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	f.delivered = append(f.delivered, msg)
	return fmt.Sprintf("%s-msg-%d", f.AdapterName, f.counter), nil
}

// Delivered returns a copy of the messages delivered so far.
func (f *FakeAdapter) Delivered() []contract.MessageOut {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]contract.MessageOut, len(f.delivered))
	copy(out, f.delivered)
	return out
}
