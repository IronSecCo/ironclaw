package skills

// Native Go fuzz targets over the skills package's untrusted-input parsing and
// path-handling surfaces. A skill bundle is fetched from a curated source but its
// bytes (manifest YAML, minisign signature, public-key blobs) and the name/version
// identifiers that compose filesystem paths are all attacker-influenced inputs that
// reach a parser. These targets assert the two properties that must hold no matter
// what bytes arrive:
//
//   1. No parser panics — malformed input fails closed with an error, never a crash
//      (a panic in the host control-plane is a DoS / availability boundary break).
//   2. Validated identifiers and asset paths can never participate in traversal —
//      anything the validators accept stays confined when joined into a path.
//
// Run a target locally with e.g.
//
//	go test ./internal/host/skills -run=Fuzz -fuzz=FuzzParse -fuzztime=30s
//
// CI runs each target for a short bounded time so OpenSSF Scorecard detects fuzzing
// (see .github/workflows/ci.yml fuzz job). The traversal seeds below ("..", "../x",
// absolute paths, NUL bytes) double as the shared corpus for any path-injection
// validation work under the Code Security parent (IRO-82).

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParse exercises the manifest decoder + validator over arbitrary bytes. The
// manifest is the richest untrusted surface: a YAML document that must fail closed
// on anything malformed or out of policy. The invariant: Parse must never panic,
// and any manifest it accepts must satisfy the policy it claims to enforce (name is
// a valid label, every asset path is confined). This catches a validator that
// regresses open as much as a decoder that crashes.
func FuzzParse(f *testing.F) {
	tools := knownTools()
	f.Add([]byte(validManifest))
	f.Add([]byte(""))
	f.Add([]byte("apiVersion: ironclaw.dev/skill/v1\nname: x\nversion: 1\n"))
	// Out-of-policy / hostile seeds: unknown key, traversal asset, wildcard egress.
	f.Add([]byte("apiVersion: ironclaw.dev/skill/v1\nname: x\nversion: 1\nsmuggled: true\n"))
	f.Add([]byte("apiVersion: ironclaw.dev/skill/v1\nname: x\nversion: 1\ngrants:\n  assets:\n    - ../../etc/passwd\n"))
	f.Add([]byte("apiVersion: ironclaw.dev/skill/v1\nname: BAD NAME\nversion: 1\ngrants:\n  egress:\n    - '*.evil.com'\n"))
	// YAML pathologies that must not hang or crash the decoder.
	f.Add([]byte("name: &a [*a]\n"))
	f.Add([]byte(strings.Repeat("- ", 4096)))

	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data, tools)
		if err != nil {
			if m != nil {
				t.Fatalf("Parse returned a manifest alongside an error: %v", err)
			}
			return
		}
		// Accepted: the manifest must actually be in-policy. Validate is the contract
		// the rest of the host trusts, so re-checking it here is the no-silent-regress
		// guard, not redundant work.
		if err := m.Validate(tools); err != nil {
			t.Fatalf("Parse accepted a manifest that fails its own Validate: %v", err)
		}
		if !validName(m.Name) {
			t.Fatalf("Parse accepted manifest with invalid name %q (becomes /skills/<name> mount)", m.Name)
		}
		for _, a := range m.Grants.Assets {
			if !validAssetPath(a) {
				t.Fatalf("Parse accepted manifest with non-confined asset path %q", a)
			}
		}
	})
}

// FuzzParseSignature exercises the minisign detached-signature parser over arbitrary
// strings. This runs BEFORE any manifest is trusted, so a crash here is reachable
// with bytes an attacker fully controls. Invariant: never panic; malformed input
// returns an error.
func FuzzParseSignature(f *testing.F) {
	f.Add("")
	f.Add("untrusted comment: x\nAAAA\ntrusted comment: y\nBBBB\n")
	// Shape-valid frame with junk base64 payloads.
	f.Add("untrusted comment: t\n" + strings.Repeat("A", 100) + "\ntrusted comment: t\n" + strings.Repeat("B", 88) + "\n")
	f.Add("only one line")
	f.Add("a\nb\nc\nd\ne\n")
	f.Add("\r\n\r\n\r\n\r\n")

	f.Fuzz(func(t *testing.T, minisig string) {
		// Discard result: we only assert it does not panic. A well-formed-looking
		// signature with non-verifying bytes is a legitimate (error) outcome.
		_, _ = parseSignature(minisig)
	})
}

