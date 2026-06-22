package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// checkStatus is a doctor check verdict.
type checkStatus string

const (
	checkOK   checkStatus = "OK"
	checkWarn checkStatus = "WARN"
	checkFail checkStatus = "FAIL"
)

// checkResult is one diagnostic line: what was checked, the verdict, what was
// seen, and (when not OK) an actionable fix.
type checkResult struct {
	Name   string
	Status checkStatus
	Detail string
	Fix    string
}

// cmdDoctor implements `ironctl doctor` — environment + reachability checks with
// actionable fixes. It exits non-zero if any check FAILs so it is usable as a
// health gate in scripts.
func cmdDoctor(addr string, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	socket := fs.String("model-proxy-socket", "/run/ironclaw/modelproxy.sock",
		"model-proxy unix socket to probe")
	runtimeBin := fs.String("runtime", "runsc", "OCI runtime binary to check (gVisor)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	results := []checkResult{
		checkAPI(addr),
		checkAuth(addr),
		checkReadiness(addr),
		checkRuntime(*runtimeBin),
		checkModelProxy(*socket),
	}
	printChecks(os.Stdout, results)

	fails := 0
	for _, r := range results {
		if r.Status == checkFail {
			fails++
		}
	}
	if fails > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", fails)
	}
	return nil
}

// checkAPI verifies the control-plane is reachable via its unauthenticated
// liveness probe.
func checkAPI(addr string) checkResult {
	r := checkResult{Name: "control-plane API"}
	resp, err := httpGet(addr + "/healthz")
	if err != nil {
		r.Status = checkFail
		r.Detail = err.Error()
		r.Fix = "is the daemon running? check --addr (default " + defaultAddr + ") and that the port is reachable"
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		r.Status = checkFail
		r.Detail = fmt.Sprintf("/healthz returned HTTP %d", resp.StatusCode)
		r.Fix = "the control-plane is up but unhealthy; check its logs"
		return r
	}
	r.Status = checkOK
	r.Detail = "reachable at " + addr
	return r
}

// checkAuth probes a bearer-gated endpoint to classify the token posture.
func checkAuth(addr string) checkResult {
	r := checkResult{Name: "API auth / token"}
	resp, err := httpGet(addr + "/v1/changes/pending")
	if err != nil {
		r.Status = checkWarn
		r.Detail = err.Error()
		r.Fix = "could not reach the API to test auth; fix control-plane reachability first"
		return r
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK && token != "":
		r.Status = checkOK
		r.Detail = "bearer token accepted"
	case resp.StatusCode == http.StatusOK && token == "":
		r.Status = checkWarn
		r.Detail = "API is ungated (no token required)"
		r.Fix = "set IRONCLAW_API_TOKEN on the daemon and client for defense-in-depth behind the mesh"
	case resp.StatusCode == http.StatusUnauthorized && token == "":
		r.Status = checkWarn
		r.Detail = "API requires a token but none is configured"
		r.Fix = "export IRONCLAW_API_TOKEN=<token> (or pass --token)"
	case resp.StatusCode == http.StatusUnauthorized && token != "":
		r.Status = checkFail
		r.Detail = "token rejected (401)"
		r.Fix = "verify IRONCLAW_API_TOKEN matches the value the daemon was started with"
	default:
		r.Status = checkWarn
		r.Detail = fmt.Sprintf("unexpected HTTP %d", resp.StatusCode)
	}
	return r
}

// checkReadiness reports the daemon's readiness probe.
func checkReadiness(addr string) checkResult {
	r := checkResult{Name: "readiness"}
	var rz struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := getJSON(addr+"/readyz", &rz); err != nil {
		r.Status = checkWarn
		r.Detail = err.Error()
		r.Fix = "could not read /readyz"
		return r
	}
	if rz.Status == "ready" {
		r.Status = checkOK
		r.Detail = "ready"
		return r
	}
	r.Status = checkWarn
	r.Detail = "not ready: " + rz.Reason
	r.Fix = "wait for dependencies to come up; if it persists, check the daemon logs"
	return r
}

// checkRuntime verifies the OCI sandbox runtime (gVisor's runsc by default) is
// installed and on PATH. Presence is the load-bearing check; a version probe is
// best-effort.
func checkRuntime(bin string) checkResult {
	r := checkResult{Name: "sandbox runtime (" + bin + ")"}
	path, err := exec.LookPath(bin)
	if err != nil {
		// gVisor's runsc is Linux-only. On macOS/Windows the production sandbox
		// runs on the Linux control-plane host, so a missing runtime here is a
		// dev-environment note (WARN), not a hard FAIL that breaks `doctor`'s
		// exit code on a platform where it was never expected.
		if runtime.GOOS != "linux" {
			r.Status = checkWarn
			r.Detail = bin + " not found (gVisor is Linux-only; not expected on " + runtime.GOOS + ")"
			r.Fix = "the production sandbox runs on the Linux control-plane host — install gVisor there; this check is informational on " + runtime.GOOS
			return r
		}
		r.Status = checkFail
		r.Detail = bin + " not found on PATH"
		r.Fix = "install gVisor (https://gvisor.dev/docs/user_guide/install/) so sandboxes can launch, or pass --runtime <bin>"
		return r
	}
	r.Status = checkOK
	r.Detail = "found at " + path
	if v, ok := tryVersion(bin); ok {
		r.Detail += " (" + v + ")"
	}
	return r
}

// checkModelProxy verifies the model-proxy unix socket exists and accepts a
// connection. The sandbox has network=none, so this socket is its only egress
// path to the model host.
func checkModelProxy(socket string) checkResult {
	r := checkResult{Name: "model-proxy socket"}
	if _, err := os.Stat(socket); err != nil {
		r.Status = checkWarn
		r.Detail = "socket not present at " + socket
		r.Fix = "the daemon creates this socket on start; confirm it is running and --model-proxy-socket matches"
		return r
	}
	conn, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		r.Status = checkFail
		r.Detail = "socket present but not accepting connections: " + err.Error()
		r.Fix = "the model-proxy listener may be stuck; restart the control-plane"
		return r
	}
	conn.Close()
	r.Status = checkOK
	r.Detail = "reachable at " + socket
	return r
}

// tryVersion runs `bin --version` with a short timeout, returning the first line.
func tryVersion(bin string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return "", false
	}
	return line, true
}

func printChecks(w io.Writer, results []checkResult) {
	fmt.Fprintln(w, "ironctl doctor — diagnostics")
	for _, r := range results {
		fmt.Fprintf(w, "  [%-4s] %s: %s\n", r.Status, r.Name, r.Detail)
		if r.Status != checkOK && r.Fix != "" {
			fmt.Fprintf(w, "         fix: %s\n", r.Fix)
		}
	}
}
