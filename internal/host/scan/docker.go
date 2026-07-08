package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// dockerInspect is the subset of `docker inspect <container>` we grade. Docker
// serializes an ARRAY of these (one per queried container). nerdctl emits a
// Docker-compatible schema, so its adapter reuses this type too.
type dockerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		User  string `json:"User"`
		Image string `json:"Image"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode    string   `json:"NetworkMode"`
		Privileged     bool     `json:"Privileged"`
		ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
		CapAdd         []string `json:"CapAdd"`
		CapDrop        []string `json:"CapDrop"`
		SecurityOpt    []string `json:"SecurityOpt"`
		PidMode        string   `json:"PidMode"`
		IpcMode        string   `json:"IpcMode"`
		Runtime        string   `json:"Runtime"`
		Binds          []string `json:"Binds"`
	} `json:"HostConfig"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

// dockerLikeFields is the normalized, source-agnostic subset that the Docker,
// Podman, and nerdctl inspect schemas all reduce to. Extracting it lets a single
// grading body (specFromDockerLike) serve all three OCI runtimes; each adapter
// only has to fill this struct from its own JSON shape.
type dockerLikeFields struct {
	id, name, user, image, runtime string
	networkMode, pidMode, ipcMode  string
	privileged, readonlyRootfs     bool
	capAdd, capDrop, securityOpt   []string
	binds                          []string
	mounts                         []mountPair
	// rootless is the userns-remap posture (rootless Podman / explicit userns);
	// Unknown for plain Docker unless the adapter determines it.
	rootless      Tristate
	userNSHostUID string
}

// mountPair is one host->container bind, source and destination.
type mountPair struct{ src, dst string }

// fieldsFromDockerInspect reduces a dockerInspect to the shared normalized form.
func (d dockerInspect) fields() dockerLikeFields {
	f := dockerLikeFields{
		id:             d.ID,
		name:           d.Name,
		user:           d.Config.User,
		image:          d.Config.Image,
		runtime:        d.HostConfig.Runtime,
		networkMode:    d.HostConfig.NetworkMode,
		pidMode:        d.HostConfig.PidMode,
		ipcMode:        d.HostConfig.IpcMode,
		privileged:     d.HostConfig.Privileged,
		readonlyRootfs: d.HostConfig.ReadonlyRootfs,
		capAdd:         d.HostConfig.CapAdd,
		capDrop:        d.HostConfig.CapDrop,
		securityOpt:    d.HostConfig.SecurityOpt,
		binds:          d.HostConfig.Binds,
		rootless:       Unknown,
	}
	for _, m := range d.Mounts {
		f.mounts = append(f.mounts, mountPair{m.Source, m.Destination})
	}
	return f
}

// SpecFromDockerInspect parses the JSON emitted by `docker inspect <container>`
// (an array) into a Spec for the first entry. It is pure: give it the bytes and
// it grades them, so it is unit-testable without a Docker daemon.
func SpecFromDockerInspect(raw []byte) (Spec, error) {
	f, err := firstDockerInspect(raw)
	if err != nil {
		return Spec{}, err
	}
	return specFromDockerLike(f, "docker"), nil
}

// firstDockerInspect unmarshals a docker/nerdctl inspect blob (array or single
// object) and returns the normalized fields of the first entry.
func firstDockerInspect(raw []byte) (dockerLikeFields, error) {
	var arr []dockerInspect
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Tolerate a single (non-array) object too.
		var one dockerInspect
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return dockerLikeFields{}, fmt.Errorf("parse inspect: %w", err)
		}
		arr = []dockerInspect{one}
	}
	if len(arr) == 0 {
		return dockerLikeFields{}, fmt.Errorf("inspect returned no containers")
	}
	return arr[0].fields(), nil
}

