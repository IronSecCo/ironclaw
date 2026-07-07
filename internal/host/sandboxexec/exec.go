// Package sandboxexec is the single source of truth for IronClaw's ephemeral,
// hardened one-shot code-exec box. Both `ironctl mcp serve` (standalone/native
// docker backend) and the control-plane's POST /v1/sandbox/exec endpoint build the
// SAME `docker run` argv here, so the containment posture (gVisor/runsc,
// network=none, drop-all-caps, non-root, read-only rootfs, no-new-privileges,
// restrictive seccomp, pids/mem/cpu caps) cannot drift between the two entrypoints.
//
// The box is torn down after every call (--rm). Nothing in this package holds
// credentials or makes model calls: it is pure containment plumbing.
package sandboxexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/isolation"
)

// DefaultImage is the image a box runs when the caller does not override it. It is a
// minimal, credential-free base; the containment guarantees come from the runtime
// flags, not the image.
const DefaultImage = "docker.io/library/alpine:3.20"

// BoxNamePrefix is the deterministic prefix for every ephemeral exec box, so the
// sweep/leak checks (and operators) can always identify and reap them.
const BoxNamePrefix = "ic-sbx-mcp-"

// Runner launches a fully-formed argv and returns the combined stdout, stderr, exit
// code, and any launch error (as opposed to a non-zero exit from the command). It is
// an injectable seam: production uses DockerRunner; tests substitute a fake so the
// hardened arg construction and result handling are verified without Docker.
type Runner func(ctx context.Context, bin string, argv []string) (stdout, stderr string, exitCode int, err error)

// Config is the injectable configuration for a hardened one-shot exec.
type Config struct {
	Image       string // default image when the call does not override it
	DockerBin   string // container runtime binary (docker/podman)
	Runtime     string // OCI runtime: "runsc" (gVisor) by default; anything else is a labelled fallback
	SeccompPath string // path to the seccomp profile JSON; empty omits the flag (tests)
	TimeoutSec  int    // default per-exec timeout in seconds; <=0 means no bound
	Run         Runner // exec seam
}

// Result is the structured outcome of a hardened exec. LaunchErr is non-empty only
// when the box FAILED TO START (docker missing, image pull denied, runsc absent,
// timeout): callers must treat that as a fail-closed tool error and never fall back
// to an unhardened path. A non-zero ExitCode with an empty LaunchErr is a normal
// command result, not a containment failure.
type Result struct {
	Stdout      string
	Stderr      string
	ExitCode    int
	Image       string
	Containment string
	GVisor      bool
	LaunchErr   string
}

// UsesGVisor reports whether the configured runtime is gVisor (runsc), the only
// runtime that gives syscall-interception containment. Anything else runs under the
// shared host kernel and must be labelled a fallback, not gVisor.
func (c Config) UsesGVisor() bool {
	return c.Runtime == isolation.DefaultRuntimeBinary
}

// ValidateImageRef rejects an image reference that docker would parse as a flag. A
// leading '-' (e.g. "--volume=/:/host") is the injection vector; everything else is
// left to docker's own reference parsing (the "--" separator handles the rest).
func ValidateImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return errors.New("image is required")
	}
	if strings.HasPrefix(image, "-") {
		return fmt.Errorf("invalid image %q: must not start with '-' (would be parsed as a docker flag)", image)
	}
	return nil
}

