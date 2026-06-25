---
title: "MCP servers: gated, audited tools outside the sandbox"
description: Add MCP server tools to a self-hosted AI agent without weakening the gVisor sandbox — a human approves named servers and tools, and every call is gated and audited.
---

# MCP servers

IronClaw can extend an agent with the tools of a **Model Context Protocol (MCP)**
server — local (a stdio subprocess) or remote (a streamable‑HTTP endpoint) — without
weakening the sandbox. The reference design that IronClaw was built to harden wired MCP
with a *blind approval surface* ("approve this server" → it brings whatever tools it
likes). IronClaw closes that gap: **a human approves a named server and a named set of
tools, every call is gated and audited, and the MCP server never runs inside — or is
reachable from — the agent sandbox.**

This is opt‑in. With no `--mcp-catalog` the daemon exposes no MCP surface at all and a
sandbox can never reach one.

## The security model in one picture

```
   AGENT SANDBOX (network=none, no runtime, read-only rootfs)
        │  only endpoint: a per-session unix socket
        │  GET /tools        POST /call            ← a plain HTTP shim, never MCP
        ▼
   ┌──────────────────────────── HOST ───────────────────────────────┐
   │  MCP BROKER (the choke point)                                    │
   │   • per-SESSION socket = the trusted identity (not a header)     │
   │   • deny-by-default: every list/call checked vs the gateway-     │
   │     approved grant for that session's agent group               │
   │   • audits every op (server, tool, status, bytes, duration)      │
   │   • expands ${ENV} secrets here, never logs them                 │
   │        │ speaks real MCP (JSON-RPC 2.0)                           │
   │        ├── LOCAL  → a hardened container (network=none, ro,       │
   │        │            non-root, dropped caps) running the stdio     │
   │        │            server  (untrusted code is isolated)         │
   │        └── REMOTE → the endpoint over HTTPS (TLS required)        │
   └──────────────────────────────────────────────────────────────────┘
```

Properties:

- **Not directly from the sandbox.** The sandbox holds no MCP client, no network, and
  no credentials. Its only MCP endpoint is the host broker socket; everything else is
  the broker's job.
- **Gateway‑approved, not blind.** Granting an agent access to a server + tools is a
  `ChangeMCPAccess` capability change. It passes a deterministic verifier (the server
  must be configured) and then the mandatory human‑approval floor — it shows up in
  **Approvals** like any other capability change.
- **Deny‑by‑default per call.** Even after a grant, the broker refuses any tool the
  grant does not name and any tool the server does not declare. A revoked grant stops
  working immediately (grants are resolved live, per call).
- **Audited.** Every list and call emits an audit record (session, server, tool,
  status, bytes, duration) to the daemon's structured logs — never the arguments or
  credential values.
- **Encrypted in transit.** Remote servers require `https` (plain `http` is allowed
  only for a loopback host, for local testing). The sandbox↔broker hop is a host‑local
  unix socket — no network, nothing on the wire.
- **Isolated.** A local server is third‑party code, so by default it runs in a
  hardened, `network=none` container (Docker, optionally with the gVisor runtime), not
  as a bare host process.
- **Secrets stay host‑side.** Write `${ENV_VAR}` in a server's env/headers; the broker
  expands it from the host environment at connect time. The catalog never stores the
  raw value, the API masks it, and it never reaches the agent.

## Enabling MCP

Start the control plane with a catalog path (and, in production, container isolation):

```
ic-controlplane \
  --mcp-catalog   /var/lib/ironclaw/mcp-catalog.json \
  --mcp-isolation container \
  --mcp-runtime   runsc \        # optional: run local servers under gVisor
  --mcp-image     ironclaw-mcp:latest   # default image for local servers
```

`--dev` enables MCP automatically with a catalog under the state dir and
`--mcp-isolation=none` (a dev box may have no container runtime) — it logs a warning
that local servers then run **unisolated**. Never use `none` in production.

