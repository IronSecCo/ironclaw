# Architecture

IronClaw is a security-hardened, open-source assistant platform written entirely
in Go. A host **control-plane** orchestrates per-session **sandboxes**; the two
sides communicate only through a pair of encrypted SQLite queues.

## Components

- **Control-plane (host, AGENT1)** — HTTP API (mesh-only), the mandatory gateway,
  isolation launcher, router, delivery, sweep, key custodian, channel adapters,
  and the model-egress proxy.
- **Sandbox (AGENT2)** — the reasoning poll loop, the model provider, in-sandbox
  tools, and the queue access layer.
- **Frozen contract** (`internal/contract`) — the only package both sides import:
  typed IDs, enums, row structs, embedded SQL schema, crypto open helpers,
  interface-segregated queue access, and the gateway protocol.

## Trust model (summary)

1. Compiled Go, no interpreter in the sandbox — the agent cannot read or edit its
   own source.
2. A mandatory gateway — every control-plane mutation is a deterministic,
   human-approved, auditable transaction. No file is the source of truth.
3. Encrypted per-session queues — disk theft and cross-session reads are useless.
4. Least-privilege queue access — the sandbox can only read inbound and write
   outbound, enforced at the Go type level and the OS mount level.
5. gVisor (runsc) wraps every sandbox, behind a pluggable Isolator.
6. Tailscale fronts the control-plane API; no public port.
7. The sandbox has `network=none`; model calls go through the host proxy.

## Future extensions

Agent egress firewalling (beyond the model-proxy allowlist) and a Kata isolation
backend are documented but not built in the skeleton.
