// OWNER: AGENT2

// Package loop is the sandbox reasoning poll loop: read pending, format the
// prompt, call the provider, parse the model's structured output into outbound
// writes, mark processing/completed, and heartbeat (touch /workspace/.heartbeat).
// It ports the reference poll-loop semantics (trigger=0 accumulate, follow-up
// polling during streaming, slash-command handling).
package loop

import (
	"context"
	"errors"
)

// Loop is the sandbox reasoning poll loop.
type Loop struct{}

// New constructs a Loop.
func New() *Loop { return &Loop{} }

// Run drives the poll loop until ctx is cancelled.
func (l *Loop) Run(ctx context.Context) error {
	return errors.New("sandbox/loop: not implemented (AGENT2)")
}
