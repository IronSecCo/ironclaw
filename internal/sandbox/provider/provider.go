// OWNER: AGENT2

// Package provider abstracts the model backend. The first implementation,
// AnthropicProvider, speaks the Messages API (tool use + streaming). Its HTTP
// client dials the host model-proxy unix socket, NOT the public internet — the
// sandbox has network=none.
package provider

import (
	"context"
	"errors"
)

// Provider is the model backend abstraction.
type Provider interface {
	Query(ctx context.Context, prompt string) (string, error)
}

// AnthropicProvider talks to the Messages API via the host model-proxy socket.
type AnthropicProvider struct{}

// NewAnthropic constructs an AnthropicProvider.
func NewAnthropic() *AnthropicProvider { return &AnthropicProvider{} }

// Query sends a prompt and returns the model's response.
func (p *AnthropicProvider) Query(ctx context.Context, prompt string) (string, error) {
	return "", errors.New("sandbox/provider: not implemented (AGENT2)")
}
