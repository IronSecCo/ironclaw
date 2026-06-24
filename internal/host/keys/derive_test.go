package keys

import "testing"

func TestDeriveSubKeyStableAndDomainSeparated(t *testing.T) {
	var master [32]byte
	for i := range master {
		master[i] = byte(i)
	}

	a1 := DeriveSubKey(master, "ironclaw/vault-policy-db/v1")
	a2 := DeriveSubKey(master, "ironclaw/vault-policy-db/v1")
	if a1 != a2 {
		t.Fatal("same master + purpose must derive the same key (stable across restarts)")
	}

	b := DeriveSubKey(master, "ironclaw/registry-db/v1")
	if a1 == b {
		t.Fatal("a different purpose must derive an unrelated key (domain separation)")
	}

	var other [32]byte
	for i := range other {
		other[i] = byte(255 - i)
	}
	if DeriveSubKey(other, "ironclaw/vault-policy-db/v1") == a1 {
		t.Fatal("a different master must derive a different key")
	}

	// The derived key must never be the raw master.
	if a1 == master {
		t.Fatal("derived key must not equal the raw master key")
	}
}
