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

// TestBuildOCISpecModelProvider asserts the multi-provider selection is
// passed to the sandbox process only when set, and that the sealed default keeps
// the bare "/sandbox" args (the default Anthropic backend).
func TestBuildOCISpecModelProvider(t *testing.T) {
	// Default (HardenedSpec): bare args, no provider flags.
	sealed, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	if got := sealed.Process.Args; len(got) != 1 || got[0] != "/sandbox" {
		t.Fatalf("default args = %v, want [/sandbox] (sealed Anthropic default)", got)
	}

	// Provider selection set: flags appended in order after /sandbox.
	s := hardenedTestSpec()
	s.ModelProvider = "openai"
	s.ModelID = "gpt-4o"
	s.ModelHost = "api.openai.com"
	withProvider, err := BuildOCISpec(s)
	if err != nil {
		t.Fatalf("BuildOCISpec (provider): %v", err)
	}
	want := []string{"/sandbox", "--provider", "openai", "--model", "gpt-4o", "--model-host", "api.openai.com"}
	got := withProvider.Process.Args
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
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

// TestBuildOCISpecEgressSocket asserts the optional egress-broker socket
// is bound only when EgressSocket is set, that the default (HardenedSpec) stays
// sealed to the model proxy alone, and that network=none holds either way.
func TestBuildOCISpecEgressSocket(t *testing.T) {
	// Default (HardenedSpec): no egress socket mount.
	sealed, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	for _, m := range sealed.Mounts {
		if m.Destination == containerEgressSock {
			t.Fatal("egress socket must NOT be bound when EgressSocket is empty (sealed default)")
		}
	}

	// Opted in: the egress socket is bound rw at the fixed container path.
	spec := hardenedTestSpec()
	spec.EgressSocket = "/run/ironclaw/host/egress.sock"
	withEgress, err := BuildOCISpec(spec)
	if err != nil {
		t.Fatalf("BuildOCISpec (egress): %v", err)
	}
	var egress *OCIMount
	for i := range withEgress.Mounts {
		if withEgress.Mounts[i].Destination == containerEgressSock {
			egress = &withEgress.Mounts[i]
		}
	}
	if egress == nil {
		t.Fatal("egress socket mount missing when EgressSocket is set")
	}
	if egress.Source != "/run/ironclaw/host/egress.sock" {
		t.Fatalf("egress source = %q", egress.Source)
	}
	if !hasOption(egress.Options, "rw") {
		t.Fatalf("egress socket must be rw, options=%v", egress.Options)
	}
	// network=none must still hold with egress enabled — egress is a host-mediated
	// socket, not a NIC.
	for _, ns := range withEgress.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatal("network namespace must remain omitted even with egress enabled")
		}
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

// mountByDest returns the mount with the given container destination, or nil.
func mountByDest(spec *OCISpec, dest string) *OCIMount {
	for i := range spec.Mounts {
		if spec.Mounts[i].Destination == dest {
			return &spec.Mounts[i]
		}
	}
	return nil
}

func TestBuildOCISpecDurableWorkspace(t *testing.T) {
	s := hardenedTestSpec()
	s.WorkspacePath = "/var/lib/ironclaw/groups/g1/workspace"
	spec, err := BuildOCISpec(s)
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	ws := mountByDest(spec, containerWorkspace)
	if ws == nil {
		t.Fatal("missing /workspace mount")
	}
	if ws.Type != "bind" {
		t.Fatalf("durable workspace must be a bind, got type %q", ws.Type)
	}
	if ws.Source != s.WorkspacePath {
		t.Fatalf("workspace source = %q, want %q", ws.Source, s.WorkspacePath)
	}
	if !hasOption(ws.Options, "rw") || hasOption(ws.Options, "ro") {
		t.Fatalf("durable workspace must be rw, options=%v", ws.Options)
	}
	for _, o := range []string{"nosuid", "nodev", "noexec"} {
		if !hasOption(ws.Options, o) {
			t.Fatalf("durable workspace must carry %q, options=%v", o, ws.Options)
		}
	}
}

func TestBuildOCISpecLegacyTmpfsWorkspace(t *testing.T) {
	// With no WorkspacePath, /workspace stays the ephemeral tmpfs (back-compat).
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	ws := mountByDest(spec, containerWorkspace)
	if ws == nil || ws.Type != "tmpfs" {
		t.Fatalf("unset WorkspacePath must keep a tmpfs workspace, got %+v", ws)
	}
}

func TestBuildOCISpecMemoryAndShared(t *testing.T) {
	s := hardenedTestSpec()
	s.MemoryPath = "/var/lib/ironclaw/groups/g1/memory"
	s.SharedReadOnlyPath = "/var/lib/ironclaw/shared"
	spec, err := BuildOCISpec(s)
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}

	mem := mountByDest(spec, containerMemory)
	if mem == nil || mem.Type != "bind" || mem.Source != s.MemoryPath {
		t.Fatalf("missing/incorrect /memory bind: %+v", mem)
	}
	if !hasOption(mem.Options, "rw") || hasOption(mem.Options, "ro") {
		t.Fatalf("/memory must be rw, options=%v", mem.Options)
	}

	shared := mountByDest(spec, containerShared)
	if shared == nil || shared.Type != "bind" || shared.Source != s.SharedReadOnlyPath {
		t.Fatalf("missing/incorrect /shared bind: %+v", shared)
	}
	if !hasOption(shared.Options, "ro") || hasOption(shared.Options, "rw") {
		t.Fatalf("/shared must be READ-ONLY, options=%v", shared.Options)
	}
}

func TestBuildOCISpecOmitsUnsetDurableMounts(t *testing.T) {
	spec, err := BuildOCISpec(hardenedTestSpec())
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}
	if m := mountByDest(spec, containerMemory); m != nil {
		t.Fatalf("/memory must be omitted when MemoryPath is empty, got %+v", m)
	}
	if m := mountByDest(spec, containerShared); m != nil {
		t.Fatalf("/shared must be omitted when SharedReadOnlyPath is empty, got %+v", m)
	}
}

func TestHardenedSpecWithStorage(t *testing.T) {
	s := HardenedSpecWithStorage("ses_x", "img:latest", "/in.db", "/out.db", "/p.sock", "/ws", "/mem", "/shared")
	if s.WorkspacePath != "/ws" || s.MemoryPath != "/mem" || s.SharedReadOnlyPath != "/shared" {
		t.Fatalf("storage paths not set: %+v", s)
	}
	// It must still be fully hardened.
	if !s.NetworkNone || !s.DropAllCaps || !s.NoNewPrivs || !s.ReadOnlyRootfs || s.NonRootUID <= 0 {
		t.Fatalf("HardenedSpecWithStorage must stay hardened: %+v", s)
	}
}

func TestLaunchCreatesDurableStorage(t *testing.T) {
	base := t.TempDir()
	s := hardenedTestSpec()
	s.WorkspacePath = filepath.Join(base, "groups", "g1", "workspace")
	s.MemoryPath = filepath.Join(base, "groups", "g1", "memory")

	r := NewRunsc(WithBundleRoot(filepath.Join(base, "bundles")), WithProvisioner(&fakeProvisioner{}))
	// Launch will fail to exec an absent runtime binary, but the durable dirs must be
	// created before that point.
	_, _ = r.Launch(context.Background(), s)

	for _, p := range []string{s.WorkspacePath, s.MemoryPath} {
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			t.Fatalf("durable storage dir %s not created: err=%v", p, err)
		}
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
