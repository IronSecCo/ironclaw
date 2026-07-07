package scan

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFile is the subset of a docker-compose file we grade.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

// composeService mirrors the security-relevant keys of a compose service. Scalars
// that may be bool-or-string in the wild (pid/ipc) are read as strings.
type composeService struct {
	User        string   `yaml:"user"`
	ReadOnly    *bool    `yaml:"read_only"`
	Privileged  *bool    `yaml:"privileged"`
	NetworkMode string   `yaml:"network_mode"`
	CapAdd      []string `yaml:"cap_add"`
	CapDrop     []string `yaml:"cap_drop"`
	SecurityOpt []string `yaml:"security_opt"`
	Pid         string   `yaml:"pid"`
	Ipc         string   `yaml:"ipc"`
	Runtime     string   `yaml:"runtime"`
	Volumes     []string `yaml:"volumes"`
}

// SpecFromCompose parses a docker-compose file and grades the named service. If
// service is empty and the file has exactly one service, that one is used. Pure
// and unit-testable: it grades bytes, no Docker required.
func SpecFromCompose(raw []byte, service string) (Spec, error) {
	var cf composeFile
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return Spec{}, fmt.Errorf("parse compose file: %w", err)
	}
	if len(cf.Services) == 0 {
		return Spec{}, fmt.Errorf("compose file declares no services")
	}
	if service == "" {
		if len(cf.Services) != 1 {
			names := make([]string, 0, len(cf.Services))
			for n := range cf.Services {
				names = append(names, n)
			}
			return Spec{}, fmt.Errorf("compose file has %d services; pass --service (one of: %s)",
				len(cf.Services), strings.Join(names, ", "))
		}
		for n := range cf.Services {
			service = n
		}
	}
	svc, ok := cf.Services[service]
	if !ok {
		return Spec{}, fmt.Errorf("service %q not found in compose file", service)
	}

	s := Spec{Source: "compose", Target: service, Runtime: svc.Runtime}

	// --- user ---------------------------------------------------------------
	u := strings.TrimSpace(svc.User)
	s.User = u
	switch {
	case u == "":
		s.RunAsNonRoot = No // compose default is the image user, unknown; fail-closed to root
		s.User = "0 (image default; not pinned)"
		s.Notes = append(s.Notes, "no 'user:' pinned; the image default may be root")
	case u == "0" || strings.HasPrefix(u, "0:") || u == "root" || strings.HasPrefix(u, "root:"):
		s.RunAsNonRoot = No
	default:
		s.RunAsNonRoot = Yes
	}

	// --- capabilities -------------------------------------------------------
	s.Privileged = triFromPtr(svc.Privileged)
	s.CapDropAll = No
	for _, c := range svc.CapDrop {
		if strings.EqualFold(strings.TrimSpace(c), "ALL") {
			s.CapDropAll = Yes
		}
	}
	for _, c := range svc.CapAdd {
		if c = strings.TrimSpace(c); c != "" {
			s.CapAdd = append(s.CapAdd, strings.ToUpper(c))
		}
	}

	// --- seccomp ------------------------------------------------------------
	s.Seccomp = "confined" // compose->docker applies the default profile unless overridden
	for _, opt := range svc.SecurityOpt {
		low := strings.ToLower(strings.TrimSpace(opt))
		switch {
		case strings.HasPrefix(low, "no-new-privileges"):
			s.NoNewPrivs = Yes
		case strings.HasPrefix(low, "seccomp"):
			if strings.Contains(low, "unconfined") {
				s.Seccomp = "unconfined"
			} else {
				s.Seccomp = "confined"
			}
		}
	}
	if s.Privileged == Yes {
		s.Seccomp = "unconfined"
	}

	// --- network ------------------------------------------------------------
	s.NetworkMode = svc.NetworkMode
	if svc.NetworkMode == "" {
		// No network_mode means the default bridge network: egress-capable.
		s.NetworkMode = "bridge"
	}
	s.HostNetwork = boolTri(strings.EqualFold(svc.NetworkMode, "host"))

	// --- filesystem ---------------------------------------------------------
	s.ReadonlyRoot = triFromPtr(svc.ReadOnly)

	// --- docker.sock / host mounts ------------------------------------------
	s.DockerSock = No
	seen := map[string]bool{}
	for _, v := range svc.Volumes {
		parts := strings.SplitN(v, ":", 3)
		src := strings.TrimSpace(parts[0])
		dst := ""
		if len(parts) > 1 {
			dst = parts[1]
		}
		if isControlSocket(src) || isControlSocket(dst) {
			s.DockerSock = Yes
		}
		if isSensitiveHostPath(src) && !seen[src] {
			seen[src] = true
			s.HostPathMounts = append(s.HostPathMounts, src)
		}
	}

	// --- namespaces ---------------------------------------------------------
	s.HostPID = boolTri(strings.EqualFold(strings.TrimSpace(svc.Pid), "host"))
	s.HostIPC = boolTri(strings.EqualFold(strings.TrimSpace(svc.Ipc), "host"))

	return s, nil
}

// triFromPtr maps a *bool (nil = key absent) to a Tristate. Absent stays Unknown
// so scorers grade it fail-closed.
func triFromPtr(p *bool) Tristate {
	if p == nil {
		return Unknown
	}
	return boolTri(*p)
}
