// OWNER: AGENT1

// Package keys handles per-session SessionKey generation and custody (a host
// keystore encrypted under a host master key) plus secure hand-off to the sandbox
// at launch. The key is delivered via a tmpfs/early-fd mechanism, never an env
// var, and the sandbox image never contains a key.
package keys

import (
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Custodian generates and holds per-session keys.
type Custodian struct{}

// New constructs a Custodian.
func New() *Custodian { return &Custodian{} }

// Generate creates and stores a fresh SessionKey for the session.
func (c *Custodian) Generate(id contract.SessionID) (contract.SessionKey, error) {
	return contract.SessionKey{}, errors.New("host/keys: not implemented (AGENT1)")
}
