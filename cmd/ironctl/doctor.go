package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/onboard"
)

// checkStatus is a doctor check verdict.
type checkStatus string

const (
	checkOK   checkStatus = "OK"
	checkWarn checkStatus = "WARN"
	checkFail checkStatus = "FAIL"
)

// docBase is the published docs site; each non-OK check links straight to the
// matching Troubleshooting anchor so an operator can go from a red line to its fix.
const docBase = "https://ironsecco.github.io/ironclaw"

// checkResult is one diagnostic line: what was checked, the verdict, what was
// seen, and (when not OK) an actionable fix plus a docs link.
type checkResult struct {
	Name   string
	Status checkStatus
	Detail string
	Fix    string
	Doc    string
}

// cmdDoctor implements `ironctl doctor` — environment + reachability checks with
// actionable fixes. It is read-only and never prints secret values (presence
// only). It exits non-zero if any check FAILs so it is usable as a health gate in
// scripts.
func cmdDoctor(addr string, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	socket := fs.String("model-proxy-socket", defaultModelProxySocket(),
		"model-proxy unix socket to probe")
	// The runtime defaults to the same resolution the control-plane uses:
	// $IRONCLAW_RUNTIME, else gVisor's runsc. An explicit --runtime wins.
	runtimeBin := fs.String("runtime", envOrDefault("IRONCLAW_RUNTIME", "runsc"),
		"OCI runtime binary to check (gVisor's runsc by default; IRONCLAW_RUNTIME selects it)")
	stateDir := fs.String("state-dir", defaultStateDir(),
		"control-plane state dir (per-session queues/keys); used by the --runtime docker file-sharing check")
	if err := fs.Parse(args); err != nil {
		return err
	}

	results := []checkResult{
		checkAPI(addr),
		checkAuth(addr),
		checkReadiness(addr),
		checkRuntime(*runtimeBin),
		checkToolchain(),
		checkModelCredential(os.Getenv),
		checkChannels(os.Getenv),
		checkConfig(),
		checkModelProxy(*socket),
	}
	// The Docker fallback runtime (macOS dev) carries the per-session queues/keys
	// into each sandbox via a bind mount; if the state dir is outside Docker
	// Desktop's shared paths the bind mounts empty and the sandbox exits 1 on its
	// session-key read (IRO-171). Only relevant when the Docker runtime is selected.
	if strings.Contains(*runtimeBin, "docker") {
		results = append(results, checkDockerFileSharing(*stateDir))
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

// envOrDefault returns the env var if set and non-empty, else def.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// checkAPI verifies the control-plane is reachable via its unauthenticated
// liveness probe.
func checkAPI(addr string) checkResult {
	r := checkResult{Name: "control-plane API", Doc: docBase + "/troubleshooting/#daemon-unreachable-connection-refused"}
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
	r := checkResult{Name: "API auth / token", Doc: docBase + "/troubleshooting/#api-token-missing-or-rejected-401"}
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
	r := checkResult{Name: "readiness", Doc: docBase + "/troubleshooting/#daemon-unreachable-connection-refused"}
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
// best-effort. A relaxed runtime (docker/podman/runc) is reported OK but flagged,
// because it does not provide gVisor's syscall-interception isolation boundary.
func checkRuntime(bin string) checkResult {
	r := checkResult{Name: "sandbox runtime (" + bin + ")", Doc: docBase + "/troubleshooting/#sandbox-runtime-runsc-not-found-gvisor"}
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
		r.Fix = "install gVisor (https://gvisor.dev/docs/user_guide/install/) so sandboxes can launch, or set IRONCLAW_RUNTIME / pass --runtime <bin>"
		return r
	}
	r.Detail = "found at " + path
	if v, ok := tryVersion(bin); ok {
		r.Detail += " (" + v + ")"
	}
	// runsc is the hardened default; anything else is the relaxed fallback.
	if !strings.Contains(bin, "runsc") {
		r.Status = checkWarn
		r.Detail += " — relaxed runtime (no gVisor syscall isolation)"
		r.Fix = "this is the runc fallback for hosts without gVisor; use runsc for the full isolation boundary in production"
		return r
	}
	r.Status = checkOK
	return r
}

// checkToolchain reports this binary's Go runtime version and restates the
// control-plane's build expectation: encrypted (SQLCipher) queues require
// CGO_ENABLED=1 and a C toolchain. ironctl itself is a pure HTTP client and needs
// no cgo, so this is informational guidance rather than a self-test.
func checkToolchain() checkResult {
	return checkResult{
		Name:   "build toolchain",
		Status: checkOK,
		Detail: runtime.Version() + " — control-plane build requires CGO_ENABLED=1 (encrypted SQLite)",
		Doc:    docBase + "/troubleshooting/#build-fails-with-a-sqlite-cgo-error",
	}
}

// checkModelCredential reports whether any model-provider credential is present.
// It is provider-agnostic and mirrors exactly what `ironctl onboard` checks (the
// detector is shared). No credential is a WARN, not a FAIL: the zero-credential
// `mock` provider still serves chat, so a fresh install is not broken — it just
// can't reach a real model yet. Presence only; secret values are never read.
func checkModelCredential(getenv func(string) string) checkResult {
	r := checkResult{Name: "model credential", Doc: docBase + "/troubleshooting/#no-model-credentials-and-the-zero-credential-mock-path"}
	have := onboard.ModelCredentials(getenv)
	if len(have) == 0 {
		r.Status = checkWarn
		r.Detail = "none set — the zero-credential `mock` provider works, but no real model is reachable"
		r.Fix = "set one of ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, or IRONCLAW_MODEL_GATEWAY_URL on the daemon"
		return r
	}
	r.Status = checkOK
	r.Detail = strings.Join(have, ", ") + " configured (held host-side; never enters a sandbox)"
	return r
}

// checkChannels reports which channel adapters are armed by the environment.
// Channels are optional (the web console and API work without one), so none is a
// WARN with guidance, not a failure. Shares the onboard detector so the two never
// drift. Presence only; tokens are never read or printed.
func checkChannels(getenv func(string) string) checkResult {
	r := checkResult{Name: "channel adapters", Doc: docBase + "/troubleshooting/#channel-adapter-not-arming-env-mismatch"}
	armed := onboard.ArmedChannels(getenv)
	if len(armed) == 0 {
		r.Status = checkWarn
		r.Detail = "no channel armed from the environment"
		r.Fix = "set e.g. SLACK_BOT_TOKEN or TELEGRAM_BOT_TOKEN, or wire one with `ironctl registry wiring ...` (channels are optional)"
		return r
	}
	r.Status = checkOK
	r.Detail = "armed from env: " + strings.Join(armed, ", ")
	return r
}

// checkConfig validates the onboarding config file (the 0600 env-file that holds
// the API token). Absence is fine — onboard creates it on demand — so it is a
// SKIP-style WARN. When present it must be readable and, on POSIX, not group/world
// readable, since it bears a secret. The token value itself is never printed.
func checkConfig() checkResult {
	path := defaultOnboardConfig()
	r := checkResult{Name: "onboard config", Doc: docBase + "/troubleshooting/#onboard-config-missing-or-too-permissive"}
	info, err := os.Stat(path)
	if err != nil {
		r.Status = checkWarn
		r.Detail = "not present at " + path
		r.Fix = "run `ironctl onboard` to mint an API token and write this file (0600)"
		return r
	}
	if info.IsDir() {
		r.Status = checkFail
		r.Detail = path + " is a directory, expected a file"
		r.Fix = "remove the directory and re-run `ironctl onboard`"
		return r
	}
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm&0o077 != 0 {
		r.Status = checkWarn
		r.Detail = fmt.Sprintf("%s is %#o — readable beyond the owner (holds a secret token)", path, perm)
		r.Fix = fmt.Sprintf("tighten it: chmod 600 %s", path)
		return r
	}
	r.Status = checkOK
	r.Detail = "present and owner-only at " + path
	return r
}

// defaultModelProxySocket mirrors the control-plane's defaultModelProxySocket
// (cmd/controlplane) so `ironctl doctor` probes the same path the daemon actually
// binds when neither side passes --model-proxy-socket. On Linux the daemon runs
// under systemd with RuntimeDirectory=ironclaw, so /run/ironclaw is its home;
// off-Linux (macOS --dev — there is no creatable /run at the SIP-protected root)
// it falls back to the user cache dir. Keeping the two in sync avoids a false WARN
// where doctor probes the Linux path while a macOS --dev daemon serves the cache
// path. Production passes --model-proxy-socket explicitly on both sides.
func defaultModelProxySocket() string {
	if runtime.GOOS == "linux" {
		return "/run/ironclaw/modelproxy.sock"
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "ironclaw", "run", "modelproxy.sock")
	}
	return filepath.Join(os.TempDir(), "ironclaw", "modelproxy.sock")
}

// checkModelProxy verifies the model-proxy unix socket exists and accepts a
// connection. The sandbox has network=none, so this socket is its only egress
// path to the model host.
func checkModelProxy(socket string) checkResult {
	r := checkResult{Name: "model-proxy socket", Doc: docBase + "/troubleshooting/#daemon-unreachable-connection-refused"}
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

// defaultStateDir mirrors the control-plane's defaultStateDir (cmd/controlplane)
// so the Docker file-sharing check probes the same dir the daemon binds into each
// sandbox. Production passes --state-dir explicitly.
func defaultStateDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "ironclaw", "state")
	}
	return filepath.Join(os.TempDir(), "ironclaw-state")
}

