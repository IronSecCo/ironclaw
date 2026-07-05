package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/version"
)

// cmdMCPServe runs IronClaw ITSELF as an MCP server so any MCP client (Claude
// Desktop, Cursor, Windsurf, Cline, ...) can route code execution through an
// ephemeral IronClaw sandbox with one config line. This is the INVERSE of the
// broker (list/add/remove/probe/grant), which makes IronClaw a CLIENT of external
// servers; here IronClaw is the server exposing a `sandbox_exec` tool.
//
//	ironctl mcp serve                 # stdio (what MCP clients spawn)
//	ironctl mcp serve --http :9000    # streamable HTTP transport
//	ironctl mcp serve --image IMG --timeout 30 --docker /path/to/docker
//
// The server holds no credentials and makes no model calls: `sandbox_exec` shells
// a HARDENED `docker run` (network=none, drop-all-caps, non-root, read-only rootfs,
// no-new-privs, pids/mem/cpu caps) into an ephemeral ic-sbx-mcp-* box, captures
// stdout/stderr + exit code, and returns them with an explicit containment status.
func cmdMCPServe(args []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	httpAddr := fs.String("http", "", "serve over streamable HTTP on this address (e.g. :9000) instead of stdio")
	image := fs.String("image", defaultSandboxImage, "container image for the ephemeral sandbox box")
	dockerBin := fs.String("docker", "docker", "container runtime binary (docker/podman) used to launch the box")
	timeout := fs.Int("timeout", 30, "default per-exec timeout in seconds (bounds a runaway command)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := sandboxExecConfig{
		image:      *image,
		dockerBin:  *dockerBin,
		timeoutSec: *timeout,
		run:        dockerExecRunner,
	}
	srv := newSandboxMCPServer(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *httpAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/", srv.Handler())
		httpServer := &http.Server{Addr: *httpAddr, Handler: mux}
		go func() {
			<-ctx.Done()
			_ = httpServer.Close()
		}()
		fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over HTTP at %s (image=%s)\n", *httpAddr, *image)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	// stdio is the default: MCP clients spawn `ironctl mcp serve` and speak
	// newline-delimited JSON-RPC over the child's stdin/stdout. Diagnostics go to
	// stderr so they never corrupt the protocol stream.
	fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over stdio (image=%s)\n", *image)
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
	image      string
	dockerBin  string
	timeoutSec int
	run        execRunner
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
			"containment status. The box has NO network (egress is impossible), drops all " +
			"Linux capabilities, runs as a non-root user with a read-only root filesystem and " +
			"no-new-privileges, and is bounded by cpu/memory/pids caps. It is torn down after " +
			"the command completes. Use this to execute untrusted or model-generated code safely.",
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
		timeoutSec := cfg.timeoutSec
		if in.Timeout > 0 {
			timeoutSec = in.Timeout
		}

		// Deterministic, unique box name per call from the RPC deadline-free clock.
		// We derive it from the runner start so the ephemeral box is always ic-sbx-mcp-*.
		boxName := "ic-sbx-mcp-" + sandboxBoxSuffix(ctx)
		argv := hardenedDockerArgs(boxName, image, in.Command)

		runCtx := ctx
		if timeoutSec > 0 {
			var cancel context.CancelFunc
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
			defer cancel()
		}

		stdout, stderr, exitCode, err := cfg.run(runCtx, cfg.dockerBin, argv)
		if err != nil {
			// A launch failure (docker missing, image pull denied, timeout) is a tool
			// error, not a protocol error: report it as text with IsError so the client
			// surfaces it to the user/model rather than tearing down the session.
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: launch failed: %v\n(runtime=%s image=%s)\ncontainment: box never gained network or host access.",
					err, cfg.dockerBin, image)}},
				IsError: true,
			}, nil
		}

		result := formatExecResult(image, exitCode, stdout, stderr)
		return mcp.TextResult(result), nil
	}
}

// hardenedDockerArgs assembles the argv for an ephemeral, hardened one-shot box. It
// mirrors the DockerIsolator posture (internal/host/isolation): network=none,
// drop-all-caps, non-root, read-only rootfs, no-new-privs, and cpu/memory/pids caps.
// The command runs via `sh -c` so snippets and pipelines work. Keep this list in
// sync with the containment guarantees documented in the tool description.
func hardenedDockerArgs(boxName, image, command string) []string {
	return []string{
		"run", "--rm",
		"--name", boxName,
		"--network", "none", // no NIC: egress is structurally impossible
		"--cap-drop", "ALL", // drop every Linux capability
		"--security-opt", "no-new-privileges", // suid binaries cannot escalate
		"--read-only",             // rootfs is read-only
		"--user", "65532:65532",   // non-root (nonroot uid, matches sandbox default)
		"--pids-limit", "256",     // cap process/thread count
		"--memory", "512m",        // cgroup memory cap
		"--cpus", "1",             // one vCPU of bandwidth
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m", // writable scratch only
		"--workdir", "/tmp",
		image,
		"sh", "-c", command,
	}
}

// formatExecResult renders the tool's text block: stdout, stderr, exit code, and an
// explicit containment summary so the caller always sees the enforced controls.
func formatExecResult(image string, exitCode int, stdout, stderr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", exitCode)
	b.WriteString("containment: network=none, caps=drop-all, user=65532 (non-root), rootfs=read-only, no-new-privileges, pids<=256, mem<=512m, cpu<=1 (ephemeral ic-sbx-mcp-* box, torn down)\n")
	fmt.Fprintf(&b, "image: %s\n", image)
	b.WriteString("--- stdout ---\n")
	b.WriteString(strings.TrimRight(stdout, "\n"))
	b.WriteString("\n--- stderr ---\n")
	b.WriteString(strings.TrimRight(stderr, "\n"))
	return b.String()
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