// FuzzParsePublicKey exercises the minisign public-key blob parser. Operator config
// supplies these, but a malformed key must fail closed (LoadTrustRoot rejects it),
// never crash. Invariant: never panic.
func FuzzParsePublicKey(f *testing.F) {
	f.Add("")
	f.Add("untrusted comment: minisign public key\nRWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3")
	f.Add(strings.Repeat("A", 56)) // 56 base64 chars decodes to ~42 bytes
	f.Add("not base64 @@@@")

	f.Fuzz(func(t *testing.T, pub string) {
		_, _, _ = parsePublicKey(pub)
	})
}

// FuzzValidAssetPath is the path-resolution security target. Whatever bytes arrive,
// validAssetPath must never accept a path that escapes the per-skill mount root.
// The assertion resolves every accepted path under a synthetic /skills/<name> and
// fails if it lands outside — the property the egress/asset mount relies on.
func FuzzValidAssetPath(f *testing.F) {
	for _, s := range []string{
		"templates/status.md", "runbooks/sev1.md", "a", ".", "..", "../x",
		"/etc/passwd", "a/../../b", "a/./b", "", "a\x00b", "foo/..", "./..",
		strings.Repeat("../", 64) + "x",
	} {
		f.Add(s)
	}
	const mount = "/skills/example"
	f.Fuzz(func(t *testing.T, p string) {
		if !validAssetPath(p) {
			return
		}
		// Accepted paths must be relative, traversal-free, and resolve under the mount.
		if strings.ContainsRune(p, 0) {
			t.Fatalf("validAssetPath accepted a path containing NUL: %q", p)
		}
		resolved := filepath.Join(mount, filepath.FromSlash(p))
		rel, err := filepath.Rel(mount, resolved)
		if err != nil {
			t.Fatalf("accepted asset %q does not resolve relative to mount: %v", p, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("validAssetPath accepted a path that escapes the mount: %q -> %q", p, rel)
		}
		// path.Clean of an accepted path must also stay confined (defense in depth).
		if c := path.Clean(p); c == ".." || strings.HasPrefix(c, "../") {
			t.Fatalf("validAssetPath accepted a path whose clean form traverses: %q -> %q", p, c)
		}
	})
}

// FuzzValidIdentifiers fuzzes the name/version validators that compose source paths
// (<root>/<name>/<version>/skill.yaml). Invariant: an accepted name or version
// contains no path separator and is never "."/".." — otherwise it could traverse
// out of the catalog root when joined.
func FuzzValidIdentifiers(f *testing.F) {
	for _, s := range []string{
		"incident-triage", "1.4.0", "", "-x", "x-", "..", ".", "/", "a/b",
		"a\\b", "A", "1+build.5", strings.Repeat("a", 64), strings.Repeat("a", 65),
		"a..b", "C:\\x",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if validName(s) {
			if strings.ContainsAny(s, "/\\") || s == "." || s == ".." {
				t.Fatalf("validName accepted a traversal-capable name: %q", s)
			}
		}
		if validVersion(s) {
			if strings.ContainsAny(s, "/\\") || s == "." || s == ".." {
				t.Fatalf("validVersion accepted a traversal-capable version: %q", s)
			}
		}
		if validHostname(s) {
			if strings.ContainsAny(s, "*/:\\?#@ \t") {
				t.Fatalf("validHostname accepted a host with a forbidden char: %q", s)
			}
		}
	})
}
