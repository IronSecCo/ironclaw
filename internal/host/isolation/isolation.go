// OWNER: AGENT1

// Package isolation launches sandboxes under gVisor (runsc). It builds a hardened
// OCI runtime spec, writes a per-session OCI bundle (config.json), and execs a
// configurable OCI runtime binary (default "runsc") to run it. The spec mounts
// inbound read-only, outbound read/write, and the model-proxy unix socket; sets
// network=none, drops all caps, sets no_new_privs, runs non-root in a userns, and
// uses a read-only rootfs with a small writable /workspace. A future Kata backend
// sits behind the same Isolator interface.
//
// One integration point remains: ROOTFS PROVISIONING. Unpacking a container image
// into the bundle's rootfs/ needs an image unpacker (containerd / an OCI image
// tool) which is an external dependency outside this stdlib-only tree. Launch
// therefore REQUIRES the rootfs to already exist under the bundle dir and returns
// a clear error if it does not. Everything else — spec building, bundle writing,
// and the runtime exec/stop — is real here.
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

// Launch builds the hardened OCI spec, writes the per-session bundle, and execs
// the runtime to run it: `<runtime> run --bundle <dir> <id>`.
//
// Rootfs provisioning is the one remaining integration point: unpacking an image
// into <bundle>/rootfs requires an external image unpacker, so Launch requires
// the rootfs directory to already exist and returns a clear error otherwise. If
// the runtime binary is absent, Launch returns a wrapped error (it never panics).
func (r *RunscIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	bundleDir, err := r.WriteBundle(spec)
	if err != nil {
		return nil, err
	}

	// Rootfs must be provisioned out of band (image unpacker). This is the single
	// documented integration point; fail clearly rather than launch an empty rootfs.
	rootfsDir := filepath.Join(bundleDir, "rootfs")
	if fi, statErr := os.Stat(rootfsDir); statErr != nil || !fi.IsDir() {
		return nil, fmt.Errorf("host/isolation: rootfs not provisioned at %s for image %q — provision it with an image unpacker (the one remaining external integration point) before Launch: %w",
			rootfsDir, spec.Image, errRootfsMissing)
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

// errRootfsMissing marks the rootfs-not-provisioned integration-point error so
// callers/tests can detect it with errors.Is.
var errRootfsMissing = errors.New("rootfs not provisioned")

// ErrRootfsMissing is the sentinel returned (wrapped) when the bundle has no
// provisioned rootfs.
func ErrRootfsMissing() error { return errRootfsMissing }

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
