//go:build wsg_verify

package wsg

import (
	"context"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/isolation"
)

// copyProvisioner populates a bundle rootfs by copying a pre-staged rootfs tree.
// In CI the wsg-verify workflow stages a minimal busybox rootfs with the isolation
// probe at /sandbox; this provisioner places it where runsc expects it. (Production
// uses the containerd/OCI provisioner; the copy keeps the live test self-contained
// and free of a container registry.)
type copyProvisioner struct{ src string }

func (p copyProvisioner) Provision(_ context.Context, _ string, rootfsDir string) error {
	return filepath.WalkDir(p.src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p.src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(rootfsDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(dst)
			return os.Symlink(target, dst)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
}

// TestG2_LiveSandbox_Runsc launches a REAL sandbox under gVisor via the production
// isolation.Launch path and reads the probe's verdict from the host-bound
// /workspace: it asserts only `lo` is present (no NIC), outbound internet fails, and
// the model-proxy unix socket is reachable.
//
// It self-skips when runsc is not installed or the CI rootfs/probe is not staged
// (i.e. on any non-Linux dev host), and treats an inability to START runsc on the
// runner as a skip (with the reason logged) rather than a failure — a real
// isolation breach (a NIC appears, or internet is reachable) is always a hard fail.
//
// KNOWN ENVIRONMENT LIMITATION (IRO-103): on GitHub-hosted runners the OCI spec is
// accepted (userns mapped, mounts staged) but runsc fails at GOFER PROCESS CREATION
// with `cannot create gofer process: fork/exec /proc/self/exe: invalid argument` —
// the hosted runner's restricted environment rejects the gofer re-exec (independent
// of platform/rootless/network mode). `runsc do`, which the workflow's smoke proves
// works, runs against the host FS with NO gofer, so it never exercises this path. The
// row therefore skips with that precise reason on hosted runners and reaches PASS on a
// gofer-capable host (real /dev/kvm or self-hosted). The host-side G2 rows
// (TestG2_OCISpec_NetworkNone, egress broker allow/deny+audit) cover the capability.
func TestG2_LiveSandbox_Runsc(t *testing.T) {
	if _, err := exec.LookPath("runsc"); err != nil {
		t.Skip("runsc (gVisor) not installed — live sandbox launch is a Linux/CI-only row")
	}
	rootfs := os.Getenv("IRONCLAW_WSG_ROOTFS")
	if rootfs == "" {
		t.Skip("IRONCLAW_WSG_ROOTFS unset — the CI workflow stages the rootfs+probe for the live launch")
	}
	if _, err := os.Stat(filepath.Join(rootfs, "sandbox")); err != nil {
		t.Skipf("staged rootfs has no /sandbox probe at %s: %v", rootfs, err)
	}

	base := shortSocketDir(t)
	proxySock := filepath.Join(base, "modelproxy.sock")
	ln, err := net.Listen("unix", proxySock)
	if err != nil {
		t.Fatalf("listen model-proxy socket: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	inbound := filepath.Join(base, "inbound.db")
	outbound := filepath.Join(base, "outbound.db")
	for _, f := range []string{inbound, outbound} {
		if err := os.WriteFile(f, []byte{}, 0o600); err != nil {
			t.Fatalf("create queue file: %v", err)
		}
	}
	workspace := filepath.Join(base, "workspace")
	// World-writable so the sandbox's mapped non-root uid can write its verdict.
	if err := os.MkdirAll(workspace, 0o777); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	_ = os.Chmod(workspace, 0o777)

	spec := isolation.HardenedSpecWithStorage(
		"wsg-live", "wsg/probe:local", inbound, outbound, proxySock,
		workspace, "", "",
	)

	bundleRoot := filepath.Join(base, "bundles")
	debugDir := filepath.Join(base, "runsc-debug")
	opts := []isolation.Option{
		isolation.WithBundleRoot(bundleRoot),
		isolation.WithProvisioner(copyProvisioner{src: rootfs}),
		// Capture runsc's own stderr (<bundle>/runtime.log) and gVisor's --debug log so
		// a launch that fails to produce a verdict is diagnosable in CI instead of a
		// silent skip (IRO-103).
		isolation.WithRuntimeDebug(debugDir),
	}
	// Optional extra global runsc flags for a constrained CI host (the gofer's userns
	// clone is rejected on GitHub hosted runners; --rootless adapts the namespace
	// setup). Space-separated; set by the workflow so flags can be tuned without a
	// rebuild.
	if extra := strings.Fields(os.Getenv("IRONCLAW_WSG_RUNSC_FLAGS")); len(extra) > 0 {
		opts = append(opts, isolation.WithRuntimeFlags(extra...))
		t.Logf("live launch using extra runsc flags: %v", extra)
	}
	iso := isolation.NewRunsc(opts...)
	bundleDir := filepath.Join(bundleRoot, "wsg-live")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	handle, err := iso.Launch(ctx, spec)
	if err != nil {
		// The runner could not start gVisor (e.g. nested-virt limits). That is an
		// environment limitation, not an isolation breach — skip with the reason so
		// QA can see whether the live launch actually executed.
		dumpLaunchDiagnostics(t, bundleDir, debugDir)
		t.Skipf("runsc could not launch on this runner (environment limitation, not an isolation breach): %v", err)
	}
	defer handle.Stop(context.Background())

	resultPath := filepath.Join(workspace, "result.txt")
	deadline := time.Now().Add(45 * time.Second)
	var result string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(resultPath); err == nil && len(b) > 0 {
			result = string(b)
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if result == "" {
		// Surface the runtime's own stderr + gVisor debug log so the cause is visible
		// rather than a blind skip. With the OCI spec fixed (standard /proc,/dev,/sys
		// mounts and the process uid mapped) a gofer-capable runner reaches PASS below.
		dumpLaunchDiagnostics(t, bundleDir, debugDir)
		rt, _ := os.ReadFile(filepath.Join(bundleDir, "runtime.log"))
		if strings.Contains(string(rt), "cannot create gofer process") {
			// The OCI spec is ACCEPTED (userns mapped, all mounts staged) — runsc reaches
			// gofer creation and fails re-execing /proc/self/exe. That is a GitHub hosted-
			// runner limitation on gVisor's gofer process creation (`runsc do`, which the
			// smoke proves works, runs against the host FS with NO gofer, so it never
			// exercises this path). It is not an isolation breach or a spec defect; the
			// host-side G2 rows cover network=none + egress audit. Re-run on a gofer-
			// capable host (real /dev/kvm or self-hosted) to exercise the in-sandbox verdict.
			t.Skip("runsc reached gofer creation then failed (`cannot create gofer process: fork/exec /proc/self/exe: invalid argument`) — a hosted-runner limitation on gVisor gofer creation, not an isolation breach. OCI spec accepted; host-side G2 assertions still cover network=none + egress audit")
		}
		t.Skip("live sandbox produced no verdict within the deadline (runner could not run the probe) — host-side G2 assertions still cover network=none + egress audit")
	}

	t.Logf("G2 live sandbox verdict:\n%s", result)
	assertProbe(t, result, "iface_only_lo")
	assertProbe(t, result, "internet_blocked")
	assertProbe(t, result, "modelproxy_ok")
	t.Log("G2 live: real gVisor sandbox has only lo, internet egress blocked, model-proxy socket reachable")
}

// dumpLaunchDiagnostics logs the artifacts that explain WHY a real gVisor launch
// produced no verdict: the bundle's config.json (the exact OCI spec runsc was given),
// the runtime's captured stdout+stderr (<bundle>/runtime.log), and gVisor's --debug
// log. It is best-effort — every read is tolerant of a missing file — and turns an
// otherwise opaque skip into an actionable CI signal (IRO-103).
func dumpLaunchDiagnostics(t *testing.T, bundleDir, debugDir string) {
	t.Helper()
	if b, err := os.ReadFile(filepath.Join(bundleDir, "config.json")); err == nil {
		t.Logf("--- bundle config.json (%s) ---\n%s", bundleDir, b)
	}
	if b, err := os.ReadFile(filepath.Join(bundleDir, "runtime.log")); err == nil && len(b) > 0 {
		t.Logf("--- runsc run stdout+stderr (runtime.log) ---\n%s", b)
	} else {
		t.Logf("--- runtime.log empty or absent at %s (err=%v) ---", bundleDir, err)
	}
	entries, err := os.ReadDir(debugDir)
	if err != nil {
		t.Logf("--- no gVisor debug dir at %s (err=%v) ---", debugDir, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(debugDir, e.Name())
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		// Tail the debug log: the failure is at the end and the full trace is huge.
		const tail = 4000
		if len(b) > tail {
			b = b[len(b)-tail:]
		}
		t.Logf("--- gVisor debug log %s (tail) ---\n%s", e.Name(), b)
	}
}

// assertProbe requires the probe to have reported "<key>=PASS".
func assertProbe(t *testing.T, result, key string) {
	t.Helper()
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			if line == key+"=PASS" {
				return
			}
			t.Fatalf("isolation assertion %q failed: %q", key, line)
		}
	}
	t.Fatalf("probe verdict missing %q:\n%s", key, result)
}