| Flag | Meaning |
|------|---------|
| `--mcp-catalog PATH` | enables MCP; the 0600 JSON file of configured servers |
| `--mcp-isolation container\|none` | how local (stdio) servers run; `container` is the default |
| `--mcp-runtime NAME` | OCI runtime for isolation, passed as `docker --runtime` (e.g. `runsc`) |
| `--mcp-image REF` | default container image for local servers with no `image` set |

## Adding a server (web console → **MCP** tab)

1. **Name** it (`github`, `files`, …) and pick **Local (stdio)** or **Remote (HTTP)**.
2. **Local:** give the **Command** and **Arguments** (e.g. `npx` /
   `-y @modelcontextprotocol/server-github`), and optionally an isolation **Image** and
   **Environment** (`GITHUB_TOKEN=${GITHUB_TOKEN}`).
   **Remote:** give the `https` **URL** and an optional **Auth header**
   (`Bearer ${GITHUB_TOKEN}`).
3. **Save server.** This is operator infrastructure config — it grants no agent
   anything yet.
4. **Discover tools & grant** on the server's card connects to it, lists its tools, and
   lets you pick a subset + an agent. **Request grant** submits the approval.
5. Approve it in **Approvals**. On the agent's next launch the broker exposes exactly
   those tools as `‹server›__‹tool›` (e.g. `github__create_issue`).

### …or via the API

```bash
# configure a local server
curl -X PUT $API/v1/registry/mcp-servers/files \
  -d '{"transport":"stdio","command":"mcp-files","args":["--root","/data"]}'

# configure a remote server (secrets as ${ENV} references)
curl -X PUT $API/v1/registry/mcp-servers/github \
  -d '{"transport":"http","url":"https://mcp.example.com/rpc",
       "headers":{"Authorization":"Bearer ${GITHUB_TOKEN}"}}'

# discover its tools
curl -X POST $API/v1/registry/mcp-servers/github/probe

# grant an agent a named subset (-> Approvals -> approve)
curl -X POST $API/v1/ui/config/change \
  -d '{"kind":"mcp_access","agentGroupID":"team-a","requestedBy":"you",
       "after":{"server":"github","tools":["create_issue","list_issues"]}}'
```

An empty `tools` array grants every tool the server declares (the human approves the
server wholesale). The broker still refuses any tool the server does not actually
expose.

## What the agent sees

The agent gets ordinary tools named `‹server›__‹tool›` with the server's own JSON
schema. Calling one forwards to the broker; a policy denial or an upstream error comes
back as a tool error the model can read. An agent that wants tools on a *configured*
server can ask for them with the `request_capability_change` tool (kind `mcp_access`).
If no configured server has what it needs, it can go one step earlier and *propose a
brand-new server endpoint* (kind `mcp_register`, RFC-0007): the human approves the exact
`command`/`image` or `url` before it lands in the catalog, and the agent then requests
its tools via the separate `mcp_access` approval. Both are just requests a human must
approve — closing the OpenClaw register→approve→access→execute loop without a blind
approval surface.

## Trying it without a real server

`cmd/mcp-sample` is a tiny, credential‑free MCP server (tools `echo` and `add`) that
runs over stdio or HTTP:

```
mcp-sample                 # stdio  → configure as a local server (command = its path)
mcp-sample --http :9000    # HTTP   → configure as a remote server (url = http://127.0.0.1:9000)
```

It is the fixture the end‑to‑end tests use and a safe first server to wire through the
whole flow.

## Notes & limits

- A local server runs `network=none`. A server that genuinely needs the internet is
  better modeled as a **remote** server the host dials over TLS.
- The broker shares one upstream connection per server across sessions; per‑session
  isolation is enforced at the tool surface (each session sees only its grants).
- Removing a server from the catalog does not revoke existing grants (that is a
  separate gateway change); the broker simply can no longer reach it.

See [threat-model.md](threat-model.md) for the STRIDE treatment and
[contract.md](contract.md) (RFC‑0005) for the one frozen‑contract value MCP adds.
