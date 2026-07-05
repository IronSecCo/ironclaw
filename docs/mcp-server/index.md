---
title: IronClaw MCP Server
description: "Run IronClaw as an MCP server to give Claude Desktop, Cursor, and Windsurf a sandboxed code-execution tool in 30 seconds."
---

# IronClaw as an MCP Server

IronClaw ships a built-in MCP server (`ironctl mcp serve`) that exposes the
`sandbox_exec` tool to any MCP-compatible AI client. Connect it once and every
AI action runs inside an isolated gVisor sandbox -- no escapes, no host
side-effects.

## What is sandbox_exec?

`sandbox_exec` lets the AI client run arbitrary shell commands inside an
ephemeral IronClaw sandbox container. The sandbox is destroyed after each
session, so nothing persists to your host.

## Supported clients

| Client | Config file |
|--------|------------|
| [Claude Desktop](claude-desktop.md) | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| [Cursor](cursor.md) | `~/.cursor/mcp.json` |
| [Windsurf](windsurf.md) | `~/.codeium/windsurf/mcp_server_config.json` |

## Prerequisites

Install IronClaw:

```bash
brew install ironsecco/ironclaw/ironclaw
```

or via the one-liner:

```bash
curl -fsSL https://ironclaw.sh/install.sh | sh
```

Then verify:

```bash
ironctl version
```

!!! note "This page covers IronClaw acting as an MCP server (outbound tool provider)."
    For the reverse -- using external MCP servers *inside* an IronClaw-guarded agent
    sandbox -- see [MCP servers](../mcp.md).
