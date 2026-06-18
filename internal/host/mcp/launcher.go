package mcp

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Launcher builds the exec.Cmd that runs a LOCAL (stdio) MCP server. A third-party
// MCP server is untrusted code, so production wraps it in a hardened container
// (ContainerLauncher) rather than running it as a bare host process — the same
// "isolate untrusted code" posture the agent sandbox gets. The resolved env (secrets
// already expanded host-side) is delivered to the process WITHOUT appearing in the
// argv, so a credential never shows up in the host process list.
type Launcher interface {
	command(ctx context.Context, cfg ServerConfig, env map[string]string) (*exec.Cmd, error)
	// describe returns a short label for logs (e.g. "gvisor (runsc)").
	describe() string
}

// DirectLauncher runs the server as a plain host subprocess — UNISOLATED. It is the
// dev/test fallback and is appropriate only for a first-party server the operator
// fully trusts; the daemon logs a warning when it is used. Production should use
// ContainerLauncher.
type DirectLauncher struct{}

func (DirectLauncher) command(ctx context.Context, cfg ServerConfig, env map[string]string) (*exec.Cmd, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp: stdio server %q has no command", cfg.Name)
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.Env = stdioEnv(env)
	return cmd, nil
}

func (DirectLauncher) describe() string { return "direct (host process — UNISOLATED)" }

// ContainerLauncher runs each local MCP server inside a hardened container with the
// command's stdio attached (`<runtime> run --rm -i ... <image> <command> <args>`):
// network=none (a local server needs no network — a server that does is modeled as a
// REMOTE server the host dials over TLS), read-only rootfs, all caps dropped,
// no-new-privileges, non-root, and memory/pids caps. Set OCIRuntime to "runsc" to run
// under gVisor. Secrets are forwarded by NAME (-e KEY) with the value placed only in
// the launcher's own environment, so they never appear in the container argv / `ps`.
type ContainerLauncher struct {
	Runtime      string   // container CLI (default "docker")
	OCIRuntime   string   // optional --runtime (e.g. "runsc" for gVisor); empty = the CLI default
	DefaultImage string   // image used when a server config sets no Image
	User         string   // --user (default "65532:65532")
	MemoryLimit  string   // --memory (default "512m")
	PidsLimit    int      // --pids-limit (default 256)
	ExtraArgs    []string // additional run args appended before the image (operator escape hatch)
}

func (l ContainerLauncher) command(ctx context.Context, cfg ServerConfig, env map[string]string) (*exec.Cmd, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp: stdio server %q has no command", cfg.Name)
	}
	image := cfg.Image
	if image == "" {
		image = l.DefaultImage
	}
	if image == "" {
		return nil, fmt.Errorf("mcp: container isolation needs an image for server %q (set the server's image or the daemon's --mcp-image)", cfg.Name)
	}
	runtime := l.Runtime
	if runtime == "" {
		runtime = "docker"
	}
	user := l.User
	if user == "" {
		user = "65532:65532"
	}
	mem := l.MemoryLimit
	if mem == "" {
		mem = "512m"
	}
	pids := l.PidsLimit
	if pids <= 0 {
		pids = 256
	}

	args := []string{
		"run", "--rm", "-i",
		"--network", "none", // a local MCP server gets no network at all
		"--read-only",       // immutable rootfs
		"--cap-drop", "ALL", // no Linux capabilities
		"--security-opt", "no-new-privileges", // suid binaries cannot escalate
		"--user", user, // non-root
		"--memory", mem,
		"--pids-limit", strconv.Itoa(pids),
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=64m", // a small writable scratch
	}
	if l.OCIRuntime != "" {
		args = append(args, "--runtime", l.OCIRuntime)
	}
	// Forward env by NAME so secret VALUES stay out of the argv (and `ps`); the value
	// is placed only in the launcher process's own environment below.
	cmdEnv := stdioEnv(nil)
	for k, v := range env {
		args = append(args, "-e", k)
		cmdEnv = append(cmdEnv, k+"="+v)
	}
	args = append(args, l.ExtraArgs...)
	args = append(args, image, cfg.Command)
	args = append(args, cfg.Args...)

	cmd := exec.CommandContext(ctx, runtime, args...)
	cmd.Env = cmdEnv
	return cmd, nil
}

func (l ContainerLauncher) describe() string {
	rt := l.Runtime
	if rt == "" {
		rt = "docker"
	}
	if l.OCIRuntime != "" {
		return fmt.Sprintf("%s (%s, isolated)", rt, l.OCIRuntime)
	}
	return rt + " (isolated)"
}
