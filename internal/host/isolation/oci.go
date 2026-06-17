// OWNER: AGENT1

package isolation

import (
	"fmt"
)

// This file defines a MINIMAL, self-contained subset of the OCI runtime
// specification (runtime-spec config.json) — only the fields IronClaw needs to
// encode its trust boundary. We deliberately do NOT import
// github.com/opencontainers/runtime-spec: the control-plane tree is stdlib-only,
// and the struct surface below is small and stable. The field names and JSON tags
// match the OCI runtime-spec so a real OCI runtime (runsc/runc) accepts the
// emitted config.json verbatim.

// OCISpec is the top-level runtime-spec document.
type OCISpec struct {
	OCIVersion string      `json:"ociVersion"`
	Process    *OCIProcess `json:"process,omitempty"`
	Root       *OCIRoot    `json:"root,omitempty"`
	Hostname   string      `json:"hostname,omitempty"`
	Mounts     []OCIMount  `json:"mounts,omitempty"`
	Linux      *OCILinux   `json:"linux,omitempty"`
}

// OCIProcess is the container process configuration.
type OCIProcess struct {
	Terminal        bool             `json:"terminal"`
	User            OCIUser          `json:"user"`
	Args            []string         `json:"args"`
	Env             []string         `json:"env,omitempty"`
	Cwd             string           `json:"cwd"`
	Capabilities    *OCICapabilities `json:"capabilities,omitempty"`
	NoNewPrivileges bool             `json:"noNewPrivileges"`
}

// OCIUser is the uid/gid the process runs as inside the container.
type OCIUser struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

// OCICapabilities holds the five Linux capability sets. IronClaw leaves every set
// empty (nil → encoded as []) so the process has NO capabilities at all.
type OCICapabilities struct {
	Bounding    []string `json:"bounding"`
	Effective   []string `json:"effective"`
	Inheritable []string `json:"inheritable"`
	Permitted   []string `json:"permitted"`
	Ambient     []string `json:"ambient"`
}

// OCIRoot is the container root filesystem.
type OCIRoot struct {
	Path     string `json:"path"`
	Readonly bool   `json:"readonly"`
}

