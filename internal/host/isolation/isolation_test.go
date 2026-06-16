// OWNER: AGENT1

package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func hardenedTestSpec() SandboxSpec {
	return HardenedSpec(
		"ses_test",
		"ghcr.io/example/sandbox:latest",
		"/host/data/ses_test/inbound.db",
		"/host/data/ses_test/outbound.db",
		"/run/ironclaw/modelproxy.sock",
	)
}

func TestBuildOCISpecHardening(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}

	// network=none: no "network" namespace present.
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatalf("network namespace must be omitted for network=none, got %+v", spec.Linux.Namespaces)
		}
	}
	// A user namespace must be present (non-root userns mapping).
	hasUser := false
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == "user" {
			hasUser = true
		}
	}
	if !hasUser {
		t.Fatal("expected a user namespace")
	}
	if len(spec.Linux.UIDMappings) == 0 || len(spec.Linux.GIDMappings) == 0 {
		t.Fatal("expected uid/gid mappings for the user namespace")
	}

	// Empty capability sets — all five.
	c := spec.Process.Capabilities
	if c == nil {
		t.Fatal("capabilities must be present (and empty)")
	}
	if len(c.Bounding) != 0 || len(c.Effective) != 0 || len(c.Permitted) != 0 || len(c.Inheritable) != 0 || len(c.Ambient) != 0 {
		t.Fatalf("all capability sets must be empty, got %+v", c)
	}

	// no_new_privs.
	if !spec.Process.NoNewPrivileges {
		t.Fatal("noNewPrivileges must be true")
	}
	// non-root uid and non-zero gid.
	if spec.Process.User.UID <= 0 {
		t.Fatalf("uid must be non-root, got %d", spec.Process.User.UID)
	}
	if spec.Process.User.GID <= 0 {
		t.Fatalf("gid must be non-zero, got %d", spec.Process.User.GID)
	}
	// read-only rootfs.
	if !spec.Root.Readonly {
		t.Fatal("rootfs must be read-only")
	}
}

func TestBuildOCISpecMounts(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}

	var inbound, outbound, workspace, proxy *OCIMount
	for i := range spec.Mounts {
		switch spec.Mounts[i].Destination {
		case containerInboundPath:
			inbound = &spec.Mounts[i]
		case containerOutboundPath:
			outbound = &spec.Mounts[i]
		case containerWorkspace:
			workspace = &spec.Mounts[i]
		case containerModelProxySock:
			proxy = &spec.Mounts[i]
		}
	}

	if inbound == nil || outbound == nil || workspace == nil || proxy == nil {
		t.Fatalf("missing a required mount: in=%v out=%v ws=%v proxy=%v", inbound, outbound, workspace, proxy)
	}

	if !hasOption(inbound.Options, "ro") || hasOption(inbound.Options, "rw") {
		t.Fatalf("inbound mount must be read-only, options=%v", inbound.Options)
	}
	if !hasOption(outbound.Options, "rw") || hasOption(outbound.Options, "ro") {
		t.Fatalf("outbound mount must be read-write, options=%v", outbound.Options)
	}
	if workspace.Type != "tmpfs" {
		t.Fatalf("workspace must be a writable tmpfs, type=%q", workspace.Type)
	}
	if inbound.Source != "/host/data/ses_test/inbound.db" {
		t.Fatalf("inbound source = %q", inbound.Source)
	}
	if outbound.Source != "/host/data/ses_test/outbound.db" {
		t.Fatalf("outbound source = %q", outbound.Source)
	}
}

func TestBuildOCISpecRejectsWeakKnobs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SandboxSpec)
	}{
		{"network not none", func(s *SandboxSpec) { s.NetworkNone = false }},
		{"caps not dropped", func(s *SandboxSpec) { s.DropAllCaps = false }},
		{"no_new_privs off", func(s *SandboxSpec) { s.NoNewPrivs = false }},
		{"rootfs writable", func(s *SandboxSpec) { s.ReadOnlyRootfs = false }},
		{"root uid", func(s *SandboxSpec) { s.NonRootUID = 0 }},
		{"missing inbound", func(s *SandboxSpec) { s.ReadOnlyInboundPath = "" }},
		{"missing outbound", func(s *SandboxSpec) { s.ReadWriteOutboundPath = "" }},
		{"missing proxy socket", func(s *SandboxSpec) { s.ModelProxySocket = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := hardenedTestSpec()
			tt.mutate(&s)
			if _, err := BuildOCISpec(s); err == nil {
				t.Fatalf("expected BuildOCISpec to reject %s", tt.name)
			}
		})
	}
}

func TestWriteBundleWritesConfig(t *testing.T) {
	dir := t.TempDir()
	r := NewRunsc(WithBundleRoot(dir), WithRuntimeBinary("runsc"))
	bundleDir, err := r.WriteBundle(hardenedTestSpec())
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	cfgPath := filepath.Join(bundleDir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var got OCISpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}
	if got.OCIVersion == "" {
		t.Fatal("config.json missing ociVersion")
	}
	if !got.Root.Readonly {
		t.Fatal("config.json rootfs should be read-only")
	}
	// The bundle dir is named for the session.
	if filepath.Base(bundleDir) != "ses_test" {
		t.Fatalf("bundle dir = %q, want it to end in the session id", bundleDir)
	}
}

func TestNewRunscDefaults(t *testing.T) {
	r := NewRunsc()
	if r.RuntimeBinary != DefaultRuntimeBinary {
		t.Fatalf("default runtime = %q, want %q", r.RuntimeBinary, DefaultRuntimeBinary)
	}
	if r.BundleRoot == "" {
		t.Fatal("default BundleRoot must not be empty")
	}
}

func TestLaunchRequiresProvisionedRootfs(t *testing.T) {
	dir := t.TempDir()
	r := NewRunsc(WithBundleRoot(dir))
	_, err := r.Launch(context.Background(), hardenedTestSpec())
	if err == nil {
		t.Fatal("expected Launch to fail without a provisioned rootfs")
	}
	if !errors.Is(err, ErrRootfsMissing) {
		t.Fatalf("expected ErrRootfsMissing, got %v", err)
	}
	// config.json should still have been written even though launch did not proceed.
	if _, statErr := os.Stat(filepath.Join(dir, "ses_test", "config.json")); statErr != nil {
		t.Fatalf("config.json should be written before the rootfs check: %v", statErr)
	}
}

func TestStopSafeWhenRuntimeAbsent(t *testing.T) {
	// A handle pointing at a non-existent runtime binary must return a wrapped error
	// from Stop, never panic.
	h := &runscHandle{runtimeBinary: "definitely-not-a-real-binary-xyz", containerID: "ironclaw-ses_test"}
	if err := h.Stop(context.Background()); err == nil {
		t.Fatal("expected an error stopping with an absent runtime binary")
	}
}

func hasOption(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}
