---
title: "Add IronClaw to Cursor (secure code execution MCP)"
description: Give Cursor's agent a secure code execution tool. Copy-paste mcpServers config that adds IronClaw's sandbox_exec, running model-generated code in an ephemeral gVisor box with no network, dropped capabilities, and a read-only root.
---

# Add IronClaw to Cursor

Add a `sandbox_exec` tool so Cursor's agent runs commands and generated code inside
an ephemeral, gVisor-hardened IronClaw box: no network, non-root, read-only root, all
capabilities dropped, torn down after each call. See
[what the box enforces](index.md#what-the-box-enforces) for the full posture.

## 1. Open the config file

Cursor reads MCP servers from a `mcp.json` file:

| Scope | Path |
|-------|------|
| Global (all projects) | `~/.cursor/mcp.json` |
| Project only | `<project>/.cursor/mcp.json` |

You can also reach the global file from **Cursor Settings -> MCP -> Add new global
MCP server**, which opens `~/.cursor/mcp.json` for editing.

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
absolute path: the launched process does not inherit your shell `PATH`.

!!! note "No gVisor installed?"
    Change the args to `["mcp", "serve", "--runtime", "runc"]`. `runc` keeps the
    no-network, non-root, read-only, seccomp, and resource caps but isolates on the
    shared host kernel rather than a gVisor guest kernel. See the
    [runtime note](index.md#no-gvisor-label-the-fallback).

## 3. Enable and verify

Open **Cursor Settings -> MCP**. The `ironclaw` server should show a green dot and
list the `sandbox_exec` tool; toggle it on if needed. Then ask the agent:

> Run `id && curl -sS https://example.com || echo NO_NETWORK` with sandbox_exec.

You get the box's non-root user and `NO_NETWORK`, plus a `containment:` line naming
the enforced controls. Egress is impossible because the box has no network
interface.

## Prerequisites

`ironctl` on your machine, a container engine (`docker` or `podman`), and gVisor
recommended. Full setup is on the [cluster overview](index.md#prerequisites).
