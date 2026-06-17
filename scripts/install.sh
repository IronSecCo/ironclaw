#!/bin/sh
# IronClaw binary installer (macOS / Linux).
#
#   curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | sh
#
# Resolves a release from GitHub, downloads the archive for your OS/arch, verifies
# its SHA-256 checksum, and installs `ironctl` + `ironclaw-controlplane`.
#
# Tunables (environment):
#   IRONCLAW_VERSION   release tag to install, e.g. v0.1.66   (default: latest)
#   IRONCLAW_BINDIR    install directory                       (default: /usr/local/bin when
#                                                               writable/root, else ~/.local/bin)
#   IRONCLAW_REPO      owner/name of the GitHub repo           (default: nivardsec/ironclaw)
#   GITHUB_TOKEN       optional; raises the GitHub API rate limit
#
# Windows users: download the .zip from the Releases page, or run scripts/install.ps1.
set -eu

REPO="${IRONCLAW_REPO:-nivardsec/ironclaw}"
VERSION="${IRONCLAW_VERSION:-latest}"

say()  { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

# --- platform ----------------------------------------------------------------
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Linux)  os="linux" ;;
  Darwin) os="darwin" ;;
  *) die "unsupported OS '$os' — for Windows use scripts/install.ps1 or the Releases page" ;;
esac
case "$arch" in
  x86_64 | amd64)  arch="amd64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) die "unsupported architecture '$arch'" ;;
esac
target="${os}_${arch}"
say "Platform: $target"

# --- downloader (curl or wget) ----------------------------------------------
if command -v curl >/dev/null 2>&1; then
  fetch()  { curl -fsSL ${GITHUB_TOKEN:+-H "Authorization: Bearer ${GITHUB_TOKEN}"} "$1"; }
  fetchto(){ curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch()  { wget -qO- "$1"; }
  fetchto(){ wget -qO "$2" "$1"; }
else
  die "need curl or wget on PATH"
fi
command -v tar >/dev/null 2>&1 || die "need tar on PATH"

# --- resolve the release -----------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  api="https://api.github.com/repos/${REPO}/releases/latest"
else
  api="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
fi
say "Resolving release ($VERSION)"
json="$(fetch "$api")" || die "could not query GitHub API ($api)"

asset_url="$(printf '%s\n' "$json" | grep -o "https://[^\"]*_${target}\.tar\.gz" | head -n1 || true)"
[ -n "$asset_url" ] || die "no asset for $target in release '$VERSION' — see https://github.com/${REPO}/releases"
sums_url="$(printf '%s\n' "$json" | grep -o "https://[^\"]*/SHA256SUMS" | head -n1 || true)"
asset="$(basename "$asset_url")"
say "Asset: $asset"

# --- download + verify -------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
fetchto "$asset_url" "$tmp/$asset" || die "download failed: $asset_url"

if [ -n "$sums_url" ]; then
  if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | awk '{print $1}'; }
  elif command -v shasum  >/dev/null 2>&1; then sha() { shasum -a 256 "$1" | awk '{print $1}'; }
  else sha() { echo ""; }
  fi
  got="$(sha "$tmp/$asset")"
  if [ -n "$got" ] && fetchto "$sums_url" "$tmp/SHA256SUMS" 2>/dev/null; then
    want="$(grep -F "$asset" "$tmp/SHA256SUMS" | awk '{print $1}' | head -n1 || true)"
    [ -n "$want" ] && [ "$got" != "$want" ] && die "checksum mismatch for $asset (want $want, got $got)"
    [ -n "$want" ] && say "Checksum OK"
  else
    warn "skipping checksum verification (no checksum tool or SHA256SUMS)"
  fi
fi

# --- extract -----------------------------------------------------------------
( cd "$tmp" && tar -xzf "$asset" )

# --- choose an install dir ---------------------------------------------------
if [ -n "${IRONCLAW_BINDIR:-}" ]; then
  bindir="$IRONCLAW_BINDIR"
elif [ "$(id -u)" = 0 ]; then
  bindir="/usr/local/bin"
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
  bindir="/usr/local/bin"
else
  bindir="$HOME/.local/bin"
fi
mkdir -p "$bindir" || die "cannot create $bindir (set IRONCLAW_BINDIR, or re-run with sudo)"

install_bin() {
  b="$1"
  [ -f "$tmp/$b" ] || { warn "$b not in archive — skipping"; return; }
  chmod +x "$tmp/$b"
  install -m 0755 "$tmp/$b" "$bindir/$b" 2>/dev/null || cp "$tmp/$b" "$bindir/$b" \
    || die "cannot write to $bindir (set IRONCLAW_BINDIR, or re-run with sudo)"
  say "Installed $bindir/$b"
}
install_bin ironctl
install_bin ironclaw-controlplane

# --- PATH hint + verify ------------------------------------------------------
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) warn "$bindir is not on your PATH — add: export PATH=\"$bindir:\$PATH\"" ;;
esac
if [ -x "$bindir/ironctl" ]; then
  say "Done: $("$bindir/ironctl" --version 2>/dev/null || echo "ironctl installed")"
else
  say "Done."
fi
