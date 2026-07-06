# n8n-nodes-ironclaw — run untrusted code in an IronClaw sandbox

An [n8n](https://n8n.io/) community node that runs a workflow's untrusted or
model-generated code inside an **isolated, hardened IronClaw sandbox box** instead
of on your n8n host. The box has **no network, no host filesystem, no Docker
socket**, runs **non-root** under a read-only rootfs, and is torn down after the
command returns — the isolation boundary IronClaw
[proves holds](../../red-team-escape/), not just promises.

If your workflow ever runs a shell command, a `python`/`node` snippet, an LLM tool
call, or a webhook payload, that is a host-shell foot-gun. Route it through the
**IronClaw Sandbox** node and it becomes provably contained.

> This is the low-code / automation-ecosystem sibling of IronClaw's
> [agent-framework integrations](../) (Vercel AI SDK, LangChain, CrewAI, …). Same
> sandbox boundary, a different audience: n8n's ops/automation users.

## The node

**IronClaw Sandbox** — one node, three inputs:

| Field | Meaning |
| --- | --- |
| **Language** | `Shell` (run verbatim), `Python`, or `Node.js` (the snippet is piped to the interpreter). |
| **Code** | The command or snippet to run inside the sandbox. |
| **Image** | Optional container image override (e.g. `python:3.12-slim`). Default: the server's image (`alpine`). |
| **Timeout (Seconds)** | Per-execution timeout that bounds a runaway command. |
| **Continue When Command Exits Non-Zero** | Pass a non-zero command exit through as data (default) or raise a node error. |

Output item:

```json
{
  "exitCode": 0,
  "stdout": "hello from inside the IronClaw sandbox\nuid=65532 gid=65532 ...",
  "stderr": "",
  "containment": "runtime=runsc (gVisor: ...), network=none, caps=drop-all, user=65532 ...",
  "image": "docker.io/library/alpine:3.20"
}
```

The `containment` field always names the controls IronClaw actually enforced, so a
run is auditable from inside the workflow.

## How it connects

The node is a thin client of IronClaw's `sandbox_exec` MCP tool. Point it at a
running IronClaw MCP server:

```bash
# On the machine that has your container runtime (Docker/Podman):
ironctl mcp serve --http :9000          # loopback-only, no token needed
```

Then create an **IronClaw Sandbox API** credential in n8n with:

- **Endpoint** — `http://127.0.0.1:9000` (or wherever `ironctl mcp serve` is bound)
- **Auth Token** — only when the server is bound to a non-loopback address
  (IronClaw refuses to expose `sandbox_exec` off-loopback without one)

`ironctl` ships inside the IronClaw binary — see the
[MCP server docs](https://ironsec.co/docs/integrations/mcp-server).

## Install (community node)

In n8n: **Settings → Community Nodes → Install**, package name:

```
n8n-nodes-ironclaw
```

Or from a checkout, build and link locally:

```bash
cd examples/integrations/n8n
npm install
npm run build          # tsc + copy the icon into dist/
# then `npm link` into your n8n custom-extensions dir, or publish to npm
```

> npm publish is a follow-up (needs an npm token); the node builds, lints, and
> runs from a local link today.

## Try it: import the example workflow

[`workflows/ironclaw-sandbox.example.json`](workflows/ironclaw-sandbox.example.json)
is an import-ready workflow: a Manual Trigger into an **IronClaw Sandbox** node
whose code runs a benign command **and** two escape attempts (network egress, the
Docker socket). Import it (**Workflows → Import from File**), attach your credential,
and run it — the escapes come back blocked in the node output.

## Prove containment in one command

No n8n instance required. `run.sh` stands up `ironctl mcp serve`, then drives the
node's **real execution path** (the same `sandbox_exec` MCP client the node calls
at runtime) through one benign task plus the shared battery of escape attempts,
and prints a PASS/FAIL containment table:

```sh
examples/integrations/n8n/run.sh
```

```
  [OK ] benign task: run agent code                    ->  [exit 0] hello from inside the IronClaw sandbox uid=65532...
  [OK ] network egress: only loopback exists           ->  [exit 0] lo
  [OK ] network egress: DNS lookup of api.anthropic...  ->  [exit 0] NO_EGRESS
  [OK ] host escape: Docker Engine socket is absent    ->  [exit 0] ABSENT
  [OK ] host escape: host filesystem is not mounted    ->  [exit 0] CONTAINED

RESULT: PASS -- benign code ran; every escape attempt was contained.
```

`run.sh --keep` leaves the MCP server running; set `IRONCLAW_MCP_ADDR` to reuse a
server you already have up. It builds `ironctl` from source if it is not on your
`PATH`.

> **gVisor (runsc) is required for this smoke.** `ironctl mcp serve` runs an
> arbitrary `sh -c` pipeline in the box, and IronClaw's restrictive seccomp
> profile omits `fork`/`vfork` — the syscalls busybox/dash use to spawn a
> subprocess in a pipeline or list. gVisor's guest kernel handles those
> internally; under plain `runc` they hit the host seccomp filter and the shell
> cannot fork, so the multi-command probes would fail for a reason unrelated to
> containment. When `runsc` is not registered, `run.sh` **skips** rather than
> report a hollow result. Install
> [gVisor](https://gvisor.dev/docs/user_guide/install/) for a real run.

Even without gVisor you can confirm the boundary with single-command probes
against the node's own `sandbox_exec` endpoint (each `sh -c` execs directly, no
fork). Observed on `runc`, `alpine:3.20`:

```
  non-root user (id)              -> uid=65532 gid=65532 groups=65532
  network: only loopback iface    -> lo
  host fs not mounted (/host)     -> ls: /host: No such file or directory
  read-only rootfs (touch /etc/x) -> touch: /etc/pwn: Read-only file system
  docker socket present?          -> ls: /var/run/docker.sock: No such file or directory
  capabilities dropped (CapEff)   -> CapEff:  0000000000000000
```

## How it works

1. The node reads the **IronClaw Sandbox API** credential (endpoint + optional token).
2. For each input item it POSTs a single JSON-RPC `tools/call` for `sandbox_exec`
   to `ironctl mcp serve` — a stateless call, no MCP session handshake needed.
3. `ironctl` launches an ephemeral, hardened `ic-sbx-mcp-*` box
   (`--runtime runsc --network none --cap-drop ALL --read-only --user 65532:65532`,
   restrictive seccomp, cpu/mem/pids caps), runs the command via `sh -c`, captures
   stdout/stderr/exit, and tears the box down.
4. The node parses the result into `{ exitCode, stdout, stderr, containment, image }`
   and emits it as the workflow item.

The command runs where it cannot reach your n8n host — no NIC, no host mounts, no
socket, non-root.

## Files

| Path | What |
| --- | --- |
| `nodes/IronClawSandbox/IronClawSandbox.node.ts` | The n8n node. |
| `nodes/IronClawSandbox/McpClient.ts` | Dependency-free `sandbox_exec` MCP client (also drives the smoke). |
| `credentials/IronClawSandboxApi.credentials.ts` | Endpoint + token credential. |
| `workflows/ironclaw-sandbox.example.json` | Import-ready example workflow. |
| `run.sh` / `run.ts` | One-command containment smoke. |
