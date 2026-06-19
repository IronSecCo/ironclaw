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
//
// To narrow the gap to the gVisor posture, the Docker path applies every host-side
// hardening knob runc can enforce, NOT just the relaxed defaults:
//
//   - network=none is AUTO-ENFORCED (hardcoded), never operator-configurable. The
//     sandbox's only egress is the bound model-proxy/egress/MCP unix sockets, so it
//     never needs a NIC; there is no env var to attach it to a network.
//   - the SAME restrictive seccomp allowlist the OCI path emits (DefaultSeccompProfile)
//     is passed via SecurityOpt seccomp=… — deny-by-default (EPERM) syscall filtering.
//   - all Linux capabilities dropped (CapDrop ALL) and no_new_privs set.
//   - read-only rootfs with a small noexec /tmp tmpfs; everything writable arrives
//     through the explicit binds.
//   - memory and pids cgroup caps mirror the OCI defaults so a sandbox is bounded.
//
// The IRREDUCIBLE residual gap (documented in docs/site/concepts/sandbox-isolation.mdx
// and docs/SETUP.md): runc shares the host kernel, so there is NO per-sandbox syscall
// interception like gVisor's user-space kernel — seccomp narrows the surface but a
// kernel-level escape is not contained the way runsc contains it. Use Linux + gVisor
// for the real security posture.
package isolation

import (
	"bytes"
	"context"
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
	client     *http.Client
	binds      []string // volume binds replicated into every sandbox, "name:/mount[:ro]"
	user       string   // container user, e.g. "0:0"
	seccompOpt string   // precomputed "seccomp=<profile-json>" SecurityOpt
}

// NewDocker constructs a DockerIsolator. socket is the Docker Engine API socket
// (e.g. /var/run/docker.sock); binds are the volume mounts ("vol:/path") that carry
// the per-session queues/key and the model-proxy socket into the sandbox at the SAME
// paths the control plane uses; and user is the container uid:gid.
//
// network=none is NOT a parameter: it is auto-enforced on every launch (see Launch),
// so there is no operator knob to attach a sandbox to a network.
func NewDocker(socket string, binds []string, user string) *DockerIsolator {
	// Precompute the seccomp SecurityOpt from the shared restrictive profile. The
	// OCISeccomp JSON shape (defaultAction/architectures/syscalls) is exactly Docker's
	// seccomp profile format, so the Engine API accepts it inline verbatim. Marshal
	// failure is impossible for this static struct; ignore the error defensively (an
	// empty opt would simply fall back to Docker's default profile, never weaker than
	// none).
	seccompOpt := ""
	if profile, err := json.Marshal(DefaultSeccompProfile()); err == nil {
		seccompOpt = "seccomp=" + string(profile)
	}
	return &DockerIsolator{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
		binds:      binds,
		user:       user,
		seccompOpt: seccompOpt,
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
	Binds          []string          `json:"Binds,omitempty"`
	NetworkMode    string            `json:"NetworkMode,omitempty"`
	AutoRemove     bool              `json:"AutoRemove"`
	CapDrop        []string          `json:"CapDrop,omitempty"`
	SecurityOpt    []string          `json:"SecurityOpt,omitempty"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	Tmpfs          map[string]string `json:"Tmpfs,omitempty"`
	Memory         int64             `json:"Memory,omitempty"`
	PidsLimit      *int64            `json:"PidsLimit,omitempty"`
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

	// SecurityOpt: no_new_privs + the restrictive seccomp allowlist (when the profile
	// marshalled). no-new-privileges is always set; seccomp is appended only when
	// non-empty so a marshal failure degrades to Docker's default profile rather than
	// silently disabling seccomp.
	secOpt := []string{"no-new-privileges:true"}
	if d.seccompOpt != "" {
		secOpt = append(secOpt, d.seccompOpt)
	}
	pids := defaultPidsLimit
	req := dockerCreateReq{
		Image:  spec.Image,
		Cmd:    sandboxArgs(spec),
		User:   d.user,
		Labels: map[string]string{"ironclaw.session": string(spec.SessionID)},
		HostConfig: dockerHostConfig{
			Binds: d.binds,
			// network=none is auto-enforced, never operator-configurable: the sandbox
			// reaches the model proxy / egress / MCP only through bound unix sockets and
			// must never get a NIC.
			NetworkMode: "none",
			AutoRemove:  false,
			// Drop every capability and forbid privilege escalation — mirror the OCI
			// path's empty cap sets + no_new_privs on the runc boundary.
			CapDrop:     []string{"ALL"},
			SecurityOpt: secOpt,
			// Read-only rootfs; the sandbox writes only to bound rw mounts. A small
			// noexec/nosuid/nodev /tmp tmpfs covers any transient scratch need.
			ReadonlyRootfs: true,
			Tmpfs:          map[string]string{"/tmp": "rw,nosuid,nodev,noexec,size=16m"},
			// cgroup caps mirror the OCI defaults so a runc sandbox is bounded too.
			Memory:    defaultMemoryLimitBytes,
			PidsLimit: &pids,
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
