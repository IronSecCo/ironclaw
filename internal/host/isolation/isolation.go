// OWNER: AGENT1

// Package isolation launches sandboxes under gVisor (runsc) via the containerd Go
// client (runtime io.containerd.runsc.v1). The OCI spec mounts inbound read-only,
// outbound read/write, and the model-proxy unix socket; sets network=none, drops
// all caps, sets no_new_privs, runs non-root in a userns, and uses a read-only
// rootfs with a small writable /workspace. A future Kata backend sits behind the
// same Isolator interface.
package isolation

import (
	"context"
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// SandboxSpec describes a sandbox to launch.
type SandboxSpec struct {
	SessionID         contract.SessionID
	Image             string
	ReadOnlyInbound   string
	ReadWriteOutbound string
	ModelProxySocket  string
}

// Handle is a running sandbox.
type Handle interface {
	Stop(ctx context.Context) error
}

// Isolator launches sandboxes. Implementations: RunscIsolator (gVisor); Kata is a
// future backend behind this same interface.
type Isolator interface {
	Launch(ctx context.Context, spec SandboxSpec) (Handle, error)
}

// RunscIsolator launches sandboxes under gVisor via containerd.
type RunscIsolator struct{}

// NewRunsc constructs a RunscIsolator.
func NewRunsc() *RunscIsolator { return &RunscIsolator{} }

// Launch starts a sandbox for the given spec.
func (r *RunscIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	return nil, errors.New("host/isolation: not implemented (AGENT1)")
}
