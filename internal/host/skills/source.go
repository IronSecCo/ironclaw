// OWNER: T-227b (skills system — signature verification + curated source)

package skills

// This file is the fetch-time trust gate (T-227b). It answers the ClawHub
// "341 malicious skills" failure mode — open marketplace + auto-install — by
// keeping NEITHER half:
//
//  1. Curated source, never an agent-supplied URL. A skill is named (name@version)
//     and resolved ONLY from a location the operator configured (a Source the host
//     constructs from its own config). Nothing in the request path supplies a URL,
//     so an agent can never point the host at an attacker's bundle.
//  2. Signature verified BEFORE the manifest is ever parsed for approval. A bundle
//     whose detached signature does not verify against a host-configured trust root
//     is refused at fetch time and never reaches the gateway/approval step. The
//     gate is fail-closed: an empty trust root, a missing signature, an unknown
//     signing key, or a malformed signature all refuse the bundle.
//
// Trust scheme: minisign (https://jedisct1.github.io/minisign/), ed25519 over the
// raw bundle bytes ("legacy" mode). minisign is chosen over cosign because the
// trust root here is a small set of operator-held keys, not an OCI/Rekor/Fulcio
// transparency chain — ed25519 verification is a stdlib primitive (crypto/ed25519)
// with no added dependency, which keeps the sealed-runtime supply chain minimal.
// minisign's "prehashed" mode (BLAKE2b) is refused (it would require a new crypto
// dependency); bundles must be signed in legacy mode (`minisign -S`).
//
// The detached signature (a `.minisig` file alongside the manifest) covers exactly
// the bytes this layer fetches. Asset bundling and the install -> ChangeRequest
// mapping build on the verified Manifest this produces (T-227c/T-227d).

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- minisign wire format -------------------------------------------------
//
// Public key blob (base64 on the second line of a .pub file), 42 bytes:
//     sig_alg[2]="Ed"  ||  key_id[8]  ||  ed25519_public_key[32]
//
// Detached signature file (.minisig), four lines:
//     untrusted comment: <text>
//     base64( sig_alg[2] || key_id[8] || ed25519_signature[64] )   // 74 bytes
//     trusted comment: <text>
//     base64( ed25519_global_signature[64] )                       // 64 bytes
//
// sig_alg is "Ed" (legacy: signature over the raw content) or "ED" (prehashed:
// signature over BLAKE2b-512(content)). The global signature is ed25519 over
// (ed25519_signature || trusted_comment_bytes), which authenticates the trusted
// comment so it cannot be altered after signing.

const (
	pubKeyBlobLen    = 42 // "Ed"(2) + keyID(8) + pubkey(32)
	sigBlobLen       = 74 // alg(2) + keyID(8) + sig(64)
	globalSigBlobLen = 64
	trustedPrefix    = "trusted comment: "
)

// TrustRoot is the host-configured set of minisign public keys a skill bundle may
// be signed by. Keys are indexed by their 8-byte minisign key id; a signature is
// only checked against the key whose id it names, and refused outright if that key
// is not in the root.
type TrustRoot struct {
	keys map[[8]byte]ed25519.PublicKey
}

// LoadTrustRoot parses one or more minisign public keys (each either a full .pub
// file's two lines or just its base64 key line) into a trust root. At least one
// key is required — an empty trust root would verify nothing and the gate would be
// inert, so this fails closed rather than returning an accept-all root. Supplying
// several keys supports key rotation (old and new keys trusted during overlap).
func LoadTrustRoot(pubKeys ...string) (*TrustRoot, error) {
	if len(pubKeys) == 0 {
		return nil, errors.New("skills: trust root requires at least one public key")
	}
	tr := &TrustRoot{keys: make(map[[8]byte]ed25519.PublicKey, len(pubKeys))}
	for i, pk := range pubKeys {
		id, pub, err := parsePublicKey(pk)
		if err != nil {
			return nil, fmt.Errorf("skills: trust root key %d: %w", i, err)
		}
		tr.keys[id] = pub
	}
	return tr, nil
}

