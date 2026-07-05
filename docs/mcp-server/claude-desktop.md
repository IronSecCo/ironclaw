---
title: "Add IronClaw to Claude Desktop (sandbox MCP server)"
description: Run untrusted or model-generated code from Claude Desktop safely. Copy-paste mcpServers config that adds IronClaw's sandbox_exec tool, running each command in an ephemeral gVisor box with no network and a read-only root.
---

# Add IronClaw to Claude Desktop

Give Claude Desktop a `sandbox_exec` tool that runs any command or code snippet in an
ephemeral, gVisor-hardened IronClaw box: no network, non-root, read-only root, all
capabilities dropped, torn down after each call. See
[what the box enforces](index.md#what-the-box-enforces) for the full posture.

## 1. Open the config file

In Claude Desktop, go to **Settings -> Developer -> Edit Config**. That opens
`claude_desktop_config.json`:

| OS | Path |
|----|------|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |

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

Replace `/opt/homebrew/bin/ironctl` with your own path from `which ironctl`. Claude
Desktop does not inherit your shell `PATH`, so a bare `"ironctl"` usually will not
launch. If you already have other servers under `mcpServers`, add `ironclaw` as one
more key.

!!! note "No gVisor installed?"
    The default runtime is gVisor (`runsc`). If it is not installed, change the args
    to `["mcp", "serve", "--runtime", "runc"]`. `runc` keeps the no-network,
    non-root, read-only, seccomp, and resource caps but isolates on the shared host
    kernel, not a gVisor guest kernel. See the [runtime note](index.md#no-gvisor-label-the-fallback).

## 3. Restart Claude Desktop

Fully quit and reopen the app so it re-reads the config. `ironclaw` then appears in
the tools menu (the slider / hammer icon in the message box).

## 4. Verify

Ask Claude:

> Use the sandbox_exec tool to run `uname -a && id && cat /etc/os-release`.

You should see the box's kernel and user, and a `containment:` line reporting
`runtime=runsc (gVisor ...)`, `network=none`, `caps=drop-all`, `user=65532`. Now try
to prove the isolation:

> Use sandbox_exec to run `curl -sS https://example.com || echo NO_NETWORK`.

It prints `NO_NETWORK`: the box has no interface, so egress is impossible.

## Prerequisites

`ironctl` on your machine, a container engine (`docker` or `podman`), and gVisor
recommended. Full setup is on the [cluster overview](index.md#prerequisites).
