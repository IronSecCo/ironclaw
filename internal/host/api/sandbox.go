package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/sandboxexec"
)

// WithSandboxExec enables the ephemeral hardened sandbox_exec endpoint
// (POST /v1/sandbox/exec). It lets a privilege-free MCP client (the slim
// ghcr.io/ironsecco/ironclaw-mcp image running `ironctl mcp serve --controlplane`)
// delegate one-shot code execution to THIS control-plane, which owns the hardened
// gVisor spawning. The container privilege stays here; the client holds none.
//
// A nil config (the default) leaves the endpoint disabled: it returns 501. Returns
// the Server for chaining.
func (s *Server) WithSandboxExec(cfg *sandboxexec.Config) *Server {
	s.sandbox = cfg
	return s
}

func (s *Server) sandboxRoutes() {
	s.mux.HandleFunc("POST /v1/sandbox/exec", s.handleSandboxExec)
}

// sandboxExecCounter disambiguates two box names created within the same nanosecond.
var sandboxExecCounter uint64

// sandboxExecReq is the request body for POST /v1/sandbox/exec.
type sandboxExecReq struct {
	Command string `json:"command"`
	Image   string `json:"image"`
	Timeout int    `json:"timeout_seconds"`
}

// sandboxExecResp is the structured response. LaunchError is set (and the status is
// 502) only when the box FAILED TO START — the client must treat that as fail-closed
// and never fall back. A normal non-zero ExitCode with an empty LaunchError is a
// completed command, returned 200.
type sandboxExecResp struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exit_code"`
	Image       string `json:"image"`
	Containment string `json:"containment"`
	LaunchError string `json:"launch_error,omitempty"`
}

// handleSandboxExec runs a single hardened, ephemeral box on behalf of a thin MCP
// client. It reuses internal/host/sandboxexec (the containment single-source-of-truth)
// so the box is spawned with the same posture as `ironctl mcp serve` standalone:
// gVisor (runsc), network=none, drop-all-caps, non-root, read-only rootfs,
// no-new-privileges, restrictive seccomp, and pids/mem/cpu caps. The box is torn down
// after the command completes. This endpoint sits behind the API's bearer-token auth
// (defense-in-depth behind the mesh boundary); an arbitrary code-exec entry must not
// be reachable without the API token when one is configured.
func (s *Server) handleSandboxExec(w http.ResponseWriter, r *http.Request) {
	if s.sandbox == nil {
		http.Error(w, "sandbox exec is not enabled on this control-plane", http.StatusNotImplemented)
		return
	}
	var req sandboxExecReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid sandbox exec request JSON", http.StatusBadRequest)
		return
	}

	suffix := "cp-" + strconv.Itoa(os.Getpid()) + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 36) + "-" +
		strconv.FormatUint(atomic.AddUint64(&sandboxExecCounter, 1), 36)

	res, err := s.sandbox.Exec(r.Context(), req.Command, req.Image, req.Timeout, suffix)
	if err != nil {
		// Invalid input (empty command, hostile image reference): 400.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := sandboxExecResp{
		Stdout:      res.Stdout,
		Stderr:      res.Stderr,
		ExitCode:    res.ExitCode,
		Image:       res.Image,
		Containment: res.Containment,
		LaunchError: res.LaunchErr,
	}
	status := http.StatusOK
	if res.LaunchErr != "" {
		// The box never started; report a bad-gateway so the client fails closed.
		status = http.StatusBadGateway
	}
	writeJSON(w, status, resp)
}
