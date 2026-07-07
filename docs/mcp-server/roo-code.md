---
title: "IronClaw MCP Server -- Roo Code"
description: "Add IronClaw sandbox_exec to Roo Code (VS Code) in 30 seconds. Roo Code's agent runs untrusted AI-generated shell inside a gVisor sandbox instead of on your host."
---

# Roo Code

[Roo Code](https://roocode.com) is a VS Code coding agent that executes shell
commands and edits files autonomously. By default those commands run on your
host. Wire Roo Code to IronClaw's `sandbox_exec` tool and the agent runs
untrusted, AI-generated commands inside an isolated gVisor sandbox instead.

## Why sandbox Roo Code?

A coding agent runs code the model wrote, not code you reviewed. One bad command
can read your credentials, exfiltrate a token, or delete files. IronClaw
contains each command in an ephemeral gVisor container: no host filesystem, no
unexpected egress, destroyed after the session.

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

In VS Code, open the Roo Code panel, open the **MCP** view, and click
**Edit Global MCP** (this opens `mcp_settings.json`). Add the `ironclaw` entry
under `mcpServers`:

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

!!! note "Project-scoped config"
    To share the sandbox with your team, put the same `mcpServers` block in
    `.roo/mcp.json` at your project root. Project settings override the global
    config when server names match.

## 3. Reload

Roo Code picks up the new server automatically. If the tool does not appear,
toggle the server off and on in the MCP view, or run **Developer: Reload
Window** from the VS Code command palette.

## What sandbox_exec does

- Runs commands in an ephemeral container managed by IronClaw
- Uses gVisor for kernel-level isolation -- the AI cannot read your host files
  or make unexpected network calls
- Container is destroyed after the session ends

## Troubleshooting

**`ironctl: command not found`** -- Make sure `ironctl` is on your `$PATH`.
Run `which ironctl` to check. If you installed via Homebrew, run
`brew link ironsecco/ironclaw/ironclaw`.

**Tool does not appear** -- Open the Roo Code MCP view and check the server
status. Toggle it off and on, or reload the VS Code window.
