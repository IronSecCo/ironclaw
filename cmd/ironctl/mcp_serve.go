package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/host/sandboxexec"
	"github.com/IronSecCo/ironclaw/internal/version"
)

// cmdMCPServe runs IronClaw ITSELF as an MCP server so any MCP client (Claude
// Desktop, Cursor, Windsurf, Cline, ...) can route code execution through an
// ephemeral IronClaw sandbox with one config line. This is the INVERSE of the
// broker (list/add/remove/probe/grant), which makes IronClaw a CLIENT of external
// servers; here IronClaw is the server exposing a `sandbox_exec` tool.
//
//	ironctl mcp serve                      # stdio, standalone: shells `docker run` on THIS host
//	ironctl mcp serve --http :9000         # HTTP transport, loopback-only by default
//	ironctl mcp serve --http 0.0.0.0:9000 --auth-token $TOK   # world-bound requires auth
//	ironctl mcp serve --controlplane http://cp:8787 --token-env IRONCLAW_API_TOKEN
//	                                       # THIN-CLIENT mode: delegate the box to a running
//	                                       # control-plane over its API; this process holds NO
//	                                       # host privilege (no docker.sock, no runsc in-image).
//
// Two backends:
//   - default (standalone): `sandbox_exec` shells a HARDENED `docker run` under gVisor
//     (runsc) with network=none, drop-all-caps, non-root, read-only rootfs,
//     no-new-privileges, a restrictive seccomp profile, and pids/mem/cpu caps. For
//     native/host use where this process may hold the runtime.
//   - --controlplane (thin client): `sandbox_exec` POSTs the command to a running
//     IronClaw control-plane, which owns the hardened gVisor spawning. Used by the
//     slim, socket-free ghcr.io/ironsecco/ironclaw-mcp image so the MCP container
//     holds no host privilege. Fail-closed: if the control-plane is unreachable the
//     tool errors; it NEVER falls back to host docker.
func cmdMCPServe(args []string) error {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	httpAddr := fs.String("http", "", "serve over streamable HTTP on this address (e.g. :9000); host defaults to loopback (127.0.0.1)")
	image := fs.String("image", sandboxexec.DefaultImage, "container image for the ephemeral sandbox box")
	dockerBin := fs.String("docker", "docker", "container runtime binary (docker/podman) used to launch the box (standalone backend only)")
	runtime := fs.String("runtime", isolation.DefaultRuntimeBinary, "OCI runtime for the box; default is gVisor (runsc). Any other value is an explicit, LABELLED fallback that does NOT provide gVisor's syscall interception (standalone backend only)")
	authToken := fs.String("auth-token", os.Getenv("IRONCLAW_MCP_AUTH_TOKEN"), "bearer token required for HTTP clients; REQUIRED for any non-loopback --http bind (env: IRONCLAW_MCP_AUTH_TOKEN)")
	timeout := fs.Int("timeout", 30, "default per-exec timeout in seconds (bounds a runaway command)")
	controlplane := fs.String("controlplane", os.Getenv("IRONCLAW_CONTROLPLANE_URL"), "delegate sandbox_exec to a running IronClaw control-plane at this URL (thin-client mode; no host privilege in this process). Env: IRONCLAW_CONTROLPLANE_URL")
	tokenEnv := fs.String("token-env", "IRONCLAW_API_TOKEN", "name of the env var holding the control-plane API bearer token (--controlplane mode)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Select the sandbox_exec backend. --controlplane wins and never touches host
	// docker/runsc; otherwise the standalone docker backend runs the box locally.
	var backend sandboxBackend
	if strings.TrimSpace(*controlplane) != "" {
		backend = &controlplaneBackend{
			baseURL:    strings.TrimRight(*controlplane, "/"),
			token:      os.Getenv(*tokenEnv),
			image:      *image,
			timeoutSec: *timeout,
			client:     &http.Client{},
		}
		fmt.Fprintf(os.Stderr, "ironctl mcp serve: thin-client backend -> control-plane %s (this process holds no host privilege)\n", *controlplane)
	} else {
		// Materialize IronClaw's restrictive default seccomp profile to a temp file so
		// the container runtime can enforce it (--security-opt seccomp=<file>). Matches
		// the DockerIsolator/OCI path; without it a box would get only the runtime's
		// default profile.
		seccompPath, cleanup, err := sandboxexec.WriteSeccompProfile()
		if err != nil {
			return fmt.Errorf("mcp serve: could not stage seccomp profile: %w", err)
		}
		defer cleanup()
		backend = sandboxExecConfig{
			image:       *image,
			dockerBin:   *dockerBin,
			runtime:     *runtime,
			seccompPath: seccompPath,
			timeoutSec:  *timeout,
			run:         sandboxexec.DockerRunner,
		}
	}
	srv := newSandboxMCPServer(backend)

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
		fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over HTTP at %s (%s, %s)\n", addr, authNote, srv.backendNote)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	// stdio is the default: MCP clients spawn `ironctl mcp serve` and speak
	// newline-delimited JSON-RPC over the child's stdin/stdout. Diagnostics go to
	// stderr so they never corrupt the protocol stream.
	fmt.Fprintf(os.Stderr, "ironctl mcp serve: MCP over stdio (%s)\n", srv.backendNote)
	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// sandboxBackend is the seam between the MCP surface and the sandbox_exec
// implementation: the standalone docker backend (sandboxExecConfig) and the
// thin-client control-plane backend (controlplaneBackend) both satisfy it.
type sandboxBackend interface {
	toolFunc() mcp.ToolFunc
	// note is a short human-readable description of the backend for the startup log.
	note() string
}

// sandboxServer wraps mcp.Server with the backend note for the startup log.
type sandboxServer struct {
	*mcp.Server
	backendNote string
}

// sandboxExecConfig is the standalone docker backend for the sandbox_exec tool. The
// run field is a seam: production uses sandboxexec.DockerRunner; tests substitute a
// fake so the hardened arg construction and result formatting are verified without
// Docker. It delegates all containment-critical logic to internal/host/sandboxexec
// (the single source of truth), so the standalone and control-plane backends cannot
// drift apart.
type sandboxExecConfig struct {
	image       string
	dockerBin   string
	runtime     string // OCI runtime: "runsc" (gVisor) by default; anything else is a labelled fallback
	seccompPath string // path to the seccomp profile JSON; empty omits the flag (tests)
	timeoutSec  int
	run         execRunner
}

// execRunner is the exec seam type, aliased to the shared Runner so tests and callers
// can pass a plain func literal.
type execRunner = sandboxexec.Runner

// shared projects the backend config onto the shared containment config.
func (cfg sandboxExecConfig) shared() sandboxexec.Config {
	return sandboxexec.Config{
		Image:       cfg.image,
		DockerBin:   cfg.dockerBin,
		Runtime:     cfg.runtime,
		SeccompPath: cfg.seccompPath,
		TimeoutSec:  cfg.timeoutSec,
		Run:         cfg.run,
	}
}

func (cfg sandboxExecConfig) note() string {
	return fmt.Sprintf("standalone docker backend, runtime=%s image=%s", cfg.runtime, cfg.image)
}

// newSandboxMCPServer builds the MCP server exposing the sandbox_exec tool, bound to
// the given backend.
func newSandboxMCPServer(backend sandboxBackend) *sandboxServer {
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
	}, backend.toolFunc())
	return &sandboxServer{Server: s, backendNote: backend.note()}
}

