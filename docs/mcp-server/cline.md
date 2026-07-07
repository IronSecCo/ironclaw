---
title: "IronClaw MCP Server -- Cline"
description: "Add IronClaw sandbox_exec to Cline (VS Code) in 30 seconds. Cline's agent runs untrusted AI-generated shell inside a gVisor sandbox instead of on your host."
---

# Cline

[Cline](https://cline.bot) is a VS Code coding agent that plans and runs shell
commands and edits files on your behalf. By default those commands run directly
on your development machine. Point Cline at IronClaw's `sandbox_exec` tool and
the agent runs untrusted, AI-generated commands inside an isolated gVisor
sandbox instead.

## Why sandbox Cline?

A coding agent executes code the model wrote, not code you reviewed. A single
bad command can read your SSH keys, exfiltrate a token, or wipe a directory.
IronClaw contains each command in an ephemeral gVisor container: no host
filesystem, no unexpected egress, destroyed after the session.

- See it stop real escape attempts: [nivardsec.com/containment](https://nivardsec.com/containment)
- Read the isolation benchmark: [Containment benchmark](../blog/containment-benchmark-docker-gvisor-e2b-daytona.md)

## 1. Install ironctl

```bash
brew install ironsecco/ironclaw/ironclaw
```

Verify:

```bash
ironctl version
```

## 2. Add the MCP server

In VS Code, open the Cline panel and click the **MCP Servers** icon, then
**Configure MCP Servers**. This opens `cline_mcp_settings.json`. Add the
`ironclaw` entry under `mcpServers`:

```json
{
  "mcpServers": {
    "ironclaw": {
      "command": "ironctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

If you already have other MCP servers, add the `"ironclaw"` key alongside them.

!!! note "Cline CLI"
    Using the Cline CLI instead of the VS Code extension? Put the same
    `mcpServers` block in `~/.cline/mcp.json`.

## 3. Reload

Cline picks up the new server automatically. If the tool does not appear, click
the refresh icon in the MCP Servers panel, or run **Developer: Reload Window**
from the VS Code command palette.

## What sandbox_exec does

- Runs commands in an ephemeral container managed by IronClaw
- Uses gVisor for kernel-level isolation -- the AI cannot read your host files
  or make unexpected network calls
- Container is destroyed after the session ends

## Troubleshooting

**`ironctl: command not found`** -- Make sure `ironctl` is on your `$PATH`.
Run `which ironctl` to check. If you installed via Homebrew, run
`brew link ironsecco/ironclaw/ironclaw`.

**Tool does not appear** -- Open the Cline MCP Servers panel and check the
server status. Use the refresh icon, or reload the VS Code window.
