// OWNER: AGENT1

// Package gateway is the single choke point through which every control-plane
// mutation flows (persona, enabled tools, packages, wiring, permissions, mounts).
// There is no file-edit path. A deterministic verifier chain runs first, then a
// human approval step, then an idempotent apply. The v1 floor is one verifier,
// AlwaysRequireHuman, so every mutation hits a human.
package gateway

import (
	"context"
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Gateway implements the mandatory-mutation protocol over the contract types.
type Gateway struct{}

// New constructs a Gateway.
func New() *Gateway { return &Gateway{} }

// Submit enqueues a ChangeRequest, runs the verifier chain, and (per the v1
// floor) holds it pending a human decision. It returns the assigned ChangeID.
func (g *Gateway) Submit(ctx context.Context, req contract.ChangeRequest) (contract.ChangeID, error) {
	return "", errors.New("host/gateway: not implemented (AGENT1)")
}
