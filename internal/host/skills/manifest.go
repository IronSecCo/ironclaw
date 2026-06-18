// Package skills implements IronClaw's host-side skills system. A skill is a
// declarative, host-curated *capability bundle* — a persona fragment, an enabled
// subset of the COMPILED sandbox tools, approved egress hosts, and read-only
// reference assets — and never executable code that runs in the sandbox. This
// preserves the sealed-runtime / no-interpreter / no-in-sandbox-install pillar:
// a skill only composes capabilities the binary already implements, and only via
// the gateway's human-approval flow.
//
// This file is the schema + loader: it parses and validates skill.yaml
// into a typed Manifest, failing closed on anything malformed or out of policy.
// Signature verification and the install→ChangeRequest mapping
// build on the validated Manifest this produces.
package skills

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only skill-manifest schema version this loader accepts in v1.
const APIVersion = "ironclaw.dev/skill/v1"

// Manifest is a parsed, validated skill.yaml. Field names mirror the manifest
// format in the spike (§3). A skill declares intent; it carries no runtime.
type Manifest struct {
	APIVersion  string `yaml:"apiVersion"`
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	Grants      Grants `yaml:"grants"`
	// Signature is the provenance attestation over (manifest + asset tree). It is
	// recorded here but VERIFIED by T-227b against a host-configured trust root;
	// this loader does not trust it.
	Signature string `yaml:"signature"`
}

// Grants is everything a skill contributes to an agent group. Each ingredient
// maps to an existing gateway-governed mechanism, so installing a skill is a set
// of human-approved capability changes — not new code.
type Grants struct {
	// Persona is appended to the group persona (a persona change-kind).
	Persona string `yaml:"persona"`
	// Tools is the subset of COMPILED sandbox tools the skill enables. It can
	// never name a tool the binary does not already implement (enforced in
	// Validate against the supplied registry).
	Tools []string `yaml:"tools"`
	// Egress are bare hostnames added to the deny-by-default egress-broker
	// allowlist. No wildcards in v1 — every host is explicit and
	// human-approved.
	Egress []string `yaml:"egress"`
	// Assets are bundled files mounted READ-ONLY at /skills/<name>. They are data,
	// never executed; the mount enforcement (nosuid,nodev,noexec) is T-227d.
	Assets []string `yaml:"assets"`
}

// Load reads a skill.yaml from r and returns the validated Manifest. knownTools
// is the compiled sandbox tool registry (e.g. the names registered by
// cmd/sandbox/buildTools); a skill may only enable tools present in it. A
// malformed or out-of-policy manifest fails closed with a clear, wrapped error —
// the host validates here BEFORE any install ChangeRequest is created.
func Load(r io.Reader, knownTools map[string]bool) (*Manifest, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("skills: read manifest: %w", err)
	}
	return Parse(data, knownTools)
}

// Parse is Load over an in-memory manifest. Unknown YAML keys are rejected
// (KnownFields) so a manifest cannot smuggle in fields the policy does not model.
func Parse(data []byte, knownTools map[string]bool) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("skills: parse manifest: %w", err)
	}
	if err := m.Validate(knownTools); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate enforces the v1 manifest policy and returns a single error listing
// every problem (so a contributor sees them all at once). It fails closed: any
// violation is an error, never a silent drop.
func (m *Manifest) Validate(knownTools map[string]bool) error {
	var problems []string

	if m.APIVersion != APIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion %q is not the supported %q", m.APIVersion, APIVersion))
	}
	if !validName(m.Name) {
		problems = append(problems, fmt.Sprintf("name %q must be a non-empty lowercase label (a-z, 0-9, -; no leading/trailing -)", m.Name))
	}
	if strings.TrimSpace(m.Version) == "" {
		problems = append(problems, "version is required")
	}

	for _, t := range m.Grants.Tools {
		if !knownTools[t] {
			problems = append(problems, fmt.Sprintf("tool %q is not in the compiled sandbox tool registry", t))
		}
	}
	for _, h := range m.Grants.Egress {
		if !validHostname(h) {
			problems = append(problems, fmt.Sprintf("egress %q must be a bare hostname (no scheme, port, path, or wildcard)", h))
		}
	}
	for _, a := range m.Grants.Assets {
		if !validAssetPath(a) {
			problems = append(problems, fmt.Sprintf("asset %q must be a relative path inside the skill (no absolute path or ..)", a))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("skills: invalid manifest %q: %s", m.Name, strings.Join(problems, "; "))
	}
	return nil
}

// validName accepts a non-empty lowercase DNS-style label used as the skill name
// (and its /skills/<name> mount point).
func validName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return s[0] != '-' && s[len(s)-1] != '-'
}

// validHostname accepts a bare hostname only: no scheme, port, path, wildcard, or
// whitespace, and well-formed DNS labels. This is what becomes an explicit
// egress-broker allowlist entry.
func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	if strings.ContainsAny(h, "*/:\\?#@ \t") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

// validAssetPath accepts a relative path within the skill bundle — no absolute
// paths and no traversal — so an asset can only ever resolve under /skills/<name>.
func validAssetPath(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