// toolFunc returns the standalone docker sandbox_exec handler bound to this config.
func (cfg sandboxExecConfig) toolFunc() mcp.ToolFunc {
	sc := cfg.shared()
	return func(ctx context.Context, args json.RawMessage) (mcp.ToolResult, error) {
		var in struct {
			Command string `json:"command"`
			Image   string `json:"image"`
			Timeout int    `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: invalid arguments: %w", err)
		}
		res, err := sc.Exec(ctx, in.Command, in.Image, in.Timeout, sandboxBoxSuffix(ctx))
		if err != nil {
			// Invalid input (empty command, hostile image ref): a protocol-level error.
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: %w", err)
		}
		if res.LaunchErr != "" {
			// A launch failure (docker missing, image pull denied, runsc absent,
			// timeout) is a tool error, not a protocol error: report it as text with
			// IsError so the client surfaces it to the user/model rather than tearing
			// down the session.
			hint := ""
			if res.GVisor {
				hint = fmt.Sprintf("\n(if the runtime %q is not registered with your engine, install gVisor/runsc "+
					"or re-run with an explicit --runtime fallback; the fallback is NOT gVisor-equivalent)", cfg.runtime)
			}
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: launch failed: %s\n(runtime=%s engine=%s image=%s)%s\ncontainment: box never gained network or host access.",
					res.LaunchErr, cfg.runtime, cfg.dockerBin, res.Image, hint)}},
				IsError: true,
			}, nil
		}
		return mcp.TextResult(sandboxexec.FormatText(res)), nil
	}
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

// sandboxBoxSuffix derives a per-call box name suffix. It avoids Math/rand and uses
// the process pid plus a monotonic counter carried on the context when present.
func sandboxBoxSuffix(ctx context.Context) string {
	if v, ok := ctx.Value(boxSuffixKey{}).(string); ok && v != "" {
		return v
	}
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

type boxSuffixKey struct{}
