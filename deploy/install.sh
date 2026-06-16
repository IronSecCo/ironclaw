#!/usr/bin/env bash
# OWNER: T-113
#
# IronClaw control-plane installer.
#
# A real, idempotent installer: it builds the control-plane + CLI, installs them,
# provisions the config/state dirs and a 0600 env file (generating an API token on
# first run), and installs + enables the service for the host OS (systemd on Linux,
# launchd on macOS). Re-running is safe: existing secrets are preserved.
#
# Run from the repository root:  sudo deploy/install.sh
#
# Tunables via environment:
#   IRONCLAW_API_ADDR            API bind address      (default: tailnet IP:8787, else 127.0.0.1:8787)
#   IRONCLAW_STATE_DIR           state directory       (default: /var/lib/ironclaw)
#   IRONCLAW_MODEL_PROXY_SOCKET  model-proxy socket    (default: /run/ironclaw/modelproxy.sock)
#   IRONCLAW_BUNDLE_ROOT         OCI bundle root       (default: $STATE_DIR/bundles)
#   IRONCLAW_SANDBOX_IMAGE       sandbox image ref     (default: ironclaw-sandbox:latest)
#   ANTHROPIC_API_KEY            host model credential (left blank if unset)
set -euo pipefail

# ---------------------------------------------------------------------------
# Resolve paths and defaults
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OS="$(uname -s)"
PREFIX="${PREFIX:-/usr/local}"
BINDIR="${PREFIX}/bin"
CONFIG_DIR="/etc/ironclaw"
ENV_FILE="${CONFIG_DIR}/ironclaw.env"

STATE_DIR="${IRONCLAW_STATE_DIR:-/var/lib/ironclaw}"
MODEL_PROXY_SOCKET="${IRONCLAW_MODEL_PROXY_SOCKET:-/run/ironclaw/modelproxy.sock}"
BUNDLE_ROOT="${IRONCLAW_BUNDLE_ROOT:-${STATE_DIR}/bundles}"
SANDBOX_IMAGE="${IRONCLAW_SANDBOX_IMAGE:-ironclaw-sandbox:latest}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Pick an API bind address: prefer the tailnet IP so the API is mesh-only.
# ---------------------------------------------------------------------------
default_api_addr() {
  if command -v tailscale >/dev/null 2>&1; then
    local ip
    ip="$(tailscale ip -4 2>/dev/null | head -n1 || true)"
    if [ -n "${ip}" ]; then
      printf '%s:8787' "${ip}"
      return
    fi
  fi
  printf '127.0.0.1:8787'
}
API_ADDR="${IRONCLAW_API_ADDR:-$(default_api_addr)}"

# ---------------------------------------------------------------------------
# 1. Build the control-plane + CLI
# ---------------------------------------------------------------------------
log "Building control-plane and ironctl (CGO_ENABLED=1)"
command -v go >/dev/null 2>&1 || die "go toolchain not found on PATH"
( cd "${REPO_ROOT}" && CGO_ENABLED=1 go build -o "${REPO_ROOT}/.bin/ironclaw-controlplane" ./cmd/controlplane )
( cd "${REPO_ROOT}" && CGO_ENABLED=1 go build -o "${REPO_ROOT}/.bin/ironctl" ./cmd/ironctl )

log "Installing binaries into ${BINDIR}"
install -d -m 0755 "${BINDIR}"
install -m 0755 "${REPO_ROOT}/.bin/ironclaw-controlplane" "${BINDIR}/ironclaw-controlplane"
install -m 0755 "${REPO_ROOT}/.bin/ironctl" "${BINDIR}/ironctl"

# ---------------------------------------------------------------------------
# 2. Config, state, and the 0600 env file (secrets live ONLY here)
# ---------------------------------------------------------------------------
log "Provisioning ${CONFIG_DIR} and ${STATE_DIR}"
install -d -m 0700 "${CONFIG_DIR}"
install -d -m 0700 "${STATE_DIR}"
install -d -m 0700 "${BUNDLE_ROOT}"
install -d -m 0700 "$(dirname "${MODEL_PROXY_SOCKET}")"

# Generate an admin API bearer token once; preserve it on re-install.
gen_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}
if [ -f "${ENV_FILE}" ] && grep -q '^IRONCLAW_API_TOKEN=' "${ENV_FILE}"; then
  log "Preserving existing API token in ${ENV_FILE}"
  API_TOKEN="$(grep '^IRONCLAW_API_TOKEN=' "${ENV_FILE}" | head -n1 | cut -d= -f2-)"
else
  API_TOKEN="$(gen_token)"
fi
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}"

umask 077
cat >"${ENV_FILE}" <<ENV
# IronClaw control-plane environment (0600). Secrets live ONLY here; they are
# never passed into a sandbox image or environment.
ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
IRONCLAW_API_TOKEN=${API_TOKEN}
IRONCLAW_API_ADDR=${API_ADDR}
IRONCLAW_STATE_DIR=${STATE_DIR}
IRONCLAW_MODEL_PROXY_SOCKET=${MODEL_PROXY_SOCKET}
IRONCLAW_BUNDLE_ROOT=${BUNDLE_ROOT}
IRONCLAW_SANDBOX_IMAGE=${SANDBOX_IMAGE}
ENV
chmod 0600 "${ENV_FILE}"
[ -n "${ANTHROPIC_API_KEY}" ] || log "NOTE: ANTHROPIC_API_KEY is blank in ${ENV_FILE} — set it before serving model traffic."

# ---------------------------------------------------------------------------
# 3. Install + enable the service for this OS
# ---------------------------------------------------------------------------
case "${OS}" in
  Linux)
    command -v systemctl >/dev/null 2>&1 || die "systemd (systemctl) not found; install the unit manually from deploy/ironclaw.service"
    log "Installing systemd unit"
    install -m 0644 "${REPO_ROOT}/deploy/ironclaw.service" /etc/systemd/system/ironclaw.service
    systemctl daemon-reload
    systemctl enable --now ironclaw.service
    log "Service status: systemctl status ironclaw"
    ;;
  Darwin)
    log "Installing launchd daemon"
    install -d -m 0755 /var/log/ironclaw
    install -m 0644 "${REPO_ROOT}/deploy/io.ironclaw.controlplane.plist" /Library/LaunchDaemons/io.ironclaw.controlplane.plist
    launchctl unload /Library/LaunchDaemons/io.ironclaw.controlplane.plist 2>/dev/null || true
    launchctl load -w /Library/LaunchDaemons/io.ironclaw.controlplane.plist
    log "Daemon loaded: launchctl print system/io.ironclaw.controlplane"
    ;;
  *)
    die "unsupported OS: ${OS}"
    ;;
esac

log "Done. Drive it: ironctl --addr http://${API_ADDR} --token <IRONCLAW_API_TOKEN from ${ENV_FILE}> change pending"
