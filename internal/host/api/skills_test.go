package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IronSecCo/ironclaw/internal/host/gateway"
	"github.com/IronSecCo/ironclaw/internal/host/skills"
)

// --- compact minisign signer (format mirrors internal/host/skills/source.go) ---

type miniSigner struct {
	priv  ed25519.PrivateKey
	keyID [8]byte
}

func newMiniSigner(seed byte) miniSigner {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed + byte(i)
	}
	var id [8]byte
	for i := range id {
		id[i] = seed*3 + byte(i)
	}
	return miniSigner{priv: ed25519.NewKeyFromSeed(s), keyID: id}
}

func (m miniSigner) pubFile() string {
	pub := m.priv.Public().(ed25519.PublicKey)
	blob := append([]byte{'E', 'd'}, m.keyID[:]...)
	blob = append(blob, pub...)
	return "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(blob) + "\n"
}

func (m miniSigner) sign(content []byte, tc string) string {
	sig := ed25519.Sign(m.priv, content)
	blob := append([]byte{'E', 'd'}, m.keyID[:]...)
	blob = append(blob, sig...)
	global := ed25519.Sign(m.priv, append(append([]byte{}, sig...), []byte(tc)...))
	return "untrusted comment: test\n" +
		base64.StdEncoding.EncodeToString(blob) + "\n" +
		"trusted comment: " + tc + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n"
}

// writeSignedBundle writes skill.yaml (+ .minisig when sig != "") into the catalog.
func writeSignedBundle(t *testing.T, root, name, version, manifest, sig string) {
	t.Helper()
	dir := filepath.Join(root, name, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		if err := os.WriteFile(filepath.Join(dir, "skill.yaml.minisig"), []byte(sig), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newSkillsServer(t *testing.T, resolver *skills.Resolver) http.Handler {
	t.Helper()
	gw := gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
	s := New(gw)
	if resolver != nil {
		s = s.WithSkills(resolver)
	}
	return s.Handler()
}

const testManifest = `apiVersion: ironclaw.dev/skill/v1
name: incident-triage
version: 1.4.0
description: Triage.
grants:
  tools:
    - http_fetch
  egress:
    - api.pagerduty.com
`

func TestSkillsDisabledReturns503(t *testing.T) {
	h := newSkillsServer(t, nil)
	for _, c := range []struct {
		method, path string
	}{
		{http.MethodPost, "/v1/skills/install"},
		{http.MethodGet, "/v1/skills"},
		{http.MethodDelete, "/v1/skills/x"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: got %d, want 503", c.method, c.path, rec.Code)
		}
	}
}

func TestSkillInstallHappyPath(t *testing.T) {
	root := t.TempDir()
	signer := newMiniSigner(1)
	writeSignedBundle(t, root, "incident-triage", "1.4.0", testManifest, signer.sign([]byte(testManifest), "incident-triage 1.4.0"))
	trust, err := skills.LoadTrustRoot(signer.pubFile())
	if err != nil {
		t.Fatal(err)
	}
	resolver := &skills.Resolver{Source: skills.DirSource{Root: root}, Trust: trust, KnownTools: map[string]bool{"http_fetch": true}}
	h := newSkillsServer(t, resolver)

	body := `{"skill":"incident-triage","version":"1.4.0","agentGroupId":"grp-1","requestedBy":"cli:admin"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/skills/install", strings.NewReader(body)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("install: got %d (%s), want 202", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("expected a change id, got body %q err %v", rec.Body.String(), err)
	}
}

func TestSkillInstallRejectsUnsigned(t *testing.T) {
	root := t.TempDir()
	signer := newMiniSigner(2)
	// Bundle present but signed by a DIFFERENT key than the trust root → refused.
	writeSignedBundle(t, root, "incident-triage", "1.4.0", testManifest, newMiniSigner(99).sign([]byte(testManifest), "x"))
	trust, _ := skills.LoadTrustRoot(signer.pubFile())
	resolver := &skills.Resolver{Source: skills.DirSource{Root: root}, Trust: trust, KnownTools: map[string]bool{"http_fetch": true}}
	h := newSkillsServer(t, resolver)

	body := `{"skill":"incident-triage","version":"1.4.0","agentGroupId":"grp-1","requestedBy":"cli:admin"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/skills/install", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("untrusted bundle: got %d, want 400", rec.Code)
	}
}

func TestSkillInstallMissingFields(t *testing.T) {
	resolver := &skills.Resolver{Source: skills.DirSource{Root: t.TempDir()}, Trust: mustTrust(t), KnownTools: map[string]bool{}}
	h := newSkillsServer(t, resolver)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/skills/install", strings.NewReader(`{"skill":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400", rec.Code)
	}
}

func TestSkillListAndRemove(t *testing.T) {
	root := t.TempDir()
	writeSignedBundle(t, root, "triage", "1.0.0", testManifest, "sig")
	writeSignedBundle(t, root, "status", "0.1.0", testManifest, "sig")
	resolver := &skills.Resolver{Source: skills.DirSource{Root: root}}
	h := newSkillsServer(t, resolver)

	// list
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/skills", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: got %d", rec.Code)
	}
	var refs []skills.SkillRef
	if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil {
		t.Fatalf("decode refs: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2: %v", len(refs), refs)
	}

	// remove one
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/skills/triage", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: got %d, want 204", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "triage")); !os.IsNotExist(err) {
		t.Error("removed skill still present")
	}
}

func mustTrust(t *testing.T) *skills.TrustRoot {
	t.Helper()
	tr, err := skills.LoadTrustRoot(newMiniSigner(7).pubFile())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}