// Verify checks a detached minisign signature over content against the trust root.
// It returns nil only when the named key is trusted AND both the content signature
// and the trusted-comment (global) signature verify. Every other outcome — empty
// trust root, malformed signature, unknown key, prehashed mode, bad signature — is
// a non-nil error. The caller must treat any error as "refuse the bundle".
func (tr *TrustRoot) Verify(content []byte, minisig string) error {
	if tr == nil || len(tr.keys) == 0 {
		return errors.New("skills: empty trust root refuses all signatures")
	}
	sg, err := parseSignature(minisig)
	if err != nil {
		return err
	}
	pub, ok := tr.keys[sg.keyID]
	if !ok {
		return fmt.Errorf("skills: signing key id %s is not in the trust root", hex.EncodeToString(sg.keyID[:]))
	}

	switch string(sg.algo[:]) {
	case "Ed": // legacy: ed25519 over the raw content
		if !ed25519.Verify(pub, content, sg.sig[:]) {
			return errors.New("skills: content signature does not verify against the trust root")
		}
	case "ED": // prehashed (BLAKE2b) — would need a new crypto dependency
		return errors.New("skills: prehashed (BLAKE2b) minisign signatures are unsupported; re-sign in legacy mode (minisign -S)")
	default:
		return fmt.Errorf("skills: unknown signature algorithm %q", string(sg.algo[:]))
	}

	// The global signature binds the trusted comment to the content signature.
	global := make([]byte, 0, len(sg.sig)+len(sg.trustedComment))
	global = append(global, sg.sig[:]...)
	global = append(global, sg.trustedComment...)
	if !ed25519.Verify(pub, global, sg.globalSig[:]) {
		return errors.New("skills: trusted-comment signature does not verify")
	}
	return nil
}

// parsedSignature holds the decoded fields of a .minisig file.
type parsedSignature struct {
	algo           [2]byte
	keyID          [8]byte
	sig            [64]byte
	trustedComment string
	globalSig      [64]byte
}

func parseSignature(minisig string) (parsedSignature, error) {
	var sg parsedSignature
	lines := strings.Split(strings.ReplaceAll(minisig, "\r\n", "\n"), "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1] // tolerate a trailing newline
	}
	if len(lines) != 4 {
		return sg, fmt.Errorf("skills: malformed minisign signature: expected 4 lines, got %d", len(lines))
	}

	sigBlob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return sg, fmt.Errorf("skills: signature is not valid base64: %w", err)
	}
	if len(sigBlob) != sigBlobLen {
		return sg, fmt.Errorf("skills: signature blob must be %d bytes, got %d", sigBlobLen, len(sigBlob))
	}
	copy(sg.algo[:], sigBlob[0:2])
	copy(sg.keyID[:], sigBlob[2:10])
	copy(sg.sig[:], sigBlob[10:74])

	if !strings.HasPrefix(lines[2], trustedPrefix) {
		return sg, errors.New("skills: signature is missing its trusted comment line")
	}
	sg.trustedComment = lines[2][len(trustedPrefix):]

	globalBlob, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil {
		return sg, fmt.Errorf("skills: global signature is not valid base64: %w", err)
	}
	if len(globalBlob) != globalSigBlobLen {
		return sg, fmt.Errorf("skills: global signature must be %d bytes, got %d", globalSigBlobLen, len(globalBlob))
	}
	copy(sg.globalSig[:], globalBlob)
	return sg, nil
}

func parsePublicKey(pub string) (keyID [8]byte, key ed25519.PublicKey, err error) {
	line := lastDataLine(pub)
	if line == "" {
		return keyID, nil, errors.New("public key is empty")
	}
	blob, err := base64.StdEncoding.DecodeString(line)
	if err != nil {
		return keyID, nil, fmt.Errorf("public key is not valid base64: %w", err)
	}
	if len(blob) != pubKeyBlobLen {
		return keyID, nil, fmt.Errorf("public key must be %d bytes, got %d", pubKeyBlobLen, len(blob))
	}
	if blob[0] != 'E' || blob[1] != 'd' {
		return keyID, nil, fmt.Errorf("unsupported public key algorithm %q", string(blob[0:2]))
	}
	copy(keyID[:], blob[2:10])
	key = ed25519.PublicKey(append([]byte(nil), blob[10:42]...))
	return keyID, key, nil
}

// lastDataLine returns the last non-empty, non-comment line — the base64 payload
// of either a bare key line or a full two-line minisign .pub file.
func lastDataLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "untrusted comment:") || strings.HasPrefix(l, "trusted comment:") {
			continue
		}
		return l
	}
	return ""
}

// --- curated source -------------------------------------------------------

