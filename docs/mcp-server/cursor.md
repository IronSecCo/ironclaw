---
title: "IronClaw MCP Server -- Cursor"
description: "Add IronClaw sandbox_exec to Cursor in 30 seconds. Run untrusted agent code safely inside a gVisor sandbox via MCP."
---

# Cursor

Add IronClaw's `sandbox_exec` tool to Cursor in three steps.

## 1. Install ironctl

```bash
brew install ironsecco/ironclaw/ironclaw
```

Verify:

```bash
ironctl version
```

## 2. Edit the config file

Open (or create) `~/.cursor/mcp.json` and add the `ironclaw` entry under
`mcpServers`:

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

If you already have other MCP servers, just add the `"ironclaw"` key alongside them.

## 3. Restart Cursor

Quit and reopen Cursor. The `sandbox_exec` tool will appear in Cursor's MCP
tool list. The Cursor agent can now run shell commands inside an isolated
sandbox on your machine.

## What sandbox_exec does

- Runs commands in an ephemeral container managed by IronClaw
- Uses gVisor for kernel-level isolation -- the AI cannot read your host files
  or make unexpected network calls
- Container is destroyed after the session ends

## Troubleshooting

**`ironctl: command not found`** -- Make sure `ironctl` is on your `$PATH`.
Run `which ironctl` to check. If you installed via Homebrew, run
`brew link ironsecco/ironclaw/ironclaw`.

**Tool does not appear** -- Open Cursor's MCP settings panel (Settings > MCP)
and check the server status. Restart Cursor fully if the status shows an error.
