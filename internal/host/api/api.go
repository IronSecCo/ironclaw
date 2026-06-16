// OWNER: AGENT1

// Package api is the control-plane HTTP API. It binds ONLY to the mesh
// (Tailscale) interface so the control-plane has no public port. It exposes
// endpoints for submitting gateway change requests, listing pending approvals,
// recording decisions, and session/registry queries; ironctl is a thin client.
package api

import (
	"context"
	"errors"
)

// Server is the control-plane HTTP server.
type Server struct{}

// New constructs a Server.
func New() *Server { return &Server{} }

// Run binds the API to addr (expected to be the Tailscale interface address) and
// serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context, addr string) error {
	return errors.New("host/api: not implemented (AGENT1)")
}
