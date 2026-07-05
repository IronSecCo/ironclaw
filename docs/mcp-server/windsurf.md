---
title: "Add IronClaw to Windsurf (sandbox MCP server)"
description: Give Windsurf Cascade a sandbox tool for running untrusted agent code. Copy-paste mcpServers config that adds IronClaw's sandbox_exec, executing commands in an ephemeral gVisor box with no network and a read-only root.
---

# Add IronClaw to Windsurf

Give Windsurf's Cascade agent a `sandbox_exec` tool that runs commands and generated
code inside an ephemeral, gVisor-hardened IronClaw box: no network, non-root,
read-only root, all capabilities dropped, torn down after each call. See
[what the box enforces](index.md#what-the-box-enforces) for the full posture.

## 1. Open the config file

Windsurf reads MCP servers from:

```
~/.codeium/windsurf/mcp_config.json
```

You can also open it from the Cascade panel: click the MCP / plugins toolbar (the
hammer icon), then **Manage plugins -> View raw config**.

## 2. Paste the server block

```json
{
  "mcpServers": {
    "ironclaw": {
      "command": "/opt/homebrew/bin/ironctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Replace `/opt/homebrew/bin/ironctl` with the path from `which ironctl`. Use the
absolute path: the spawned process does not inherit your shell `PATH`.

!!! note "No gVisor installed?"
    Change the args to `["mcp", "serve", "--runtime", "runc"]`. `runc` keeps the
    no-network, non-root, read-only, seccomp, and resource caps but isolates on the
    shared host kernel rather than a gVisor guest kernel. See the
    [runtime note](index.md#no-gvisor-label-the-fallback).

## 3. Refresh and verify

In the Cascade MCP panel, click **Refresh** so Windsurf re-reads the config. The
`ironclaw` server appears with its `sandbox_exec` tool. Ask Cascade:

> Use sandbox_exec to run `id && curl -sS https://example.com || echo NO_NETWORK`.

You get the box's non-root user and `NO_NETWORK`, plus a `containment:` line naming
the enforced controls. The box has no network interface, so egress cannot happen.

## Prerequisites

`ironctl` on your machine, a container engine (`docker` or `podman`), and gVisor
recommended. Full setup is on the [cluster overview](index.md#prerequisites).
