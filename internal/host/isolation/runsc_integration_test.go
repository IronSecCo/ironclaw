// Real-launch integration test for the runsc/gVisor isolator.
//
// This is the env-gated test the R3 acceptance calls for: it exercises an
// ACTUAL `runsc run` of a hardened bundle and asserts the trust boundary from
// inside a live sandbox — least-privilege queue mounts (inbound read-only,
// outbound read-write) and network=none. It is deliberately NOT part of the
// normal `go test` run because most CI hosts (and all macOS dev machines) lack
// gVisor; it only executes when explicitly opted in:
//
//	IRONCLAW_RUNSC_INTEGRATION=1 go test -run TestRunscRealLaunch ./internal/host/isolation
//
// Optionally point at a specific runtime binary with IRONCLAW_RUNSC_BIN
// (defaults to "runsc" on PATH). The test builds a tiny static probe binary at
// /sandbox in a from-scratch rootfs, launches it through the production
// RunscIsolator.Launch path, and reads back what the sandbox observed via the
// read-write outbound mount.

package isolation

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// probeSource is the in-sandbox program staged as /sandbox. It records what the
// sandbox can see about its own boundary and writes the verdict to the
// read-write outbound mount (the host reads it back after the container exits):
//   - inbound_ro: opening the inbound queue for WRITE must fail (it is bound ro).
//   - nonloop_ifaces: count of non-loopback interfaces with addresses; under
//     network=none this must be zero.
//
// It is pure stdlib and built static (CGO_ENABLED=0) so it runs in a rootfs that
// contains nothing but this one binary.
const probeSource = `package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	inboundRO := false
	if f, err := os.OpenFile("/queue/inbound.db", os.O_WRONLY, 0); err != nil {
		inboundRO = true // good: cannot open the read-only inbound mount for write
	} else {
		f.Close()
	}

	nonLoop := 0
	if ifaces, err := net.Interfaces(); err == nil {
		for _, ifi := range ifaces {
			if ifi.Flags&net.FlagLoopback != 0 {
				continue
			}
			if addrs, _ := ifi.Addrs(); len(addrs) > 0 {
				nonLoop++
			}
		}
	}

	out := fmt.Sprintf("PROBE inbound_ro=%v nonloop_ifaces=%d\n", inboundRO, nonLoop)
	if err := os.WriteFile("/queue/outbound.db", []byte(out), 0o600); err != nil {
		os.Exit(3) // could not write the read-write outbound mount
	}
}
`

func TestRunscRealLaunch(t *testing.T) {
	if os.Getenv("IRONCLAW_RUNSC_INTEGRATION") != "1" {
		t.Skip("set IRONCLAW_RUNSC_INTEGRATION=1 to run the real runsc/gVisor launch test")
	}

	runtimeBin := os.Getenv("IRONCLAW_RUNSC_BIN")
	if runtimeBin == "" {
		runtimeBin = DefaultRuntimeBinary
	}
	if _, err := exec.LookPath(runtimeBin); err != nil {
		t.Skipf("runtime %q not found on PATH — install gVisor or set IRONCLAW_RUNSC_BIN: %v", runtimeBin, err)
	}

	root := t.TempDir()
	bundleRoot := filepath.Join(root, "bundles")

	// Host-side mount sources. The inbound/outbound queue files stand in for the
	// real encrypted SQLite queues; the probe only needs them to exist so the
	// bind mounts resolve, then checks the read-only vs read-write distinction.
	hostDir := filepath.Join(root, "host")
	if err := os.MkdirAll(hostDir, 0o700); err != nil {
		t.Fatalf("mkdir host dir: %v", err)
	}
	inboundPath := filepath.Join(hostDir, "inbound.db")
	outboundPath := filepath.Join(hostDir, "outbound.db")
	if err := os.WriteFile(inboundPath, []byte("inbound"), 0o600); err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if err := os.WriteFile(outboundPath, nil, 0o600); err != nil {
		t.Fatalf("seed outbound: %v", err)
	}

	// A real unix socket for the model-proxy bind mount. The probe does not
	// connect to it; it only needs to exist so the OCI bind resolves.
	sockPath := filepath.Join(hostDir, "modelproxy.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen model-proxy socket: %v", err)
	}
	defer ln.Close()

	const sessionID = "ses_runsc_integration"

	// Pre-stage the rootfs: build the static probe as /sandbox and create the
	// bind-mount target dirs the OCI spec expects. We use the no-provisioner
	// path so the test stays self-contained (no containerd dependency); the
	// provisioner seam is covered by the provisioner unit tests.
	rootfs := filepath.Join(bundleRoot, sessionID, "rootfs")
	for _, d := range []string{rootfs, filepath.Join(rootfs, "queue"), filepath.Join(rootfs, "workspace"), filepath.Join(rootfs, "run", "ironclaw")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir rootfs dir %s: %v", d, err)
		}
	}
	buildProbe(t, filepath.Join(rootfs, "sandbox"))

	iso := NewRunsc(WithRuntimeBinary(runtimeBin), WithBundleRoot(bundleRoot))
	spec := HardenedSpec(sessionID, "ironclaw-sandbox:integration", inboundPath, outboundPath, sockPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	handle, err := iso.Launch(ctx, spec)
	if err != nil {
		t.Fatalf("Launch under %q: %v", runtimeBin, err)
	}
	t.Cleanup(func() { _ = handle.Stop(context.Background()) })

	// Wait for the init process (the probe) to exit. `runsc run` reaps the
	// container when init exits, so Alive flips to false.
	deadline := time.Now().Add(45 * time.Second)
	for handle.Alive(ctx) {
		if time.Now().After(deadline) {
			t.Fatalf("sandbox did not exit within deadline")
		}
		time.Sleep(250 * time.Millisecond)
	}

	data, err := os.ReadFile(outboundPath)
	if err != nil {
		t.Fatalf("read outbound mount back: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got == "" {
		t.Fatalf("probe wrote nothing to the read-write outbound mount — sandbox did not run or could not write outbound")
	}
	t.Logf("sandbox probe verdict: %q", got)

	if !strings.Contains(got, "inbound_ro=true") {
		t.Errorf("inbound queue was NOT read-only inside the sandbox: %q", got)
	}
	if !strings.Contains(got, "nonloop_ifaces=0") {
		t.Errorf("sandbox saw non-loopback network interfaces (network=none violated): %q", got)
	}
}

// buildProbe compiles probeSource into a static linux binary at outPath.
func buildProbe(t *testing.T, outPath string) {
	t.Helper()
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "main.go")
	if err := os.WriteFile(srcFile, []byte(probeSource), 0o600); err != nil {
		t.Fatalf("write probe source: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", outPath, srcFile)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}
	if err := os.Chmod(outPath, 0o755); err != nil {
		t.Fatalf("chmod probe: %v", err)
	}
	// Sanity: the probe must be a real file the runtime can exec.
	if fi, err := os.Stat(outPath); err != nil || fi.Size() == 0 {
		t.Fatalf("probe binary missing or empty at %s (err=%v)", outPath, err)
	}
}
