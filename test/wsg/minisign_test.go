//go:build wsg_verify

package wsg

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// This file builds minisign-format artifacts independently of the host verifier
// under test, following the documented wire format (internal/host/skills/source.go):
// a public-key blob is "Ed"||keyID[8]||pub[32]; a detached .minisig is four lines
// (untrusted comment, base64(alg||keyID||sig), trusted comment, base64(globalSig)).
// The ed25519 math is stdlib; only the framing lives here. This is a genuine
// minisign keypair + signature — the CI workflow additionally drives the real
// `minisign` CLI to prove interop (see g8_skills_test.go: realMinisignBundle).

type minisigner struct {
	priv  ed25519.PrivateKey
	keyID [8]byte
}

// newMinisigner derives a deterministic keypair + key id from a seed so the
// harness is reproducible without a RNG.
func newMinisigner(seed byte) minisigner {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed + byte(i)
	}
	priv := ed25519.NewKeyFromSeed(s)
	var keyID [8]byte
	for i := range keyID {
		keyID[i] = seed*7 + byte(i)
	}
	return minisigner{priv: priv, keyID: keyID}
}

// pubFile renders the public key as a two-line minisign .pub file.
func (s minisigner) pubFile() string {
	pub := s.priv.Public().(ed25519.PublicKey)
	blob := append([]byte{'E', 'd'}, s.keyID[:]...)
	blob = append(blob, pub...)
	return "untrusted comment: wsg test public key\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n"
}

// sign produces a legacy-mode (.minisig) detached signature over content. Legacy
// ("Ed") is the only mode the host accepts (prehashed is refused).
func (s minisigner) sign(content []byte, trustedComment string) string {
	sig := ed25519.Sign(s.priv, content)
	blob := append([]byte("Ed"), s.keyID[:]...)
	blob = append(blob, sig...)
	global := append(append([]byte{}, sig...), []byte(trustedComment)...)
	gsig := ed25519.Sign(s.priv, global)
	return "untrusted comment: wsg test signature\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n" +
		"trusted comment: " + trustedComment + "\n" +
		base64.StdEncoding.EncodeToString(gsig) + "\n"
}

// writeBundle lays out a curated DirSource bundle on disk at
// <root>/<name>/<version>/{skill.yaml,skill.yaml.minisig}.
func writeBundle(t *testing.T, root, name, version, manifest, signature string) {
	t.Helper()
	dir := filepath.Join(root, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml.minisig"), []byte(signature), 0o644); err != nil {
		t.Fatalf("write signature: %v", err)
	}
}
