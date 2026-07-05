---
title: "Run IronClaw as an MCP server (sandbox any tool call)"
description: Add IronClaw to Claude Desktop, Cursor, Windsurf, Cline, or any MCP client with one config line. The sandbox_exec tool runs any command or code snippet inside an ephemeral, hardened IronClaw box under gVisor (runsc) with no network, dropped capabilities, a non-root user, a restrictive seccomp profile, and a read-only root filesystem. Model-generated code runs where it cannot reach your machine.
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
line. By default the box runs under **gVisor (runsc)** - a user-space guest kernel
that intercepts syscalls - so a kernel-level exploit cannot reach the host. It has
no network card, drops every Linux capability, runs as a non-root user with a
read-only root filesystem, `no-new-privileges`, and a restrictive seccomp profile,
is bounded by cpu/memory/pids caps, and is torn down after the command returns.

!!! warning "gVisor (runsc) is required for the full guarantee"
    The primary boundary is gVisor's syscall interception. `ironctl mcp serve`
    passes `--runtime runsc` by default; if runsc is not installed the launch fails
    closed rather than silently downgrading. You can opt into a plain-runc fallback
    with `--runtime runc`, but that shares the host kernel and is **not**
    gVisor-equivalent - the tool's containment status labels it as a fallback so a
    caller is never misled. Install gVisor from
    [gvisor.dev](https://gvisor.dev/docs/user_guide/install/) and register the
    `runsc` Docker runtime for the hardened default.

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

Prefer HTTP transport? Run it explicitly. The server binds **loopback
(`127.0.0.1`) by default** - a bare `:9000` becomes `127.0.0.1:9000`, reachable only
from the same host:

```bash
ironctl mcp serve --http :9000
```

To expose the server on a routable address (a shared or remote server) you **must**
provide an authentication token; IronClaw refuses to bind a non-loopback address
without one, because `sandbox_exec` runs arbitrary code:

```bash
IRONCLAW_MCP_AUTH_TOKEN=$(openssl rand -hex 32) \
  ironctl mcp serve --http 0.0.0.0:9000
# clients must then send:  Authorization: Bearer <token>
```

Terminate TLS at a reverse proxy (or an SSH tunnel) in front of the server; the
bearer token is transport-agnostic and does not encrypt traffic on its own.

## The `sandbox_exec` tool

| Argument | Type | Default | Meaning |
|---|---|---|---|
| `command` | string (required) | | Command run inside the box via `sh -c`. |
| `image` | string | `alpine:3.20` | Container image override (e.g. `python:3.12-slim`). Rejected if it starts with `-` (would be parsed as a docker flag). |
| `timeout_seconds` | integer | `30` | Per-exec timeout that bounds a runaway command. |

The tool returns the command's **stdout**, **stderr**, **exit code**, and an explicit
**containment status** listing the controls that were enforced.

### What the box enforces

Every call runs `docker run --rm` against an ephemeral `ic-sbx-mcp-*` box with the
same hardened posture as an IronClaw session sandbox:

- `--runtime runsc` : **gVisor** user-space guest kernel intercepts syscalls (the primary boundary). A non-runsc runtime is an explicit, labelled fallback.
- `--network none` : no NIC, so egress is structurally impossible.
- `--cap-drop ALL` : every Linux capability dropped.
- `--security-opt seccomp=<profile>` : IronClaw's restrictive deny-by-default seccomp profile (same as the OCI path).
- `--security-opt no-new-privileges` : suid binaries cannot escalate.
- `--read-only` rootfs, with a small writable `tmpfs` at `/tmp` only.
- `--user 65532:65532` : non-root.
- `--pids-limit 256`, `--memory 512m`, `--cpus 1` : bounded resources.

The tool's `containment:` line names the actual runtime, so a caller always sees
whether it ran under gVisor or a labelled fallback.

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
