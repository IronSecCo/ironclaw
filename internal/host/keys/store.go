package keys

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// SealedStore persists the sealed (nonce||ciphertext) session-key blobs a
// Custodian holds at rest. Persisting them — paired with a durable master via a
// KeySource — is what lets in-flight session keys survive a control-plane
// restart. A store only ever sees sealed blobs, never plaintext keys.
type SealedStore interface {
	// Load returns every persisted sealed blob keyed by session.
	Load() (map[contract.SessionID][]byte, error)
	// Save persists (or overwrites) the sealed blob for id.
	Save(id contract.SessionID, blob []byte) error
	// Delete removes any sealed blob for id. Deleting an absent id is not an error.
	Delete(id contract.SessionID) error
}

// MemStore is a non-durable SealedStore (the default custody behavior). It does
// not survive a restart; pair a Custodian with a FileStore for durability.
type MemStore struct {
	mu     sync.Mutex
	sealed map[contract.SessionID][]byte
}

// NewMemStore returns an empty in-memory SealedStore.
func NewMemStore() *MemStore {
	return &MemStore{sealed: make(map[contract.SessionID][]byte)}
}

// Load returns a deep copy of the in-memory blobs.
func (m *MemStore) Load() (map[contract.SessionID][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneSealed(m.sealed), nil
}

// Save stores a copy of blob for id.
func (m *MemStore) Save(id contract.SessionID, blob []byte) error {
	cp := make([]byte, len(blob))
	copy(cp, blob)
	m.mu.Lock()
	m.sealed[id] = cp
	m.mu.Unlock()
	return nil
}

// Delete removes id.
func (m *MemStore) Delete(id contract.SessionID) error {
	m.mu.Lock()
	delete(m.sealed, id)
	m.mu.Unlock()
	return nil
}

// FileStore is a durable SealedStore backed by a single 0600 JSON file. It keeps
// an in-memory mirror and rewrites the whole file atomically (temp + rename) on
// every mutation. JSON encodes each []byte blob as base64; the file holds only
// sealed blobs, never plaintext keys.
type FileStore struct {
	path string
	mu   sync.Mutex
	mem  map[contract.SessionID][]byte
}

// NewFileStore opens (load-or-create) the sealed-keystore file at path. A missing
// file starts empty; an existing file that is group/other-accessible is rejected.
func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("host/keys: FileStore path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("host/keys: sealed-store dir: %w", err)
	}
	s := &FileStore{path: path, mem: make(map[contract.SessionID][]byte)}
	if err := s.read(); err != nil {
		return nil, err
	}
	return s, nil
}

// Load returns a deep copy of the persisted blobs.
func (s *FileStore) Load() (map[contract.SessionID][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneSealed(s.mem), nil
}

// Save persists a copy of blob for id and flushes the file.
func (s *FileStore) Save(id contract.SessionID, blob []byte) error {
	cp := make([]byte, len(blob))
	copy(cp, blob)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem[id] = cp
	return s.flushLocked()
}

// Delete removes id and flushes the file.
func (s *FileStore) Delete(id contract.SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.mem[id]; !ok {
		return nil
	}
	delete(s.mem, id)
	return s.flushLocked()
}

// read loads the on-disk file into s.mem. A missing file is treated as empty.
func (s *FileStore) read() error {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("host/keys: stat sealed-store: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("host/keys: sealed-store %s is too open (mode %04o, want 0600)", s.path, perm)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("host/keys: read sealed-store: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	var on map[contract.SessionID][]byte
	if err := json.Unmarshal(raw, &on); err != nil {
		return fmt.Errorf("host/keys: parse sealed-store %s: %w", s.path, err)
	}
	if on != nil {
		s.mem = on
	}
	return nil
}

// flushLocked atomically rewrites the file from s.mem. The caller holds s.mu.
func (s *FileStore) flushLocked() error {
	raw, err := json.Marshal(s.mem)
	if err != nil {
		return fmt.Errorf("host/keys: encode sealed-store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("host/keys: write sealed-store: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("host/keys: chmod sealed-store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("host/keys: commit sealed-store: %w", err)
	}
	return nil
}

// cloneSealed deep-copies a sealed-blob map so callers cannot mutate internal state.
func cloneSealed(in map[contract.SessionID][]byte) map[contract.SessionID][]byte {
	out := make(map[contract.SessionID][]byte, len(in))
	for k, v := range in {
		cp := make([]byte, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}
