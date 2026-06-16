// OWNER: AGENT1

// Package modelproxy is the host-side model egress proxy: it listens on a unix
// socket bound into the sandbox and forwards to the model API with a destination
// allowlist. It is the single outbound path — the sandbox has network=none.
// Future work: cap, log, and redact.
package modelproxy

import (
	"context"
	"errors"
)

// Proxy is the host-side model egress proxy.
type Proxy struct{}

// New constructs a Proxy.
func New() *Proxy { return &Proxy{} }

// Serve listens on socketPath and forwards allowlisted model requests until ctx
// is cancelled.
func (p *Proxy) Serve(ctx context.Context, socketPath string) error {
	return errors.New("host/modelproxy: not implemented (AGENT1)")
}
