// Package harness spins up the host for black-box parity specs and exposes a
// documented fake-sandbox hook that the sandbox-side specs use. It boots the
// control-plane; the parity specs themselves are shared and additive.
package harness

// Harness drives a control-plane instance plus a fake sandbox for parity tests.
type Harness struct{}

// New constructs a Harness.
func New() *Harness { return &Harness{} }
