#!/usr/bin/env bash
# Regenerate the Homebrew formula (Formula/ironclaw.rb) from a published release.
#
#   scripts/update-homebrew-formula.sh [--out PATH] [TAG]
#
# With no TAG it resolves the latest release; pass a tag (e.g. v0.1.80) to pin a
# specific one. `--out PATH` writes the generated formula somewhere other than
# Formula/ironclaw.rb, which is how scripts/verify-homebrew-formula.sh re-derives
# the expected formula into a temp file and byte-compares it against a PR head.
#
# The SHA-256 of every binary archive is read from the release's SHA256SUMS — the
# same trust anchor install.sh uses — so the formula never carries a checksum that
# was not published with the release. Before any digest is read, SHA256SUMS is
# cosign-verified against the release's detached signature + certificate, pinned to
# the Release workflow's OIDC identity. A digest list whose signature nobody checked
# is not a trust anchor: it is whatever the last person to touch the release assets
# decided it should be. This verification is FAIL-CLOSED and has no opt-out.
#
# Requires: cosign (2.x), and gh (authenticated) OR a public release + curl.
set -euo pipefail

REPO="${IRONCLAW_REPO:-IronSecCo/ironclaw}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${ROOT}/Formula/ironclaw.rb"

say()  { printf '==> %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

TAG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || die "--out needs a PATH"; OUT="$2"; shift 2 ;;
    --out=*) OUT="${1#--out=}"; shift ;;
    -h|--help) sed -n '2,18p' "$0"; exit 0 ;;
    -*) die "unknown option: $1" ;;
    *) [ -z "$TAG" ] || die "unexpected extra argument: $1"; TAG="$1"; shift ;;
  esac
done

if [ -z "$TAG" ]; then
  say "Resolving latest release of ${REPO}"
  TAG="$(gh release view --repo "$REPO" --json tagName -q .tagName)" \
    || die "could not resolve latest release (is gh authenticated?)"
fi
VERSION="${TAG#v}"
say "Pinning formula to ${TAG} (version ${VERSION})"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

# fetch_asset <name> -> $tmp/<name>. gh first (works for private/draft releases and
# reuses the caller's auth), plain curl as the anonymous fallback.
fetch_asset() {
  local name="$1"
  if gh release download "$TAG" --repo "$REPO" --pattern "$name" --dir "$tmp" 2>/dev/null; then
    return 0
  fi
  curl -fsSL "https://github.com/${REPO}/releases/download/${TAG}/${name}" -o "$tmp/${name}" \
    || die "could not fetch ${name} for ${TAG} — is the release fully published?"
}

# Pull the trust anchor and the two artifacts that make it one.
fetch_asset SHA256SUMS
fetch_asset SHA256SUMS.sig
fetch_asset SHA256SUMS.pem

# The identity that signed the release. This is the FULL workflow identity, not a
# repo-prefix regexp: only .github/workflows/release.yml running on refs/heads/main
# is allowed to have produced this signature. Any other workflow in this repo — or
# release.yml running off a branch or a fork — is rejected.
COSIGN_IDENTITY="https://github.com/${REPO}/.github/workflows/release.yml@refs/heads/main"
COSIGN_ISSUER="https://token.actions.githubusercontent.com"

command -v cosign >/dev/null 2>&1 \
  || die "cosign not found. SHA256SUMS cannot be verified, so the formula cannot be
  derived from a trusted digest list. Install cosign 2.x (https://docs.sigstore.dev/
  cosign/installation/) and re-run. This is deliberately fail-closed: generating the
  formula from an unverified digest list is exactly the failure this script prevents."

say "cosign-verifying SHA256SUMS for ${TAG}"
say "  identity: ${COSIGN_IDENTITY}"
say "  issuer:   ${COSIGN_ISSUER}"
# NOTE: cosign's own --output-certificate writes the cert base64-ENCODED, so the
# published SHA256SUMS.pem is a base64-wrapped PEM. cosign verify-blob accepts that
# wrapper directly, so it is passed through as-is. If you ever hand this file to
# openssl instead, `base64 -d` it first — otherwise openssl reports "could not find
# certificate", which reads exactly like a forged signature (IRO-665).
cosign verify-blob "$tmp/SHA256SUMS" \
  --signature "$tmp/SHA256SUMS.sig" \
  --certificate "$tmp/SHA256SUMS.pem" \
  --certificate-identity "$COSIGN_IDENTITY" \
  --certificate-oidc-issuer "$COSIGN_ISSUER" \
  || die "cosign could NOT verify SHA256SUMS for ${TAG} against ${COSIGN_IDENTITY}.
  Refusing to derive a formula from an unverified digest list. Either the release
  signature is missing/untrusted, or the assets were replaced after publication —
  treat ${TAG} as suspect and see the release runbook."
