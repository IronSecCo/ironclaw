package skills

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- in-test minisign signer ----------------------------------------------
//
// These helpers build minisign-format artifacts independently from the verifier
// under test, following the documented wire format (see source.go). The ed25519
// math is stdlib; only the framing is exercised here. Negative tests below
// (forged key id, tampered content, tampered trusted comment) ensure the verifier
// is not merely agreeing with a matching framing bug.

type signer struct {
	priv  ed25519.PrivateKey
	keyID [8]byte
}

// newSigner derives a deterministic keypair + key id from a seed byte so tests are
// reproducible without a RNG.
func newSigner(seed byte) signer {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed + byte(i)
	}
	priv := ed25519.NewKeyFromSeed(s)
	var keyID [8]byte
	for i := range keyID {
		keyID[i] = seed*7 + byte(i)
	}
	return signer{priv: priv, keyID: keyID}
}

// pubFile renders the signer's public key as a two-line minisign .pub file.
func (s signer) pubFile() string {
	pub := s.priv.Public().(ed25519.PublicKey)
	blob := append([]byte{'E', 'd'}, s.keyID[:]...)
	blob = append(blob, pub...)
	return "untrusted comment: test public key\n" + base64.StdEncoding.EncodeToString(blob) + "\n"
}

// sign produces a legacy-mode (.minisig) detached signature over content.
func (s signer) sign(content []byte, trustedComment string) string {
	return s.signAs("Ed", s.keyID, s.priv, content, trustedComment)
}

// signAs is the low-level form, letting a test mix algorithm, advertised key id,
// and the actual signing key (to forge a key-id while signing with another key).
func (s signer) signAs(algo string, advertisedID [8]byte, signWith ed25519.PrivateKey, content []byte, trustedComment string) string {
	sig := ed25519.Sign(signWith, content)
	blob := append([]byte(algo), advertisedID[:]...)
	blob = append(blob, sig...)
	global := append(append([]byte{}, sig...), []byte(trustedComment)...)
	gsig := ed25519.Sign(signWith, global)
	return "untrusted comment: test signature\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n" +
		"trusted comment: " + trustedComment + "\n" +
		base64.StdEncoding.EncodeToString(gsig) + "\n"
}

func trustRoot(t *testing.T, keys ...string) *TrustRoot {
	t.Helper()
	tr, err := LoadTrustRoot(keys...)
	if err != nil {
		t.Fatalf("LoadTrustRoot: %v", err)
	}
	return tr
}

// --- TrustRoot / Verify ---------------------------------------------------

