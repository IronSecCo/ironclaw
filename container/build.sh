#!/usr/bin/env bash
#
# Build the IronClaw sandbox image from container/Dockerfile.
#
# The build context is the repository ROOT (the Dockerfile compiles ./cmd/sandbox),
# so this script sets it regardless of where it is invoked from.
#
#   container/build.sh [IMAGE_REF]
#
# Env:
#   IMAGE        image reference to tag        (default: ironclaw-sandbox:latest, or $1)
#   ENGINE       container engine              (default: docker, else podman/nerdctl/buildah)
#   PUSH         when "1", push after building (default: 0)
#
# After building, the printed RepoDigest is the value to PIN in the provisioner's
# trust policy (internal/host/isolation PinnedDigestPolicy) and in
# IRONCLAW_SANDBOX_IMAGE.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${1:-${IMAGE:-ironclaw-sandbox:latest}}"
PUSH="${PUSH:-0}"

# Pick a container engine.
pick_engine() {
  if [ -n "${ENGINE:-}" ]; then printf '%s' "${ENGINE}"; return; fi
  for e in docker podman nerdctl buildah; do
    if command -v "$e" >/dev/null 2>&1; then printf '%s' "$e"; return; fi
  done
  printf ''
}
ENGINE="$(pick_engine)"
[ -n "${ENGINE}" ] || { printf 'error: no container engine found (docker/podman/nerdctl/buildah)\n' >&2; exit 1; }

printf '==> Building %s with %s (context: %s)\n' "${IMAGE}" "${ENGINE}" "${REPO_ROOT}"
if [ "${ENGINE}" = "buildah" ]; then
  buildah bud -t "${IMAGE}" -f "${REPO_ROOT}/container/Dockerfile" "${REPO_ROOT}"
else
  "${ENGINE}" build -t "${IMAGE}" -f "${REPO_ROOT}/container/Dockerfile" "${REPO_ROOT}"
fi

if [ "${PUSH}" = "1" ]; then
  printf '==> Pushing %s\n' "${IMAGE}"
  "${ENGINE}" push "${IMAGE}"
fi

# Best-effort: surface the digest to pin in the trust policy.
printf '==> Image digest (pin this in the provisioner trust policy):\n'
"${ENGINE}" inspect --format '{{ range .RepoDigests }}{{ . }}{{ "\n" }}{{ end }}' "${IMAGE}" 2>/dev/null || true
