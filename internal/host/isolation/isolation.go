// OWNER: AGENT1

// Package isolation launches sandboxes under gVisor (runsc). It builds a hardened
// OCI runtime spec, writes a per-session OCI bundle (config.json), and execs a
// configurable OCI runtime binary (default "runsc") to run it. The spec mounts
// inbound read-only, outbound read/write, and the model-proxy unix socket; sets
// network=none, drops all caps, sets no_new_privs, runs non-root in a userns, and
// uses a read-only rootfs with a small writable /workspace. A future Kata backend
// sits behind the same Isolator interface.
//
// ROOTFS PROVISIONING is a pluggable seam (RootfsProvisioner, see provisioner.go).
// Unpacking a container image into the bundle's rootfs/ needs an image unpacker
// (containerd / an OCI image tool) which is an external host dependency outside
// this stdlib-only tree, so it shells out behind the RootfsProvisioner interface
// rather than vendoring a client. When a provisioner is configured Launch
// populates rootfs out of band; when none is, Launch REQUIRES the rootfs to
// already exist and returns a clear error otherwise. Either way the rootfs check
// stays a real post-condition, so a missing or broken provisioner fails loudly
// instead of launching an empty rootfs.
package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
	// sandbox's primary egress path (combined with NetworkNone).
	ModelProxySocket string

	// EgressSocket is an OPTIONAL second host unix socket — the egress broker
	// (T-111) — bound into the sandbox at a fixed path so an agent can reach
	// explicitly-approved EXTERNAL APIs. Empty (the default, incl. HardenedSpec)
	// binds no egress socket, leaving the sandbox able to reach only the model
	// proxy. Set it to opt a session into brokered egress; the sandbox stays
	// network=none either way (egress is a host-mediated socket, not a NIC).
	EgressSocket string

	// Security knobs — all should be the hardened value in production.
	NetworkNone    bool // network=none: no NIC inside the sandbox at all.
	DropAllCaps    bool // drop every Linux capability.
	NoNewPrivs     bool // set PR_SET_NO_NEW_PRIVS so suid binaries cannot escalate.
	NonRootUID     int  // run as this non-zero UID inside a user namespace.
	ReadOnlyRootfs bool // mount the rootfs read-only; only the writable mounts below.

	// Resource limits (cgroup). A zero value applies BuildOCISpec's safe default
	// so a sandbox is ALWAYS bounded; set explicitly to override.
	//   MemoryLimitBytes   — cgroup memory cap in bytes            (default 512 MiB)
	//   CPUQuota/CPUPeriod — CPU bandwidth: Quota µs per Period µs (default 1 vCPU)
	//   PidsLimit          — max processes/threads in the cgroup   (default 256)
	MemoryLimitBytes int64
	CPUQuota         int64
	CPUPeriod        uint64
	PidsLimit        int64

	// Durable storage (host-side paths). When set they replace the ephemeral
	// tmpfs-only workspace with per-group persistent storage:
	//   WorkspacePath      — per-group durable scratch, bound rw at /workspace
	//                        (empty keeps the legacy ephemeral tmpfs workspace).
	//   MemoryPath         — per-group durable memory, bound rw at /memory
	//                        (empty omits the mount).
	//   SharedReadOnlyPath — global shared assets, bound READ-ONLY at /shared
	//                        (empty omits the mount).
	// All are outside the read-only rootfs; the rw ones carry nosuid,nodev,noexec.
	WorkspacePath      string
	MemoryPath         string
	SharedReadOnlyPath string
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