// specFromDockerLike grades the shared normalized fields into a Spec. It is the
// single source of truth for the Docker-family (docker/podman/nerdctl) posture
// extraction; each adapter fills dockerLikeFields from its own JSON and stamps
// the source label.
func specFromDockerLike(f dockerLikeFields, source string) Spec {
	s := Spec{
		Source:        source,
		Target:        strings.TrimPrefix(f.name, "/"),
		Image:         strings.TrimSpace(f.image),
		Runtime:       f.runtime,
		Rootless:      f.rootless,
		UserNSHostUID: f.userNSHostUID,
	}
	if s.Target == "" {
		s.Target = shortID(f.id)
	}

	// --- user / uid ---------------------------------------------------------
	// The Docker default (empty User) is uid 0. A non-empty user that is not
	// "0"/"root" runs as a non-root uid.
	u := strings.TrimSpace(f.user)
	s.User = u
	switch {
	case u == "" || u == "0" || strings.HasPrefix(u, "0:") || u == "root" || strings.HasPrefix(u, "root:"):
		s.RunAsNonRoot = No
		if u == "" {
			s.User = "0 (default)"
		}
	default:
		s.RunAsNonRoot = Yes
	}

	// --- capabilities -------------------------------------------------------
	s.Privileged = boolTri(f.privileged)
	s.CapDropAll = No
	for _, c := range f.capDrop {
		if strings.EqualFold(strings.TrimSpace(c), "ALL") {
			s.CapDropAll = Yes
		}
	}
	for _, c := range f.capAdd {
		if c = strings.TrimSpace(c); c != "" {
			s.CapAdd = append(s.CapAdd, strings.ToUpper(c))
		}
	}

	// --- seccomp ------------------------------------------------------------
	// Docker/Podman apply their DEFAULT seccomp profile unless SecurityOpt
	// overrides it (or the container is privileged). So absence of a seccomp opt
	// means the default profile is active — a determinable, confined posture.
	s.Seccomp = "confined"
	for _, opt := range f.securityOpt {
		low := strings.ToLower(strings.TrimSpace(opt))
		switch {
		case low == "no-new-privileges" || low == "no-new-privileges:true":
			s.NoNewPrivs = Yes
		case strings.HasPrefix(low, "seccomp="):
			val := strings.TrimPrefix(opt, opt[:strings.Index(opt, "=")+1])
			if strings.EqualFold(strings.TrimSpace(val), "unconfined") {
				s.Seccomp = "unconfined"
			} else {
				s.Seccomp = "confined" // custom profile path
			}
		}
	}
	if s.Privileged == Yes {
		s.Seccomp = "unconfined" // privileged disables seccomp
	}

	// --- network ------------------------------------------------------------
	s.NetworkMode = f.networkMode
	s.HostNetwork = boolTri(strings.EqualFold(f.networkMode, "host"))

	// --- filesystem ---------------------------------------------------------
	s.ReadonlyRoot = boolTri(f.readonlyRootfs)

	// --- docker.sock / host mounts ------------------------------------------
	s.DockerSock = No
	seen := map[string]bool{}
	consider := func(src, dst string) {
		if src == "" {
			return
		}
		if isControlSocket(src) || isControlSocket(dst) {
			s.DockerSock = Yes
		}
		if isSensitiveHostPath(src) && !seen[src] {
			seen[src] = true
			s.HostPathMounts = append(s.HostPathMounts, src)
		}
	}
	for _, m := range f.mounts {
		consider(m.src, m.dst)
	}
	for _, b := range f.binds {
		// Binds are "src:dst[:opts]".
		parts := strings.SplitN(b, ":", 3)
		src := parts[0]
		dst := ""
		if len(parts) > 1 {
			dst = parts[1]
		}
		consider(src, dst)
	}

	// --- namespaces ---------------------------------------------------------
	s.HostPID = boolTri(strings.EqualFold(f.pidMode, "host"))
	s.HostIPC = boolTri(strings.EqualFold(f.ipcMode, "host"))

	return s
}

// isControlSocket reports whether a path is a container-runtime control socket,
// whose exposure is a full host-root escape primitive.
func isControlSocket(p string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	return strings.HasSuffix(p, "docker.sock") ||
		strings.HasSuffix(p, "containerd.sock") ||
		strings.HasSuffix(p, "crio.sock") ||
		strings.HasSuffix(p, "podman.sock") ||
		strings.Contains(p, "docker.sock")
}

// isSensitiveHostPath flags host bind sources that materially widen the blast
// radius (host root, /proc, /sys, /etc, /var/run). Informational evidence.
func isSensitiveHostPath(p string) bool {
	p = strings.TrimSpace(p)
	switch p {
	case "/", "/proc", "/sys", "/etc", "/var/run", "/run":
		return true
	}
	return false
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
