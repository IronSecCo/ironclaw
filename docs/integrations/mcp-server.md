---
title: "Run IronClaw as an MCP server (sandbox any tool call)"
description: Add IronClaw to Claude Desktop, Cursor, Windsurf, Cline, or any MCP client with one config line. The sandbox_exec tool runs any command or code snippet inside an ephemeral, hardened IronClaw box with no network, dropped capabilities, a non-root user, and a read-only root filesystem. Model-generated code runs where it cannot reach your machine.
---

# Run IronClaw as an MCP server

The [Model Context Protocol](https://modelcontextprotocol.io) (MCP) is how modern AI
clients (Claude Desktop, Cursor, Windsurf, Cline, and others) discover and call
tools. Most MCP servers that "run code" run it **on your machine**: a prompt
injection or a hallucinated command becomes a shell on your host, with your
filesystem and unrestricted network.

IronClaw ships an MCP server that exposes a single, blunt tool, **`sandbox_exec`**,
which runs the command inside an **ephemeral, hardened sandbox box** instead. Any
MCP client can route untrusted, model-generated code through it with one config
line. The box has no network card, drops every Linux capability, runs as a non-root
user with a read-only root filesystem and `no-new-privileges`, is bounded by
cpu/memory/pids caps, and is torn down after the command returns.

!!! note "This is the inverse of registering an external MCP server"
    IronClaw can also act as a *client* of other MCP servers (see
    `ironctl mcp add`, which registers an external server behind the human-approval
    gateway). This page is the opposite direction: IronClaw **is** the server, and
    your MCP client is the caller.

## One-line install

`sandbox_exec` ships inside the `ironctl` binary you already installed
([Quickstart](../quickstart.md)). Nothing new to download.

Add IronClaw to your MCP client's config. For **Claude Desktop**
(`claude_desktop_config.json`), **Cursor**, **Windsurf**, or **Cline**, the shape is
the same:

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

That is it. The client spawns `ironctl mcp serve`, speaks MCP over stdio, and sees
one tool: `sandbox_exec`. IronClaw needs a container runtime (Docker or Podman)
reachable on the host to launch the sandbox box.

Prefer HTTP transport (for a shared or remote server)? Run it explicitly:

```bash
ironctl mcp serve --http :9000
```

## The `sandbox_exec` tool

| Argument | Type | Default | Meaning |
|---|---|---|---|
| `command` | string (required) | | Command run inside the box via `sh -c`. |
| `image` | string | `alpine:3.20` | Container image override (e.g. `python:3.12-slim`). |
| `timeout_seconds` | integer | `30` | Per-exec timeout that bounds a runaway command. |

The tool returns the command's **stdout**, **stderr**, **exit code**, and an explicit
**containment status** listing the controls that were enforced.

### What the box enforces

Every call runs `docker run --rm` against an ephemeral `ic-sbx-mcp-*` box with the
same hardened posture as an IronClaw session sandbox:

- `--network none` : no NIC, so egress is structurally impossible.
- `--cap-drop ALL` : every Linux capability dropped.
- `--security-opt no-new-privileges` : suid binaries cannot escalate.
- `--read-only` rootfs, with a small writable `tmpfs` at `/tmp` only.
- `--user 65532:65532` : non-root.
- `--pids-limit 256`, `--memory 512m`, `--cpus 1` : bounded resources.

## Prove containment yourself

The value of the tool is what it **stops**. Ask your MCP client (or drive the server
directly) to run an escape attempt and watch it fail closed.

**Network escape is blocked** (no NIC):

```
sandbox_exec { "command": "wget -T3 -qO- http://example.com || echo BLOCKED" }
-> stdout: BLOCKED
   stderr: wget: bad address 'example.com'
```

**Host filesystem write is blocked** (read-only rootfs):

```
sandbox_exec { "command": "touch /etc/pwned || echo BLOCKED" }
-> stdout: touch: /etc/pwned: Read-only file system
           BLOCKED
```

**The process is non-root:**

```
sandbox_exec { "command": "id" }
-> stdout: uid=65532 gid=65532 groups=65532
```

Meanwhile ordinary work runs normally, including writes to the scratch `/tmp`:

```
sandbox_exec { "command": "echo hello && python3 -c 'print(2+2)'", "image": "python:3.12-slim" }
-> stdout: hello
           4
```

## How it compares

The framework guides ([LangChain](langchain.md), [CrewAI](crewai.md),
[OpenAI Agents SDK](openai-sdk.md), [Claude Agent SDK](claude-sdk.md)) sandbox an
agent you built. The MCP server sandboxes **any** MCP client's tool calls without
touching your agent code at all: it is a drop-in perimeter for the code an assistant
decides to run. See how the sandbox holds under attack in
[Isolation, proven](../security-isolation.md) and the
[threat model](../threat-model.md).
