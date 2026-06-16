// OWNER: AGENT1

// Package channels is the channel-adapter registry plus a fake adapter for tests.
// Concrete platform adapters are out of scope for the skeleton; only one stub
// adapter ships here.
package channels

import (
	"context"
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Adapter delivers an outbound message to a concrete platform and returns the
// platform-assigned message ID.
type Adapter interface {
	Name() string
	Deliver(ctx context.Context, msg contract.MessageOut) (string, error)
}

// Registry holds the available channel adapters.
type Registry struct{}

// NewRegistry constructs a Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds an adapter to the registry.
func (r *Registry) Register(a Adapter) error {
	return errors.New("host/channels: not implemented (AGENT1)")
}

// fakeAdapter is a stub adapter used by tests.
type fakeAdapter struct{}

func (fakeAdapter) Name() string { return "fake" }

func (fakeAdapter) Deliver(ctx context.Context, msg contract.MessageOut) (string, error) {
	return "", errors.New("host/channels: not implemented (AGENT1)")
}
