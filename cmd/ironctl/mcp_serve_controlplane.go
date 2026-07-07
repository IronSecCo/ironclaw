package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/host/mcp"
	"github.com/IronSecCo/ironclaw/internal/host/sandboxexec"
)

// controlplaneBackend is the THIN-CLIENT sandbox_exec backend: it delegates the box
// to a running IronClaw control-plane over its authenticated API instead of shelling
// `docker run` locally. This process holds NO host privilege (no docker.sock, no
// runsc in-image) — the control-plane, which already owns hardened gVisor spawning,
// does the work.
//
// Fail-closed: if the control-plane is unreachable or returns a launch failure, the
// tool reports an error. It NEVER falls back to an unhardened host path.
type controlplaneBackend struct {
	baseURL    string // e.g. http://cp:8787 (no trailing slash)
	token      string // control-plane API bearer token (may be empty for a mesh-only CP)
	image      string // default image when the call does not override it
	timeoutSec int    // default per-exec timeout in seconds
	client     *http.Client
}

func (b *controlplaneBackend) note() string {
	return "thin-client backend -> control-plane " + b.baseURL + " (no host privilege)"
}

// sandboxExecRequest is the POST body sent to the control-plane's exec endpoint.
type sandboxExecRequest struct {
	Command string `json:"command"`
	Image   string `json:"image,omitempty"`
	Timeout int    `json:"timeout_seconds,omitempty"`
}

// toolFunc returns the sandbox_exec handler that POSTs to the control-plane.
func (b *controlplaneBackend) toolFunc() mcp.ToolFunc {
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
		image := in.Image
		if strings.TrimSpace(image) == "" {
			image = b.image
		}
		timeout := in.Timeout
		if timeout <= 0 {
			timeout = b.timeoutSec
		}

		body, _ := json.Marshal(sandboxExecRequest{Command: in.Command, Image: image, Timeout: timeout})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/sandbox/exec", bytes.NewReader(body))
		if err != nil {
			return mcp.ToolResult{}, fmt.Errorf("sandbox_exec: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if b.token != "" {
			req.Header.Set("Authorization", "Bearer "+b.token)
		}

		resp, err := b.client.Do(req)
		if err != nil {
			// Fail-closed: the control-plane is unreachable. The box never ran; there
			// is NO local fallback. Surface as a tool error.
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: control-plane unreachable at %s: %v\n"+
						"containment: no box was launched; this thin client never has host docker/runsc access (fail-closed).",
					b.baseURL, err)}},
				IsError: true,
			}, nil
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

		// The control-plane returns a structured sandboxexec-style body. A non-2xx
		// status or a body carrying launch_error is a tool error (fail-closed).
		var out struct {
			Stdout      string `json:"stdout"`
			Stderr      string `json:"stderr"`
			ExitCode    int    `json:"exit_code"`
			Image       string `json:"image"`
			Containment string `json:"containment"`
			LaunchError string `json:"launch_error"`
			Error       string `json:"error"`
		}
		_ = json.Unmarshal(raw, &out)

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := strings.TrimSpace(out.Error)
			if msg == "" {
				msg = strings.TrimSpace(string(raw))
			}
			if msg == "" {
				msg = resp.Status
			}
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: control-plane rejected the request (%d): %s\ncontainment: no box gained network or host access (fail-closed).",
					resp.StatusCode, msg)}},
				IsError: true,
			}, nil
		}
		if strings.TrimSpace(out.LaunchError) != "" {
			return mcp.ToolResult{
				Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf(
					"sandbox_exec: launch failed on control-plane: %s\n(image=%s)\ncontainment: %s",
					out.LaunchError, out.Image, out.Containment)}},
				IsError: true,
			}, nil
		}
		// Success (or a normal non-zero exit): render the same text block the docker
		// backend produces so a client cannot tell the backends apart.
		return mcp.TextResult(sandboxexec.FormatText(sandboxexec.Result{
			Stdout:      out.Stdout,
			Stderr:      out.Stderr,
			ExitCode:    out.ExitCode,
			Image:       out.Image,
			Containment: out.Containment,
		})), nil
	}
}
