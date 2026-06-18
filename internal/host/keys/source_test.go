package keys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStaticKeySource(t *testing.T) {
	var m [32]byte
	m[0] = 7
	got, err := StaticKeySource(m).Master()
	if err != nil {
		t.Fatal(err)
	}
	if got != m {
		t.Fatalf("StaticKeySource returned %x, want %x", got, m)
	}
}

func TestFileKeySourceCreateThenLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "master.key")

	src, err := NewFileKeySource(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created, err := src.Master()
	if err != nil {
		t.Fatal(err)
	}
	if created == ([32]byte{}) {
		t.Fatal("created master is all-zero")
	}

	// The file must exist at 0600 and round-trip on reopen.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("keystore mode = %04o, want 0600", perm)
	}

	reopened, err := NewFileKeySource(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	loaded, err := reopened.Master()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != created {
		t.Fatalf("master not durable: reopened %x != created %x", loaded, created)
	}
}

func TestFileKeySourceRotate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	src, err := NewFileKeySource(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := src.Master()

	rotated, err := src.Rotate()
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated == before {
		t.Fatal("rotate returned the same master")
	}
	now, _ := src.Master()
	if now != rotated {
		t.Fatalf("Master after rotate = %x, want rotated %x", now, rotated)
	}

	// The rotated master must be the one persisted on disk.
	reopened, err := NewFileKeySource(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, _ := reopened.Master()
	if persisted != rotated {
		t.Fatalf("persisted master %x != rotated %x", persisted, rotated)
	}
	if persisted == before {
		t.Fatal("rotation did not persist a new master")
	}
}

func TestFileKeySourceRejectsTooOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	// A well-formed file but with group/other-readable permissions must be refused.
	buf := append([]byte(masterFileMagic), make([]byte, 32)...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileKeySource(path); err == nil {
		t.Fatal("expected NewFileKeySource to reject a 0644 keystore")
	}
}

func TestFileKeySourceRejectsMalformed(t *testing.T) {
	cases := map[string][]byte{
		"bad magic":  append([]byte("XXXXXXXXXX"), make([]byte, 32)...),
		"short":      []byte(masterFileMagic),
		"wrong size": append([]byte(masterFileMagic), make([]byte, 8)...),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "master.key")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewFileKeySource(path); err == nil {
				t.Fatalf("expected rejection of %s keystore", name)
			}
		})
	}
}

func TestFileKeySourceEmptyPath(t *testing.T) {
	if _, err := NewFileKeySource(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// FileKeySource must satisfy the RotatableKeySource (and thus KeySource) contract.
var _ RotatableKeySource = (*FileKeySource)(nil)
var _ KeySource = StaticKeySource{}
