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

# --- auth (optional; required for a private repo) ----------------------------
TOKEN="${GITHUB_TOKEN:-${GH_TOKEN:-}}"

# --- downloader (curl or wget) ----------------------------------------------
# api_get  GETs a URL to stdout (the JSON API).
# download GETs a URL to a file. In token mode it sends an octet-stream Accept
#          header so private-repo release assets — which 404 on the public
#          browser URL — download via the GitHub API.
if command -v curl >/dev/null 2>&1; then
  api_get()  { if [ -n "$TOKEN" ]; then curl -fsSL -H "Authorization: Bearer ${TOKEN}" "$1"; else curl -fsSL "$1"; fi; }
  download() { if [ -n "$TOKEN" ]; then curl -fsSL -H "Authorization: Bearer ${TOKEN}" -H "Accept: application/octet-stream" -o "$2" "$1"; else curl -fsSL -o "$2" "$1"; fi; }
elif command -v wget >/dev/null 2>&1; then
  api_get()  { if [ -n "$TOKEN" ]; then wget -qO- --header="Authorization: Bearer ${TOKEN}" "$1"; else wget -qO- "$1"; fi; }
  download() { if [ -n "$TOKEN" ]; then wget -qO "$2" --header="Authorization: Bearer ${TOKEN}" --header="Accept: application/octet-stream" "$1"; else wget -qO "$2" "$1"; fi; }
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
json="$(api_get "$api")" || die "could not query GitHub API ($api) — a private repo needs GITHUB_TOKEN"

# dl_url <name-suffix> echoes the download URL for the matching asset: the asset
# API URL in token mode (private-repo safe), else the public browser_download_url.
dl_url() {
  if [ -n "$TOKEN" ]; then
    printf '%s\n' "$json" | awk -v t="$1" '
      /"url": *"https:\/\/api\.github\.com\/.*\/releases\/assets\/[0-9]+"/ { match($0, /https:[^"]+/); u = substr($0, RSTART, RLENGTH) }
      index($0, t) && /browser_download_url/ { print u; exit }'
  else
    printf '%s\n' "$json" | grep -o "https://[^\"]*$1" | head -n1
  fi
}

asset="$(printf '%s\n' "$json" | grep -o "ironclaw_[A-Za-z0-9._-]*_${target}\.tar\.gz" | head -n1 || true)"
[ -n "$asset" ] || die "no asset for $target in release '$VERSION' — see https://github.com/${REPO}/releases"
asset_url="$(dl_url "_${target}.tar.gz")"
[ -n "$asset_url" ] || die "could not resolve a download URL for $asset"
sums_url="$(dl_url "SHA256SUMS" || true)"
say "Asset: $asset"

# --- download + verify -------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM
download "$asset_url" "$tmp/$asset" || die "download failed for $asset"

if [ -n "$sums_url" ]; then
  if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | awk '{print $1}'; }
  elif command -v shasum  >/dev/null 2>&1; then sha() { shasum -a 256 "$1" | awk '{print $1}'; }
  else sha() { echo ""; }
  fi
  got="$(sha "$tmp/$asset")"
  if [ -n "$got" ] && download "$sums_url" "$tmp/SHA256SUMS" 2>/dev/null; then
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
