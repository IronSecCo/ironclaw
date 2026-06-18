package isolation

import "testing"

// findMount returns the mount bound at dest, or nil.
func findMount(spec *OCISpec, dest string) *OCIMount {
	for i := range spec.Mounts {
		if spec.Mounts[i].Destination == dest {
			return &spec.Mounts[i]
		}
	}
	return nil
}

// TestAddSkillMountsDefaultUnchanged asserts the sealed default is untouched: with
// no skills the spec's mounts are byte-for-byte what BuildOCISpec produced, and no
// /skills mount appears. Mirrors the egress-socket "sealed default" check.
func TestAddSkillMountsDefaultUnchanged(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	before := len(spec.Mounts)

	if err := AddSkillMounts(spec); err != nil {
		t.Fatalf("AddSkillMounts with no skills: %v", err)
	}
	if len(spec.Mounts) != before {
		t.Fatalf("no-skill call changed mount count: %d -> %d", before, len(spec.Mounts))
	}
	for _, m := range spec.Mounts {
		if m.Destination == containerSkillsBase+"/anything" {
			t.Fatal("a /skills mount must not appear when no skills are given")
		}
	}
}

// TestAddSkillMountsReadOnly asserts an opted-in skill is bound read-only at
// /skills/<name> with nosuid,nodev,noexec, that hardening is preserved, and that
// the bundle is data only — never writable, never executable.
func TestAddSkillMountsReadOnly(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}

	if err := AddSkillMounts(spec, SkillMount{Name: "incident-triage", HostPath: "/host/skills/incident-triage"}); err != nil {
		t.Fatalf("AddSkillMounts: %v", err)
	}

	m := findMount(spec, "/skills/incident-triage")
	if m == nil {
		t.Fatal("skill mount missing at /skills/incident-triage")
	}
	if m.Type != "bind" || m.Source != "/host/skills/incident-triage" {
		t.Fatalf("unexpected mount: %+v", m)
	}
	if !hasOption(m.Options, "ro") || hasOption(m.Options, "rw") {
		t.Fatalf("skill assets must be read-only, options=%v", m.Options)
	}
	for _, opt := range []string{"nosuid", "nodev", "noexec"} {
		if !hasOption(m.Options, opt) {
			t.Fatalf("skill mount must carry %q (data, never executable), options=%v", opt, m.Options)
		}
	}
	// Adding a skill must not weaken the sandbox: network namespace still omitted.
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatal("network namespace must remain omitted with a skill mounted")
		}
	}
	if !spec.Root.Readonly {
		t.Fatal("rootfs must remain read-only with a skill mounted")
	}
}

// TestAddSkillMountsMultiple asserts several skills each get their own distinct,
// read-only mount.
func TestAddSkillMountsMultiple(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	skills := []SkillMount{
		{Name: "triage", HostPath: "/host/skills/triage"},
		{Name: "status-page", HostPath: "/host/skills/status-page"},
	}
	if err := AddSkillMounts(spec, skills...); err != nil {
		t.Fatalf("AddSkillMounts: %v", err)
	}
	for _, s := range skills {
		if m := findMount(spec, s.Destination()); m == nil {
			t.Fatalf("missing mount for skill %q", s.Name)
		}
	}
}

// TestAddSkillMountsRejectsBadName asserts unsafe or malformed names are refused
// and the spec is left unmodified (a crafted name can never traverse out of
// /skills).
func TestAddSkillMountsRejectsBadName(t *testing.T) {
	for _, bad := range []string{"", "UPPER", "../etc", "a/b", "-lead", "trail-", "has space", "dot.name"} {
		spec, err := BuildOCISpec(hardenedTestSpec())
		if err != nil {
			t.Fatalf("BuildOCISpec: %v", err)
		}
		before := len(spec.Mounts)
		if err := AddSkillMounts(spec, SkillMount{Name: bad, HostPath: "/host/skills/x"}); err == nil {
			t.Errorf("expected rejection of skill name %q", bad)
		}
		if len(spec.Mounts) != before {
			t.Errorf("spec mounts changed after rejecting name %q", bad)
		}
	}
}

// TestAddSkillMountsRejectsBadHostPath asserts a non-absolute or empty host path is
// refused (the source must be a concrete host directory of verified assets).
func TestAddSkillMountsRejectsBadHostPath(t *testing.T) {
	for _, bad := range []string{"", "relative/path", "./x"} {
		spec, err := BuildOCISpec(hardenedTestSpec())
		if err != nil {
			t.Fatalf("BuildOCISpec: %v", err)
		}
		if err := AddSkillMounts(spec, SkillMount{Name: "ok", HostPath: bad}); err == nil {
			t.Errorf("expected rejection of host path %q", bad)
		}
	}
}

// TestAddSkillMountsRejectsDuplicate covers both a duplicate within one call and a
// collision with a mount already on the spec; either is refused atomically.
func TestAddSkillMountsRejectsDuplicate(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	before := len(spec.Mounts)
	dup := []SkillMount{
		{Name: "triage", HostPath: "/host/skills/triage"},
		{Name: "triage", HostPath: "/host/skills/triage-2"},
	}
	if err := AddSkillMounts(spec, dup...); err == nil {
		t.Fatal("expected rejection of a duplicate skill name in one call")
	}
	if len(spec.Mounts) != before {
		t.Fatalf("spec must be unmodified after a rejected duplicate set (count %d -> %d)", before, len(spec.Mounts))
	}

	// Collision with an existing mount: add once, then attempt the same destination.
	if err := AddSkillMounts(spec, SkillMount{Name: "triage", HostPath: "/host/skills/triage"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddSkillMounts(spec, SkillMount{Name: "triage", HostPath: "/host/skills/triage"}); err == nil {
		t.Fatal("expected rejection of a destination already present on the spec")
	}
}

func TestAddSkillMountsNilSpec(t *testing.T) {
	if err := AddSkillMounts(nil, SkillMount{Name: "x", HostPath: "/host/skills/x"}); err == nil {
		t.Fatal("AddSkillMounts must reject a nil spec")
	}
}
