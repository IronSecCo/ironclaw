package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// testTrustKeyFile writes a valid minisign public-key file and returns its path.
func testTrustKeyFile(t *testing.T) string {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 1
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	blob := append([]byte{'E', 'd'}, make([]byte, 8)...) // sig_alg + 8-byte key id
	blob = append(blob, pub...)
	content := "untrusted comment: test trust key\n" + base64.StdEncoding.EncodeToString(blob) + "\n"
	path := filepath.Join(t.TempDir(), "trust.pub")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildSkillsResolverDisabled(t *testing.T) {
	r, err := buildSkillsResolver("", "")
	if err != nil || r != nil {
		t.Fatalf("empty source must disable skills: r=%v err=%v", r, err)
	}
	// A trust key without a source is still disabled (source is the switch).
	r, err = buildSkillsResolver("", testTrustKeyFile(t))
	if err != nil || r != nil {
		t.Fatalf("no source must disable skills regardless of trust key: r=%v err=%v", r, err)
	}
}

func TestBuildSkillsResolverFailsClosed(t *testing.T) {
	if _, err := buildSkillsResolver(t.TempDir(), ""); err == nil {
		t.Error("source without a trust key must error")
	}
	if _, err := buildSkillsResolver(t.TempDir(), "/no/such/key"); err == nil {
		t.Error("unreadable trust key must error")
	}
	// A present but malformed key file must error.
	bad := filepath.Join(t.TempDir(), "bad.pub")
	if err := os.WriteFile(bad, []byte("not a key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildSkillsResolver(t.TempDir(), bad); err == nil {
		t.Error("malformed trust key must error")
	}
}

func TestBuildSkillsResolverValid(t *testing.T) {
	dir := t.TempDir()
	r, err := buildSkillsResolver(dir, testTrustKeyFile(t))
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if r == nil || r.Trust == nil || len(r.KnownTools) == 0 {
		t.Fatalf("resolver not fully built: %+v", r)
	}
	// KnownTools must be the compiled sandbox tool set.
	for _, want := range []string{"http_fetch", "send_message", "read_file"} {
		if !r.KnownTools[want] {
			t.Errorf("KnownTools missing compiled tool %q", want)
		}
	}
}
