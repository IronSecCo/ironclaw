package keys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// DeriveSubKey deterministically derives a 32-byte host-internal subkey from the
// master key for a named purpose, via HMAC-SHA256(master, purpose). It gives each
// host-internal encrypted store (e.g. the durable vault-policy DB, a future
// durable registry DB) its own stable, domain-separated key without ever reusing
// the raw master or a per-session key as a database key. The result is stable
// across restarts for a fixed master, and a distinct purpose yields an unrelated
// key. The returned key is a secret: never log it.
func DeriveSubKey(master [32]byte, purpose string) [32]byte {
	mac := hmac.New(sha256.New, master[:])
	mac.Write([]byte(purpose))
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// KeySource supplies the 32-byte host master key under which the session
// keystore is sealed. It is the pluggable seam between an ephemeral in-process
// key (StaticKeySource), a file-sealed key (FileKeySource), and an external KMS:
// a KMS-backed implementation only has to satisfy Master.
type KeySource interface {
	// Master returns the host master key. Implementations return a stable value
	// for the life of the source unless Rotate (where supported) is called.
	Master() ([32]byte, error)
}

// RotatableKeySource is the optional capability of a KeySource that can mint and
// durably persist a fresh master key in place. FileKeySource implements it; a
// KMS-backed source may too.
type RotatableKeySource interface {
	KeySource
	// Rotate generates a new master key, persists it durably, and returns it so
	// the caller can re-wrap an existing keystore via Custodian.Rotate.
	Rotate() ([32]byte, error)
}

// StaticKeySource is an in-memory KeySource wrapping a fixed master key. It is
// the ephemeral, non-durable source — equivalent to the legacy per-process
// master generated with crypto/rand at boot — and does not survive a restart.
type StaticKeySource [32]byte

// Master returns the fixed key.
func (s StaticKeySource) Master() ([32]byte, error) { return [32]byte(s), nil }

// masterFileMagic tags the host master-key file and pins its version. A change
// in on-disk layout bumps this so an old file is rejected rather than
// misinterpreted.
const masterFileMagic = "ICKMASTER1" // IronClaw host master key, v1

// masterFileLen is the exact size of a valid master-key file: magic + 32-byte key.
const masterFileLen = len(masterFileMagic) + 32

// FileKeySource persists the host master key in a 0600 keystore file so the
// master — and therefore every session key sealed under it — survives a
// control-plane restart. The file holds a versioned magic header followed by the
// raw 32-byte master; at-rest protection is the 0600 file mode (an external KMS
// source is the path to envelope sealing). Access is serialized.
type FileKeySource struct {
	path string
	mu   sync.Mutex
	key  [32]byte
}

// NewFileKeySource opens the master-key file at path (load-or-create): an
// existing 0600 file is loaded, otherwise a fresh random master is generated and
// written at 0600. The parent directory is created at 0700 when missing. An
// existing file that is group/other-accessible is rejected rather than trusted.
func NewFileKeySource(path string) (*FileKeySource, error) {
	if path == "" {
		return nil, errors.New("host/keys: FileKeySource path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("host/keys: keystore dir: %w", err)
	}
	s := &FileKeySource{path: path}
	loaded, err := s.load()
	if err != nil {
		return nil, err
	}
	if !loaded {
		if _, err := s.Rotate(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Master returns the persisted master key.
func (s *FileKeySource) Master() ([32]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key, nil
}

// Rotate mints a fresh random master, writes it to the keystore file at 0600
// (atomically via a temp file + rename), and returns it. Any keystore sealed
// under the prior master must be re-wrapped with Custodian.Rotate.
func (s *FileKeySource) Rotate() ([32]byte, error) {
	var next [32]byte
	if _, err := io.ReadFull(rand.Reader, next[:]); err != nil {
		return [32]byte{}, fmt.Errorf("host/keys: master: %w", err)
	}
	if err := s.persist(next); err != nil {
		return [32]byte{}, err
	}
	s.mu.Lock()
	s.key = next
	s.mu.Unlock()
	return next, nil
}

// load reads an existing keystore file into s.key. It returns (false, nil) when
// the file does not exist, and an error when it exists but is malformed or has
// permissions broader than 0600.
func (s *FileKeySource) load() (bool, error) {
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("host/keys: stat keystore: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return false, fmt.Errorf("host/keys: keystore %s is too open (mode %04o, want 0600)", s.path, perm)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return false, fmt.Errorf("host/keys: read keystore: %w", err)
	}
	if len(raw) != masterFileLen {
		return false, fmt.Errorf("host/keys: keystore %s: bad length %d (want %d)", s.path, len(raw), masterFileLen)
	}
	if string(raw[:len(masterFileMagic)]) != masterFileMagic {
		return false, fmt.Errorf("host/keys: keystore %s: bad magic", s.path)
	}
	s.mu.Lock()
	copy(s.key[:], raw[len(masterFileMagic):])
	s.mu.Unlock()
	return true, nil
}

// persist atomically writes master to the keystore file at 0600.
func (s *FileKeySource) persist(master [32]byte) error {
	buf := make([]byte, 0, masterFileLen)
	buf = append(buf, masterFileMagic...)
	buf = append(buf, master[:]...)

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("host/keys: write keystore: %w", err)
	}
	// WriteFile only applies the mode on creation; force 0600 in case tmp existed.
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("host/keys: chmod keystore: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("host/keys: commit keystore: %w", err)
	}
	return nil
}
