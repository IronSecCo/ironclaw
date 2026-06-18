package egress

// A vaulted credential use crosses two host-side principals: the egress broker
// (which audits the request) and the vault injector (which audits the injection +
// policy decision). To make a single use traceable end to end, the broker stamps a
// host-generated correlation id on the broker->vault request and records the SAME id
// in its per-request audit; the vault echoes it in its own audit. The id is the JOIN
// KEY between the two logs — the §5 Repudiation control extended across the boundary.
//
// The id is generated HOST-SIDE, never by the sandbox: Stamp overwrites any inbound
// value so a compromised sandbox cannot forge or collide the audit correlation.
//
// This file is the correlation unit. Wiring Stamp into the broker Handler
// and threading the returned id into the AuditRecord is the follow-on integration;
// this unit is standalone and fully tested.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
)

// CorrelationHeader carries the correlation id on the broker->vault request.
const CorrelationHeader = "X-Ironclaw-Vault-Request-Id"

// correlationIDBytes is the entropy of a correlation id: 128 bits => 32 hex chars.
const correlationIDBytes = 16

// Correlator generates and stamps correlation ids onto broker->vault requests. The
// generator is a field (not a constructor arg) so production uses crypto/rand while
// same-package tests can substitute a deterministic one.
type Correlator struct {
	gen func() (string, error)
}

// NewCorrelator returns a Correlator that mints 128-bit crypto/rand ids.
func NewCorrelator() *Correlator {
	return &Correlator{gen: newCorrelationID}
}

// Stamp mints a fresh host-side correlation id, sets it on the request — OVERWRITING
// any inbound value so the sandbox cannot forge the audit join — and returns it for
// the broker's AuditRecord. The returned id and the header value are identical: the
// join key the vault will echo in its own audit.
func (c *Correlator) Stamp(req *http.Request) (string, error) {
	gen := c.gen
	if gen == nil {
		gen = newCorrelationID
	}
	id, err := gen()
	if err != nil {
		return "", fmt.Errorf("host/egress: generate correlation id: %w", err)
	}
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set(CorrelationHeader, id)
	return id, nil
}

// CorrelationID reads the correlation id already stamped on a request. The vault
// side reads it with this to log the same join key; returns "" when absent.
func CorrelationID(req *http.Request) string {
	if req == nil || req.Header == nil {
		return ""
	}
	return req.Header.Get(CorrelationHeader)
}

// newCorrelationID returns a 128-bit crypto/rand id as 32 lowercase hex chars.
func newCorrelationID() (string, error) {
	b := make([]byte, correlationIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
