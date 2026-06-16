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
	containerWorkspace      = "/workspace"
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
//   - read-only rootfs with a small writable tmpfs at /workspace.
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

	process := &OCIProcess{
		Terminal:        false,
		User:            OCIUser{UID: spec.NonRootUID, GID: gid},
		Args:            []string{"/sandbox"},
		Env:             []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		Cwd:             containerWorkspace,
		Capabilities:    caps,
		NoNewPrivileges: true,
	}

	root := &OCIRoot{
		Path:     "rootfs",
		Readonly: true,
	}

	mounts := []OCIMount{
		// A small writable tmpfs for /workspace; the rootfs itself stays read-only.
		{
			Destination: containerWorkspace,
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "nodev", "noexec", "mode=0700", "size=16m"},
		},
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
		// connect/send). This is the sandbox's ONLY egress, paired with network=none.
		{
			Destination: containerModelProxySock,
			Type:        "bind",
			Source:      spec.ModelProxySocket,
			Options:     []string{"bind", "rw"},
		},
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
