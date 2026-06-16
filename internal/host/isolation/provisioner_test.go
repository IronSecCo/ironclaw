// OWNER: T-022

package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeProvisioner is a RootfsProvisioner that records calls and, unless told to
// fail or stay empty, populates rootfsDir with a sentinel file so Launch's gate
// passes.
type fakeProvisioner struct {
	calls int
	fail  error
	empty bool // when true, "succeeds" but leaves rootfs empty (a broken provisioner)
}

func (f *fakeProvisioner) Provision(ctx context.Context, image, rootfsDir string) error {
	f.calls++
	if f.fail != nil {
		return f.fail
	}
	if f.empty {
		return nil
	}
	if err := os.MkdirAll(rootfsDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rootfsDir, "sentinel"), []byte(image), 0o600)
}

// TestLaunchProvisionsRootfs: a configured provisioner populates rootfs so Launch
// gets PAST the ErrRootfsMissing gate (it may then fail to exec an absent runtime
// binary — that is fine; the point is the rootfs gate no longer blocks the launch).
func TestLaunchProvisionsRootfs(t *testing.T) {
	dir := t.TempDir()
	fp := &fakeProvisioner{}
	r := NewRunsc(WithBundleRoot(dir), WithProvisioner(fp))

	_, err := r.Launch(context.Background(), hardenedTestSpec())
	if fp.calls != 1 {
		t.Fatalf("provisioner should be invoked exactly once, got %d", fp.calls)
	}
	if errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("a populated rootfs must pass the gate, got ErrRootfsMissing: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "ses_test", "rootfs", "sentinel")); statErr != nil {
		t.Fatalf("provisioned rootfs sentinel missing: %v", statErr)
	}
}

// TestLaunchProvisionerErrorPropagates: a provisioner failure aborts Launch and is
// wrapped (never reported as ErrRootfsMissing).
func TestLaunchProvisionerErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("pull exploded")
	r := NewRunsc(WithBundleRoot(dir), WithProvisioner(&fakeProvisioner{fail: boom}))

	_, err := r.Launch(context.Background(), hardenedTestSpec())
	if !errors.Is(err, boom) {
		t.Fatalf("provisioner error should propagate, got %v", err)
	}
	if errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("a provisioner failure is not a missing-rootfs error: %v", err)
	}
}

// TestLaunchBrokenProvisionerStillGated: a provisioner that returns nil but leaves
// rootfs empty must still hit the post-condition gate (ErrRootfsMissing). This
// proves the gate is a real post-condition, not bypassed by the provisioner hook.
func TestLaunchBrokenProvisionerStillGated(t *testing.T) {
	dir := t.TempDir()
	r := NewRunsc(WithBundleRoot(dir), WithProvisioner(&fakeProvisioner{empty: true}))

	_, err := r.Launch(context.Background(), hardenedTestSpec())
	if !errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("an empty rootfs must still yield ErrRootfsMissing, got %v", err)
	}
}

// TestWithProvisionerNilIsIgnored: WithProvisioner(nil) leaves the isolator with no
// provisioner, so the pre-staged-rootfs gate behaves exactly as before.
func TestWithProvisionerNilIsIgnored(t *testing.T) {
	dir := t.TempDir()
	r := NewRunsc(WithBundleRoot(dir), WithProvisioner(nil))
	if r.Provisioner != nil {
		t.Fatal("WithProvisioner(nil) must not set a provisioner")
	}
	_, err := r.Launch(context.Background(), hardenedTestSpec())
	if !errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("with no provisioner and no rootfs, expected ErrRootfsMissing, got %v", err)
	}
}

// TestContainerdProvisionerUnpacksOnceMaterializesEach: the shared image is
// unpacked exactly once (ready marker) but each distinct bundle is materialized.
func TestContainerdProvisionerUnpacksOnceMaterializesEach(t *testing.T) {
	shared := t.TempDir()
	var unpacks, materializes int

	p := NewContainerdProvisioner(WithSharedRoot(shared))
	p.pull = func(ctx context.Context, image string) (string, error) { return "sha256:deadbeef", nil }
	p.unpack = func(ctx context.Context, image, destDir string) error {
		unpacks++
		return os.WriteFile(filepath.Join(destDir, "etc-os-release"), []byte("ironclaw"), 0o600)
	}
	p.materialize = func(ctx context.Context, srcDir, dstDir string) error {
		materializes++
		if err := os.MkdirAll(dstDir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dstDir, "rootfs-file"), []byte("x"), 0o600)
	}

	bundleA := filepath.Join(t.TempDir(), "a", "rootfs")
	bundleB := filepath.Join(t.TempDir(), "b", "rootfs")
	for _, b := range []string{bundleA, bundleB} {
		if err := p.Provision(context.Background(), "ghcr.io/example/sandbox:latest", b); err != nil {
			t.Fatalf("Provision(%s): %v", b, err)
		}
	}

	if unpacks != 1 {
		t.Fatalf("shared image should unpack exactly once, got %d", unpacks)
	}
	if materializes != 2 {
		t.Fatalf("each bundle should materialize, got %d", materializes)
	}
	// The ready marker must exist in the digest-keyed shared dir.
	if _, err := os.Stat(filepath.Join(shared, sanitizeKey("sha256:deadbeef"), readyMarker)); err != nil {
		t.Fatalf("ready marker missing: %v", err)
	}
}

