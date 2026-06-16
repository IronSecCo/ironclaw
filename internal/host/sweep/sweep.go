// OWNER: AGENT1

// Package sweep runs the periodic maintenance loop: stale-sandbox detection via
// heartbeat file mtime, due-message wake, recurrence expansion, and orphan reset
// with backoff.
package sweep

import (
	"context"
	"errors"
)

// Sweeper runs the periodic maintenance loop.
type Sweeper struct{}

// New constructs a Sweeper.
func New() *Sweeper { return &Sweeper{} }

// Run executes the sweep loop until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) error {
	return errors.New("host/sweep: not implemented (AGENT1)")
}