// checkDockerFileSharing verifies that, under the Docker fallback runtime, the host
// state dir (per-session encrypted queues + keys) lands inside Docker Desktop's
// shared paths — so the bind mount actually delivers it into each sandbox
// container. A state dir outside the shared set mounts EMPTY in the container, and
// the sandbox then exits 1 on its session-key read (the IRO-171 macOS failure). On
// Linux there is no Docker Desktop VM boundary, so bind mounts always deliver.
func checkDockerFileSharing(stateDir string) checkResult {
	shared, ok := dockerDesktopSharedDirs()
	return evalDockerFileSharing(stateDir, shared, ok, runtime.GOOS)
}

// evalDockerFileSharing is the pure decision core of checkDockerFileSharing,
// separated from the OS/settings lookup so it is exhaustively unit-testable.
func evalDockerFileSharing(stateDir string, shared []string, sharedOK bool, goos string) checkResult {
	r := checkResult{Name: "docker file sharing", Doc: docBase + "/troubleshooting/#sandbox-exits-on-startup-macos-docker-file-sharing"}
	if goos == "linux" {
		r.Status = checkOK
		r.Detail = "bind mounts deliver directly on Linux; Docker Desktop file sharing does not apply"
		return r
	}
	// Resolve symlinks on both sides so the comparison is apples-to-apples (macOS
	// /tmp -> /private/tmp, /var/folders -> /private/var/folders).
	resolved := resolveExistingPath(stateDir)
	probeHint := "verify it is delivered with: docker run --rm -v " + stateDir + ":/probe alpine ls -A /probe"
	if sharedOK {
		if pathUnderAny(resolved, resolveAll(shared)) {
			r.Status = checkOK
			r.Detail = stateDir + " is inside Docker Desktop's shared paths"
			return r
		}
		r.Status = checkWarn
		r.Detail = stateDir + " is NOT inside Docker Desktop's shared paths " + strings.Join(shared, ", ")
		r.Fix = "add it under Docker Desktop → Settings → Resources → File sharing (or move --state-dir under a shared path); otherwise the per-session queues/keys bind mounts empty and the sandbox exits 1 on its session-key read. " + probeHint
		return r
	}
	// Could not read Docker Desktop's settings — fall back to its default-shared roots.
	if pathUnderAny(resolved, resolveAll(dockerDesktopDefaultRoots(goos))) {
		r.Status = checkOK
		r.Detail = stateDir + " is under a Docker Desktop default-shared root (Docker Desktop settings unreadable, so a custom config could not be confirmed)"
		return r
	}
	r.Status = checkWarn
	r.Detail = stateDir + " may be outside Docker Desktop's shared paths (could not read Docker Desktop settings to confirm)"
	r.Fix = "ensure it is shared under Docker Desktop → Settings → Resources → File sharing; otherwise the per-session queues/keys bind mounts empty and the sandbox exits 1 on its session-key read. " + probeHint
	return r
}

