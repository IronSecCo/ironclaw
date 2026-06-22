package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func catalogBundle(t *testing.T, root, name, version string) {
	t.Helper()
	dir := filepath.Join(root, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte("apiVersion: x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogList(t *testing.T) {
	root := t.TempDir()
	catalogBundle(t, root, "incident-triage", "1.4.0")
	catalogBundle(t, root, "incident-triage", "1.5.0")
	catalogBundle(t, root, "status-page", "0.1.0")
	// Noise that must be ignored: a non-skill dir, a bad version, a loose file.
	if err := os.MkdirAll(filepath.Join(root, "UPPER", "1.0.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ok", "no manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := (DirSource{Root: root}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []SkillRef{
		{"incident-triage", "1.4.0"}, {"incident-triage", "1.5.0"}, {"status-page", "0.1.0"},
	}
	if len(refs) != len(want) {
		t.Fatalf("got %d refs %v, want %d", len(refs), refs, len(want))
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("ref %d = %v, want %v", i, refs[i], want[i])
		}
	}
}

func TestCatalogListEmptyRoot(t *testing.T) {
	if _, err := (DirSource{}).List(); err == nil {
		t.Error("List on an unconfigured DirSource should error")
	}
	// An empty but valid root lists nothing without error.
	refs, err := (DirSource{Root: t.TempDir()}).List()
	if err != nil || len(refs) != 0 {
		t.Fatalf("empty catalog: refs=%v err=%v", refs, err)
	}
}

func TestCatalogRemoveVersion(t *testing.T) {
	root := t.TempDir()
	catalogBundle(t, root, "triage", "1.0.0")
	catalogBundle(t, root, "triage", "2.0.0")

	if err := (DirSource{Root: root}).Remove("triage", "1.0.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "triage", "1.0.0")); !os.IsNotExist(err) {
		t.Error("removed version still present")
	}
	if _, err := os.Stat(filepath.Join(root, "triage", "2.0.0")); err != nil {
		t.Error("other version must remain")
	}
}

func TestCatalogRemoveAllVersions(t *testing.T) {
	root := t.TempDir()
	catalogBundle(t, root, "triage", "1.0.0")
	catalogBundle(t, root, "triage", "2.0.0")

	if err := (DirSource{Root: root}).Remove("triage", ""); err != nil {
		t.Fatalf("Remove all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "triage")); !os.IsNotExist(err) {
		t.Error("whole skill dir should be gone")
	}
}

func TestCatalogRemoveRejects(t *testing.T) {
	root := t.TempDir()
	catalogBundle(t, root, "triage", "1.0.0")
	d := DirSource{Root: root}

	// Absent bundle.
	if err := d.Remove("triage", "9.9.9"); err == nil {
		t.Error("removing an absent version should error")
	}
	if err := d.Remove("ghost", ""); err == nil {
		t.Error("removing an absent skill should error")
	}
	// Unsafe identifiers.
	for _, c := range [][2]string{{"../etc", ""}, {"a/b", "1"}, {"ok", "../../x"}, {"", ""}} {
		if err := d.Remove(c[0], c[1]); err == nil {
			t.Errorf("Remove accepted unsafe identifier name=%q version=%q", c[0], c[1])
		}
	}
	// The valid bundle must survive all the rejected attempts.
	if _, err := os.Stat(filepath.Join(root, "triage", "1.0.0")); err != nil {
		t.Error("valid bundle must be untouched by rejected removes")
	}
}

// TestCatalogRemoveRejectsTraversal proves Remove cannot delete anything outside
// the catalog root: every traversal/absolute payload is refused and a sentinel
// directory placed next to (but outside) the root survives. This exercises the
// filepath.IsLocal confinement barrier on the destructive os.RemoveAll path.
func TestCatalogRemoveRejectsTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A sentinel a traversal would try to delete.
	sentinel := filepath.Join(parent, "sentinel")
	if err := os.MkdirAll(sentinel, 0o755); err != nil {
		t.Fatal(err)
	}
	catalogBundle(t, root, "triage", "1.0.0")
	d := DirSource{Root: root}

	traversals := [][2]string{
		{"..", "sentinel"},
		{"../sentinel", ""},
		{"../..", ""},
		{"../../../../../../tmp", ""},
		{"/etc", ""},
		{"ok", "/etc"},
		{"ok", "../../sentinel"},
		{".", ""},
		{"ok", ".."},
	}
	for _, c := range traversals {
		if err := d.Remove(c[0], c[1]); err == nil {
			t.Errorf("Remove(%q,%q) was not rejected", c[0], c[1])
		}
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("a traversal Remove deleted the out-of-root sentinel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "triage", "1.0.0")); err != nil {
		t.Error("valid bundle must survive rejected traversal removes")
	}
}
