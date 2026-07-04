#!/usr/bin/env bash
#
# Codespaces post-attach banner: shown every time you open/attach to the Codespace.
# Kept lightweight (no Docker calls) so attaching stays fast. IRO-329.
set -uo pipefail

cat <<'EOF'

  IronClaw — zero-install try (Codespaces)

  The demo control-plane is set up to run on port 8787 (offline mock-agent,
  zero credentials). Open the forwarded port 8787 and add /ui/ to chat;
  paste the token  ironclaw-demo  if prompted.

  Start / restart the demo:   .devcontainer/post-create.sh
  Watch a real escape denied:  examples/live-containment/run.sh
  Stop the demo:              docker compose -f docker-compose.demo.yml down

EOF
