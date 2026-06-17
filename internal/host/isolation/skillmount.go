// OWNER: T-227d (skills system — read-only skill asset mount)

package isolation

// A skill is a host-curated *capability bundle*, never code that runs in the
// sandbox (see .agents/spikes/skills-system.md §2). Its bundled reference assets
// (templates, runbooks, schemas) are mounted read-only at /skills/<name>, mirroring
// the global /shared mount in oci.go and carrying the same hardening — ro so the
// sandbox can never write them, and nosuid,nodev,noexec so the mount can never
// carry privilege or be used to execute code. The assets are data; the read-only
// rootfs and the no-interpreter invariant are untouched.
//
// This file is the mount primitive (T-227d): it builds and appends those mounts to
// an already-hardened OCI spec. It deliberately layers ON TOP of BuildOCISpec
// rather than changing it, so the sealed default (no skills) is byte-for-byte
// unchanged and skills are a purely additive, opt-in capability. Wiring this into
// the per-group sandbox launch is the integration step that follows.

import (
	"fmt"
	"path/filepath"
)

// containerSkillsBase is the fixed container-side parent for skill asset mounts.
// Each installed skill's assets appear at /skills/<name>, a namespace distinct
// from /workspace, /memory, and /shared.
const containerSkillsBase = "/skills"

// skillMountOptions are the bind options every skill asset mount carries: the
// read-only sibling of the /shared mount's options. noexec is load-bearing here —
// it is what keeps a skill's bundled files data rather than a code-execution path.
var skillMountOptions = []string{"bind", "ro", "nosuid", "nodev", "noexec"}

// SkillMount is one installed skill's bundled, read-only asset directory. HostPath
// is the host-side directory of already-verified assets — the host fetches and
// signature-verifies a skill bundle before it ever reaches a sandbox (T-227b) — and
// Name is the skill's lowercase DNS-label name, which fixes its mount point at
// /skills/<name>.
type SkillMount struct {
	Name     string
	HostPath string
}

// Destination is the fixed container path the skill's assets are bound at.
func (m SkillMount) Destination() string {
	return containerSkillsBase + "/" + m.Name
}

func (m SkillMount) validate() error {
	if !validSkillName(m.Name) {
		return fmt.Errorf("host/isolation: invalid skill name %q (want a non-empty lowercase DNS label: a-z, 0-9, -)", m.Name)
	}
	if m.HostPath == "" || !filepath.IsAbs(m.HostPath) {
		return fmt.Errorf("host/isolation: skill %q asset path must be a non-empty absolute path, got %q", m.Name, m.HostPath)
	}
	return nil
}

// ociMount builds the read-only OCI bind mount for the skill's assets.
func (m SkillMount) ociMount() OCIMount {
	return OCIMount{
		Destination: m.Destination(),
		Type:        "bind",
		Source:      m.HostPath,
		// Copy the shared options slice so callers can never alias/mutate it.
		Options: append([]string(nil), skillMountOptions...),
	}
}

// AddSkillMounts appends each skill's read-only asset mount to spec.Mounts. It is
// the optional, composable counterpart to the /shared mount: build a hardened spec
// with BuildOCISpec, then layer in whatever skills an agent group has installed.
// Called with no skills it leaves spec untouched, so the sealed default is
// unchanged.
//
// It fails closed and atomically: an invalid name, a non-absolute host path, or a
// destination that collides with an existing mount (including a duplicate skill
// name within the call) is an error, and on any error spec is left unmodified.
// Every appended mount is ro,nosuid,nodev,noexec under /skills — outside the
// read-only rootfs and never executable.
func AddSkillMounts(spec *OCISpec, skills ...SkillMount) error {
	if spec == nil {
		return fmt.Errorf("host/isolation: AddSkillMounts requires a non-nil spec")
	}
	if len(skills) == 0 {
		return nil
	}

	// Validate and build the whole set before touching spec.Mounts, so a bad entry
	// can never leave a half-applied set of mounts behind.
	seen := make(map[string]struct{}, len(spec.Mounts)+len(skills))
	for _, existing := range spec.Mounts {
		seen[existing.Destination] = struct{}{}
	}
	built := make([]OCIMount, 0, len(skills))
	for _, s := range skills {
		if err := s.validate(); err != nil {
			return err
		}
		dest := s.Destination()
		if _, dup := seen[dest]; dup {
			return fmt.Errorf("host/isolation: skill mount destination %q collides with an existing mount", dest)
		}
		seen[dest] = struct{}{}
		built = append(built, s.ociMount())
	}

	spec.Mounts = append(spec.Mounts, built...)
	return nil
}

// validSkillName accepts a non-empty lowercase DNS-style label (a-z, 0-9, -; no
// leading/trailing -), matching the name the host-side skills loader validates
// (internal/host/skills). Restricting the name is what keeps /skills/<name>
// confined: it can contain no slash and no "..", so a skill mount can only ever
// resolve directly under /skills.
func validSkillName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return s[0] != '-' && s[len(s)-1] != '-'
}
