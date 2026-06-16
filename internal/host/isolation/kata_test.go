// OWNER: AGENT1

package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeKataRuntime writes a stand-in OCI runtime binary that accepts only the
// run/kill/delete subcommands (exit 0) and rejects anything else (exit 3). It
// mocks the Kata CLI so Launch/Stop can be exercised without a real runtime: a
// successful Stop (which waits on kill+delete) proves the correct subcommands
// were driven.
func fakeKataRuntime(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-kata")
	body := "#!/bin/sh\ncase \"$1\" in\n  run|kill|delete) exit 0 ;;\n  *) exit 3 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}
	return script
}

func TestNewKataDefaults(t *testing.T) {
	k := NewKata()
	if k.RuntimeBinary() != DefaultKataRuntimeBinary {
		t.Fatalf("default runtime = %q, want %q", k.RuntimeBinary(), DefaultKataRuntimeBinary)
	}
	if k.BundleRoot() == "" {
		t.Fatal("default BundleRoot must not be empty")
	}
}

func TestKataRuntimeBinaryOverride(t *testing.T) {
	// A caller-supplied WithRuntimeBinary (e.g. a containerd shim) wins over the
	// kata-runtime default.
	k := NewKata(WithRuntimeBinary("containerd-shim-kata-v2"))
	if got := k.RuntimeBinary(); got != "containerd-shim-kata-v2" {
		t.Fatalf("override ignored: RuntimeBinary = %q", got)
	}
}

func TestKataWritesHardenedBundle(t *testing.T) {
	dir := t.TempDir()
	k := NewKata(WithBundleRoot(dir))
	bundleDir, err := k.WriteBundle(hardenedTestSpec())
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(bundleDir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var got OCISpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}
	if got.OCIVersion == "" {
		t.Fatal("config.json missing ociVersion")
	}
	// Same hardened spec as the gVisor backend: read-only rootfs.
	if !got.Root.Readonly {
		t.Fatal("kata bundle rootfs must be read-only (hardened spec)")
	}
}

func TestKataLaunchRequiresProvisionedRootfs(t *testing.T) {
	dir := t.TempDir()
	k := NewKata(WithBundleRoot(dir))
	_, err := k.Launch(context.Background(), hardenedTestSpec())
	if err == nil {
		t.Fatal("expected Launch to fail without a provisioned rootfs")
	}
	if !errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("expected ErrRootfsMissing, got %v", err)
	}
	// The hardened bundle is still written before the rootfs gate.
	if _, statErr := os.Stat(filepath.Join(dir, "ses_test", "config.json")); statErr != nil {
		t.Fatalf("config.json should be written before the rootfs check: %v", statErr)
	}
}

func TestKataLaunchAndStopDriveKataRuntime(t *testing.T) {
	dir := t.TempDir()
	rt := fakeKataRuntime(t)
	k := NewKata(WithBundleRoot(dir), WithRuntimeBinary(rt))

	spec := hardenedTestSpec()
	// Pre-stage an (empty) rootfs so the gate passes and the runtime is exec'd.
	if err := os.MkdirAll(filepath.Join(dir, string(spec.SessionID), "rootfs"), 0o755); err != nil {
		t.Fatalf("stage rootfs: %v", err)
	}

	h, err := k.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// The launched bundle is the hardened spec.
	data, err := os.ReadFile(filepath.Join(dir, string(spec.SessionID), "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var got OCISpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("config.json invalid: %v", err)
	}
	if !got.Root.Readonly {
		t.Fatal("kata bundle rootfs must be read-only (same hardened spec)")
	}

	// Stop synchronously execs `<rt> kill ...` then `<rt> delete ...`; the fake
	// returns 0 only for those subcommands, so a nil error proves KataIsolator's
	// handle drove the Kata runtime CLI with the right verbs.
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop via kata runtime: %v", err)
	}
}