// OCIMount is a single mount entry.
type OCIMount struct {
	Destination string   `json:"destination"`
	Type        string   `json:"type,omitempty"`
	Source      string   `json:"source,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// OCILinux holds the Linux-specific portion of the spec.
type OCILinux struct {
	// Namespaces deliberately OMITS the "network" namespace. See BuildOCISpec.
	Namespaces  []OCINamespace `json:"namespaces"`
	UIDMappings []OCIIDMapping `json:"uidMappings,omitempty"`
	GIDMappings []OCIIDMapping `json:"gidMappings,omitempty"`
	// Resources caps memory/CPU/pids via the cgroup. Seccomp restricts the
	// syscall surface. Both are always set by BuildOCISpec (defense in depth).
	Resources *OCIResources `json:"resources,omitempty"`
	Seccomp   *OCISeccomp   `json:"seccomp,omitempty"`
}

// OCIResources is the linux.resources cgroup-limit section.
type OCIResources struct {
	Memory *OCIMemoryLimit `json:"memory,omitempty"`
	CPU    *OCICPULimit    `json:"cpu,omitempty"`
	Pids   *OCIPidsLimit   `json:"pids,omitempty"`
}

// OCIMemoryLimit caps the cgroup memory (bytes).
type OCIMemoryLimit struct {
	Limit int64 `json:"limit"`
}

// OCICPULimit caps CPU bandwidth: Quota microseconds of runtime per Period.
type OCICPULimit struct {
	Quota  int64  `json:"quota"`
	Period uint64 `json:"period"`
}

// OCIPidsLimit caps the number of processes/threads in the cgroup.
type OCIPidsLimit struct {
	Limit int64 `json:"limit"`
}

// OCINamespace is one Linux namespace the container enters.
type OCINamespace struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

// OCIIDMapping is one entry of a uid/gid namespace map.
type OCIIDMapping struct {
	ContainerID int `json:"containerID"`
	HostID      int `json:"hostID"`
	Size        int `json:"size"`
}

// Fixed container-side paths for the bound queue files and the model-proxy
// socket. These are stable so the sandbox knows where to look regardless of the
// host-side path.
const (
	containerInboundPath    = "/queue/inbound.db"
	containerOutboundPath   = "/queue/outbound.db"
	containerModelProxySock = "/run/ironclaw/modelproxy.sock"
	// containerEgressSock is the OPTIONAL second egress unix socket (the egress
	// broker, T-111). It is bound only when SandboxSpec.EgressSocket is set; when
	// empty the sandbox reaches nothing but the model proxy, preserving the fully
	// sealed default. The sandbox stays network=none either way.
	containerEgressSock = "/run/ironclaw/egress.sock"
	containerWorkspace  = "/workspace"
	// containerMemory is the per-group DURABLE memory mount (rw). Persists across
	// sessions of the same agent group; omitted when SandboxSpec.MemoryPath is empty.
	containerMemory = "/memory"
	// containerShared is the global READ-ONLY shared assets mount; omitted when
	// SandboxSpec.SharedReadOnlyPath is empty.
	containerShared = "/shared"
)

// Default cgroup resource limits, applied by BuildOCISpec when the corresponding
// SandboxSpec knob is left at its zero value so a sandbox is ALWAYS bounded.
const (
	defaultMemoryLimitBytes int64  = 512 * 1024 * 1024 // 512 MiB
	defaultCPUQuota         int64  = 100_000           // with the period below => 1 vCPU
	defaultCPUPeriod        uint64 = 100_000           // 100 ms scheduling period
	defaultPidsLimit        int64  = 256
)

// BuildOCISpec turns a SandboxSpec into a hardened OCI runtime spec. It encodes
// the IronClaw trust boundary directly into the spec the runtime will enforce:
//
//   - network=none → the "network" namespace is OMITTED from linux.namespaces, so
//     the runtime does NOT create (or join) a network namespace and the container
//     shares no NIC. We deliberately omit it rather than reference an empty path:
//     a path-less network namespace would tell the runtime to create a fresh,
//     empty net namespace (loopback only); omission is the stricter, clearer
//     "no network stack at all" signal for runsc. NetworkNone MUST be true.
//   - ALL capabilities dropped → every capability set is empty.
//   - no_new_privs = true → suid/setcap binaries cannot raise privileges.
//   - non-root uid/gid in a user namespace → the container's root maps to an
//     unprivileged host uid; the process itself runs as NonRootUID / a non-zero
//     gid.
//   - read-only rootfs. /workspace is writable: a per-group DURABLE bind when
//     SandboxSpec.WorkspacePath is set, otherwise the legacy ephemeral tmpfs.
//   - optional per-group DURABLE /memory (rw) and a global READ-ONLY /shared mount
//     when their host paths are set; all writable mounts carry nosuid,nodev,noexec.
//   - inbound bound read-only, outbound bound read-write, model-proxy socket bound
//     in.
//
// It returns an error if a hardening knob is not set to its safe value, so a
// misconfiguration fails loudly at spec-build time rather than silently launching
// a weaker sandbox.
func BuildOCISpec(spec SandboxSpec) (*OCISpec, error) {
	if !spec.NetworkNone {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec refuses NetworkNone=false (the sandbox must have no network; model egress is the proxy socket only)")
	}
	if !spec.DropAllCaps {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec refuses DropAllCaps=false (all Linux capabilities must be dropped)")
	}
	if !spec.NoNewPrivs {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec refuses NoNewPrivs=false (no_new_privs must be set)")
	}
	if !spec.ReadOnlyRootfs {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec refuses ReadOnlyRootfs=false (rootfs must be read-only)")
	}
	if spec.NonRootUID <= 0 {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec requires a non-zero NonRootUID, got %d", spec.NonRootUID)
	}
	if spec.ReadOnlyInboundPath == "" || spec.ReadWriteOutboundPath == "" {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec requires inbound and outbound queue paths")
	}
	if spec.ModelProxySocket == "" {
		return nil, fmt.Errorf("host/isolation: BuildOCISpec requires a model-proxy socket path")
	}

	// Run as a non-zero gid too; default to the same value as the uid which is the
	// conventional distroless "nonroot" group.
	gid := spec.NonRootUID

	// Capabilities: every set empty. Use non-nil empty slices so the JSON encodes
	// each set as [] (an explicit "no capabilities") rather than null.
	caps := &OCICapabilities{
		Bounding:    []string{},
		Effective:   []string{},
		Inheritable: []string{},
		Permitted:   []string{},
		Ambient:     []string{},
	}

	// Model provider selection (T-233): pass non-secret provider/model/host flags to
	// the sandbox process when set. The default (all empty) keeps the bare
	// "/sandbox" args, so the sealed Anthropic posture is unchanged. Credential
	// injection and the egress allowlist remain host-side regardless of these flags.
	args := []string{"/sandbox"}
	if spec.ModelProvider != "" {
		args = append(args, "--provider", spec.ModelProvider)
	}
	if spec.ModelID != "" {
		args = append(args, "--model", spec.ModelID)
	}
	if spec.ModelHost != "" {
		args = append(args, "--model-host", spec.ModelHost)
	}

	process := &OCIProcess{
		Terminal:        false,
		User:            OCIUser{UID: spec.NonRootUID, GID: gid},
		Args:            args,
		Env:             []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		Cwd:             containerWorkspace,
		Capabilities:    caps,
		NoNewPrivileges: true,
	}

	root := &OCIRoot{
		Path:     "rootfs",
		Readonly: true,
	}

	// /workspace: a per-group DURABLE bind when WorkspacePath is set, else the legacy
	// ephemeral tmpfs. Either way the rootfs stays read-only and the workspace carries
	// nosuid,nodev,noexec.
	workspaceMount := OCIMount{
		Destination: containerWorkspace,
		Type:        "tmpfs",
		Source:      "tmpfs",
		Options:     []string{"nosuid", "nodev", "noexec", "mode=0700", "size=16m"},
	}
	if spec.WorkspacePath != "" {
		workspaceMount = OCIMount{
			Destination: containerWorkspace,
			Type:        "bind",
			Source:      spec.WorkspacePath,
			Options:     []string{"bind", "rw", "nosuid", "nodev", "noexec"},
		}
	}

	mounts := []OCIMount{
		workspaceMount,
		// Inbound queue: bound READ-ONLY. The sandbox can never write it (defense in
		// depth alongside interface segregation and PRAGMA query_only).
		{
			Destination: containerInboundPath,
			Type:        "bind",
			Source:      spec.ReadOnlyInboundPath,
			Options:     []string{"bind", "ro"},
		},
		// Outbound queue: bound READ-WRITE. The sandbox is its sole writer.
		{
			Destination: containerOutboundPath,
			Type:        "bind",
			Source:      spec.ReadWriteOutboundPath,
			Options:     []string{"bind", "rw"},
		},
		// Model-proxy unix socket: bound in read-write (a socket needs write to
		// connect/send). This is the sandbox's primary egress, paired with network=none.
		{
			Destination: containerModelProxySock,
			Type:        "bind",
			Source:      spec.ModelProxySocket,
			Options:     []string{"bind", "rw"},
		},
	}

	// Optional egress-broker unix socket (T-111). Bound only when an EgressSocket is
	// configured; otherwise the sandbox reaches nothing but the model proxy. Like the
	// model-proxy socket it is rw (a socket needs write to connect) and, being a
	// host-mediated unix socket rather than a NIC, it keeps the sandbox network=none.
	if spec.EgressSocket != "" {
		mounts = append(mounts, OCIMount{
			Destination: containerEgressSock,
			Type:        "bind",
			Source:      spec.EgressSocket,
			Options:     []string{"bind", "rw"},
		})
	}

	// Optional per-group DURABLE memory (rw) — persists across the agent group's
	// sessions. Writable but nosuid,nodev,noexec like the workspace.
	if spec.MemoryPath != "" {
		mounts = append(mounts, OCIMount{
			Destination: containerMemory,
			Type:        "bind",
			Source:      spec.MemoryPath,
			Options:     []string{"bind", "rw", "nosuid", "nodev", "noexec"},
		})
	}
	// Optional global READ-ONLY shared assets. Bound ro (the sandbox can never write
	// it) and nosuid,nodev,noexec.
	if spec.SharedReadOnlyPath != "" {
		mounts = append(mounts, OCIMount{
			Destination: containerShared,
			Type:        "bind",
			Source:      spec.SharedReadOnlyPath,
			Options:     []string{"bind", "ro", "nosuid", "nodev", "noexec"},
		})
	}

	linux := &OCILinux{
		// Namespaces: pid/mount/ipc/uts/user — but deliberately NO "network"
		// namespace entry, which (for runsc) means the sandbox gets no network stack.
		Namespaces: []OCINamespace{
			{Type: "pid"},
			{Type: "mount"},
			{Type: "ipc"},
			{Type: "uts"},
			{Type: "user"},
		},
		// Map the container's uid/gid range onto an unprivileged host range. A single
		// mapping is sufficient for the single non-root process the sandbox runs.
		UIDMappings: []OCIIDMapping{
			{ContainerID: 0, HostID: spec.NonRootUID, Size: 1},
		},
		GIDMappings: []OCIIDMapping{
			{ContainerID: 0, HostID: gid, Size: 1},
		},
		// cgroup resource caps (zero knobs fall back to the safe defaults) and the
		// restrictive default seccomp profile — both always present so a sandbox is
		// bounded and its syscall surface reduced without per-call opt-in.
		Resources: buildResources(spec),
		Seccomp:   DefaultSeccompProfile(),
	}

	return &OCISpec{
		OCIVersion: "1.0.2",
		Process:    process,
		Root:       root,
		Hostname:   "ironclaw-sandbox",
		Mounts:     mounts,
		Linux:      linux,
	}, nil
}

// buildResources derives the cgroup limits from spec, substituting the safe
// defaults for any knob left at its zero value, so every emitted spec carries
// memory, CPU, and pids caps.
func buildResources(spec SandboxSpec) *OCIResources {
	memLimit := spec.MemoryLimitBytes
	if memLimit <= 0 {
		memLimit = defaultMemoryLimitBytes
	}
	cpuQuota := spec.CPUQuota
	if cpuQuota <= 0 {
		cpuQuota = defaultCPUQuota
	}
	cpuPeriod := spec.CPUPeriod
	if cpuPeriod == 0 {
		cpuPeriod = defaultCPUPeriod
	}
	pidsLimit := spec.PidsLimit
	if pidsLimit <= 0 {
		pidsLimit = defaultPidsLimit
	}
	return &OCIResources{
		Memory: &OCIMemoryLimit{Limit: memLimit},
		CPU:    &OCICPULimit{Quota: cpuQuota, Period: cpuPeriod},
		Pids:   &OCIPidsLimit{Limit: pidsLimit},
	}
}