// TestContainerdProvisionerSkipsPopulatedBundle: an already-populated rootfs is not
// re-materialized (per-bundle idempotency).
func TestContainerdProvisionerSkipsPopulatedBundle(t *testing.T) {
	p := NewContainerdProvisioner(WithSharedRoot(t.TempDir()))
	p.pull = func(ctx context.Context, image string) (string, error) { return "", nil }
	p.unpack = func(ctx context.Context, image, destDir string) error { return nil }
	materializes := 0
	p.materialize = func(ctx context.Context, srcDir, dstDir string) error { materializes++; return nil }

	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := os.MkdirAll(rootfs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootfs, "already-there"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.Provision(context.Background(), "img:latest", rootfs); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if materializes != 0 {
		t.Fatalf("a populated bundle rootfs must not be re-materialized, got %d", materializes)
	}
}

// TestContainerdProvisionerPullErrorIsFatal: a failed pull aborts Provision.
func TestContainerdProvisionerPullErrorIsFatal(t *testing.T) {
	p := NewContainerdProvisioner(WithSharedRoot(t.TempDir()))
	boom := errors.New("no such image")
	p.pull = func(ctx context.Context, image string) (string, error) { return "", boom }
	p.unpack = func(ctx context.Context, image, destDir string) error {
		t.Fatal("unpack must not run after a pull failure")
		return nil
	}

	err := p.Provision(context.Background(), "img:latest", filepath.Join(t.TempDir(), "rootfs"))
	if !errors.Is(err, boom) {
		t.Fatalf("pull error should be fatal, got %v", err)
	}
}

// TestContainerdProvisionerRejectsEmptyImage guards the obvious misuse.
func TestContainerdProvisionerRejectsEmptyImage(t *testing.T) {
	p := NewContainerdProvisioner()
	if err := p.Provision(context.Background(), "", t.TempDir()); err == nil {
		t.Fatal("expected an error for an empty image reference")
	}
}

// TestNewContainerdProvisionerDefaults verifies the constructor wires defaults and
// non-nil steps so the zero-option provisioner is usable.
func TestNewContainerdProvisionerDefaults(t *testing.T) {
	p := NewContainerdProvisioner()
	if p.CtrBinary != defaultCtrBinary || p.Namespace != defaultCtrNamespace {
		t.Fatalf("defaults not applied: bin=%q ns=%q", p.CtrBinary, p.Namespace)
	}
	if p.SharedRoot == "" {
		t.Fatal("SharedRoot default must not be empty")
	}
	if p.pull == nil || p.unpack == nil || p.materialize == nil {
		t.Fatal("default steps must be non-nil")
	}
}

// TestPinnedDigestPolicyVerify covers the pass/fail cases of the baseline policy.
func TestPinnedDigestPolicyVerify(t *testing.T) {
	pol := NewPinnedDigestPolicy(map[string]string{
		"ghcr.io/example/sandbox:latest": "sha256:ABCDEF", // mixed case -> normalized
	})
	cases := []struct {
		name, image, digest string
		wantErr             bool
	}{
		{"match (case-insensitive)", "ghcr.io/example/sandbox:latest", "sha256:abcdef", false},
		{"digest mismatch", "ghcr.io/example/sandbox:latest", "sha256:0000", true},
		{"unresolved digest fails closed", "ghcr.io/example/sandbox:latest", "", true},
		{"unpinned image rejected", "ghcr.io/evil/img:latest", "sha256:abcdef", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := pol.Verify(context.Background(), c.image, c.digest)
			if (err != nil) != c.wantErr {
				t.Fatalf("Verify(%q,%q) err=%v, wantErr=%v", c.image, c.digest, err, c.wantErr)
			}
		})
	}
}

