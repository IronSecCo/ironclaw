// OWNER: AGENT1

package keys

import (
	"bytes"
	"testing"

	"github.com/nivardsec/ironclaw/internal/contract"
)

func TestGenerateGetRoundTrip(t *testing.T) {
	var master [32]byte
	for i := range master {
		master[i] = byte(i)
	}
	c, err := New(master)
	if err != nil {
		t.Fatal(err)
	}
	key, err := c.Generate("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.Get("sess-1")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if got != key {
		t.Fatalf("round-trip mismatch: %x != %x", got, key)
	}
}

func TestGetUnknownSession(t *testing.T) {
	c, err := New([32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("nope"); ok {
		t.Fatal("expected not found for unknown session")
	}
}

func TestSealedBytesDifferFromRawKey(t *testing.T) {
	c, err := New([32]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	key, err := c.Generate("s")
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	blob := c.sealed["s"]
	c.mu.Unlock()
	if bytes.Contains(blob, key[:]) {
		t.Fatal("sealed blob contains the raw key bytes")
	}
	if len(blob) <= len(key) {
		t.Fatalf("sealed blob len %d not larger than key (nonce+tag expected)", len(blob))
	}
}

func TestWrongMasterFailsToOpen(t *testing.T) {
	var m1, m2 [32]byte
	m1[0] = 1
	m2[0] = 2
	c1, _ := New(m1)
	key, err := c1.Generate("s")
	if err != nil {
		t.Fatal(err)
	}
	c1.mu.Lock()
	blob := c1.sealed["s"]
	c1.mu.Unlock()

	// Hand the sealed blob to a custodian with a different master.
	c2, _ := New(m2)
	c2.mu.Lock()
	c2.sealed["s"] = blob
	c2.mu.Unlock()
	if got, ok := c2.Get("s"); ok {
		t.Fatalf("wrong master opened the key: %x", got)
	}
	_ = key
}

var _ = contract.SessionID("")
