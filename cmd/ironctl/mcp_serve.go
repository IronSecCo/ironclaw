package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/version"
)

// cmdMCPServe runs IronClaw ITSELF as an MCP server so any MCP client (Claude
// Desktop, Cursor, Windsurf, Cline, ...) can route code execution through an
// ephemeral IronClaw sandbox with one config line. This is the INVERSE of the
// broker (list/add/remove/probe/grant), which makes IronClaw a CLIENT of external
// servers; here IronClaw is the server exposing a `sandbox_exec` tool.
//
//	ironctl mcp serve                      # stdio (what MCP clients spawn)
//	ironctl mcp serve --http :9000         # HTTP transport, loopback-only by default
//	ironctl mcp serve --http 0.0.0.0:9000 --auth-token $TOK   # world-bound requires auth
//	ironctl mcp serve --image IMG --timeout 30 --docker /path/to/docker
//	ironctl mcp serve --runtime runc       # explicit, LABELLED runc fallback (NOT gVisor)
//
// The server holds no credentials and makes no model calls: `sandbox_exec` shells
// a HARDENED `docker run` under gVisor (runsc) with network=none, drop-all-caps,
// non-root, read-only rootfs, no-new-privileges, a restrictive seccomp profile, and
// pids/mem/cpu caps into an ephemeral ic-sbx-mcp-* box, captures stdout/stderr +
// exit code, and returns them with an explicit containment status.
func cmdMCPServe(args []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	httpAddr := fs.String("http", "", "serve over streamable HTTP on this address (e.g. :9000); host defaults to loopback (127.0.0.1)")
	image := fs.String("image", defaultSandboxImage, "container image for the ephemeral sandbox box")
	dockerBin := fs.String("docker", "docker", "container runtime binary (docker/podman) used to launch the box")
	runtime := fs.String("runtime", isolation.DefaultRuntimeBinary, "OCI runtime for the box; default is gVisor (runsc). Any other value is an explicit, LABELLED fallback that does NOT provide gVisor's syscall interception")
	authToken := fs.String("auth-token", os.Getenv("IRONCLAW_MCP_AUTH_TOKEN"), "bearer token required for HTTP clients; REQUIRED for any non-loopback --http bind (env: IRONCLAW_MCP_AUTH_TOKEN)")
	timeout := fs.Int("timeout", 30, "default per-exec timeout in seconds (bounds a runaway command)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Materialize IronClaw's restrictive default seccomp profile to a temp file so
	// the container runtime can enforce it (--security-opt seccomp=<file>). Matches
	// the DockerIsolator/OCI path (internal/host/isolation/seccomp.go); without it a
	// box would get only the runtime's default profile.
	seccompPath, cleanup, err := writeSeccompProfile()
	if err != nil {
		return fmt.Errorf("mcp serve: could not stage seccomp profile: %w", err)
	}
	defer cleanup()

	cfg := sandboxExecConfig{
		image:       *image,
		dockerBin:   *dockerBin,
		runtime:     *runtime,
		seccompPath: seccompPath,
		timeoutSec:  *timeout,
		run:         dockerExecRunner,
	}
	srv := newSandboxMCPServer(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		addr, loopback, err := normalizeHTTPAddr(*httpAddr)
		if err != nil {
			return fmt.Errorf("mcp serve: %w", err)
		}
		// Fail closed: never expose arbitrary sandbox_exec to the network without
		// authentication. A non-loopback bind is only allowed with a bearer token.
		if !loopback && *authToken == "" {
			return fmt.Errorf("mcp serve: refusing to bind %s without authentication: "+
				"set --auth-token (or IRONCLAW_MCP_AUTH_TOKEN), or bind a loopback address", addr)
		}
		var handler http.Handler = srv.Handler()
		if *authToken != "" {
			handler = bearerAuth(*authToken, handler)
		}
		mux := http.NewServeMux()
		mux.Handle("/", handler)
		httpServer := &http.Server{Addr: addr, Handler: mux}
		go func() {
			<-ctx.Done()
			_ = httpServer.Close()
		}()
		authNote := "no auth (loopback)"
		if *authToken != "" {
			authNote = "bearer-auth required"
		}
		fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over HTTP at %s (%s, runtime=%s image=%s)\n", addr, authNote, *runtime, *image)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	// stdio is the default: MCP clients spawn `ironctl mcp serve` and speak
	// newline-delimited JSON-RPC over the child's stdin/stdout. Diagnostics go to
	// stderr so they never corrupt the protocol stream.
	fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over stdio (runtime=%s image=%s)\n", *runtime, *image)
	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// defaultSandboxImage is the image a sandbox box runs when the caller does not
// override it. It is a minimal, credential-free base; the containment guarantees
// come from the runtime flags, not the image.
const defaultSandboxImage = "docker.io/library/alpine:3.20"

// sandboxExecConfig is the injectable configuration for the sandbox_exec tool. The
// run field is a seam: production uses dockerExecRunner; tests substitute a fake so
// the hardened arg construction and result formatting are verified without Docker.
type sandboxExecConfig struct {
	image       string
	dockerBin   string
	runtime     string // OCI runtime: "runsc" (gVisor) by default; anything else is a labelled fallback
	seccompPath string // path to the seccomp profile JSON; empty omits the flag (tests)
	timeoutSec  int
	run         execRunner
}

// usesGVisor reports whether the configured runtime is gVisor (runsc), which is the
// only runtime that gives syscall-interception containment. Anything else runs under
// the shared host kernel and must be labelled as a fallback, not as gVisor.
func (cfg sandboxExecConfig) usesGVisor() bool {
	return cfg.runtime == isolation.DefaultRuntimeBinary
}

// execRunner launches a fully-formed argv and returns the combined stdout, stderr,
// exit code, and any launch error (as opposed to a non-zero exit).
type execRunner func(ctx context.Context, bin string, argv []string) (stdout, stderr string, exitCode int, err error)

// newSandboxMCPServer builds the MCP server exposing the sandbox_exec tool.
func newSandboxMCPServer(cfg sandboxExecConfig) *mcp.Server {
	s := mcp.NewServer("ironclaw", version.String())
	s.AddTool(mcp.Tool{
		Name: "sandbox_exec",
		Description: "Run a shell command (or code snippet via `sh -c`) inside an ephemeral, " +
			"hardened IronClaw sandbox box and return its stdout, stderr, exit code, and " +
			"containment status. By default the box runs under gVisor (runsc), which intercepts " +
			"syscalls in a user-space guest kernel; it has NO network (egress is impossible), drops " +
			"all Linux capabilities, runs as a non-root user with a read-only root filesystem, " +
			"no-new-privileges, a restrictive seccomp profile, and is bounded by cpu/memory/pids " +
			"caps. It is torn down after the command completes. Use this to execute untrusted or " +
			"model-generated code safely.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"command":{"type":"string","description":"Command to run inside the sandbox, executed via sh -c."},` +
			`"image":{"type":"string","description":"Optional container image override (default: the server's configured image)."},` +
			`"timeout_seconds":{"type":"integer","minimum":1,"maximum":600,"description":"Optional per-exec timeout override in seconds."}` +
			`},"required":["command"],"additionalProperties":false}`),
	}, cfg.toolFunc())
	return s
}

// toolFunc returns the sandbox_exec handler bound to this config.
func (cfg sandboxExecConfig) toolFunc() mcp.ToolFunc {
	return func(ctx context.Context, args json.RawMessage) (mcp.ToolResult, error) {
		var in struct {
			Command string `json:"command"`
			Image   string `json:"image"`
			Timeout int    `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: invalid arguments: %w", err)
		}
		if strings.TrimSpace(in.Command) == "" {
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: command is required")
		}
		image := cfg.image
		if strings.TrimSpace(in.Image) != "" {
			image = in.Image
		}
		// Argument-injection guard: the image is placed in the docker argv. A value
		// like "--volume=/:/host" or "--user=0:0" would otherwise be parsed by docker
		// as a flag and could relax the sandbox. Reject a leading '-'; the "--"
		// end-of-options separator in the argv (see hardenedDockerArgs) is the second
		// layer of defense.
		if err := validateImageRef(image); err != nil {
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: %w", err)
		}
		timeoutSec := cfg.timeoutSec
		if in.Timeout > 0 {
			timeoutSec = in.Timeout
		}

		// Deterministic, unique box name per call from the RPC deadline-free clock.
		// We derive it from the runner start so the ephemeral box is always ic-sbx-mcp-*.
		boxName := "ic-sbx-mcp-" + sandboxBoxSuffix(ctx)
		argv := hardenedDockerArgs(cfg, boxName, image, in.Command)

		runCtx := ctx
		if timeoutSec > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
		}

		stdout, stderr, exitCode, err := cfg.run(runCtx, cfg.dockerBin, argv)
		if err != nil {
			// A launch failure (docker missing, image pull denied, runsc absent,
			// timeout) is a tool error, not a protocol error: report it as text with
			// IsError so the client surfaces it to the user/model rather than tearing
			// down the session.
			hint := ""
			if cfg.usesGVisor() {
				hint = "\n(if the runtime %q is not registered with your engine, install gVisor/runsc " +
					"or re-run with an explicit --runtime fallback; the fallback is NOT gVisor-equivalent)"
				hint = fmt.Sprintf(hint, cfg.runtime)
			}
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: launch failed: %v\n(runtime=%s engine=%s image=%s)%s\ncontainment: box never gained network or host access.",
					err, cfg.runtime, cfg.dockerBin, image, hint)}},
				IsError: true,
			}, nil
		}

		result := formatExecResult(cfg, image, exitCode, stdout, stderr)
		return mcp.TextResult(result), nil
	}
}

// validateImageRef rejects an image reference that docker would parse as a flag. A
// leading '-' (e.g. "--volume=/:/host") is the injection vector; everything else is
// left to docker's own reference parsing (the "--" separator handles the rest).
func validateImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("image is required")
	}
	if strings.HasPrefix(image, "-") {
		return fmt.Errorf("invalid image %q: must not start with '-' (would be parsed as a docker flag)", image)
	}
	return nil
}

// hardenedDockerArgs assembles the argv for an ephemeral, hardened one-shot box. It
// mirrors the DockerIsolator/OCI posture (internal/host/isolation): gVisor (runsc)
// runtime, network=none, drop-all-caps, non-root, read-only rootfs, no-new-privs, a
// restrictive seccomp profile, and cpu/memory/pids caps. The command runs via `sh -c`
// so snippets and pipelines work. A "--" end-of-options separator precedes the image
// so a hostile image reference cannot be parsed as a flag. Keep this list in sync
// with the containment guarantees documented in the tool description.
func hardenedDockerArgs(cfg sandboxExecConfig, boxName, image, command string) []string {
	argv := []string{
		"run", "--rm",
		"--name", boxName,
		"--runtime", cfg.runtime, // gVisor (runsc) by default: syscall interception, guest kernel
		"--network", "none", // no NIC: egress is structurally impossible
		"--cap-drop", "ALL", // drop every Linux capability
		"--security-opt", "no-new-privileges", // suid binaries cannot escalate
	}
	if cfg.seccompPath != "" {
		// Restrictive deny-by-default seccomp profile (matches the OCI path).
		argv = append(argv, "--security-opt", "seccomp="+cfg.seccompPath)
	}
	argv = append(argv,
		"--read-only",           // rootfs is read-only
		"--user", "65532:65532", // non-root (nonroot uid, matches sandbox default)
		"--pids-limit", "256", // cap process/thread count
		"--memory", "512m", // cgroup memory cap
		"--cpus", "1", // one vCPU of bandwidth
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m", // writable scratch only
		"--workdir", "/tmp",
		"--", // end of options: nothing after this is parsed as a flag
		image,
		"sh", "-c", command,
	)
	return argv
}

// formatExecResult renders the tool's text block: stdout, stderr, exit code, and an
// explicit containment summary so the caller always sees the enforced controls and
// the true isolation boundary (gVisor vs a labelled fallback).
func formatExecResult(cfg sandboxExecConfig, image string, exitCode int, stdout, stderr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", exitCode)
	b.WriteString("containment: " + containmentSummary(cfg) + "\n")
	fmt.Fprintf(&b, "image: %s\n", image)
	b.WriteString("--- stdout ---\n")
	b.WriteString(strings.TrimRight(stdout, "\n"))
	b.WriteString("\n--- stderr ---\n")
	b.WriteString(strings.TrimRight(stderr, "\n"))
	return b.String()
}

// containmentSummary describes the enforced controls, honestly labelling the
// isolation boundary. Under gVisor (runsc) it advertises syscall interception; under
// any other runtime it states plainly that the boundary is the shared host kernel and
// is NOT gVisor-equivalent, so a client is never misled about the guarantee.
func containmentSummary(cfg sandboxExecConfig) string {
	boundary := fmt.Sprintf("runtime=%s (FALLBACK: shared host kernel, NOT gVisor)", cfg.runtime)
	if cfg.usesGVisor() {
		boundary = "runtime=runsc (gVisor: user-space guest kernel, syscall interception)"
	}
	seccomp := "seccomp=restrictive-default"
	if cfg.seccompPath == "" {
		seccomp = "seccomp=engine-default"
	}
	return boundary + ", network=none, caps=drop-all, user=65532 (non-root), rootfs=read-only, " +
		"no-new-privileges, " + seccomp + ", pids<=256, mem<=512m, cpu<=1 (ephemeral ic-sbx-mcp-* box, torn down)"
}

// writeSeccompProfile serializes IronClaw's restrictive default seccomp profile to a
// temp file the container runtime can read via --security-opt seccomp=<file>, and
// returns the path plus a cleanup func to remove it on shutdown.
func writeSeccompProfile() (path string, cleanup func(), err error) {
	data, err := json.Marshal(isolation.DefaultSeccompProfile())
	if err != nil {
		return "", func() {}, err
	}
	f, err := os.CreateTemp("", "ic-mcp-seccomp-*.json")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	if _, werr := f.Write(data); werr != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", func() {}, werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(name)
		return "", func() {}, cerr
	}
	return name, func() { _ = os.Remove(name) }, nil
}

// normalizeHTTPAddr resolves the --http bind address and reports whether it is a
// loopback bind. A bare port (":9000") or empty host defaults to 127.0.0.1 so the
// server is not world-exposed by accident; a caller must opt into a routable address
// explicitly. Returns the normalized "host:port" and whether host is loopback.
func normalizeHTTPAddr(addr string) (normalized string, loopback bool, err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false, fmt.Errorf("invalid --http address %q: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1" // default to loopback rather than all interfaces
	}
	return net.JoinHostPort(host, port), isLoopbackHost(host), nil
}

// isLoopbackHost reports whether host names the loopback interface. Literal IPs are
// checked via net.IP.IsLoopback; the "localhost" name is treated as loopback.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// bearerAuth wraps h so every request must carry "Authorization: Bearer <token>".
// The comparison is constant-time to avoid leaking the token via timing.
func bearerAuth(token string, h http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeEq(int32(len(got)), int32(len(want))) != 1 ||
			subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// sandboxBoxSuffix derives a per-call box name suffix. It avoids Math/rand and the
// wall clock (kept deterministic-friendly for tests) by using the process pid plus a
// monotonic counter carried on the context when present.
func sandboxBoxSuffix(ctx context.Context) string {
	if v, ok := ctx.Value(boxSuffixKey{}).(string); ok && v != "" {
		return v
	}
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

type boxSuffixKey struct{}

// dockerExecRunner is the production execRunner: it launches the runtime binary,
// captures stdout/stderr separately, and normalizes the exit code.
func dockerExecRunner(ctx context.Context, bin string, argv []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, bin, argv...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
			// A non-zero exit from the sandboxed command is a normal result, not a
			// launch failure: clear err so the caller reports stdout/stderr/exit.
			return outBuf.String(), errBuf.String(), exitCode, nil
		}
		return outBuf.String(), errBuf.String(), -1, err
	}
	return outBuf.String(), errBuf.String(), exitCode, nil
}
