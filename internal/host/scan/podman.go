package scan

import (
	"encoding/json"
	"fmt"
	"strings"
)

// podmanInspect is the subset of `podman inspect <container>` we grade. Podman's
// Config/HostConfig mirror Docker's for CLI compatibility, but two fields matter
// specifically here and live OUTSIDE the Docker-compatible shape:
//
//   - OCIRuntime is the ACTUAL low-level runtime ("crun", "runc", "runsc",
//     "kata"…). Podman's HostConfig.Runtime is a generic "oci" string, so the
//     hardened-runtime detection must read OCIRuntime instead.
//   - HostConfig.IDMappings carries the user-namespace uid/gid maps. A non-zero
//     host base in the uid map means container-uid 0 is remapped to an
//     unprivileged host uid — i.e. rootless / userns isolation, a real posture
//     win that the scorer credits.
type podmanInspect struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	OCIRuntime string `json:"OCIRuntime"`
	Config     struct {
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
		IDMappings     struct {
			UidMap []string `json:"UidMap"`
			GidMap []string `json:"GidMap"`
		} `json:"IDMappings"`
	} `json:"HostConfig"`
	// EffectiveCaps/BoundingCaps are the ACTUAL granted capability set (podman
	// top-level). A non-nil-but-empty set is the authoritative "no caps" signal;
	// null means podman did not report it and we fall back to the drop list.
	EffectiveCaps []string `json:"EffectiveCaps"`
	BoundingCaps  []string `json:"BoundingCaps"`
	Mounts        []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	} `json:"Mounts"`
}

// podmanDefaultCaps is the standard containers.conf default_capabilities set that
// podman grants when nothing is dropped. Podman expands `--cap-drop ALL` into
// exactly these named caps in HostConfig.CapDrop rather than emitting the "ALL"
// sentinel that Docker uses, so a drop list that COVERS this whole set leaves an
// empty effective capability set — semantically identical to cap_drop=[ALL].
var podmanDefaultCaps = []string{
	"CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "KILL", "NET_BIND_SERVICE",
	"SETFCAP", "SETGID", "SETPCAP", "SETUID", "SYS_CHROOT",
}

// normCap upper-cases a capability and strips the CAP_ prefix for comparison.
func normCap(c string) string {
	return strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(c)), "CAP_")
}

// podmanDropsAll reports whether a podman container has effectively dropped ALL
// capabilities, accounting for podman's habit of expanding `--cap-drop ALL` into
// the explicit default set. Signals, strongest first: the literal ALL sentinel;
// a non-nil-but-empty EffectiveCaps/BoundingCaps set; or a drop list that covers
// the entire default capability set. Fail-closed: partial coverage is NOT "all".
func podmanDropsAll(capDrop, effectiveCaps, boundingCaps []string) bool {
	dropped := map[string]bool{}
	for _, c := range capDrop {
		n := normCap(c)
		if n == "ALL" {
			return true
		}
		dropped[n] = true
	}
	// An explicitly reported empty granted set is authoritative.
	if boundingCaps != nil && len(boundingCaps) == 0 {
		return true
	}
	if effectiveCaps != nil && len(effectiveCaps) == 0 {
		return true
	}
	if len(dropped) == 0 {
		return false
	}
	for _, def := range podmanDefaultCaps {
		if !dropped[def] {
			return false
		}
	}
	return true
}

// SpecFromPodmanInspect parses `podman inspect <container>` into a Spec.
//
// rootlessHint carries the daemon-level rootless signal the CLI obtains from
// `podman info` (Yes/No), or Unknown when it was not probed. It is OR-combined
// with the per-container evidence in HostConfig.IDMappings so the adapter still
// detects userns remapping from the inspect JSON alone (which keeps it pure and
// unit-testable with fixtures).
func SpecFromPodmanInspect(raw []byte, rootlessHint Tristate) (Spec, error) {
	var arr []podmanInspect
	if err := json.Unmarshal(raw, &arr); err != nil {
		var one podmanInspect
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return Spec{}, fmt.Errorf("parse podman inspect: %w", err)
		}
		arr = []podmanInspect{one}
	}
	if len(arr) == 0 {
		return Spec{}, fmt.Errorf("podman inspect returned no containers")
	}
	p := arr[0]

	// Build the shared Docker-family fields, then layer podman specifics on top.
	f := dockerLikeFields{
		id:             p.ID,
		name:           p.Name,
		user:           p.Config.User,
		image:          p.Config.Image,
		runtime:        firstNonEmpty(p.OCIRuntime, p.HostConfig.Runtime),
		networkMode:    p.HostConfig.NetworkMode,
		pidMode:        p.HostConfig.PidMode,
		ipcMode:        p.HostConfig.IpcMode,
		privileged:     p.HostConfig.Privileged,
		readonlyRootfs: p.HostConfig.ReadonlyRootfs,
		capAdd:         p.HostConfig.CapAdd,
		capDrop:        p.HostConfig.CapDrop,
		securityOpt:    p.HostConfig.SecurityOpt,
		// (capDrop is normalized to the "ALL" sentinel just below when podman
		// expanded a cap-drop-all into the explicit default set.)
		binds: p.HostConfig.Binds,
	}
	for _, m := range p.Mounts {
		f.mounts = append(f.mounts, mountPair{m.Source, m.Destination})
	}

	// Normalize podman's expanded cap-drop back to the canonical "ALL" sentinel
	// the shared grader understands, so a rootless/hardened podman container is
	// credited for dropping every capability. Preserve any explicit CapAdd so the
	// grader still applies the "dropped ALL but re-added N" partial-credit path.
	if podmanDropsAll(p.HostConfig.CapDrop, p.EffectiveCaps, p.BoundingCaps) {
		f.capDrop = []string{"ALL"}
	}

	// Rootless / userns: prefer the explicit hint, fall back to the uid-map.
	hostUID, remapped := userNSHostUID(p.HostConfig.IDMappings.UidMap)
	switch {
	case rootlessHint == Yes || remapped:
		f.rootless = Yes
		f.userNSHostUID = hostUID
	case rootlessHint == No:
		f.rootless = No
	default:
		f.rootless = Unknown
	}

	s := specFromDockerLike(f, "podman")
	if s.Rootless == Yes {
		s.Notes = append(s.Notes, "rootless podman: container-uid 0 is remapped to an unprivileged host uid via a user namespace")
	}
	return s, nil
}

// userNSHostUID inspects a podman uid map (entries "containerID:hostID:size",
// e.g. "0:100000:65536") and reports the host uid that container-uid 0 maps to,
// plus whether that mapping is a real remap (host base != 0). An empty map, or a
// 0:0 identity map, means no remap (not rootless).
func userNSHostUID(uidMap []string) (string, bool) {
	for _, e := range uidMap {
		parts := strings.Split(strings.TrimSpace(e), ":")
		if len(parts) != 3 {
			continue
		}
		containerID := strings.TrimSpace(parts[0])
		hostID := strings.TrimSpace(parts[1])
		if containerID != "0" {
			continue
		}
		if hostID != "" && hostID != "0" {
			return hostID, true
		}
	}
	return "", false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