// HardenedArgs assembles the argv for an ephemeral, hardened one-shot box. It mirrors
// the DockerIsolator/OCI posture (internal/host/isolation): gVisor (runsc) runtime,
// network=none, drop-all-caps, non-root, read-only rootfs, no-new-privs, a
// restrictive seccomp profile, and cpu/memory/pids caps. The command runs via `sh -c`
// so snippets and pipelines work. A "--" end-of-options separator precedes the image
// so a hostile image reference cannot be parsed as a flag. Keep this list in sync
// with the containment guarantees documented in the tool description.
func HardenedArgs(c Config, boxName, image, command string) []string {
	argv := []string{
		"run", "--rm",
		"--name", boxName,
		"--runtime", c.Runtime, // gVisor (runsc) by default: syscall interception, guest kernel
		"--network", "none", // no NIC: egress is structurally impossible
		"--cap-drop", "ALL", // drop every Linux capability
		"--security-opt", "no-new-privileges", // suid binaries cannot escalate
	}
	if c.SeccompPath != "" {
		// Restrictive deny-by-default seccomp profile (matches the OCI path).
		argv = append(argv, "--security-opt", "seccomp="+c.SeccompPath)
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

// ContainmentSummary describes the enforced controls, honestly labelling the
// isolation boundary. Under gVisor (runsc) it advertises syscall interception; under
// any other runtime it states plainly that the boundary is the shared host kernel and
// is NOT gVisor-equivalent, so a caller is never misled about the guarantee.
func ContainmentSummary(c Config) string {
	boundary := fmt.Sprintf("runtime=%s (FALLBACK: shared host kernel, NOT gVisor)", c.Runtime)
	if c.UsesGVisor() {
		boundary = "runtime=runsc (gVisor: user-space guest kernel, syscall interception)"
	}
	seccomp := "seccomp=restrictive-default"
	if c.SeccompPath == "" {
		seccomp = "seccomp=engine-default"
	}
	return boundary + ", network=none, caps=drop-all, user=65532 (non-root), rootfs=read-only, " +
		"no-new-privileges, " + seccomp + ", pids<=256, mem<=512m, cpu<=1 (ephemeral " + BoxNamePrefix + "* box, torn down)"
}

// Exec runs command in an ephemeral hardened box and returns a structured Result. A
// returned error is reserved for INVALID INPUT (empty command, hostile image ref);
// a box that fails to start is reported via Result.LaunchErr (fail-closed), never as
// a silent fallback. image overrides c.Image when non-empty; timeoutSec overrides
// c.TimeoutSec when > 0. boxSuffix makes the box name unique per call.
func (c Config) Exec(ctx context.Context, command, image string, timeoutSec int, boxSuffix string) (Result, error) {
	if strings.TrimSpace(command) == "" {
		return Result{}, errors.New("command is required")
	}
	if strings.TrimSpace(image) == "" {
		image = c.Image
	}
	if err := ValidateImageRef(image); err != nil {
		return Result{}, err
	}
	if timeoutSec <= 0 {
		timeoutSec = c.TimeoutSec
	}

	boxName := BoxNamePrefix + boxSuffix
	argv := HardenedArgs(c, boxName, image, command)

	runCtx := ctx
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	res := Result{Image: image, Containment: ContainmentSummary(c), GVisor: c.UsesGVisor()}
	stdout, stderr, exitCode, err := c.Run(runCtx, c.DockerBin, argv)
	res.Stdout, res.Stderr, res.ExitCode = stdout, stderr, exitCode
	if err != nil {
		res.ExitCode = -1
		res.LaunchErr = err.Error()
	}
	return res, nil
}

// DockerRunner is the production Runner: it launches the runtime binary, captures
// stdout/stderr separately, and normalizes the exit code. A non-zero exit from the
// sandboxed command is a normal result (err cleared); only a launch failure returns
// a non-nil error.
func DockerRunner(ctx context.Context, bin string, argv []string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, bin, argv...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return outBuf.String(), errBuf.String(), ee.ExitCode(), nil
		}
		return outBuf.String(), errBuf.String(), -1, err
	}
	return outBuf.String(), errBuf.String(), 0, nil
}

// WriteSeccompProfile serializes IronClaw's restrictive default seccomp profile to a
// temp file the container runtime can read via --security-opt seccomp=<file>, and
// returns the path plus a cleanup func to remove it on shutdown.
func WriteSeccompProfile() (path string, cleanup func(), err error) {
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

// FormatText renders a Result as the human/model-facing text block used by the MCP
// sandbox_exec tool: exit code, containment summary, image, then stdout/stderr. It is
// shared by the docker and control-plane backends so their output is identical.
func FormatText(res Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", res.ExitCode)
	b.WriteString("containment: " + res.Containment + "\n")
	fmt.Fprintf(&b, "image: %s\n", res.Image)
	b.WriteString("--- stdout ---\n")
	b.WriteString(strings.TrimRight(res.Stdout, "\n"))
	b.WriteString("\n--- stderr ---\n")
	b.WriteString(strings.TrimRight(res.Stderr, "\n"))
	return b.String()
}
