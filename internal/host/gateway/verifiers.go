// OWNER: AGENT1

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// The verifiers here are deterministic, dependency-free, and additive: each can
// only ADD a rejection. They run BEFORE AlwaysRequireHuman in the chain, so a
// clean change still hits the human floor — they never bypass it.

// MountAllowlistVerifier rejects mount changes that escape an allowlist of
// permitted host path prefixes. It parses the ChangeMounts After payload, which is
// a JSON array of mount specs each carrying a host "source" path. A source that
// contains ".." or is an absolute path outside every allowed prefix is rejected.
type MountAllowlistVerifier struct {
	// AllowedPrefixes are absolute host path prefixes a mount source may live under.
	AllowedPrefixes []string
}

// mountSpec is the minimal shape we read from a ChangeMounts payload.
type mountSpec struct {
	Source string `json:"source"`
}

// Name identifies the verifier.
func (MountAllowlistVerifier) Name() string { return "mount-allowlist" }

// Verify rejects out-of-allowlist or traversal mount sources. It applies only to
// ChangeMounts; other kinds pass through untouched.
func (v MountAllowlistVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if req.Kind != contract.ChangeMounts {
		return contract.VerdictPass, "", nil
	}
	if len(req.After) == 0 {
		return contract.VerdictPass, "no mounts", nil
	}
	var mounts []mountSpec
	if err := json.Unmarshal(req.After, &mounts); err != nil {
		// Unparseable mount payload is suspicious — reject rather than guess.
		return contract.VerdictReject, "unparseable mounts payload", nil
	}
	for _, m := range mounts {
		src := strings.TrimSpace(m.Source)
		if src == "" {
			return contract.VerdictReject, "mount with empty source", nil
		}
		if strings.Contains(src, "..") {
			return contract.VerdictReject, fmt.Sprintf("mount source contains path traversal: %q", src), nil
		}
		if !v.allowed(src) {
			return contract.VerdictReject, fmt.Sprintf("mount source outside allowlist: %q", src), nil
		}
	}
	return contract.VerdictPass, "mounts within allowlist", nil
}

// allowed reports whether src is under one of the allowed prefixes. A source must
// be absolute and clean to be considered.
func (v MountAllowlistVerifier) allowed(src string) bool {
	if !filepath.IsAbs(src) {
		return false
	}
	clean := filepath.Clean(src)
	for _, p := range v.AllowedPrefixes {
		p = filepath.Clean(p)
		if clean == p || strings.HasPrefix(clean, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// packageNameRe matches a safe package name: alphanumerics plus a small set of
// punctuation common in npm/apt names. Anything else (shell metacharacters,
// whitespace, control chars) is rejected.
var packageNameRe = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._@/+-]*$`)

// PackageNameVerifier rejects package install changes whose names contain shell
// metacharacters. It accepts either of two ChangePackages After payload shapes
// (see docs/contract.md "Capability-change payload conventions"):
//   - a flat JSON array of names: ["ripgrep", "@scope/pkg"]
//   - the container package config object: {"apt": ["..."], "npm": ["..."]}
type PackageNameVerifier struct{}

// Name identifies the verifier.
func (PackageNameVerifier) Name() string { return "package-name" }

// Verify rejects unsafe package names. It applies only to ChangePackages; other
// kinds pass through untouched.
func (PackageNameVerifier) Verify(ctx context.Context, req contract.ChangeRequest) (contract.Verdict, string, error) {
	if req.Kind != contract.ChangePackages {
		return contract.VerdictPass, "", nil
	}
	if len(req.After) == 0 {
		return contract.VerdictPass, "no packages", nil
	}
	pkgs, ok := parsePackageNames(req.After)
	if !ok {
		return contract.VerdictReject, "unparseable packages payload", nil
	}
	for _, p := range pkgs {
		name := strings.TrimSpace(p)
		if !packageNameRe.MatchString(name) {
			return contract.VerdictReject, fmt.Sprintf("unsafe package name: %q", p), nil
		}
	}
	return contract.VerdictPass, "package names clean", nil
}

// parsePackageNames extracts package names from either supported payload shape: a
// flat array of strings, or an {"apt":[...],"npm":[...]} object. ok is false if
// the payload is neither.
func parsePackageNames(after json.RawMessage) (names []string, ok bool) {
	var flat []string
	if err := json.Unmarshal(after, &flat); err == nil {
		return flat, true
	}
	var obj struct {
		APT []string `json:"apt"`
		NPM []string `json:"npm"`
	}
	if err := json.Unmarshal(after, &obj); err == nil {
		return append(append([]string{}, obj.APT...), obj.NPM...), true
	}
	return nil, false
}
