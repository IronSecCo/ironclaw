// DockerIsolator launches each sandbox as a plain Docker container (the default
// runc runtime — NOT gVisor) via the Docker Engine API over its unix socket. It
// exists for hosts without runsc/gVisor (e.g. macOS dev under Docker Desktop): the
// control plane can still spin up a real, per-conversation sandbox container
// instead of deferring the launch, so the full engage→reply path (and the web Chat
// playground) works end-to-end.
//
// This is NOT the sealed production posture — runc shares the host kernel and the
// per-session queues/key + model-proxy socket are handed in via shared volumes
// rather than a hardened OCI bundle. The model credential is still injected
// host-side (the sandbox reaches only the model-proxy socket); only the isolation
// boundary is relaxed. Select it with --runtime docker (IRONCLAW_RUNTIME=docker).
package isolation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DockerIsolator launches each sandbox as a sibling Docker container, talking to
// the Docker Engine API over a unix socket (no docker CLI dependency).
type DockerIsolator struct {
	client  *http.Client
	network string   // docker network to attach (e.g. a private bridge)
	binds   []string // volume binds replicated into every sandbox, "name:/mount[:ro]"
	user    string   // container user, e.g. "0:0"
}

// NewDocker constructs a DockerIsolator. socket is the Docker Engine API socket
// (e.g. /var/run/docker.sock); network is the docker network to attach; binds are
// the volume mounts ("vol:/path") that carry the per-session queues/key and the
// model-proxy socket into the sandbox at the SAME paths the control plane uses; and
// user is the container uid:gid.
func NewDocker(socket, network string, binds []string, user string) *DockerIsolator {
	return &DockerIsolator{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
		network: network,
		binds:   binds,
		user:    user,
	}
}

type dockerCreateReq struct {
	Image      string            `json:"Image"`
	Cmd        []string          `json:"Cmd"`
	User       string            `json:"User,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	HostConfig dockerHostConfig  `json:"HostConfig"`
}

type dockerHostConfig struct {
	Binds       []string `json:"Binds,omitempty"`
	NetworkMode string   `json:"NetworkMode,omitempty"`
	AutoRemove  bool     `json:"AutoRemove"`
}

type dockerCreateResp struct {
	ID string `json:"Id"`
}

// Launch creates and starts a sandbox container for spec and returns a Handle that
// force-removes it on Stop.
func (d *DockerIsolator) Launch(ctx context.Context, spec SandboxSpec) (Handle, error) {
	name := "ic-sbx-" + dockerSafeName(string(spec.SessionID))
	// Best-effort: remove any stale container of the same name (a prior crashed run)
	// so create does not 409. Ignore the error (usually "no such container").
	_ = d.do(ctx, http.MethodDelete, "/containers/"+name+"?force=true", nil, nil)

	req := dockerCreateReq{
		Image:  spec.Image,
		Cmd:    sandboxArgs(spec),
		User:   d.user,
		Labels: map[string]string{"ironclaw.session": string(spec.SessionID)},
		HostConfig: dockerHostConfig{
			Binds:       d.binds,
			NetworkMode: d.network,
			AutoRemove:  false,
		},
	}
	var created dockerCreateResp
	if err := d.do(ctx, http.MethodPost, "/containers/create?name="+name, req, &created); err != nil {
		return nil, fmt.Errorf("host/isolation: docker create %s: %w", name, err)
	}
	if err := d.do(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil); err != nil {
		_ = d.do(ctx, http.MethodDelete, "/containers/"+created.ID+"?force=true", nil, nil)
		return nil, fmt.Errorf("host/isolation: docker start %s: %w", name, err)
	}
	return &dockerHandle{iso: d, id: created.ID}, nil
}

type dockerHandle struct {
	iso *DockerIsolator
	id  string
}

// The Docker handle surfaces early container exits to the Manager (IRO-171).
var _ EarlyExitReporter = (*dockerHandle)(nil)

// Stop force-removes the sandbox container. Idempotent.
func (h *dockerHandle) Stop(ctx context.Context) error {
	if err := h.iso.do(ctx, http.MethodDelete, "/containers/"+h.id+"?force=true", nil, nil); err != nil {
		return fmt.Errorf("host/isolation: docker rm %s: %w", h.id, err)
	}
	return nil
}

// Alive reports whether the sandbox container is still running, via a container
// inspect. A gone container (HTTP 404 — crashed and auto-removed, OOM-killed, or
// `docker rm`'d out-of-band) or one no longer in the running state reports false so
// the Manager relaunches promptly. A transient/unexpected Engine API error reports
// true: we do not tear down a sandbox we cannot prove is dead — the sweep's
// heartbeat ceiling remains the backstop.
func (h *dockerHandle) Alive(ctx context.Context) bool {
	running, exists, err := h.iso.inspectState(ctx, h.id)
	if err != nil {
		return true
	}
	return exists && running
}

// ExitInfo implements EarlyExitReporter: it reports whether the sandbox container
// has already exited and, if so, with what code and its first log line. The
// Manager calls it shortly after launch so a container that dies on startup — the
// macOS Docker file-sharing case where the in-container session-key read misses a
// host file (IRO-171) — is surfaced to the control-plane log instead of being
// hidden behind "launched sandbox".
//
// A still-running container, or one that is already gone (HTTP 404 — a prior crash
// auto-removed or a relaunch reclaimed the name), reports exited=false so the
// caller logs nothing misleading. AutoRemove is off for sandbox containers, so an
// exited-but-not-yet-removed container can still be inspected and its logs read.
func (h *dockerHandle) ExitInfo(ctx context.Context) (exited bool, code int, logLine string, err error) {
	running, exitCode, exists, ierr := h.iso.inspectExit(ctx, h.id)
	if ierr != nil {
		return false, 0, "", ierr
	}
	if !exists || running {
		// Gone (can't tell) or still running: nothing to report.
		return false, 0, "", nil
	}
	// Exited: best-effort fetch of the first log line for a one-line diagnostic. A
	// log-fetch failure is non-fatal — the exit code alone is still worth surfacing.
	line, _ := h.iso.firstLogLine(ctx, h.id)
	return true, exitCode, line, nil
}

// inspectExit inspects a container's running state and exit code. Mirrors
// inspectState but also returns State.ExitCode for the early-exit diagnostic.
func (d *DockerIsolator) inspectExit(ctx context.Context, id string) (running bool, exitCode int, exists bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+id+"/json", nil)
	if err != nil {
		return false, 0, false, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, 0, false, nil // container is gone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, 0, true, fmt.Errorf("docker api inspect %s: %s: %s", id, resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		State struct {
			Running  bool `json:"Running"`
			ExitCode int  `json:"ExitCode"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, 0, true, err
	}
	return out.State.Running, out.State.ExitCode, true, nil
}

