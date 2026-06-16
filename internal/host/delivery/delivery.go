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

	"github.com/nivardsec/ironclaw/internal/host/channels"
	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

// Delivery polls outbound queues and delivers via channel adapters. It holds the
// adapter registry (to deliver) and the gateway (to re-authorize any
// privilege-bearing system action the sandbox emits).
type Delivery struct {
	registry *channels.Registry
	gw       *gateway.Gateway
}

// New constructs a Delivery.
func New(registry *channels.Registry, gw *gateway.Gateway) *Delivery {
	return &Delivery{registry: registry, gw: gw}
}

// Poll reads due outbound messages and delivers them.
//
// Design (gated on host/queue, which is gated on RFC-0001):
//   - For each active session, open the outbound DB read-only (reopen-per-poll)
//     via host/queue.OpenOutbound and read DueMessages + ProcessingAcks.
//   - DEDUP: skip any message already present in the inbound `delivered` table;
//     after a successful adapter.Deliver, record it there via the InboundWriter's
//     MarkDelivered (the host is the inbound writer; it NEVER writes outbound).
//   - RE-AUTHORIZE: a "system" message is not trusted blindly. Any action that
//     would change agent config or run privileged work is turned into a
//     contract.ChangeRequest and pushed through d.gw.Submit — there is NO
//     unapproved script/RCE path. Plain chat replies deliver directly.
//   - Resolve the adapter by channel via d.registry and Deliver, capturing the
//     platform message id for the dedup record.
func (d *Delivery) Poll(ctx context.Context) error {
	return errors.New("host/delivery: Poll not implemented — gated on host/queue (RFC-0001)")
}
