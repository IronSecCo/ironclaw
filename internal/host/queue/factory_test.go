// OWNER: T-010

package queue

import (
	"path/filepath"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func testKey(b byte) contract.SessionKey {
	var k contract.SessionKey
	for i := range k {
		k[i] = b
	}
	return k
}

// TestFactoryRoundTrip provisions a session and exercises all four contract
// openers through the factory: the host writes inbound (RW) and the sandbox reads
// it (RO); the sandbox writes outbound (RW) and the host reads it (RO).
func TestFactoryRoundTrip(t *testing.T) {
	f := NewFactory(t.TempDir())
	const sid = "sess-1"
	k := testKey(0x11)

	if err := f.Provision(sid, k); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Host writes an inbound message (sole inbound writer, even seq).
	hin, err := f.OpenHostInbound(sid, k)
	if err != nil {
		t.Fatalf("OpenHostInbound: %v", err)
	}
	defer hin.Close()
	if err := hin.WriteMessageIn(contract.MessageIn{ID: "in-1", Seq: 0, Content: "hello-sandbox"}); err != nil {
		t.Fatalf("WriteMessageIn: %v", err)
	}

	// Sandbox reads inbound read-only and sees the host's write (same key, fresh
	// handle across the simulated mount).
	sin, err := f.OpenSandboxInbound(sid, k)
	if err != nil {
		t.Fatalf("OpenSandboxInbound: %v", err)
	}
	defer sin.Close()
	var content string
	if err := sin.QueryRow("SELECT content FROM messages_in WHERE id = ?", "in-1").Scan(&content); err != nil {
		t.Fatalf("read inbound RO: %v", err)
	}
	if content != "hello-sandbox" {
		t.Fatalf("inbound content = %q, want hello-sandbox", content)
	}

	// Sandbox writes an outbound message (sole outbound writer, odd seq).
	sout, err := f.OpenSandboxOutbound(sid, k)
	if err != nil {
		t.Fatalf("OpenSandboxOutbound: %v", err)
	}
	defer sout.Close()
	if _, err := sout.Exec(
		`INSERT INTO messages_out (id, seq, timestamp, kind, content) VALUES (?,?,?,?,?)`,
		"out-1", 1, "", string(contract.KindChat), "hello-host",
	); err != nil {
		t.Fatalf("write outbound RW: %v", err)
	}

	// Host reads outbound read-only and sees the sandbox's write.
	hout, err := f.OpenHostOutbound(sid, k)
	if err != nil {
		t.Fatalf("OpenHostOutbound: %v", err)
	}
	defer hout.Close()
	due, err := hout.DueMessages()
	if err != nil {
		t.Fatalf("DueMessages: %v", err)
	}
	if len(due) != 1 || due[0].ID != "out-1" || due[0].Content != "hello-host" {
		t.Fatalf("host outbound read = %+v, want one out-1/hello-host", due)
	}
}

// TestFactoryWrongKey verifies a session provisioned with one key cannot be opened
// with another (SQLITE_NOTADB on the forced page read), on both the host-read and
// sandbox-read paths.
func TestFactoryWrongKey(t *testing.T) {
	f := NewFactory(t.TempDir())
	const sid = "sess-wrong"
	good := testKey(0x11)
	bad := testKey(0x22)

	if err := f.Provision(sid, good); err != nil {
		t.Fatalf("provision: %v", err)
	}

	if db, err := f.OpenHostOutbound(sid, bad); err == nil {
		db.Close()
		t.Fatal("OpenHostOutbound with wrong key should fail")
	}
	if db, err := f.OpenSandboxInbound(sid, bad); err == nil {
		db.Close()
		t.Fatal("OpenSandboxInbound with wrong key should fail")
	}
	// And provisioning a *different* session over the same root with a fresh key
	// must still succeed (keys are per-session, not per-root).
	if err := f.Provision("sess-other", bad); err != nil {
		t.Fatalf("provision other session: %v", err)
	}
}

// TestFactoryProvisionIdempotent verifies re-provisioning keeps data intact.
func TestFactoryProvisionIdempotent(t *testing.T) {
	f := NewFactory(t.TempDir())
	const sid = "sess-idem"
	k := testKey(0x33)

	if err := f.Provision(sid, k); err != nil {
		t.Fatalf("provision 1: %v", err)
	}
	hin, err := f.OpenHostInbound(sid, k)
	if err != nil {
		t.Fatalf("OpenHostInbound: %v", err)
	}
	if err := hin.WriteMessageIn(contract.MessageIn{ID: "keep", Seq: 0, Content: "persist"}); err != nil {
		t.Fatalf("WriteMessageIn: %v", err)
	}
	hin.Close()

	// Second provision must not wipe the row.
	if err := f.Provision(sid, k); err != nil {
		t.Fatalf("provision 2: %v", err)
	}
	sin, err := f.OpenSandboxInbound(sid, k)
	if err != nil {
		t.Fatalf("OpenSandboxInbound: %v", err)
	}
	defer sin.Close()
	var n int
	if err := sin.QueryRow("SELECT count(*) FROM messages_in WHERE id = ?", "keep").Scan(&n); err != nil {
		t.Fatalf("count after re-provision: %v", err)
	}
	if n != 1 {
		t.Fatalf("row count after re-provision = %d, want 1", n)
	}
}

// TestFactoryPaths checks the layout and that unsafe session ids are rejected so
// they cannot escape the factory root.
func TestFactoryPaths(t *testing.T) {
	root := t.TempDir()
	f := NewFactory(root)

	p, err := f.Paths("ok-1")
	if err != nil {
		t.Fatalf("Paths(ok-1): %v", err)
	}
	if p.Dir != filepath.Join(root, "ok-1") ||
		p.Inbound != filepath.Join(root, "ok-1", "inbound.db") ||
		p.Outbound != filepath.Join(root, "ok-1", "outbound.db") {
		t.Fatalf("unexpected layout: %+v", p)
	}

	for _, bad := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "x..y"} {
		if _, err := f.Paths(bad); err == nil {
			t.Fatalf("Paths(%q) should have been rejected", bad)
		}
	}
}
