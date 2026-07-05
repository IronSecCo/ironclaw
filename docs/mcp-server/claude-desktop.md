---
title: "IronClaw MCP Server -- Claude Desktop"
description: "Add IronClaw sandbox_exec to Claude Desktop in 30 seconds. Run untrusted agent code safely inside a gVisor sandbox via MCP."
---

# Claude Desktop

Add IronClaw's `sandbox_exec` tool to Claude Desktop in three steps.

## 1. Install ironctl

```bash
brew install ironsecco/ironclaw/ironclaw
```

Verify:

```bash
ironctl version
```

## 2. Edit the config file

Open (or create) `~/Library/Application Support/Claude/claude_desktop_config.json`
and add the `ironclaw` entry under `mcpServers`:

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

## 3. Restart Claude Desktop

Quit and reopen Claude Desktop. The `sandbox_exec` tool will appear in the
tool list. Claude can now run shell commands inside an isolated sandbox on your
machine.

## What sandbox_exec does

- Runs commands in an ephemeral container managed by IronClaw
- Uses gVisor for kernel-level isolation -- the AI cannot read your host files
  or make unexpected network calls
- Container is destroyed after the session ends

## Troubleshooting

**`ironctl: command not found`** -- Make sure `ironctl` is on your `$PATH`.
Run `which ironctl` to check. If you installed via Homebrew, run
`brew link ironsecco/ironclaw/ironclaw`.

**Tool does not appear** -- Restart Claude Desktop fully (Cmd+Q, not just close
the window).