// firstLogLine fetches the container's combined stdout+stderr and returns its
// first non-empty line, truncated for a one-line log diagnostic. The Engine API
// multiplexes stdout/stderr into a framed stream (an 8-byte header per frame) when
// the container has no TTY (the sandbox case), so the payload is demultiplexed
// before scanning for the first line.
func (d *DockerIsolator) firstLogLine(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://docker/containers/"+id+"/logs?stdout=1&stderr=1&tail=20", nil)
	if err != nil {
		return "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("docker api logs %s: %s", id, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	return firstNonEmptyLine(demuxDockerStream(raw)), nil
}

// demuxDockerStream decodes Docker's multiplexed log framing (8-byte header:
// [stream_type, 0,0,0, size:uint32be] then size payload bytes) into the raw
// payload text. A stream that does not match the framing (a TTY container, or a
// short/garbled buffer) is returned as-is, so the caller still gets usable text.
func demuxDockerStream(b []byte) string {
	var out bytes.Buffer
	rest := b
	framed := false
	for len(rest) >= 8 {
		// A valid header has a known stream type (0,1,2) and three zero pad bytes.
		if rest[0] > 2 || rest[1] != 0 || rest[2] != 0 || rest[3] != 0 {
			break
		}
		size := int(binary.BigEndian.Uint32(rest[4:8]))
		if size < 0 || 8+size > len(rest) {
			break
		}
		out.Write(rest[8 : 8+size])
		rest = rest[8+size:]
		framed = true
	}
	if framed {
		return out.String()
	}
	return string(b)
}

// firstNonEmptyLine returns the first non-blank line of s, trimmed and truncated
// to a sane length for a single log line.
func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		const max = 300
		if len(line) > max {
			line = line[:max] + "…"
		}
		return line
	}
	return ""
}

// inspectState inspects a container's running state. exists is false when the
// container is gone (HTTP 404). err is non-nil only on a transient/unexpected
// failure (a connection error or a non-404 non-2xx status) — distinct from a clean
// 404, so the caller can treat "gone" and "can't tell" differently.
func (d *DockerIsolator) inspectState(ctx context.Context, id string) (running, exists bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+id+"/json", nil)
	if err != nil {
		return false, false, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, false, nil // container is gone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, true, fmt.Errorf("docker api inspect %s: %s: %s", id, resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, true, err
	}
	return out.State.Running, true, nil
}

// sandboxArgs renders the cmd/sandbox flags for spec. The image ENTRYPOINT is
// "/sandbox", so these are appended to it. Paths are absolute and resolve inside
// the shared volume binds (the same mount points the control plane uses).
func sandboxArgs(spec SandboxSpec) []string {
	a := []string{
		"--inbound", spec.ReadOnlyInboundPath,
		"--outbound", spec.ReadWriteOutboundPath,
		"--model-socket", spec.ModelProxySocket,
	}
	if spec.KeyPath != "" {
		a = append(a, "--key", spec.KeyPath)
	}
	if spec.WorkspacePath != "" {
		a = append(a, "--workspace", spec.WorkspacePath)
	}
	if spec.EgressSocket != "" {
		a = append(a, "--egress-socket", spec.EgressSocket)
	}
	if spec.MCPSocket != "" {
		// Per-session MCP broker socket. Reachable in-container at the same host
		// path via the shared volume binds, like the model-proxy/egress sockets.
		a = append(a, "--mcp-socket", spec.MCPSocket)
	}
	if spec.ModelProvider != "" {
		a = append(a, "--provider", spec.ModelProvider)
	}
	if spec.ModelID != "" {
		a = append(a, "--model", spec.ModelID)
	}
	if spec.ModelHost != "" {
		a = append(a, "--model-host", spec.ModelHost)
	}
	if spec.Persona != "" {
		a = append(a, "--persona", spec.Persona)
	}
	if len(spec.EnabledTools) > 0 {
		a = append(a, "--enabled-tools", strings.Join(spec.EnabledTools, ","))
	}
	if spec.SearchBackend != "" {
		a = append(a, "--search-backend", spec.SearchBackend)
	}
	return a
}

// do performs one Engine API call. If out is non-nil the JSON response body is
// decoded into it. Non-2xx responses are returned as errors with the body.
func (d *DockerIsolator) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker api %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// dockerSafeName maps a string to the docker container-name charset [a-zA-Z0-9_.-].
func dockerSafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
