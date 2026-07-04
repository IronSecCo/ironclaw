#!/usr/bin/env bash
#
# Codespaces post-create: run the IronClaw zero-credential demo end to end and
# leave it running so a fresh visitor can chat with a real sandboxed agent in the
# browser — no local install, no API key (IRO-329).
#
# What it does (via examples/hello-ironclaw/run.sh --keep):
#   1. builds the sandbox image (container/build.sh) and the demo control-plane image
#   2. brings up the offline mock-agent control-plane (docker-compose.demo.yml)
#   3. sends a chat message and ASSERTS the reply comes back through the real
#      engage -> per-session sandbox -> encrypted queue -> delivery path
#   4. leaves the demo running on 127.0.0.1:8787 (Codespaces forwards it)
#
# The demo relaxes the sandbox seal to run without gVisor (runc, shared host kernel);
# the approval gateway, encrypted per-session queues, and network=none per sandbox are
# unchanged. See .devcontainer/README.md and examples/hello-ironclaw/README.md.
#
# We do NOT hard-fail the Codespace on a demo error: the container should still open
# so the visitor can read the diagnostics and retry. run.sh already fails loud and
# dumps control-plane + sandbox logs on a broken round-trip.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "======================================================================"
echo " IronClaw — building and starting the zero-credential demo"
echo " First run builds the sandbox + control-plane images (~2-3 min)."
echo "======================================================================"

if bash examples/hello-ironclaw/run.sh --keep; then
  cat <<'EOF'

======================================================================
 IronClaw is running. Zero install, zero credentials.
======================================================================

 Open the web console:
   - Click the forwarded port 8787 (PORTS tab) and open it in your browser,
     then add /ui/ to the URL.
   - In the Chat tab pick "Mock Agent (offline)" and say hi.
   - If it asks for an API token, paste:  ironclaw-demo

 Now watch it catch a real escape (a jailbroken agent tries to break out
 of the sandbox and every attempt is denied):

   examples/live-containment/run.sh

 Stop the demo:
   docker compose -f docker-compose.demo.yml down

 Rerun this whole demo:
   .devcontainer/post-create.sh
======================================================================
EOF
else
  cat <<'EOF'

======================================================================
 The demo did not come up cleanly.
======================================================================
 The Codespace is still usable. Common causes: the nested Docker daemon
 was still starting, or the first image build timed out.

 Retry with:
   .devcontainer/post-create.sh

 Or run the pieces by hand:
   bash container/build.sh
   docker compose -f docker-compose.demo.yml up --build -d
   examples/hello-ironclaw/run.sh --attach
======================================================================
EOF
fi

# Always exit 0 so a demo hiccup doesn't mark Codespace creation as failed.
exit 0
