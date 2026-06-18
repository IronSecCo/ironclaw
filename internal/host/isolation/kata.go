package isolation

import "context"

// DefaultKataRuntimeBinary is the Kata Containers OCI runtime binary. kata-runtime
// is OCI-CLI compatible (run/kill/delete over a bundle), so the host launches the
// IDENTICAL hardened bundle the gVisor backend uses — only the runtime differs. A
// containerd-based deployment can instead point this at the
// containerd-shim-kata-v2 entrypoint (or a wrapper) via WithRuntimeBinary.
const DefaultKataRuntimeBinary = "kata-runtime"

// KataIsolator launches sandboxes under Kata Containers — hardware-virtualized
// (a lightweight per-sandbox VM with its own guest kernel) rather than gVisor's
// user-space kernel — behind the SAME hardened OCI bundle as the runsc backend.
//
// An OCI bundle is runtime-agnostic: the config.json and rootfs are identical
// whether runc, runsc, or kata-runtime runs them. KataIsolator therefore composes
// the shared, binary-parameterized bundle/exec machinery and swaps in the Kata
// OCI runtime, so a Kata sandbox can never silently diverge from the gVisor
// sandbox's hardening (read-only rootfs, no network namespace, dropped caps,
// no-new-privs, user namespace). It satisfies Isolator, so the control plane can
// pick a backend without any other change.
type KataIsolator struct {
	// oci is the runtime-generic OCI bundle/exec engine (RunscIsolator is
	// binary-parameterized; here it is configured with the Kata runtime).
	oci *RunscIsolator
}

// NewKata constructs a KataIsolator defaulting to the Kata OCI runtime. The same
// Options as NewRunsc compose: WithBundleRoot, WithProvisioner, and
// WithRuntimeBinary (to override the default kata-runtime, e.g. a containerd shim
// wrapper). Options are applied after the Kata default, so a caller-supplied
// WithRuntimeBinary wins.
func NewKata(opts ...Option) *KataIsolator {
	base := append([]Option{WithRuntimeBinary(DefaultKataRuntimeBinary)}, opts...)
	return &KataIsolator{oci: NewRunsc(base...)}
}

// Launch builds the hardened OCI bundle, provisions the rootfs (when a
// RootfsProvisioner is configured), and execs the Kata runtime to run it. It
// returns ErrRootfsMissing when the bundle has no provisioned rootfs — exactly
// like the gVisor backend — so a broken/absent provisioner fails clearly rather
// than starting a sandbox with no filesystem.
func (k *KataIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	return k.oci.Launch(ctx, spec)
}

// WriteBundle writes the per-session hardened OCI bundle (config.json) without
// launching — for pre-staging and tests. It does not provision rootfs.
func (k *KataIsolator) WriteBundle(spec SandboxSpec) (bundleDir string, err error) {
	return k.oci.WriteBundle(spec)
}

// RuntimeBinary reports the OCI runtime KataIsolator will exec.
func (k *KataIsolator) RuntimeBinary() string { return k.oci.RuntimeBinary }

// BundleRoot reports the directory under which per-session bundles are written.
func (k *KataIsolator) BundleRoot() string { return k.oci.BundleRoot }

// KataIsolator satisfies the Isolator interface.
var _ Isolator = (*KataIsolator)(nil)
