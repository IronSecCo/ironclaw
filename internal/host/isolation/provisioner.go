package isolation

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// RootfsProvisioner populates a bundle's rootfs/ directory with the sandbox
// image's filesystem before Launch's rootfs gate. It is the seam that closes the
// one remaining external integration point (rootfs provisioning).
//
// Implementations MUST be idempotent (a no-op when the rootfs is already present)
// and MUST NOT require network access from inside any sandbox — image pull is a
// host-side action (the sandbox is network=none with no package install). A nil provisioner preserves the original behavior: the caller
// pre-stages rootfs out of band, else Launch returns ErrRootfsMissing.
type RootfsProvisioner interface {
	// Provision ensures rootfsDir contains the filesystem of image. ctx bounds any
	// host-side pull/unpack/copy work.
	Provision(ctx context.Context, image, rootfsDir string) error
}

// WithProvisioner attaches a RootfsProvisioner so Launch can populate rootfs out
// of band. A nil provisioner is ignored, preserving the pre-staged-rootfs path.
func WithProvisioner(p RootfsProvisioner) Option {
	return func(r *RunscIsolator) {
		if p != nil {
			r.Provisioner = p
		}
	}
}

// Defaults for the containerd-backed provisioner.
const (
	defaultCtrBinary    = "ctr"
	defaultCtrNamespace = "ironclaw"
	// readyMarker names the file written into a shared rootfs once it is fully
	// unpacked, so concurrent sessions unpack the same image exactly once.
	readyMarker = ".ironclaw-rootfs-ready"
)

// Materializer copies/binds the shared, already-unpacked rootfs at srcDir into a
// per-session bundle rootfs at dstDir. The default (copyTree) is privilege-free
// and portable; bind/reflink alternatives exist for hosts that
// permit them — supply one via WithMaterializer.
type Materializer func(ctx context.Context, srcDir, dstDir string) error

// ContainerdProvisioner provisions rootfs by pulling + unpacking the sandbox
// image ONCE into a shared, content-addressed directory (keyed by the resolved
// image digest, falling back to the image reference) and then materializing that
// tree into each per-session bundle's rootfs/. It shells out to `ctr`
// (containerd's CLI) via os/exec — mirroring how RunscIsolator invokes `runsc` —
// so it adds no Go dependency to this deliberately stdlib-only tree.
//
// containerd is already a required host dependency (deploy/README.md), so this
// reuses it rather than introducing a second image toolchain. The shared rootfs
// is safe to reuse across concurrent sessions because every bundle mounts it
// read-only and keeps writable state in separate /workspace + queue mounts.
type ContainerdProvisioner struct {
	// CtrBinary is the containerd CLI (default "ctr"). Overridable for tests/alt hosts.
	CtrBinary string
	// Namespace is the containerd namespace images are pulled into (default "ironclaw").
	Namespace string
	// SharedRoot holds the per-image extracted rootfs trees; each image is unpacked
	// into SharedRoot/<key>/ exactly once and reused across sessions.
	SharedRoot string

	// policy, when set, must approve the pulled image (by reference + resolved
	// digest) before any rootfs is unpacked. Nil preserves the prior behavior
	// (no verification); production hosts should configure one.
	policy TrustPolicy

	// Injectable host steps; default to os/exec shell-outs. They let the
	// orchestration (idempotency, ordering, error wrapping) be exercised without a
	// real containerd, and let hosts swap the unpack/materialize strategy.
	pull        func(ctx context.Context, image string) (key string, err error)
	unpack      func(ctx context.Context, image, destDir string) error
	materialize Materializer

	mu sync.Mutex // serializes shared-rootfs unpacking within this process
}

// TrustPolicy decides whether a pulled sandbox image is trusted to be unpacked
// into a rootfs, given its reference and the host-resolved content digest. A
// policy MUST fail closed: an unresolved digest or an unknown image is a
// rejection, not a pass. It is the gate that stops an attacker-substituted or
// tampered image from ever reaching a sandbox.
type TrustPolicy interface {
	Verify(ctx context.Context, image, digest string) error
}

// TrustPolicyFunc adapts a function to a TrustPolicy — e.g. to wrap an external
// signature verifier (cosign/notation) shelled out from the host.
type TrustPolicyFunc func(ctx context.Context, image, digest string) error

// Verify implements TrustPolicy.
func (f TrustPolicyFunc) Verify(ctx context.Context, image, digest string) error {
	return f(ctx, image, digest)
}

// PinnedDigestPolicy trusts an image only when its host-resolved digest exactly
// matches the digest pinned for that exact image reference. An image with no
// pin, or whose digest could not be resolved, is rejected. This is the
// dependency-free baseline: pin the digests you build and the provisioner
// refuses anything else (including a tag silently repointed to a new image).
type PinnedDigestPolicy struct {
	// pins maps an image reference to its expected "sha256:<hex>" digest.
	pins map[string]string
}