say "cosign OK — SHA256SUMS for ${TAG} is signed by the Release workflow."

# sum <archive-name> -> the sha256 recorded in SHA256SUMS (fail closed if absent).
sum() {
  local name="$1" got
  got="$(grep -F "$name" "$tmp/SHA256SUMS" | awk '{print $1}' | head -n1 || true)"
  [ -n "$got" ] || die "no SHA256SUMS entry for ${name} — is ${TAG} fully published?"
  printf '%s' "$got"
}

DARWIN_ARM64="$(sum "ironclaw_${VERSION}_darwin_arm64.tar.gz")"
DARWIN_AMD64="$(sum "ironclaw_${VERSION}_darwin_amd64.tar.gz")"
LINUX_ARM64="$(sum "ironclaw_${VERSION}_linux_arm64.tar.gz")"
LINUX_AMD64="$(sum "ironclaw_${VERSION}_linux_amd64.tar.gz")"

base="https://github.com/${REPO}/releases/download/${TAG}"

mkdir -p "$(dirname "$OUT")"
# NOTE: this is an UNQUOTED heredoc so ${VERSION}/${TAG}/${base}/${sha} expand. Do
# NOT put backticks or $(...) in the template below — the shell would execute them.
# Ruby string interpolation (#{version}, #{bin}) is safe (no leading $).
cat > "$OUT" <<EOF
# typed: false
# frozen_string_literal: true

# IronClaw — security-hardened, self-hosted AI assistant platform.
#
# This formula is GENERATED by scripts/update-homebrew-formula.sh from a published
# release's SHA256SUMS — do not hand-edit the version/url/sha256 lines. To track a
# newer release, re-run that script and commit the result.
#
# Install:  brew tap IronSecCo/ironclaw https://github.com/IronSecCo/ironclaw
#           brew install ironsecco/ironclaw/ironclaw
#
# NOTE: homebrew-core ships an UNRELATED formula also named "ironclaw", and core
# wins the bare name. Always install the fully-qualified tap formula above; a bare
# "brew install ironclaw" would fetch the core package, not this one.
class Ironclaw < Formula
  desc "Security-hardened, self-hosted AI assistant platform (secured Go port)"
  homepage "https://github.com/IronSecCo/ironclaw"
  version "${VERSION}"
  license "AGPL-3.0-or-later"

  on_macos do
    on_arm do
      url "${base}/ironclaw_${VERSION}_darwin_arm64.tar.gz"
      sha256 "${DARWIN_ARM64}"
    end
    on_intel do
      url "${base}/ironclaw_${VERSION}_darwin_amd64.tar.gz"
      sha256 "${DARWIN_AMD64}"
    end
  end

  on_linux do
    on_arm do
      url "${base}/ironclaw_${VERSION}_linux_arm64.tar.gz"
      sha256 "${LINUX_ARM64}"
    end
    on_intel do
      url "${base}/ironclaw_${VERSION}_linux_amd64.tar.gz"
      sha256 "${LINUX_AMD64}"
    end
  end

  def install
    bin.install "ironctl"
    bin.install "ironclaw-controlplane"
    bin.install "ironclaw-sandbox"
  end

  def caveats
    <<~EOS
      ironctl, ironclaw-controlplane, and ironclaw-sandbox are now on your PATH.

      Get started:
        ironctl version
        ironctl doctor      # preflight: model creds, toolchain, sockets

      For production the control plane usually runs as a container:
        ghcr.io/ironsecco/ironclaw-controlplane:${TAG}
      See https://ironsecco.github.io/ironclaw/quickstart/ for the full first-run flow.
    EOS
  end

  test do
    assert_match "ironctl v#{version}", shell_output("#{bin}/ironctl version")
  end
end
EOF

say "Wrote ${OUT}"
say "Pinned archives (sha256 from ${TAG} SHA256SUMS):"
printf '  darwin/arm64  %s\n' "$DARWIN_ARM64"
printf '  darwin/amd64  %s\n' "$DARWIN_AMD64"
printf '  linux/arm64   %s\n' "$LINUX_ARM64"
printf '  linux/amd64   %s\n' "$LINUX_AMD64"
