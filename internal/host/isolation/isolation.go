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

// SandboxSpec describes a sandbox to launch, including the security knobs that the
// OCI spec must enforce. The defaults that matter for the trust boundary
// (NetworkNone, DropAllCaps, NoNewPrivs, ReadOnlyRootfs) should all be true in
// production; they are explicit fields rather than implicit so a misconfiguration
// is visible at the call site.
type SandboxSpec struct {
	SessionID contract.SessionID
	Image     string

	// Queue mounts. Inbound is bound read-only (the sandbox can never write it);
	// outbound is bound read/write (sandbox is the sole writer).
	ReadOnlyInboundPath   string
	ReadWriteOutboundPath string

	// ModelProxySocket is the host unix socket bound into the sandbox; it is the
	// sandbox's ONLY egress path (combined with NetworkNone).
	ModelProxySocket string

	// Security knobs — all should be the hardened value in production.
	NetworkNone    bool // network=none: no NIC inside the sandbox at all.
	DropAllCaps    bool // drop every Linux capability.
	NoNewPrivs     bool // set PR_SET_NO_NEW_PRIVS so suid binaries cannot escalate.
	NonRootUID     int  // run as this non-zero UID inside a user namespace.
	ReadOnlyRootfs bool // mount the rootfs read-only; only /workspace is writable.
}

// HardenedSpec returns spec with all security knobs set to their hardened values.
// Call sites should prefer this over hand-setting the booleans.
func HardenedSpec(sessionID contract.SessionID, image, inboundRO, outboundRW, proxySock string) SandboxSpec {
	return SandboxSpec{
		SessionID:             sessionID,
		Image:                 image,
		ReadOnlyInboundPath:   inboundRO,
		ReadWriteOutboundPath: outboundRW,
		ModelProxySocket:      proxySock,
		NetworkNone:           true,
		DropAllCaps:           true,
		NoNewPrivs:            true,
		NonRootUID:            65532, // conventional "nonroot" distroless UID
		ReadOnlyRootfs:        true,
	}
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
//
// TODO(AGENT1): production wiring requires the containerd Go client and the
// io.containerd.runsc.v1 runtime — both EXTERNAL dependencies that are
// intentionally NOT added in this stdlib-only pass. When added, Launch will:
//   - dial containerd, pull/resolve spec.Image;
//   - build the OCI spec applying every SandboxSpec security knob (network=none,
//     drop all caps, no_new_privs, non-root userns, read-only rootfs + writable
//     /workspace);
//   - add the three bind mounts (inbound ro, outbound rw, model-proxy socket);
//   - create + start the task with containerd.WithRuntime("io.containerd.runsc.v1", nil)
//     and return a Handle wrapping the task.
func (r *RunscIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	return nil, errors.New("host/isolation: Launch not implemented — needs containerd client + io.containerd.runsc.v1 (external deps, omitted in stdlib-only pass)")
}