// NewPinnedDigestPolicy builds a PinnedDigestPolicy from a copy of pins (image
// reference -> "sha256:<hex>").
func NewPinnedDigestPolicy(pins map[string]string) *PinnedDigestPolicy {
	cp := make(map[string]string, len(pins))
	for k, v := range pins {
		cp[k] = strings.ToLower(strings.TrimSpace(v))
	}
	return &PinnedDigestPolicy{pins: cp}
}

// Verify implements TrustPolicy.
func (p *PinnedDigestPolicy) Verify(_ context.Context, image, digest string) error {
	want, ok := p.pins[image]
	if !ok {
		return fmt.Errorf("image %q has no pinned digest in the trust policy", image)
	}
	got := strings.ToLower(strings.TrimSpace(digest))
	if got == "" {
		return fmt.Errorf("image %q digest could not be resolved; refusing to unpack unverified content", image)
	}
	if got != want {
		return fmt.Errorf("image %q digest %s does not match pinned %s", image, got, want)
	}
	return nil
}

// WithTrustPolicy attaches an image TrustPolicy enforced before any unpack. A nil
// policy is ignored, preserving the unverified path.
func WithTrustPolicy(tp TrustPolicy) ContainerdOption {
	return func(p *ContainerdProvisioner) {
		if tp != nil {
			p.policy = tp
		}
	}
}

// ContainerdOption configures a ContainerdProvisioner.
type ContainerdOption func(*ContainerdProvisioner)

// WithCtrBinary overrides the containerd CLI (default "ctr").
func WithCtrBinary(bin string) ContainerdOption {
	return func(p *ContainerdProvisioner) {
		if bin != "" {
			p.CtrBinary = bin
		}
	}
}

// WithCtrNamespace overrides the containerd namespace (default "ironclaw").
func WithCtrNamespace(ns string) ContainerdOption {
	return func(p *ContainerdProvisioner) {
		if ns != "" {
			p.Namespace = ns
		}
	}
}

// WithSharedRoot overrides the shared extracted-rootfs directory.
func WithSharedRoot(dir string) ContainerdOption {
	return func(p *ContainerdProvisioner) {
		if dir != "" {
			p.SharedRoot = dir
		}
	}
}

// WithMaterializer overrides how the shared rootfs is placed into each bundle
// (default copyTree). Use this to plug in a bind- or reflink-based strategy on
// hosts that permit it.
func WithMaterializer(m Materializer) ContainerdOption {
	return func(p *ContainerdProvisioner) {
		if m != nil {
			p.materialize = m
		}
	}
}

// NewContainerdProvisioner constructs a ContainerdProvisioner with defaults,
// applying any options. The default steps shell out to `ctr`; the default
// materializer is a portable, privilege-free recursive copy.
func NewContainerdProvisioner(opts ...ContainerdOption) *ContainerdProvisioner {
	p := &ContainerdProvisioner{
		CtrBinary:  defaultCtrBinary,
		Namespace:  defaultCtrNamespace,
		SharedRoot: filepath.Join(os.TempDir(), "ironclaw", "rootfs"),
	}
	for _, o := range opts {
		o(p)
	}
	if p.pull == nil {
		p.pull = p.ctrPull
	}
	if p.unpack == nil {
		p.unpack = p.ctrUnpack
	}
	if p.materialize == nil {
		p.materialize = copyTree
	}
	return p
}

// Provision implements RootfsProvisioner. It is idempotent at both layers: the
// shared unpack is skipped when its ready marker exists, and the per-bundle
// materialize is skipped when rootfsDir is already populated.
func (p *ContainerdProvisioner) Provision(ctx context.Context, image, rootfsDir string) error {
	if image == "" {
		return fmt.Errorf("host/isolation: containerd provisioner requires a non-empty image reference")
	}
	// Ensure the image is present host-side and resolve its content digest. Pull
	// is idempotent: containerd skips content it already has.
	digest, err := p.pull(ctx, image)
	if err != nil {
		return fmt.Errorf("host/isolation: pull %q: %w", image, err)
	}

	// Verify the resolved digest/signature against the trust policy BEFORE any
	// rootfs is unpacked, so a substituted or tampered image never reaches a
	// sandbox. The policy fails closed on an unresolved digest.
	if p.policy != nil {
		if err := p.policy.Verify(ctx, image, digest); err != nil {
			return fmt.Errorf("host/isolation: refusing to unpack untrusted image %q: %w", image, err)
		}
	}

	// Key the shared rootfs by digest when known, falling back to the reference.
	key := digest
	if key == "" {
		key = image
	}
	sharedDir := filepath.Join(p.SharedRoot, sanitizeKey(key))

	if err := p.ensureUnpacked(ctx, image, sharedDir); err != nil {
		return err
	}

	if populated, _ := isPopulatedDir(rootfsDir); populated {
		return nil // a previous Provision already materialized this bundle
	}
	if err := p.materialize(ctx, sharedDir, rootfsDir); err != nil {
		return fmt.Errorf("host/isolation: materialize rootfs for %q into %s: %w", image, rootfsDir, err)
	}
	return nil
}