// HardenedSpecWithStorage is HardenedSpec plus per-group durable storage: a
// persistent /workspace and /memory (both rw) and a global read-only /shared
// mount, replacing the ephemeral tmpfs-only workspace. Any empty storage path
// falls back to the ephemeral/omitted behavior for that mount, so callers can opt
// in incrementally.
func HardenedSpecWithStorage(sessionID contract.SessionID, image, inboundRO, outboundRW, proxySock, workspacePath, memoryPath, sharedROPath string) SandboxSpec {
	s := HardenedSpec(sessionID, image, inboundRO, outboundRW, proxySock)
	s.WorkspacePath = workspacePath
	s.MemoryPath = memoryPath
	s.SharedReadOnlyPath = sharedROPath
	return s
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

// Defaults for the runsc isolator.
const (
	// DefaultRuntimeBinary is the OCI runtime invoked to run a bundle. runsc is
	// gVisor's OCI runtime.
	DefaultRuntimeBinary = "runsc"
)

// RunscIsolator launches sandboxes under gVisor by writing an OCI bundle and
// invoking a runtime binary (runsc) over os/exec. It holds no external client.
type RunscIsolator struct {
	// RuntimeBinary is the OCI runtime executable (default "runsc"). Overridable for
	// tests or alternate gVisor-compatible runtimes.
	RuntimeBinary string
	// BundleRoot is the directory under which per-session bundles are written. Each
	// session gets BundleRoot/<sessionID>/ containing config.json and rootfs/.
	BundleRoot string
	// Provisioner populates each bundle's rootfs/ before Launch's rootfs gate. It is
	// optional: a nil Provisioner preserves the pre-staged-rootfs behavior (the
	// caller must ensure rootfs/ exists, else Launch returns ErrRootfsMissing). Set
	// it with WithProvisioner — typically a *ContainerdProvisioner.
	Provisioner RootfsProvisioner
}

// Option configures a RunscIsolator.
type Option func(*RunscIsolator)

// WithRuntimeBinary overrides the runtime executable (default "runsc").
func WithRuntimeBinary(bin string) Option {
	return func(r *RunscIsolator) {
		if bin != "" {
			r.RuntimeBinary = bin
		}
	}
}

// WithBundleRoot overrides the bundle root directory.
func WithBundleRoot(dir string) Option {
	return func(r *RunscIsolator) {
		if dir != "" {
			r.BundleRoot = dir
		}
	}
}

// NewRunsc constructs a RunscIsolator with sane defaults, applying any options.
func NewRunsc(opts ...Option) *RunscIsolator {
	r := &RunscIsolator{
		RuntimeBinary: DefaultRuntimeBinary,
		BundleRoot:    filepath.Join(os.TempDir(), "ironclaw", "bundles"),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// runscHandle is a running (or attempted) sandbox launched by RunscIsolator. Stop
// kills then deletes the container via the runtime binary.
type runscHandle struct {
	runtimeBinary string
	bundleDir     string
	containerID   string
}

// WriteBundle builds the OCI spec for spec and writes the bundle to
// BundleRoot/<sessionID>/, returning the bundle directory. It creates the bundle
// directory and writes config.json. It does NOT provision rootfs (the documented
// integration point); callers that intend to actually launch must ensure
// rootfs/ exists.
func (r *RunscIsolator) WriteBundle(spec SandboxSpec) (bundleDir string, err error) {
	if spec.SessionID == "" {
		return "", fmt.Errorf("host/isolation: WriteBundle requires a session ID")
	}
	ociSpec, err := BuildOCISpec(spec)
	if err != nil {
		return "", err
	}
	bundleDir = filepath.Join(r.BundleRoot, string(spec.SessionID))
	if err := os.MkdirAll(bundleDir, 0o700); err != nil {
		return "", fmt.Errorf("host/isolation: create bundle dir %s: %w", bundleDir, err)
	}
	data, err := json.MarshalIndent(ociSpec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("host/isolation: marshal OCI spec: %w", err)
	}
	cfgPath := filepath.Join(bundleDir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		return "", fmt.Errorf("host/isolation: write %s: %w", cfgPath, err)
	}
	return bundleDir, nil
}

// Launch builds the hardened OCI spec, writes the per-session bundle, provisions
// the rootfs (when a RootfsProvisioner is configured), and execs the runtime to
// run it: `<runtime> run --bundle <dir> <id>`.
//
// When a Provisioner is set it populates <bundle>/rootfs out of band (a host-side
// image pull/unpack). With no Provisioner, the rootfs must already exist. In both
// cases the rootfs directory check remains a post-condition, so a missing or
// broken provisioner yields a clear ErrRootfsMissing rather than an empty-rootfs
// launch. If the runtime binary is absent, Launch returns a wrapped error (it
// never panics).
func (r *RunscIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	bundleDir, err := r.WriteBundle(spec)
	if err != nil {
		return nil, err
	}
	rootfsDir := filepath.Join(bundleDir, "rootfs")

	// Ensure the per-group durable rw dirs exist before the runtime binds them. The
	// read-only /shared mount is operator-managed and is NOT created here.
	if err := ensureWritableStorage(spec); err != nil {
		return nil, err
	}

	// Provision the rootfs out of band when a provisioner is configured. Image pull
	// and unpack are host-side actions (the sandbox is network=none); see
	// provisioner.go and the T-012 spike (.agents/spikes/rootfs.md).
	if r.Provisioner != nil {
		if err := r.Provisioner.Provision(ctx, spec.Image, rootfsDir); err != nil {
			return nil, fmt.Errorf("host/isolation: provision rootfs for image %q: %w", spec.Image, err)
		}
	}

	// Post-condition (unchanged gate): fail clearly rather than launch an empty
	// rootfs. This stays a real check even with a provisioner, so a broken one is
	// caught here instead of starting a sandbox with no filesystem.
	if fi, statErr := os.Stat(rootfsDir); statErr != nil || !fi.IsDir() {
		return nil, fmt.Errorf("host/isolation: rootfs not provisioned at %s for image %q — configure a RootfsProvisioner or pre-stage the rootfs before Launch: %w",
			rootfsDir, spec.Image, ErrRootfsMissing)
	}

	containerID := "ironclaw-" + string(spec.SessionID)
	bin := r.RuntimeBinary
	if bin == "" {
		bin = DefaultRuntimeBinary
	}

	cmd := exec.CommandContext(ctx, bin, "run", "--bundle", bundleDir, containerID)
	cmd.Dir = bundleDir
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("host/isolation: start runtime %q (is it installed?): %w", bin, err)
	}

	return &runscHandle{
		runtimeBinary: bin,
		bundleDir:     bundleDir,
		containerID:   containerID,
	}, nil
}

// ensureWritableStorage idempotently creates the per-group durable rw storage dirs
// (workspace, memory) when configured, so the runtime can bind them. The dirs are
// created 0700; the deploy layer is responsible for chowning them to the sandbox's
// mapped non-root uid (see deploy/README.md). The read-only /shared mount is
// operator-managed and intentionally not created here.
func ensureWritableStorage(spec SandboxSpec) error {
	for _, p := range []string{spec.WorkspacePath, spec.MemoryPath} {
		if p == "" {
			continue
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			return fmt.Errorf("host/isolation: create durable storage dir %s: %w", p, err)
		}
	}
	return nil
}

// ErrRootfsMissing is the sentinel Launch wraps when the bundle has no provisioned
// rootfs (the one remaining external integration point). Callers/tests detect it
// with errors.Is(err, isolation.ErrRootfsMissing).
var ErrRootfsMissing = errors.New("rootfs not provisioned")

// Stop kills then deletes the container via the runtime binary. It is safe to call
// when the runtime binary is absent — any exec error is wrapped and returned
// rather than panicking.
func (h *runscHandle) Stop(ctx context.Context) error {
	bin := h.runtimeBinary
	if bin == "" {
		bin = DefaultRuntimeBinary
	}
	// Best-effort kill, then delete. We collect the first error but always attempt
	// both so a stuck container is still removed.
	var firstErr error
	if out, err := exec.CommandContext(ctx, bin, "kill", h.containerID, "SIGKILL").CombinedOutput(); err != nil {
		firstErr = fmt.Errorf("host/isolation: %q kill %s: %w (%s)", bin, h.containerID, err, string(out))
	}
	if out, err := exec.CommandContext(ctx, bin, "delete", "--force", h.containerID).CombinedOutput(); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("host/isolation: %q delete %s: %w (%s)", bin, h.containerID, err, string(out))
		}
	}
	return firstErr
}
