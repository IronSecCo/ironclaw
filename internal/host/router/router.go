// OWNER: AGENT1

// Package router performs inbound routing: messaging-group resolution, fan-out to
// wired agent groups, engage-mode evaluation, session resolution, and
// sender/access gating. It writes inbound via contract.InboundWriter.
//
// Identity is ALWAYS namespaced as userID = channelType + ":" + handle; the
// handle's own content is never trusted to carry a colon (this closes the
// identity-spoofing bug).
package router

import (
	"context"
	"errors"
)

// Router routes inbound platform messages into per-session inbound queues.
type Router struct{}

// New constructs a Router.
func New() *Router { return &Router{} }

// RouteInbound processes pending inbound platform messages and writes them into
// the resolved sessions' inbound queues.
func (r *Router) RouteInbound(ctx context.Context) error {
	return errors.New("host/router: not implemented (AGENT1)")
}