// Source is a host-configured origin for skill bundles. Its defining security
// property is the shape of this interface: a caller may only NAME a skill
// (name@version); it cannot supply a URL or path. The operator decides where
// skills come from by constructing the concrete Source from host config. No
// URL-taking Source is provided on purpose — that is the agent-supplied-URL vector
// the curated-source rule exists to remove.
type Source interface {
	// Open returns the manifest bytes and the detached minisign signature for the
	// named skill. A missing signature is an error (unsigned bundles are refused),
	// never an empty-but-ok result.
	Open(name, version string) (manifest []byte, signature string, err error)
}

const (
	manifestFileName  = "skill.yaml"
	signatureFileName = "skill.yaml.minisig"
)

// DirSource is the simplest curated Source: an operator-controlled directory laid
// out as <root>/<name>/<version>/{skill.yaml,skill.yaml.minisig}. A pinned Git
// checkout or an OCI pull populates that directory out of band, which keeps this
// package free of git/registry/network dependencies while still being a real,
// usable source. name/version are charset-validated and the resolved path is
// confined to the root, so a crafted name can never traverse out of it.
type DirSource struct {
	Root string
}

func (d DirSource) Open(name, version string) ([]byte, string, error) {
	if strings.TrimSpace(d.Root) == "" {
		return nil, "", errors.New("skills: DirSource has no configured root")
	}
	if !validName(name) {
		return nil, "", fmt.Errorf("skills: invalid skill name %q", name)
	}
	if !validVersion(version) {
		return nil, "", fmt.Errorf("skills: invalid skill version %q", version)
	}
	base := filepath.Join(d.Root, name, version)
	if !withinRoot(d.Root, base) {
		return nil, "", errors.New("skills: resolved skill path escapes the source root")
	}
	manifest, err := os.ReadFile(filepath.Join(base, manifestFileName))
	if err != nil {
		return nil, "", fmt.Errorf("skills: read manifest: %w", err)
	}
	sig, err := os.ReadFile(filepath.Join(base, signatureFileName))
	if err != nil {
		return nil, "", fmt.Errorf("skills: read signature (unsigned bundles are refused): %w", err)
	}
	return manifest, string(sig), nil
}

// withinRoot reports whether p resolves to a location inside root, guarding the
// filesystem fetch against traversal even if validation upstream regresses.
func withinRoot(root, p string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// validVersion accepts a non-empty semver-style version token (letters, digits,
// '.', '+', '-'). It forbids path separators and "."/"..", so a version can never
// participate in traversal when joined into a source path.
func validVersion(v string) bool {
	if v == "" || len(v) > 64 || v == "." || v == ".." {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// --- resolver -------------------------------------------------------------

// Resolver ties a curated Source to a TrustRoot: it fetches a named skill and
// refuses to return it unless the detached signature verifies — the fetch-time,
// fail-closed gate. Verification happens BEFORE the manifest is parsed, so an
// untrusted bundle is never even decoded into something an operator might approve.
type Resolver struct {
	Source     Source
	Trust      *TrustRoot
	KnownTools map[string]bool // compiled sandbox tool registry, passed to manifest validation
}

// Resolve fetches name@version from the curated source, verifies its signature
// against the trust root, then parses and validates the manifest. It additionally
// binds identity: the signed manifest's own name/version must match what was
// requested, so a validly-signed bundle cannot be served under another skill's
// name. Any failure returns a non-nil error and no manifest.
func (r *Resolver) Resolve(name, version string) (*Manifest, error) {
	if r.Source == nil {
		return nil, errors.New("skills: resolver has no curated source")
	}
	if r.Trust == nil || len(r.Trust.keys) == 0 {
		return nil, errors.New("skills: resolver refuses to fetch with an empty trust root")
	}

	manifestBytes, sig, err := r.Source.Open(name, version)
	if err != nil {
		return nil, fmt.Errorf("skills: fetch %s@%s: %w", name, version, err)
	}
	if err := r.Trust.Verify(manifestBytes, sig); err != nil {
		return nil, fmt.Errorf("skills: %s@%s refused at fetch time: %w", name, version, err)
	}

	m, err := Parse(manifestBytes, r.KnownTools)
	if err != nil {
		return nil, err
	}
	if m.Name != name || m.Version != version {
		return nil, fmt.Errorf("skills: signed bundle identity %s@%s does not match requested %s@%s",
			m.Name, m.Version, name, version)
	}
	return m, nil
}
