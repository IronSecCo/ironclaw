// OWNER: AGENT1

// Package keys handles per-session SessionKey generation and custody (a host
// keystore encrypted under a host master key) plus secure hand-off to the sandbox
// at launch. The key is delivered via a tmpfs/early-fd mechanism, never an env
// var, and the sandbox image never contains a key.
//
// The host master key under which the keystore is sealed is supplied by a
// pluggable KeySource (source.go): an ephemeral StaticKeySource (the legacy
// per-process master), a file-sealed FileKeySource (durable at 0600), or an
// external KMS. Sealed session keys may additionally be mirrored to a durable
// SealedStore (store.go) so that — paired with a durable KeySource — in-flight
// session keys survive a control-plane restart.
package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Custodian generates per-session keys and holds them at rest sealed under a host
// master key with AES-256-GCM. The plaintext key only exists transiently when
// Generate produces it and when Get unseals it for hand-off to a sandbox.
//
// The sealed map is the in-memory working set. When store is non-nil every seal
// is mirrored to it durably, and NewDurable rehydrates the map from it at start,
// which is what lets session keys outlive a restart.
type Custodian struct {
	gcm    cipher.AEAD
	mu     sync.Mutex
	sealed map[contract.SessionID][]byte // nonce || ciphertext
	store  SealedStore                   // optional durable mirror; nil = in-memory only
}

// New constructs a Custodian whose keystore is encrypted under master. The
// resulting custodian is in-memory only (no durable mirror); use NewDurable for
// restart-surviving custody.
func New(master [32]byte) (*Custodian, error) {
	gcm, err := newGCM(master)
	if err != nil {
		return nil, err
	}
	return &Custodian{gcm: gcm, sealed: make(map[contract.SessionID][]byte)}, nil
}

// NewFromSource builds an in-memory Custodian whose keystore is sealed under the
// master loaded from src. It is the pluggable equivalent of New: pass a
// FileKeySource (durable master) or a KMS-backed source instead of a raw key.
func NewFromSource(src KeySource) (*Custodian, error) {
	master, err := src.Master()
	if err != nil {
		return nil, fmt.Errorf("host/keys: load master: %w", err)
	}
	return New(master)
}

// NewDurable builds a Custodian sealed under src's master and mirrored to store.
// Any sealed session keys already in store are loaded into the working set, so
// when src is durable (e.g. FileKeySource) in-flight session keys survive a
// control-plane restart.
func NewDurable(src KeySource, store SealedStore) (*Custodian, error) {
	if store == nil {
		return nil, errors.New("host/keys: NewDurable requires a non-nil store")
	}
	c, err := NewFromSource(src)
	if err != nil {
		return nil, err
	}
	existing, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("host/keys: load sealed store: %w", err)
	}
	c.mu.Lock()
	for id, blob := range existing {
		c.sealed[id] = blob
	}
	c.store = store
	c.mu.Unlock()
	return c, nil
}

// Generate creates a fresh random SessionKey for the session, stores it sealed at
// rest, and returns the plaintext key for immediate hand-off. Generating twice
// for the same session overwrites the prior key. When a durable store is
// configured the sealed key is persisted before Generate returns.
func (c *Custodian) Generate(id contract.SessionID) (contract.SessionKey, error) {
	var key contract.SessionKey
	if _, err := rand.Read(key[:]); err != nil {
		return contract.SessionKey{}, fmt.Errorf("host/keys: generate: %w", err)
	}
	c.mu.Lock()
	blob, err := sealKey(c.gcm, key)
	if err != nil {
		c.mu.Unlock()
		return contract.SessionKey{}, err
	}
	c.sealed[id] = blob
	store := c.store
	c.mu.Unlock()

	if store != nil {
		if err := store.Save(id, blob); err != nil {
			return contract.SessionKey{}, fmt.Errorf("host/keys: persist sealed key: %w", err)
		}
	}
	return key, nil
}

// Get unseals and returns the SessionKey for the session. The bool is false if no
// key has been generated for that session.
func (c *Custodian) Get(id contract.SessionID) (contract.SessionKey, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	blob, ok := c.sealed[id]
	if !ok {
		return contract.SessionKey{}, false
	}
	key, err := openKey(c.gcm, blob)
	if err != nil {
		// A decrypt failure here means the keystore is corrupt or the master is
		// wrong; treat it as "no usable key".
		return contract.SessionKey{}, false
	}
	return key, true
}

// Rotate re-keys the keystore under a new master: every sealed session key is
// unsealed under the current master and re-sealed under next, atomically in
// memory, and mirrored to the durable store when one is configured. The caller
// is responsible for persisting next via its KeySource first (e.g.
// FileKeySource.Rotate) so that the new master itself survives a restart.
//
// Rotate is all-or-nothing for the in-memory keystore: if any entry fails to
// unseal under the old master the keystore is left untouched and an error is
// returned.
func (c *Custodian) Rotate(next [32]byte) error {
	ngcm, err := newGCM(next)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	rewrapped := make(map[contract.SessionID][]byte, len(c.sealed))
	for id, blob := range c.sealed {
		key, err := openKey(c.gcm, blob)
		if err != nil {
			return fmt.Errorf("host/keys: rotate: unseal %q: %w", id, err)
		}
		nb, err := sealKey(ngcm, key)
		if err != nil {
			return fmt.Errorf("host/keys: rotate: reseal %q: %w", id, err)
		}
		rewrapped[id] = nb
	}

	// Commit the in-memory keystore atomically, then mirror to the durable store.
	c.gcm = ngcm
	c.sealed = rewrapped
	if c.store != nil {
		for id, blob := range rewrapped {
			if err := c.store.Save(id, blob); err != nil {
				return fmt.Errorf("host/keys: rotate: persist %q: %w", id, err)
			}
		}
	}
	return nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte master key.
func newGCM(master [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(master[:])
	if err != nil {
		return nil, fmt.Errorf("host/keys: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("host/keys: new gcm: %w", err)
	}
	return gcm, nil
}

// sealKey encrypts key under gcm with a fresh random nonce, returning
// nonce || ciphertext.
func sealKey(gcm cipher.AEAD, key contract.SessionKey) ([]byte, error) {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("host/keys: nonce: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, key[:], nil)
	return ct, nil
}

// openKey reverses sealKey.
func openKey(gcm cipher.AEAD, blob []byte) (contract.SessionKey, error) {
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return contract.SessionKey{}, errors.New("host/keys: sealed blob too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return contract.SessionKey{}, fmt.Errorf("host/keys: open: %w", err)
	}
	if len(pt) != len(contract.SessionKey{}) {
		return contract.SessionKey{}, errors.New("host/keys: unsealed key wrong length")
	}
	var key contract.SessionKey
	copy(key[:], pt)
	return key, nil
}
