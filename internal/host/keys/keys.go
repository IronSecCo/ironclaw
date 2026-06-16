// OWNER: AGENT1

// Package keys handles per-session SessionKey generation and custody (a host
// keystore encrypted under a host master key) plus secure hand-off to the sandbox
// at launch. The key is delivered via a tmpfs/early-fd mechanism, never an env
// var, and the sandbox image never contains a key.
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
type Custodian struct {
	gcm    cipher.AEAD
	mu     sync.Mutex
	sealed map[contract.SessionID][]byte // nonce || ciphertext
}

// New constructs a Custodian whose keystore is encrypted under master.
func New(master [32]byte) (*Custodian, error) {
	block, err := aes.NewCipher(master[:])
	if err != nil {
		return nil, fmt.Errorf("host/keys: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("host/keys: new gcm: %w", err)
	}
	return &Custodian{gcm: gcm, sealed: make(map[contract.SessionID][]byte)}, nil
}

// Generate creates a fresh random SessionKey for the session, stores it sealed at
// rest, and returns the plaintext key for immediate hand-off. Generating twice
// for the same session overwrites the prior key.
func (c *Custodian) Generate(id contract.SessionID) (contract.SessionKey, error) {
	var key contract.SessionKey
	if _, err := rand.Read(key[:]); err != nil {
		return contract.SessionKey{}, fmt.Errorf("host/keys: generate: %w", err)
	}
	blob, err := c.seal(key)
	if err != nil {
		return contract.SessionKey{}, err
	}
	c.mu.Lock()
	c.sealed[id] = blob
	c.mu.Unlock()
	return key, nil
}

// Get unseals and returns the SessionKey for the session. The bool is false if no
// key has been generated for that session.
func (c *Custodian) Get(id contract.SessionID) (contract.SessionKey, bool) {
	c.mu.Lock()
	blob, ok := c.sealed[id]
	c.mu.Unlock()
	if !ok {
		return contract.SessionKey{}, false
	}
	key, err := c.open(blob)
	if err != nil {
		// A decrypt failure here means the keystore is corrupt or the master is
		// wrong; treat it as "no usable key".
		return contract.SessionKey{}, false
	}
	return key, true
}

// seal encrypts key under the master key with a fresh random nonce, returning
// nonce || ciphertext.
func (c *Custodian) seal(key contract.SessionKey) ([]byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("host/keys: nonce: %w", err)
	}
	ct := c.gcm.Seal(nonce, nonce, key[:], nil)
	return ct, nil
}

// open reverses seal.
func (c *Custodian) open(blob []byte) (contract.SessionKey, error) {
	ns := c.gcm.NonceSize()
	if len(blob) < ns {
		return contract.SessionKey{}, errors.New("host/keys: sealed blob too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := c.gcm.Open(nil, nonce, ct, nil)
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