// dockerDesktopSharedDirs reads the host directories Docker Desktop is configured
// to share into containers, from its settings file (macOS/Windows). ok is false
// when no settings file is found or it has no file-sharing list, so the caller can
// fall back to the default-shared roots heuristic.
func dockerDesktopSharedDirs() (dirs []string, ok bool) {
	for _, p := range dockerDesktopSettingsPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if d := parseSharedDirs(data); len(d) > 0 {
			return d, true
		}
	}
	return nil, false
}

// dockerDesktopSettingsPaths returns the candidate Docker Desktop settings files,
// newest layout first. Docker Desktop renamed settings.json to settings-store.json
// in 4.34; both are tried.
func dockerDesktopSettingsPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		base := filepath.Join(home, "Library", "Group Containers", "group.com.docker")
		return []string{
			filepath.Join(base, "settings-store.json"),
			filepath.Join(base, "settings.json"),
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return nil
		}
		base := filepath.Join(appData, "Docker")
		return []string{
			filepath.Join(base, "settings-store.json"),
			filepath.Join(base, "settings.json"),
		}
	default:
		return nil
	}
}

// parseSharedDirs extracts the filesharingDirectories list from a Docker Desktop
// settings blob. Tolerant of the surrounding schema — it only reads the one field.
func parseSharedDirs(data []byte) []string {
	var s struct {
		FilesharingDirectories []string `json:"filesharingDirectories"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return s.FilesharingDirectories
}

// dockerDesktopDefaultRoots returns the directories Docker Desktop shares by
// default when no custom file-sharing config can be read.
func dockerDesktopDefaultRoots(goos string) []string {
	if goos == "darwin" {
		return []string{"/Users", "/Volumes", "/private", "/tmp", "/var/folders"}
	}
	return nil
}

// resolveExistingPath cleans path and resolves symlinks on its longest existing
// ancestor (the state dir may not exist yet), so comparisons against shared roots
// are apples-to-apples (e.g. macOS /tmp -> /private/tmp).
func resolveExistingPath(path string) string {
	path = filepath.Clean(path)
	dir := path
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			if dir == path {
				return resolved
			}
			// Re-attach the non-existent tail to the resolved existing prefix.
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				return resolved
			}
			return filepath.Join(resolved, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path // reached the root without resolving anything
		}
		dir = parent
	}
}

// resolveAll resolves symlinks on each root (best-effort), so shared-path
// comparisons survive macOS's /tmp and /var symlinks.
func resolveAll(roots []string) []string {
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, resolveExistingPath(r))
	}
	return out
}

// pathUnderAny reports whether path equals or is nested under any of roots.
func pathUnderAny(path string, roots []string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if path == root {
			return true
		}
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func printChecks(w io.Writer, results []checkResult) {
	fmt.Fprintln(w, "ironctl doctor — diagnostics")
	for _, r := range results {
		fmt.Fprintf(w, "  [%-4s] %s: %s\n", r.Status, r.Name, r.Detail)
		if r.Status != checkOK && r.Fix != "" {
			fmt.Fprintf(w, "         fix: %s\n", r.Fix)
		}
		if r.Status != checkOK && r.Doc != "" {
			fmt.Fprintf(w, "         see: %s\n", r.Doc)
		}
	}
}
