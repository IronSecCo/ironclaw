// OWNER: AGENT1

// Package delivery polls the outbound queue via contract.OutboundReader, delivers
// messages through channel adapters, and dedups in the inbound `delivered` table
// (the host never writes outbound). System actions are re-authorized host-side —
// no blind trust — and there is no unapproved script/RCE path: any such action
// routes through the gateway.
package delivery

import (
	"context"
	"errors"
)

// Delivery polls outbound queues and delivers via channel adapters.
type Delivery struct{}

// New constructs a Delivery.
func New() *Delivery { return &Delivery{} }

// Poll reads due outbound messages and delivers them.
func (d *Delivery) Poll(ctx context.Context) error {
	return errors.New("host/delivery: not implemented (AGENT1)")
}