// TestProvisionVerifiesBeforeUnpack: a trusted digest proceeds to unpack; an
// untrusted one aborts Provision and unpack never runs.
func TestProvisionVerifiesBeforeUnpack(t *testing.T) {
	const image = "ghcr.io/example/sandbox:latest"
	pol := NewPinnedDigestPolicy(map[string]string{image: "sha256:trusted"})

	newProv := func(t *testing.T, digest string, unpacked *bool) *ContainerdProvisioner {
		p := NewContainerdProvisioner(WithSharedRoot(t.TempDir()), WithTrustPolicy(pol))
		p.pull = func(ctx context.Context, img string) (string, error) { return digest, nil }
		p.unpack = func(ctx context.Context, img, destDir string) error {
			*unpacked = true
			return os.WriteFile(filepath.Join(destDir, "f"), []byte("x"), 0o600)
		}
		p.materialize = func(ctx context.Context, srcDir, dstDir string) error {
			if err := os.MkdirAll(dstDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dstDir, "rootfs-file"), []byte("x"), 0o600)
		}
		return p
	}

	t.Run("trusted digest unpacks", func(t *testing.T) {
		var unpacked bool
		p := newProv(t, "sha256:trusted", &unpacked)
		if err := p.Provision(context.Background(), image, filepath.Join(t.TempDir(), "rootfs")); err != nil {
			t.Fatalf("Provision (trusted): %v", err)
		}
		if !unpacked {
			t.Fatal("a trusted image should be unpacked")
		}
	})

	t.Run("untrusted digest is refused before unpack", func(t *testing.T) {
		var unpacked bool
		p := newProv(t, "sha256:tampered", &unpacked)
		err := p.Provision(context.Background(), image, filepath.Join(t.TempDir(), "rootfs"))
		if err == nil {
			t.Fatal("an untrusted digest must abort Provision")
		}
		if unpacked {
			t.Fatal("unpack must NOT run for an untrusted image")
		}
	})
}

// TestProvisionTrustPolicyFuncAdapter: a TrustPolicyFunc is honored and can wrap
// an external verifier; its rejection aborts Provision.
func TestProvisionTrustPolicyFuncAdapter(t *testing.T) {
	called := false
	deny := TrustPolicyFunc(func(_ context.Context, image, digest string) error {
		called = true
		return errors.New("signature invalid")
	})
	p := NewContainerdProvisioner(WithSharedRoot(t.TempDir()), WithTrustPolicy(deny))
	p.pull = func(ctx context.Context, image string) (string, error) { return "sha256:whatever", nil }
	p.unpack = func(ctx context.Context, image, destDir string) error {
		t.Fatal("unpack must not run when the policy rejects")
		return nil
	}

	err := p.Provision(context.Background(), "img:latest", filepath.Join(t.TempDir(), "rootfs"))
	if err == nil || !called {
		t.Fatalf("policy func should be called and reject; called=%v err=%v", called, err)
	}
}

// TestProvisionNoPolicyUnverified: with no trust policy the prior behavior is
// preserved (unpack proceeds without verification).
func TestProvisionNoPolicyUnverified(t *testing.T) {
	var unpacked bool
	p := NewContainerdProvisioner(WithSharedRoot(t.TempDir()))
	p.pull = func(ctx context.Context, image string) (string, error) { return "sha256:any", nil }
	p.unpack = func(ctx context.Context, image, destDir string) error {
		unpacked = true
		return os.WriteFile(filepath.Join(destDir, "f"), []byte("x"), 0o600)
	}
	p.materialize = func(ctx context.Context, srcDir, dstDir string) error {
		if err := os.MkdirAll(dstDir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dstDir, "rootfs-file"), []byte("x"), 0o600)
	}
	if err := p.Provision(context.Background(), "img:latest", filepath.Join(t.TempDir(), "rootfs")); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !unpacked {
		t.Fatal("with no policy, unpack should proceed as before")
	}
}

// TestWithTrustPolicyNilIsIgnored: WithTrustPolicy(nil) leaves verification off.
func TestWithTrustPolicyNilIsIgnored(t *testing.T) {
	p := NewContainerdProvisioner(WithTrustPolicy(nil))
	if p.policy != nil {
		t.Fatal("WithTrustPolicy(nil) must not set a policy")
	}
}

// TestCopyTree exercises the default materializer: dirs, files, a symlink, and the
// ready-marker skip.
func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "sandbox"), []byte("#!bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, readyMarker), []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin/sandbox", filepath.Join(src, "entrypoint")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "rootfs")
	if err := copyTree(context.Background(), src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(dst, "bin", "sandbox")); err != nil || string(got) != "#!bin" {
		t.Fatalf("copied file mismatch: %q err=%v", got, err)
	}
	if link, err := os.Readlink(filepath.Join(dst, "entrypoint")); err != nil || link != "bin/sandbox" {
		t.Fatalf("symlink not preserved: %q err=%v", link, err)
	}
	if _, err := os.Stat(filepath.Join(dst, readyMarker)); !os.IsNotExist(err) {
		t.Fatalf("ready marker must not be copied into the bundle rootfs (err=%v)", err)
	}
}