// ensureUnpacked unpacks image into sharedDir exactly once, guarded by an
// in-process mutex and a persistent ready marker so concurrent sessions (and
// process restarts) reuse a single extracted tree.
func (p *ContainerdProvisioner) ensureUnpacked(ctx context.Context, image, sharedDir string) error {
	marker := filepath.Join(sharedDir, readyMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := os.Stat(marker); err == nil {
		return nil // another goroutine finished while we waited for the lock
	}
	if err := os.MkdirAll(sharedDir, 0o700); err != nil {
		return fmt.Errorf("host/isolation: create shared rootfs dir %s: %w", sharedDir, err)
	}
	if err := p.unpack(ctx, image, sharedDir); err != nil {
		return fmt.Errorf("host/isolation: unpack %q into %s: %w", image, sharedDir, err)
	}
	if err := os.WriteFile(marker, []byte(image+"\n"), 0o600); err != nil {
		return fmt.Errorf("host/isolation: write ready marker %s: %w", marker, err)
	}
	return nil
}

// ctrPull pulls image into the namespace and best-effort resolves its digest as a
// content key. A pull failure is fatal; an unresolvable digest is not (the caller
// falls back to keying by the image reference).
func (p *ContainerdProvisioner) ctrPull(ctx context.Context, image string) (string, error) {
	if out, err := exec.CommandContext(ctx, p.CtrBinary, "-n", p.Namespace, "images", "pull", image).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s images pull: %w (%s)", p.CtrBinary, err, strings.TrimSpace(string(out)))
	}
	out, err := exec.CommandContext(ctx, p.CtrBinary, "-n", p.Namespace, "images", "ls", "name=="+image).CombinedOutput()
	if err != nil {
		return "", nil // digest unresolved; key by the image reference instead
	}
	return parseDigest(string(out)), nil
}

// ctrUnpack materializes the image's flattened rootfs into destDir. It mounts the
// image (snapshotter view) at a temp mountpoint, copies the tree into the
// persistent shared dir, then unmounts. `ctr images mount` needs CAP_SYS_ADMIN on
// the HOST (never inside a sandbox); hosts that disallow it can supply an
// extract-based unpack via the injectable seam (see ContainerdProvisioner.unpack).
func (p *ContainerdProvisioner) ctrUnpack(ctx context.Context, image, destDir string) error {
	mnt, err := os.MkdirTemp("", "ironclaw-img-*")
	if err != nil {
		return fmt.Errorf("create mountpoint: %w", err)
	}
	defer os.RemoveAll(mnt)

	if out, err := exec.CommandContext(ctx, p.CtrBinary, "-n", p.Namespace, "images", "mount", "--rw=false", image, mnt).CombinedOutput(); err != nil {
		return fmt.Errorf("%s images mount: %w (%s)", p.CtrBinary, err, strings.TrimSpace(string(out)))
	}
	copyErr := copyTree(ctx, mnt, destDir)
	// Always attempt the unmount, even if the copy failed.
	if out, err := exec.CommandContext(ctx, p.CtrBinary, "-n", p.Namespace, "images", "unmount", mnt).CombinedOutput(); err != nil && copyErr == nil {
		copyErr = fmt.Errorf("%s images unmount: %w (%s)", p.CtrBinary, err, strings.TrimSpace(string(out)))
	}
	return copyErr
}

// copyTree recursively copies the directory tree at src into dst, creating dst and
// preserving file modes and symlinks. It is the default, privilege-free rootfs
// materializer. The top-level ready marker is never copied so a per-session rootfs
// stays clean.
func copyTree(ctx context.Context, src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o700)
		}
		if rel == readyMarker {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o100) // keep dirs traversable
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
}

// copyFile copies a single regular file, creating parent dirs and preserving perm.
func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// isPopulatedDir reports whether dir exists, is a directory, and holds at least
// one entry. A missing dir is reported as not-populated without an error.
func isPopulatedDir(dir string) (bool, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !fi.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// sanitizeKey maps an image reference or digest to a safe single path segment.
func sanitizeKey(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}

// parseDigest extracts the first sha256:<hex> token from `ctr images ls` output,
// returning "" when none is present.
func parseDigest(ctrOutput string) string {
	for _, f := range strings.Fields(ctrOutput) {
		if strings.HasPrefix(f, "sha256:") {
			return f
		}
	}
	return ""
}
