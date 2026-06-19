package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/isolation"
)

// TestSandboxLaunchSmoke is an env-gated LIVE sandbox-launch smoke test. Unlike
// TestFullLifecycleRunscGated (which only builds the OCI spec), this test actually
// invokes the real OCI runtime to launch and tear down a hardened sandbox, so it
// can only run where gVisor's runsc is installed (Linux) AND the operator has
// staged a sandbox rootfs. It is skipped everywhere else — including non-Linux dev
// machines and CI without a sandbox image — so the default `go test` stays
// hermetic.
//
// Gating (all required, else skip):
//   - runsc present on PATH (the gVisor OCI runtime).
//   - IRONCLAW_E2E_SANDBOX_ROOTFS set to a directory holding a provisioned sandbox
//     rootfs (must contain the /sandbox entrypoint the hardened spec execs).
//
// What it asserts:
//   - The real RunscIsolator.Launch returns a live Handle with no error (the OCI
//     bundle is hardened: network=none, caps dropped, no-new-privs, ro rootfs).
//   - Handle.Stop tears the container down cleanly.
//
// The rootfs is staged into the isolator's own bundle dir before Launch so no
// containerd/ctr dependency is required; an operator that prefers a containerd
// provisioner can wire one and drop the env var.
func TestSandboxLaunchSmoke(t *testing.T) {
	if _, err := exec.LookPath("runsc"); err != nil {
		t.Skip("runsc (gVisor) not installed; skipping live sandbox-launch smoke test")
	}
	rootfsSrc := os.Getenv("IRONCLAW_E2E_SANDBOX_ROOTFS")
	if rootfsSrc == "" {
		t.Skip("IRONCLAW_E2E_SANDBOX_ROOTFS not set; skipping live sandbox-launch smoke test")
	}
	if fi, err := os.Stat(rootfsSrc); err != nil || !fi.IsDir() {
		t.Skipf("IRONCLAW_E2E_SANDBOX_ROOTFS=%q is not a directory; skipping", rootfsSrc)
	}

	const sessionID = "e2e-smoke"
	bundleRoot := t.TempDir()
	iso := isolation.NewRunsc(isolation.WithBundleRoot(bundleRoot))

	// Mount sources the hardened spec binds must exist for the runtime to start.
	dataDir := t.TempDir()
	inbound := filepath.Join(dataDir, "inbound.db")
	outbound := filepath.Join(dataDir, "outbound.db")
	proxySock := filepath.Join(dataDir, "modelproxy.sock")
	for _, p := range []string{inbound, outbound, proxySock} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatalf("stage mount source %s: %v", p, err)
		}
	}

	spec := isolation.HardenedSpec(sessionID, "e2e-smoke-image", inbound, outbound, proxySock)

	// Write the bundle, then stage the provided rootfs into <bundle>/rootfs so the
	// no-provisioner Launch path finds it (Launch rewrites config.json idempotently
	// and keeps the rootfs gate as a real post-condition).
	bundleDir, err := iso.WriteBundle(spec)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if err := stageRootfs(rootfsSrc, filepath.Join(bundleDir, "rootfs")); err != nil {
		t.Fatalf("stage rootfs: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	h, err := iso.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("live sandbox Launch failed under runsc: %v", err)
	}
	if h == nil {
		t.Fatal("Launch returned a nil handle")
	}
	t.Logf("sandbox launched under runsc; alive=%v", h.Alive(ctx))

	if err := h.Stop(ctx); err != nil {
		t.Fatalf("Stop after live launch: %v", err)
	}
}

// stageRootfs copies the provisioned rootfs tree at src into dst using the host's
// cp(1) (preserving modes/symlinks), so the smoke test does not depend on a
// containerd image unpacker.
func stageRootfs(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	// "cp -a src/. dst" copies the directory CONTENTS into dst.
	cmd := exec.Command("cp", "-a", filepath.Join(src, "."), dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &stageError{msg: string(out), err: err}
	}
	return nil
}

type stageError struct {
	msg string
	err error
}

func (e *stageError) Error() string { return "cp rootfs: " + e.err.Error() + ": " + e.msg }
func (e *stageError) Unwrap() error  { return e.err }
