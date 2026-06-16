#!/usr/bin/env bash
# OWNER: AGENT1
#
# IronClaw control-plane install scaffold.
#
# This is a COMMENTED STUB outlining the production install steps. It is not meant
# to run end-to-end as-is — each step needs host-specific values (tailnet IP,
# package versions, service paths). Review and adapt before use.

set -euo pipefail

# ---------------------------------------------------------------------------
# 1. containerd + gVisor (runsc)
# ---------------------------------------------------------------------------
# Install containerd and the gVisor runtime, then register the
# io.containerd.runsc.v1 runtime in containerd's config so the isolator can
# launch sandboxes under gVisor.
#
#   # Debian/Ubuntu example (versions elided — pin them):
#   apt-get install -y containerd
#   curl -fsSL https://storage.googleapis.com/gvisor/releases/release/latest/$(uname -m)/runsc -o /usr/local/bin/runsc
#   chmod 0755 /usr/local/bin/runsc
#   # Add to /etc/containerd/config.toml:
#   #   [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
#   #     runtime_type = "io.containerd.runsc.v1"
#   systemctl restart containerd

# ---------------------------------------------------------------------------
# 2. Tailscale (admin-only access to the control-plane API)
# ---------------------------------------------------------------------------
#   curl -fsSL https://tailscale.com/install.sh | sh
#   tailscale up
#   TAILNET_IP="$(tailscale ip -4)"
#
# Firewall: drop inbound to the API port on every interface EXCEPT tailscale0.
#   # (nftables/ufw rules elided — restrict to the tailnet interface only.)

# ---------------------------------------------------------------------------
# 3. Build the control-plane + CLI
# ---------------------------------------------------------------------------
#   go build -o /usr/local/bin/ironclaw-controlplane ./cmd/controlplane
#   go build -o /usr/local/bin/ironctl              ./cmd/ironctl
#   install -d -m 0700 /run/ironclaw

# ---------------------------------------------------------------------------
# 4. systemd unit (Linux) — bind the API to the tailnet IP only
# ---------------------------------------------------------------------------
#   cat >/etc/systemd/system/ironclaw.service <<UNIT
#   [Unit]
#   Description=IronClaw control-plane
#   After=network-online.target containerd.service tailscaled.service
#
#   [Service]
#   ExecStart=/usr/local/bin/ironclaw-controlplane \
#     --api-addr ${TAILNET_IP}:8787 \
#     --model-proxy-socket /run/ironclaw/modelproxy.sock
#   Restart=on-failure
#
#   [Install]
#   WantedBy=multi-user.target
#   UNIT
#   systemctl daemon-reload
#   systemctl enable --now ironclaw

echo "install.sh is a scaffold — read the comments and adapt before running."
