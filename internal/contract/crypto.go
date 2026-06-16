// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import (
	"database/sql"
	"encoding/hex"
	"errors"
)

// SessionKey is the per-session 256-bit raw key shared between the host and the
// one sandbox for that session. It is never embedded in the sandbox image.
type SessionKey [32]byte

// Hex returns the lowercase hex encoding of the key, suitable for a raw-key
// PRAGMA.
func (k SessionKey) Hex() string { return hex.EncodeToString(k[:]) }

// KeyPragma returns the exact raw-key PRAGMA string. Raw-key mode (no per-open
// KDF) is mandatory and pinned (see KDFRawKey). The key must be applied on a
// fresh handle before any page is read.
func KeyPragma(k SessionKey) string {
	return `PRAGMA key = "x'` + k.Hex() + `'";`
}

// ErrCryptoBindingPending is returned by every Open* helper until the encrypted
// SQLite binding (SQLite3 Multiple Ciphers, via CGo) is wired in. The skeleton
// builds stdlib-only without it; see docs/building.md.
var ErrCryptoBindingPending = errors.New("contract: encrypted SQLite binding not yet wired (SQLite3 Multiple Ciphers, CGo)")

// OpenInboundRO opens the inbound queue read-only.
//
// Per design-plan §1 the exact connection string + PRAGMA ordering is centralized
// here so neither side drifts: open with mode=ro, apply the cipher pragmas, then
// the raw-key pragma BEFORE any page read, then PRAGMA query_only=ON and PRAGMA
// mmap_size=0. The file is NEVER opened with immutable=1 — it changes underneath
// the reader, and the handle is reopened per poll to defeat the guest page cache.
func OpenInboundRO(path string, k SessionKey) (*sql.DB, error) {
	return nil, ErrCryptoBindingPending
}

// OpenOutboundRW opens the outbound queue read/write (sandbox side, sole writer).
//
// Per design-plan §1 it uses journal_mode=DELETE (not WAL — the WAL -shm mmap
// does not refresh across the bind mount) and the same raw-key discipline as the
// read-only helpers.
func OpenOutboundRW(path string, k SessionKey) (*sql.DB, error) {
	return nil, ErrCryptoBindingPending
}

// OpenOutboundRO opens the outbound queue read-only (host side, sole reader).
//
// Per design-plan §1 it uses mode=ro with the same reopen-per-poll discipline as
// OpenInboundRO so the host observes fresh writes across the bind mount.
func OpenOutboundRO(path string, k SessionKey) (*sql.DB, error) {
	return nil, ErrCryptoBindingPending
}
