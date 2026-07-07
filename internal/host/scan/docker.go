package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// dockerInspect is the subset of `docker inspect <container>` we grade. Docker
// serializes an ARRAY of these (one per queried container).
type dockerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		User string `json:"User"`
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

// SpecFromDockerInspect parses the JSON emitted by `docker inspect <container>`
// (an array) into a Spec for the first entry. It is pure: give it the bytes and
// it grades them, so it is unit-testable without a Docker daemon.
func SpecFromDockerInspect(raw []byte) (Spec, error) {
	var arr []dockerInspect
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Tolerate a single (non-array) object too.
		var one dockerInspect
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return Spec{}, fmt.Errorf("parse docker inspect: %w", err)
		}
		arr = []dockerInspect{one}
	}
	if len(arr) == 0 {
		return Spec{}, fmt.Errorf("docker inspect returned no containers")
	}
	d := arr[0]

	s := Spec{
		Source:  "docker",
		Target:  strings.TrimPrefix(d.Name, "/"),
		Runtime: d.HostConfig.Runtime,
	}
	if s.Target == "" {
		s.Target = shortID(d.ID)
	}

	// --- user / uid ---------------------------------------------------------
	// Docker's default (empty User) is uid 0. A non-empty user that is not
	// "0"/"root" runs as a non-root uid.
	u := strings.TrimSpace(d.Config.User)
	s.User = u
	switch {
	case u == "" || u == "0" || strings.HasPrefix(u, "0:") || u == "root" || strings.HasPrefix(u, "root:"):
		s.RunAsNonRoot = No
		if u == "" {
			s.User = "0 (docker default)"
		}
	default:
		s.RunAsNonRoot = Yes
	}

	// --- capabilities -------------------------------------------------------
	s.Privileged = boolTri(d.HostConfig.Privileged)
	s.CapDropAll = No
	for _, c := range d.HostConfig.CapDrop {
		if strings.EqualFold(strings.TrimSpace(c), "ALL") {
			s.CapDropAll = Yes
		}
	}
	for _, c := range d.HostConfig.CapAdd {
		if c = strings.TrimSpace(c); c != "" {
			s.CapAdd = append(s.CapAdd, strings.ToUpper(c))
		}
	}

	// --- seccomp ------------------------------------------------------------
	// Docker applies its DEFAULT seccomp profile unless SecurityOpt overrides it
	// (or the container is privileged). So absence of a seccomp opt means the
	// default profile is active — a determinable, confined posture.
	s.Seccomp = "confined"
	for _, opt := range d.HostConfig.SecurityOpt {
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
	s.NetworkMode = d.HostConfig.NetworkMode
	s.HostNetwork = boolTri(strings.EqualFold(d.HostConfig.NetworkMode, "host"))

	// --- filesystem ---------------------------------------------------------
	s.ReadonlyRoot = boolTri(d.HostConfig.ReadonlyRootfs)

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
	for _, m := range d.Mounts {
		consider(m.Source, m.Destination)
	}
	for _, b := range d.HostConfig.Binds {
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
	s.HostPID = boolTri(strings.EqualFold(d.HostConfig.PidMode, "host"))
	s.HostIPC = boolTri(strings.EqualFold(d.HostConfig.IpcMode, "host"))

	return s, nil
}

// isControlSocket reports whether a path is a container-runtime control socket,
// whose exposure is a full host-root escape primitive.
func isControlSocket(p string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	return strings.HasSuffix(p, "docker.sock") ||
		strings.HasSuffix(p, "containerd.sock") ||
		strings.HasSuffix(p, "crio.sock") ||
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
