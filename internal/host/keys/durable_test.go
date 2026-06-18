package keys

import (
	"path/filepath"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// openDurable opens a Custodian over the file-backed master + sealed store at the
// given paths, the way the control plane would on each boot.
func openDurable(t *testing.T, masterPath, storePath string) *Custodian {
	t.Helper()
	src, err := NewFileKeySource(masterPath)
	if err != nil {
		t.Fatalf("key source: %v", err)
	}
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatalf("sealed store: %v", err)
	}
	c, err := NewDurable(src, store)
	if err != nil {
		t.Fatalf("custodian: %v", err)
	}
	return c
}

// A session key generated before a restart must still be retrievable after the
// control plane reopens the durable master + sealed store.
func TestSessionKeySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	masterPath := filepath.Join(dir, "master.key")
	storePath := filepath.Join(dir, "sealed.json")

	c1 := openDurable(t, masterPath, storePath)
	key, err := c1.Generate(contract.SessionID("sess-1"))
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a control-plane restart: brand-new objects over the same files.
	c2 := openDurable(t, masterPath, storePath)
	got, ok := c2.Get(contract.SessionID("sess-1"))
	if !ok {
		t.Fatal("session key did not survive restart")
	}
	if got != key {
		t.Fatalf("restored key %x != original %x", got, key)
	}

	// An unknown session is still reported missing.
	if _, ok := c2.Get(contract.SessionID("nope")); ok {
		t.Fatal("unexpected key for unknown session")
	}
}

// An ephemeral (StaticKeySource + MemStore) custodian must NOT retain keys when a
// fresh custodian is built — this guards that the durability above comes from the
// file backends, not accidental shared state.
func TestEphemeralCustodianDoesNotPersist(t *testing.T) {
	master := [32]byte{1, 2, 3}
	c1, err := New(master)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Generate(contract.SessionID("s")); err != nil {
		t.Fatal(err)
	}
	c2, err := New(master)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get(contract.SessionID("s")); ok {
		t.Fatal("ephemeral custodian leaked a key into a fresh instance")
	}
}

// Rotating the master must re-wrap existing session keys (still retrievable under
// the new master) while making them unreadable under the old master.
func TestRotateRewrapsSessionKeys(t *testing.T) {
	dir := t.TempDir()
	masterPath := filepath.Join(dir, "master.key")
	storePath := filepath.Join(dir, "sealed.json")

	src, err := NewFileKeySource(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewDurable(src, store)
	if err != nil {
		t.Fatal(err)
	}

	key, err := c.Generate(contract.SessionID("sess-1"))
	if err != nil {
		t.Fatal(err)
	}
	oldMaster, _ := src.Master()

	// Mint + persist a new master, then re-wrap the keystore under it.
	newMaster, err := src.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if newMaster == oldMaster {
		t.Fatal("rotate produced the same master")
	}
	if err := c.Rotate(newMaster); err != nil {
		t.Fatalf("custodian rotate: %v", err)
	}

	// Same session key is still retrievable under the rotated custodian.
	got, ok := c.Get(contract.SessionID("sess-1"))
	if !ok || got != key {
		t.Fatalf("key not preserved across rotation: ok=%v got=%x want=%x", ok, got, key)
	}

	// The persisted blobs must now be unreadable under the OLD master, proving
	// they were genuinely re-sealed (not left under the prior key).
	stale, err := NewDurable(StaticKeySource(oldMaster), mustReopenStore(t, storePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stale.Get(contract.SessionID("sess-1")); ok {
		t.Fatal("old master still opens the re-wrapped keystore")
	}

	// A full restart with the NEW (persisted) master still recovers the key.
	fresh := openDurable(t, masterPath, storePath)
	if got, ok := fresh.Get(contract.SessionID("sess-1")); !ok || got != key {
		t.Fatalf("key not recoverable after rotate+restart: ok=%v got=%x want=%x", ok, got, key)
	}
}

func mustReopenStore(t *testing.T, path string) SealedStore {
	t.Helper()
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	return s
}

// NewDurable requires a real store.
func TestNewDurableRejectsNilStore(t *testing.T) {
	if _, err := NewDurable(StaticKeySource([32]byte{}), nil); err == nil {
		t.Fatal("expected NewDurable to reject a nil store")
	}
}
