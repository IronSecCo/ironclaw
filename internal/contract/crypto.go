// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"

	_ "github.com/mutecomm/go-sqlcipher/v4" // registers the "sqlite3" SQLCipher driver
)

// ErrCryptoBindingPending is retained for compatibility. The encrypted-SQLite
// binding is now wired (see openEncrypted), so the Open* helpers no longer return
// it — it remains defined because the sandbox tree still references it as a
// sentinel and removing an exported contract symbol would break that tree.
var ErrCryptoBindingPending = errors.New("contract: encrypted SQLite binding pending")

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

// openEncrypted opens an encrypted SQLite database at path with the pinned cipher
// parameters and the cross-mount discipline (design-plan §1). The per-session key
// and pragmas are applied via a ConnectHook so EVERY pooled connection is keyed
// before any page is read; raw-key mode (no KDF) is used. journal_mode=DELETE is
// set on writers only (not WAL — the WAL -shm mmap does not refresh across a bind
// mount); read-only opens add mode=ro + PRAGMA query_only and never set
// journal_mode (which would require a write). mmap_size=0 always, to defeat the
// guest page cache so reopen-per-poll observes fresh writes. The file is NEVER
// opened immutable=1 — it changes underneath the reader.
func openEncrypted(path string, k SessionKey, readOnly bool) (*sql.DB, error) {
	// The raw key travels in the DSN (_pragma_key) so SQLCipher applies it on every
	// pooled connection before any page is read; raw-key mode (no KDF). The cipher
	// page size is left at SQLCipher v4's default, which equals the pinned
	// CipherPageSize (4096) — setting it explicitly can desync writer and reader.
	q := url.Values{}
	q.Set("_pragma_key", `x'`+k.Hex()+`'`)
	q.Set("_busy_timeout", "5000")
	if readOnly {
		// mode=ro + query_only: the reader can never write. DELETE journal is set by
		// the writer only; readers must not set journal_mode (it would require a write).
		q.Set("mode", "ro")
		q.Set("_query_only", "true")
	} else {
		// journal_mode=DELETE (not WAL — the WAL -shm mmap does not refresh across a
		// bind mount).
		q.Set("_journal_mode", "DELETE")
	}
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("contract: open %q: %w", path, err)
	}
	// One connection matches the open-write-close, reopen-per-poll discipline and
	// avoids multiple keyed handles racing on the same file across the mount.
	db.SetMaxOpenConns(1)
	// Defeat the guest page cache so reopen-per-poll observes fresh writes. The file
	// is never opened immutable=1 — it changes underneath the reader.
	if _, err := db.Exec("PRAGMA mmap_size = 0;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("contract: set mmap_size for %q: %w", path, err)
	}
	// Force a real page read so a wrong key fails here (SQLITE_NOTADB) rather than
	// silently at first query.
	if _, err := db.Exec("SELECT count(*) FROM sqlite_master;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("contract: open encrypted db %q (wrong key or cipher mismatch?): %w", path, err)
	}
	return db, nil
}

// ensureSchema applies ddl once, when probeTable is absent (idempotent across
// reopens). Used by the read/write openers, which own creating their file.
func ensureSchema(db *sql.DB, ddl, probeTable string) error {
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", probeTable).Scan(&name)
	if err == sql.ErrNoRows {
		if _, e := db.Exec(ddl); e != nil {
			return fmt.Errorf("apply schema: %w", e)
		}
		return nil
	}
	return err
}

// OpenInboundRO opens the inbound queue read-only (sandbox side).
//
// mode=ro + PRAGMA query_only; reopened per poll to observe the host's fresh
// writes across the bind mount. The file is never opened immutable=1.
func OpenInboundRO(path string, k SessionKey) (*sql.DB, error) {
	return openEncrypted(path, k, true)
}

// OpenInboundRW opens the inbound queue read/write (host side, sole inbound
// writer). RFC-0001. Same raw-key discipline and journal_mode=DELETE as the other
// writer, WITHOUT query_only (the host must write), and ensures the inbound schema.
func OpenInboundRW(path string, k SessionKey) (*sql.DB, error) {
	db, err := openEncrypted(path, k, false)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(db, InboundSchema, "messages_in"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenOutboundRW opens the outbound queue read/write (sandbox side, sole writer).
//
// journal_mode=DELETE (not WAL) and the same raw-key discipline; ensures the
// outbound schema.
func OpenOutboundRW(path string, k SessionKey) (*sql.DB, error) {
	db, err := openEncrypted(path, k, false)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(db, OutboundSchema, "messages_out"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// OpenOutboundRO opens the outbound queue read-only (host side, sole reader).
//
// mode=ro with the same reopen-per-poll discipline as OpenInboundRO so the host
// observes fresh writes across the bind mount.
func OpenOutboundRO(path string, k SessionKey) (*sql.DB, error) {
	return openEncrypted(path, k, true)
}
