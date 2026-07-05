---
title: "IronClaw MCP Server -- Windsurf"
description: "Add IronClaw sandbox_exec to Windsurf in 30 seconds. Run untrusted agent code safely inside a gVisor sandbox via MCP."
---

# Windsurf

Add IronClaw's `sandbox_exec` tool to Windsurf in three steps.

## 1. Install ironctl

```bash
brew install ironsecco/ironclaw/ironclaw
```

Verify:

```bash
ironctl version
```

## 2. Edit the config file

Open (or create) `~/.codeium/windsurf/mcp_server_config.json` and add the
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

If you already have other MCP servers, just add the `"ironclaw"` key alongside them.

## 3. Restart Windsurf

Quit and reopen Windsurf. The `sandbox_exec` tool will appear in the Cascade
tool list. The Windsurf agent can now run shell commands inside an isolated
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

**Tool does not appear** -- Open Windsurf's MCP settings and verify the server
entry. Restart Windsurf fully (not just close the window) to reload the MCP
configuration.
