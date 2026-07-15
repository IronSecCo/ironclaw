package scan

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// k8sObject is a minimal Kubernetes object: enough to locate the pod spec inside
// a bare Pod, inside a workload template (Deployment/StatefulSet/DaemonSet/Job/
// ReplicaSet/ReplicationController via spec.template.spec), or inside a CronJob
// (spec.jobTemplate.spec.template.spec).
type k8sObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		podSpec `yaml:",inline"` // bare Pod: spec IS the pod spec
		// Workload kinds nest the pod under spec.template.spec.
		Template struct {
			Spec podSpec `yaml:"spec"`
		} `yaml:"template"`
		// CronJob nests one level deeper: spec.jobTemplate.spec.template.spec.
		JobTemplate struct {
			Spec struct {
				Template struct {
					Spec podSpec `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

// podSpecOf returns the graded pod spec for a parsed object, resolving the three
// nesting shapes (bare Pod / workload template / CronJob jobTemplate), and true
// when a pod spec with at least one container was found.
func (obj k8sObject) podSpecOf() (podSpec, bool) {
	if len(obj.Spec.podSpec.Containers) > 0 {
		return obj.Spec.podSpec, true
	}
	if len(obj.Spec.Template.Spec.Containers) > 0 {
		return obj.Spec.Template.Spec, true
	}
	if len(obj.Spec.JobTemplate.Spec.Template.Spec.Containers) > 0 {
		return obj.Spec.JobTemplate.Spec.Template.Spec, true
	}
	return podSpec{}, false
}

type podSpec struct {
	HostPID         *bool          `yaml:"hostPID"`
	HostIPC         *bool          `yaml:"hostIPC"`
	HostNetwork     *bool          `yaml:"hostNetwork"`
	SecurityContext *podSecCtx     `yaml:"securityContext"`
	Containers      []k8sContainer `yaml:"containers"`
	Volumes         []k8sVolume    `yaml:"volumes"`
	RuntimeClass    string         `yaml:"runtimeClassName"`
}

// seccompProfile mirrors a Kubernetes securityContext.seccompProfile block. Named
// (rather than inlined) so non-YAML adapters (e.g. Terraform JSON) can construct
// the same graded pod spec without re-declaring an anonymous struct.
type seccompProfile struct {
	Type string `yaml:"type"`
}

// capabilities mirrors a container securityContext.capabilities block.
type capabilities struct {
	Add  []string `yaml:"add"`
	Drop []string `yaml:"drop"`
}

type podSecCtx struct {
	RunAsNonRoot   *bool           `yaml:"runAsNonRoot"`
	RunAsUser      *int64          `yaml:"runAsUser"`
	SeccompProfile *seccompProfile `yaml:"seccompProfile"`
}

// containerSecCtx mirrors a container-level securityContext.
type containerSecCtx struct {
	RunAsNonRoot             *bool           `yaml:"runAsNonRoot"`
	RunAsUser                *int64          `yaml:"runAsUser"`
	Privileged               *bool           `yaml:"privileged"`
	ReadOnlyRootFilesystem   *bool           `yaml:"readOnlyRootFilesystem"`
	AllowPrivilegeEscalation *bool           `yaml:"allowPrivilegeEscalation"`
	Capabilities             *capabilities   `yaml:"capabilities"`
	SeccompProfile           *seccompProfile `yaml:"seccompProfile"`
}

// volumeMount mirrors a container volumeMounts entry.
type volumeMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
}

type k8sContainer struct {
	Name            string           `yaml:"name"`
	SecurityContext *containerSecCtx `yaml:"securityContext"`
	VolumeMounts    []volumeMount    `yaml:"volumeMounts"`
}

type k8sVolume struct {
	Name     string `yaml:"name"`
	HostPath *struct {
		Path string `yaml:"path"`
	} `yaml:"hostPath"`
}

// SpecFromK8s parses a Kubernetes manifest (a bare Pod or a workload whose
// template carries a pod spec) and grades its FIRST container. Pure and
// unit-testable. Kubernetes network isolation is governed by NetworkPolicy,
// which a pod manifest does not carry, so egress is graded conservatively
// (fail-closed) unless hostNetwork makes it strictly worse.
func SpecFromK8s(raw []byte) (Spec, error) {
	var obj k8sObject
	if err := yaml.Unmarshal(raw, &obj); err != nil {
		return Spec{}, fmt.Errorf("parse kubernetes manifest: %w", err)
	}
	ps, ok := obj.podSpecOf()
	if !ok {
		return Spec{}, fmt.Errorf("manifest has no containers to grade")
	}
	return specFromPodSpec("k8s", obj.Metadata.Name, ps), nil
}

// specFromPodSpec maps a parsed Kubernetes pod spec to a normalized Spec, grading
// its FIRST container. source labels the origin ("k8s" or "helm"); name is the
// workload's metadata.name (falls back to the container name). It is pure and
// shared by the single-manifest (SpecFromK8s) and multi-document/Helm paths.
func specFromPodSpec(source, name string, ps podSpec) Spec {
	c := ps.Containers[0]

	target := name
	if target == "" {
		target = c.Name
	}
	s := Spec{Source: source, Target: target, Runtime: ps.RuntimeClass}

	// --- user (container securityContext overrides pod-level) ---------------
	var nonRoot *bool
	var runAsUser *int64
	if ps.SecurityContext != nil {
		nonRoot = ps.SecurityContext.RunAsNonRoot
		runAsUser = ps.SecurityContext.RunAsUser
	}
	if c.SecurityContext != nil {
		if c.SecurityContext.RunAsNonRoot != nil {
			nonRoot = c.SecurityContext.RunAsNonRoot
		}
		if c.SecurityContext.RunAsUser != nil {
			runAsUser = c.SecurityContext.RunAsUser
		}
	}
	switch {
	case nonRoot != nil && *nonRoot:
		s.RunAsNonRoot = Yes
		s.User = "runAsNonRoot: true"
	case runAsUser != nil && *runAsUser != 0:
		s.RunAsNonRoot = Yes
		s.User = fmt.Sprintf("runAsUser: %d", *runAsUser)
	case (nonRoot != nil && !*nonRoot) || (runAsUser != nil && *runAsUser == 0):
		s.RunAsNonRoot = No
		s.User = "runAsUser: 0"
	default:
		s.RunAsNonRoot = Unknown // no runAsNonRoot/runAsUser: image default may be root
		s.Notes = append(s.Notes, "no runAsNonRoot/runAsUser set; the image default may be root")
	}

	// --- capabilities / privilege ------------------------------------------
	if c.SecurityContext != nil {
		s.Privileged = triFromPtr(c.SecurityContext.Privileged)
		if c.SecurityContext.ReadOnlyRootFilesystem != nil {
			s.ReadonlyRoot = boolTri(*c.SecurityContext.ReadOnlyRootFilesystem)
		}
		if c.SecurityContext.Capabilities != nil {
			for _, d := range c.SecurityContext.Capabilities.Drop {
				if strings.EqualFold(strings.TrimSpace(d), "ALL") {
					s.CapDropAll = Yes
				}
			}
			if s.CapDropAll == Unknown {
				s.CapDropAll = No
			}
			for _, a := range c.SecurityContext.Capabilities.Add {
				if a = strings.TrimSpace(a); a != "" {
					s.CapAdd = append(s.CapAdd, strings.ToUpper(a))
				}
			}
		}
		// --- seccomp (container overrides pod) ------------------------------
		if c.SecurityContext.SeccompProfile != nil {
			s.Seccomp = normalizeK8sSeccomp(c.SecurityContext.SeccompProfile.Type)
		}
	}
	if s.Seccomp == "" && ps.SecurityContext != nil && ps.SecurityContext.SeccompProfile != nil {
		s.Seccomp = normalizeK8sSeccomp(ps.SecurityContext.SeccompProfile.Type)
	}
	// Kubernetes does NOT apply a seccomp profile by default (pre-SeccompDefault):
	// an unset profile means unconfined. Leave Seccomp="" so it grades as unknown
	// (fail-closed) and note it.
	if s.Seccomp == "" {
		s.Notes = append(s.Notes, "no seccompProfile set; Kubernetes leaves the syscall surface unconfined by default")
	}

	// --- network ------------------------------------------------------------
	s.HostNetwork = triFromPtr(ps.HostNetwork)
	if s.HostNetwork == Yes {
		s.NetworkMode = "host"
	} else {
		// Pod networking is egress-capable unless a NetworkPolicy restricts it,
		// which is not visible in the pod spec. Grade conservatively.
		s.NetworkMode = "pod"
		s.Notes = append(s.Notes, "egress depends on a NetworkPolicy not visible in this manifest; graded as egress-capable")
	}

	// --- namespaces ---------------------------------------------------------
	s.HostPID = triFromPtr(ps.HostPID)
	s.HostIPC = triFromPtr(ps.HostIPC)

	// --- docker.sock / host mounts ------------------------------------------
	s.DockerSock = No
	sockVols := map[string]bool{}
	seen := map[string]bool{}
	for _, v := range ps.Volumes {
		if v.HostPath == nil {
			continue
		}
		p := v.HostPath.Path
		if isControlSocket(p) {
			s.DockerSock = Yes
			sockVols[v.Name] = true
		}
		if isSensitiveHostPath(p) && !seen[p] {
			seen[p] = true
			s.HostPathMounts = append(s.HostPathMounts, p)
		}
	}
	// Confirm the socket volume is actually mounted into the graded container.
	if s.DockerSock == Yes {
		mounted := false
		for _, vm := range c.VolumeMounts {
			if sockVols[vm.Name] || isControlSocket(vm.MountPath) {
				mounted = true
			}
		}
		if !mounted {
			// Declared as a volume but not mounted into this container: not an
			// exposure for the graded container.
			s.DockerSock = No
		}
	}

	return s
}

// normalizeK8sSeccomp maps a k8s seccompProfile.type to the Spec vocabulary.
func normalizeK8sSeccomp(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "runtimedefault", "localhost":
		return "confined"
	case "unconfined":
		return "unconfined"
	default:
		return ""
	}
}
