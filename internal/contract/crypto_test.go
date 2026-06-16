// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testKey(b byte) SessionKey {
	var k SessionKey
	for i := range k {
		k[i] = b
	}
	return k
}

func TestEncryptedInboundRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbound.db")
	k := testKey(0x11)

	// Host opens inbound read/write (RFC-0001), schema is ensured, writes a row.
	rw, err := OpenInboundRW(path, k)
	if err != nil {
		t.Fatalf("OpenInboundRW: %v", err)
	}
	if _, err := rw.Exec(
		`INSERT INTO messages_in (id, seq, kind, status, content) VALUES (?,?,?,?,?)`,
		"m1", 2, "chat", "queued", "secret-hello",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rw.Close()

	// Sandbox opens inbound read-only with the same key and reads it back.
	ro, err := OpenInboundRO(path, k)
	if err != nil {
		t.Fatalf("OpenInboundRO: %v", err)
	}
	var content string
	if err := ro.QueryRow(`SELECT content FROM messages_in WHERE id=?`, "m1").Scan(&content); err != nil {
		t.Fatalf("read: %v", err)
	}
	if content != "secret-hello" {
		t.Fatalf("content = %q, want secret-hello", content)
	}
	// Read-only inbound must reject a write (PRAGMA query_only).
	if _, err := ro.Exec(`INSERT INTO messages_in (id, seq) VALUES ('m2', 4)`); err == nil {
		t.Fatal("read-only inbound accepted a write")
	}
	ro.Close()

	// Wrong key must fail to open (decryption fails on first page read).
	if db, err := OpenInboundRO(path, testKey(0x22)); err == nil {
		db.Close()
		t.Fatal("open with wrong key succeeded; encryption not enforced")
	}

	// The plaintext must NOT appear in the on-disk file.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if bytes.Contains(raw, []byte("secret-hello")) {
		t.Fatal("plaintext found in encrypted database file")
	}
}

func TestEncryptedOutboundRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbound.db")
	k := testKey(0x33)

	rw, err := OpenOutboundRW(path, k)
	if err != nil {
		t.Fatalf("OpenOutboundRW: %v", err)
	}
	if _, err := rw.Exec(
		`INSERT INTO messages_out (id, seq, kind, content) VALUES (?,?,?,?)`,
		"o1", 1, "chat", "reply",
	); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rw.Close()

	ro, err := OpenOutboundRO(path, k)
	if err != nil {
		t.Fatalf("OpenOutboundRO: %v", err)
	}
	defer ro.Close()
	var content string
	if err := ro.QueryRow(`SELECT content FROM messages_out WHERE id=?`, "o1").Scan(&content); err != nil {
		t.Fatalf("read: %v", err)
	}
	if content != "reply" {
		t.Fatalf("content = %q, want reply", content)
	}
}
