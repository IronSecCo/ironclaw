package keys

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// sealedStores returns one of each implementation for table-driven coverage.
func sealedStores(t *testing.T) map[string]SealedStore {
	t.Helper()
	fs, err := NewFileStore(filepath.Join(t.TempDir(), "sealed.json"))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]SealedStore{
		"mem":  NewMemStore(),
		"file": fs,
	}
}

func TestSealedStoreRoundTrip(t *testing.T) {
	for name, store := range sealedStores(t) {
		t.Run(name, func(t *testing.T) {
			blob := []byte("nonce-and-ciphertext")
			if err := store.Save("s1", blob); err != nil {
				t.Fatal(err)
			}
			got, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got["s1"], blob) {
				t.Fatalf("Load[s1] = %q, want %q", got["s1"], blob)
			}

			// Load returns a copy: mutating it must not affect the store.
			got["s1"][0] ^= 0xff
			again, _ := store.Load()
			if bytes.Equal(again["s1"], got["s1"]) {
				t.Fatal("Load did not return an isolated copy")
			}

			if err := store.Delete("s1"); err != nil {
				t.Fatal(err)
			}
			after, _ := store.Load()
			if _, ok := after["s1"]; ok {
				t.Fatal("Delete left the entry behind")
			}
			// Deleting an absent id is a no-op, not an error.
			if err := store.Delete("s1"); err != nil {
				t.Fatalf("Delete of absent id errored: %v", err)
			}
		})
	}
}

func TestFileStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sealed.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte{0x01, 0x02, 0x03, 0x04}
	if err := store.Save("sess-42", blob); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got["sess-42"], blob) {
		t.Fatalf("reopened blob = %x, want %x", got["sess-42"], blob)
	}

	// The file must be 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sealed-store mode = %04o, want 0600", perm)
	}
}

func TestFileStoreRejectsTooOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sealed.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("expected NewFileStore to reject a 0644 file")
	}
}

func TestFileStoreEmptyPath(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

// A sealed blob produced by a Custodian must not leak the plaintext session key
// onto disk through the FileStore.
func TestFileStoreHoldsNoPlaintextKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sealed.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewDurable(StaticKeySource([32]byte{9}), store)
	if err != nil {
		t.Fatal(err)
	}
	key, err := c.Generate(contract.SessionID("s"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, key[:]) {
		t.Fatal("sealed-store file contains the raw session key")
	}
}

var _ SealedStore = (*MemStore)(nil)
var _ SealedStore = (*FileStore)(nil)
