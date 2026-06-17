#!/bin/sh
# IronClaw control-plane container entrypoint.
#
# Responsibilities, in order:
#   1. Mint the admin/API token on FIRST run (printed once, no recovery) unless the
#      operator supplied IRONCLAW_API_TOKEN. A re-run reuses the persisted token, so
#      restarts are idempotent and the token survives in the durable state volume.
#   2. exec the control-plane daemon with container-appropriate defaults.
#
# Everything is overridable by environment (see .env.example):
#   IRONCLAW_API_TOKEN   pre-set to skip minting (operator-managed secret)
#   IRONCLAW_STATE_DIR   durable state dir            (default /var/lib/ironclaw/state)
#   IRONCLAW_API_ADDR    daemon listen addr           (default 0.0.0.0:8787)
#   IRONCLAW_LOG_FORMAT  text | json                  (default json)
#   IRONCLAW_DEV         "1" seeds a dev owner/agent-group for local testing
set -eu

STATE_DIR="${IRONCLAW_STATE_DIR:-/var/lib/ironclaw/state}"
TOKEN_FILE="${STATE_DIR}/admin-api-token"

mkdir -p "${STATE_DIR}"

# --- 1. admin/API token -----------------------------------------------------
# Precedence: an operator-supplied env token always wins. Otherwise reuse the token
# persisted on a prior first run. Otherwise mint a fresh 256-bit token, persist it
# 0600, and print it ONCE with an unmistakable claim-it/no-recovery warning.
if [ -z "${IRONCLAW_API_TOKEN:-}" ]; then
  if [ -s "${TOKEN_FILE}" ]; then
    IRONCLAW_API_TOKEN="$(cat "${TOKEN_FILE}")"
  else
    # 32 bytes of /dev/urandom as hex (64 chars). od+tr are in coreutils — no extra
    # package and no openssl dependency in the runtime image.
    IRONCLAW_API_TOKEN="$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
    ( umask 077; printf '%s' "${IRONCLAW_API_TOKEN}" > "${TOKEN_FILE}" )
    chmod 0600 "${TOKEN_FILE}" 2>/dev/null || true
    cat >&2 <<EOF

============================================================================
  IronClaw admin/API token MINTED (first run)
----------------------------------------------------------------------------
  IRONCLAW_API_TOKEN=${IRONCLAW_API_TOKEN}
----------------------------------------------------------------------------
  CLAIM IT NOW. This is the bearer token for the control-plane admin API and
  it is shown ONCE. There is NO RECOVERY: store it in your secrets manager and
  pass it back to ironctl as IRONCLAW_API_TOKEN.

  Lost it? Stop the stack, delete ${TOKEN_FILE} from the
  state volume, and restart to mint a new one (the old token stops working).
============================================================================

EOF
  fi
fi
export IRONCLAW_API_TOKEN

# --- 2. launch the daemon ---------------------------------------------------
# Bind 0.0.0.0 INSIDE the container; docker-compose maps it to host loopback only,
# so there is no public port (front it with Tailscale for remote access). The
# sandbox network=none posture is enforced by the daemon's OCI spec, not here.
DEV_FLAG=""
[ "${IRONCLAW_DEV:-0}" = "1" ] && DEV_FLAG="--dev"

exec ironclaw-controlplane \
  --state-dir "${STATE_DIR}" \
  --api-addr "${IRONCLAW_API_ADDR:-0.0.0.0:8787}" \
  --log-format "${IRONCLAW_LOG_FORMAT:-json}" \
  ${DEV_FLAG} \
  "$@"