func TestVerifyAcceptsValidSignature(t *testing.T) {
	s := newSigner(1)
	tr := trustRoot(t, s.pubFile())
	content := []byte("apiVersion: ironclaw.dev/skill/v1\nname: ok\nversion: 1.0.0\n")
	if err := tr.Verify(content, s.sign(content, "skill ok 1.0.0")); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func TestVerifyRejectsTamperedContent(t *testing.T) {
	s := newSigner(2)
	tr := trustRoot(t, s.pubFile())
	content := []byte("trusted bundle")
	sig := s.sign(content, "v1")
	if err := tr.Verify([]byte("trusted bundle!"), sig); err == nil {
		t.Fatal("tampered content accepted")
	}
}

func TestVerifyRejectsUnknownKey(t *testing.T) {
	signed := newSigner(3)
	other := newSigner(4)
	tr := trustRoot(t, other.pubFile()) // root trusts a DIFFERENT key
	content := []byte("payload")
	err := tr.Verify(content, signed.sign(content, "v1"))
	if err == nil || !strings.Contains(err.Error(), "not in the trust root") {
		t.Fatalf("expected unknown-key refusal, got %v", err)
	}
}

// TestVerifyRejectsForgedKeyID is the strongest signature-math test: the signature
// advertises a trusted key's id but was actually produced by a different private
// key. The id matches, so the verifier must still reject on the ed25519 check.
func TestVerifyRejectsForgedKeyID(t *testing.T) {
	trusted := newSigner(5)
	attacker := newSigner(6)
	tr := trustRoot(t, trusted.pubFile())
	content := []byte("payload")
	// Stamp the trusted key id onto a signature made with the attacker's key.
	forged := attacker.signAs("Ed", trusted.keyID, attacker.priv, content, "v1")
	if err := tr.Verify(content, forged); err == nil {
		t.Fatal("signature with forged key id accepted")
	}
}

// TestVerifyRejectsTamperedTrustedComment proves the global (trusted-comment)
// signature is actually checked: the content signature is left valid, but the
// trusted comment line is altered after signing.
func TestVerifyRejectsTamperedTrustedComment(t *testing.T) {
	s := newSigner(7)
	tr := trustRoot(t, s.pubFile())
	content := []byte("payload")
	sig := s.sign(content, "release v1.0.0")
	tampered := strings.Replace(sig, "trusted comment: release v1.0.0", "trusted comment: release v9.9.9", 1)
	if err := tr.Verify(content, tampered); err == nil {
		t.Fatal("tampered trusted comment accepted")
	}
}

func TestVerifyRejectsPrehashed(t *testing.T) {
	s := newSigner(8)
	tr := trustRoot(t, s.pubFile())
	content := []byte("payload")
	// Advertise prehashed mode ("ED"); the verifier must refuse before any hashing.
	sig := s.signAs("ED", s.keyID, s.priv, content, "v1")
	err := tr.Verify(content, sig)
	if err == nil || !strings.Contains(err.Error(), "prehashed") {
		t.Fatalf("expected prehashed refusal, got %v", err)
	}
}

func TestVerifyRejectsMalformedSignatures(t *testing.T) {
	s := newSigner(9)
	tr := trustRoot(t, s.pubFile())
	content := []byte("payload")
	good := s.sign(content, "v1")
	cases := map[string]string{
		"empty":          "",
		"two lines":      "untrusted comment: x\n" + strings.Split(good, "\n")[1] + "\n",
		"bad base64 sig": "untrusted comment: x\n!!!notbase64!!!\ntrusted comment: v1\n" + strings.Split(good, "\n")[3] + "\n",
		"short sig blob": "untrusted comment: x\n" + base64.StdEncoding.EncodeToString([]byte("too short")) + "\ntrusted comment: v1\n" + strings.Split(good, "\n")[3] + "\n",
		"no trusted":     "untrusted comment: x\n" + strings.Split(good, "\n")[1] + "\nNOT a trusted comment\n" + strings.Split(good, "\n")[3] + "\n",
	}
	for name, sig := range cases {
		if err := tr.Verify(content, sig); err == nil {
			t.Errorf("%s: malformed signature accepted", name)
		}
	}
}

func TestVerifyEmptyTrustRootRefuses(t *testing.T) {
	var nilRoot *TrustRoot
	if err := nilRoot.Verify([]byte("x"), "whatever"); err == nil {
		t.Error("nil trust root accepted a signature")
	}
	empty := &TrustRoot{keys: map[[8]byte]ed25519.PublicKey{}}
	if err := empty.Verify([]byte("x"), "whatever"); err == nil {
		t.Error("empty trust root accepted a signature")
	}
}

func TestLoadTrustRoot(t *testing.T) {
	if _, err := LoadTrustRoot(); err == nil {
		t.Error("LoadTrustRoot with no keys should fail closed")
	}
	for _, bad := range []string{"untrusted comment: x\n!!!\n", "dG9vc2hvcnQ=", ""} {
		if _, err := LoadTrustRoot(bad); err == nil {
			t.Errorf("LoadTrustRoot accepted bad key %q", bad)
		}
	}
	// A bare base64 key line (no comment) must also load.
	s := newSigner(10)
	bare := strings.TrimSpace(strings.Split(s.pubFile(), "\n")[1])
	if _, err := LoadTrustRoot(bare); err != nil {
		t.Errorf("bare key line rejected: %v", err)
	}
}

// --- DirSource ------------------------------------------------------------

func writeBundle(t *testing.T, root, name, version, manifest, sig string) {
	t.Helper()
	dir := filepath.Join(root, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		if err := os.WriteFile(filepath.Join(dir, signatureFileName), []byte(sig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDirSourceOpen(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, root, "triage", "1.0.0", "manifest-bytes", "sig-bytes")
	src := DirSource{Root: root}

	manifest, sig, err := src.Open("triage", "1.0.0")
	if err != nil {
		t.Fatalf("Open valid bundle: %v", err)
	}
	if string(manifest) != "manifest-bytes" || sig != "sig-bytes" {
		t.Errorf("unexpected bytes: %q / %q", manifest, sig)
	}
}

func TestDirSourceRefusesUnsigned(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, root, "triage", "1.0.0", "manifest-bytes", "") // no .minisig
	if _, _, err := (DirSource{Root: root}).Open("triage", "1.0.0"); err == nil {
		t.Fatal("an unsigned bundle (no .minisig) was not refused")
	}
}

func TestDirSourceRejectsBadIdentifiers(t *testing.T) {
	src := DirSource{Root: t.TempDir()}
	bad := [][2]string{
		{"../etc", "1.0.0"},
		{"a/b", "1.0.0"},
		{"UPPER", "1.0.0"},
		{"ok", "../../1"},
		{"ok", "1/0"},
		{"ok", ".."},
		{"", "1.0.0"},
		{"ok", ""},
	}
	for _, c := range bad {
		if _, _, err := src.Open(c[0], c[1]); err == nil {
			t.Errorf("Open accepted unsafe identifier name=%q version=%q", c[0], c[1])
		}
	}
	if _, _, err := (DirSource{Root: ""}).Open("ok", "1.0.0"); err == nil {
		t.Error("DirSource with empty root should error")
	}
}

// TestResolveBundlePathContainment exercises the path barrier directly: every value
// it returns must be confined to root, and any traversal/charset violation must be
// refused before a filesystem path is produced. This is the sanitizer CodeQL follows
// for the go/path-injection sinks in Open/Remove.
func TestResolveBundlePathContainment(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Clean(root) + string(filepath.Separator)

	// Accepted: concrete name@version and (Remove-only) whole-name selection.
	for _, c := range []struct {
		name, version  string
		requireVersion bool
	}{
		{"triage", "1.0.0", true},
		{"triage", "", false},
	} {
		got, err := resolveBundlePath(root, c.name, c.version, c.requireVersion)
		if err != nil {
			t.Fatalf("resolveBundlePath(%q,%q) rejected a valid bundle: %v", c.name, c.version, err)
		}
		if got != filepath.Clean(root) && !strings.HasPrefix(got, prefix) {
			t.Errorf("resolved path %q escapes root %q", got, root)
		}
	}

	// Refused: traversal tokens, bad charset, empty root, and empty version when required.
	for _, c := range []struct {
		root, name, version string
		requireVersion      bool
	}{
		{root, "../etc", "1.0.0", true},
		{root, "ok", "../../1", true},
		{root, "a/b", "1.0.0", true},
		{root, "ok", "..", false},
		{root, "ok", "", true},
		{"", "ok", "1.0.0", true},
	} {
		if _, err := resolveBundlePath(c.root, c.name, c.version, c.requireVersion); err == nil {
			t.Errorf("resolveBundlePath(root=%q,name=%q,version=%q) accepted an unsafe input", c.root, c.name, c.version)
		}
	}
}

// --- Resolver (end to end) ------------------------------------------------

const resolverManifest = `apiVersion: ironclaw.dev/skill/v1
name: incident-triage
version: 1.4.0
description: Triage alerts.
grants:
  tools:
    - http_fetch
  egress:
    - api.pagerduty.com
`

func resolverTools() map[string]bool {
	return map[string]bool{"http_fetch": true, "send_message": true}
}

func TestResolverResolvesSignedBundle(t *testing.T) {
	root := t.TempDir()
	s := newSigner(20)
	sig := s.sign([]byte(resolverManifest), "incident-triage 1.4.0")
	writeBundle(t, root, "incident-triage", "1.4.0", resolverManifest, sig)

	r := &Resolver{Source: DirSource{Root: root}, Trust: trustRoot(t, s.pubFile()), KnownTools: resolverTools()}
	m, err := r.Resolve("incident-triage", "1.4.0")
	if err != nil {
		t.Fatalf("Resolve signed bundle: %v", err)
	}
	if m.Name != "incident-triage" || len(m.Grants.Tools) != 1 {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestResolverRefusesTamperedManifest(t *testing.T) {
	root := t.TempDir()
	s := newSigner(21)
	sig := s.sign([]byte(resolverManifest), "incident-triage 1.4.0")
	// Sign the clean manifest, but serve a manifest with an extra egress host added.
	tampered := strings.Replace(resolverManifest, "    - api.pagerduty.com\n", "    - api.pagerduty.com\n    - evil.example.com\n", 1)
	writeBundle(t, root, "incident-triage", "1.4.0", tampered, sig)

	r := &Resolver{Source: DirSource{Root: root}, Trust: trustRoot(t, s.pubFile()), KnownTools: resolverTools()}
	_, err := r.Resolve("incident-triage", "1.4.0")
	if err == nil || !strings.Contains(err.Error(), "refused at fetch time") {
		t.Fatalf("tampered manifest not refused at fetch time: %v", err)
	}
}

// TestResolverVerifiesBeforeParse proves ordering: a manifest that is BOTH unsigned
// and malformed must fail on the signature gate, not the parser.
func TestResolverVerifiesBeforeParse(t *testing.T) {
	root := t.TempDir()
	s := newSigner(22)
	garbage := "name: [unterminated"
	wrongSig := s.sign([]byte("something else entirely"), "x")
	writeBundle(t, root, "incident-triage", "1.4.0", garbage, wrongSig)

	r := &Resolver{Source: DirSource{Root: root}, Trust: trustRoot(t, s.pubFile()), KnownTools: resolverTools()}
	_, err := r.Resolve("incident-triage", "1.4.0")
	if err == nil || !strings.Contains(err.Error(), "refused at fetch time") {
		t.Fatalf("expected fetch-time signature refusal before parse, got %v", err)
	}
}

func TestResolverRefusesIdentityMismatch(t *testing.T) {
	root := t.TempDir()
	s := newSigner(23)
	// Validly sign and store the bundle, but under a directory name that differs
	// from the manifest's own name.
	sig := s.sign([]byte(resolverManifest), "incident-triage 1.4.0")
	writeBundle(t, root, "renamed-skill", "1.4.0", resolverManifest, sig)

	r := &Resolver{Source: DirSource{Root: root}, Trust: trustRoot(t, s.pubFile()), KnownTools: resolverTools()}
	_, err := r.Resolve("renamed-skill", "1.4.0")
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("expected identity-mismatch refusal, got %v", err)
	}
}

func TestResolverRequiresTrustRoot(t *testing.T) {
	r := &Resolver{Source: DirSource{Root: t.TempDir()}, KnownTools: resolverTools()}
	if _, err := r.Resolve("x", "1.0.0"); err == nil {
		t.Error("resolver with no trust root should refuse")
	}
	r2 := &Resolver{Trust: &TrustRoot{}, KnownTools: resolverTools()}
	if _, err := r2.Resolve("x", "1.0.0"); err == nil {
		t.Error("resolver with no source should refuse")
	}
}
