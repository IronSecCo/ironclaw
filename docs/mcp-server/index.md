---
title: "Sandbox MCP server: run untrusted agent code safely"
description: Add IronClaw as a sandbox MCP server so Claude Desktop, Cursor, or Windsurf can run model-generated code in an ephemeral gVisor box with no network, dropped capabilities, and a read-only root. One copy-paste config, secure code execution over MCP.
---

# Run IronClaw as a sandbox MCP server

`ironctl mcp serve` turns IronClaw into a **Model Context Protocol (MCP) server** that
any MCP client can add in one config block. It exposes a single tool, `sandbox_exec`,
that runs a shell command or a model-generated code snippet inside an **ephemeral,
gVisor-hardened box** and hands back stdout, stderr, exit code, and an explicit
containment summary. The box has no network, drops every Linux capability, runs
non-root on a read-only root filesystem, and is torn down when the command finishes.

If your agent writes code, this is the safe place to run it.

!!! note "Two directions, do not confuse them"
    This page is IronClaw **as a server** for your MCP client. The separate
    [MCP servers](../mcp.md) guide is the inverse: IronClaw **as a broker** that
    lets an IronClaw agent reach *other* MCP servers behind a human-approval gate.

## Add it to your client

Pick your client for the copy-paste config and where the config file lives:

- [Claude Desktop](claude-desktop.md) - run untrusted agent code from Claude Desktop
- [Cursor](cursor.md) - secure code execution for Cursor's agent
- [Windsurf](windsurf.md) - a sandbox tool for Windsurf Cascade

Any other MCP client (Cline, Continue, Zed, your own) uses the same `mcpServers`
schema shown below.

## The one config block

Every MCP client spawns the server as a subprocess and speaks JSON-RPC over stdio.
The universal block is:

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

!!! warning "Use an absolute path for `command`"
    Most MCP clients do **not** inherit your shell `PATH`, so a bare `"ironctl"`
    often fails to launch. Run `which ironctl` and paste the full path (for example
    `/opt/homebrew/bin/ironctl` on Apple Silicon, `/usr/local/bin/ironctl` on Intel
    macOS or Linux).

## Prerequisites

1. **`ironctl` installed.** One line installs the host binaries:

    ```bash
    curl -fsSL https://raw.githubusercontent.com/IronSecCo/ironclaw/main/scripts/install.sh | sh
    ```

    or on macOS via Homebrew:

    ```bash
    brew install ironsecco/ironclaw/ironclaw
    ```

2. **A container engine** (`docker` or `podman`) reachable on your machine.
   `sandbox_exec` launches a hardened `docker run` per call.

3. **gVisor (runsc), recommended.** By default the box runs under the gVisor
   runtime, which intercepts syscalls in a user-space guest kernel. If `runsc` is
   not installed, either [install gVisor](https://gvisor.dev/docs/user_guide/install/)
   or fall back explicitly (see below).

### No gVisor? Label the fallback

Without `runsc`, every `sandbox_exec` call fails to launch until you opt into a
weaker runtime. Add `--runtime runc` to the args:

```json
{
  "mcpServers": {
    "ironclaw": {
      "command": "ironctl",
      "args": ["mcp", "serve", "--runtime", "runc"]
    }
  }
}
```

`runc` still gives you no network, dropped capabilities, non-root, a read-only
root, seccomp, and resource caps, but the isolation boundary is the **shared host
kernel**, not a gVisor guest kernel. The tool's containment summary says so on
every call, so a client is never misled about the guarantee.

## What `sandbox_exec` does

The tool takes one required and two optional arguments:

| Argument | Required | Meaning |
|----------|----------|---------|
| `command` | yes | Shell command or snippet, run via `sh -c` inside the box |
| `image` | no | Container image override (default: `alpine:3.20`) |
| `timeout_seconds` | no | Per-call timeout, 1 to 600 (default: 30) |

It returns a text block with the exit code, the containment summary, and the
captured stdout and stderr. A launch failure (engine missing, `runsc` absent, image
pull denied, timeout) comes back as a readable tool error, not a broken session.

## What the box enforces

Every call runs in an ephemeral `ic-sbx-mcp-*` box under these controls:

- **gVisor (runsc)** runtime by default: syscall interception in a guest kernel.
- **`network=none`**: no interface, so egress is structurally impossible.
- **All capabilities dropped**, **non-root** user (uid 65532), **no-new-privileges**.
- **Read-only root filesystem**; only a small `tmpfs` at `/tmp` is writable.
- **Restrictive seccomp profile** (the same deny-by-default profile IronClaw uses
  for agent sandboxes).
- **Resource caps**: 1 vCPU, 512 MiB memory, 256 pids.
- **Ephemeral**: the box is torn down as soon as the command returns.

These controls are enforced by the runtime flags, not by the image, and they are
covered by IronClaw's live containment tests. See
[Breaking our own sandbox](../breaking-our-own-sandbox.md) for the escape attempts
they survive and [Why we run AI agents in gVisor](../gvisor-deep-dive.md) for the
isolation model.

## The server holds no secrets

`ironctl mcp serve` makes **no model calls** and stores **no credentials**. It is a
thin bridge from an MCP tool call to a hardened `docker run`. Nothing you send it is
logged as arguments, and the box it spawns can reach neither the network nor your
host.

## Optional: HTTP transport

Stdio is the default and what clients spawn. To run one long-lived server that
several clients dial, use streamable HTTP:

```bash
ironctl mcp serve --http :9000                 # loopback only (127.0.0.1)
ironctl mcp serve --http 0.0.0.0:9000 --auth-token "$TOKEN"   # any other bind requires auth
```

A non-loopback bind without `--auth-token` is refused: the server never exposes
`sandbox_exec` to the network unauthenticated. Point an HTTP-capable client at
`http://127.0.0.1:9000`.
