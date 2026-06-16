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

## In-memory dev backends (control-plane)

The full control-plane pipeline — registry, router, queues, delivery, sweep, and
the gateway's durability — runs today against interface-driven, in-memory backends
so it is testable WITHOUT the pending encrypted-SQLite binding:

- **`internal/host/registry`** — `Registry` interface + `MemRegistry`, the
  control-plane data model (agent groups, messaging groups, wirings, sessions,
  users, roles, members, destinations). Host-internal; the sandbox never sees it,
  so it is NOT part of the frozen contract. It owns session partitioning
  (shared / per-thread / agent-shared) and the access precedence
  (owner > global-admin > scoped-admin > member).
- **`internal/host/queue`** — `MemInbound` (implements both `contract.InboundWriter`
  and `InboundReader`) and `MemOutbound` (both `OutboundWriter` and
  `OutboundReader`) over a shared in-memory store, so a host writer and a test
  sandbox reader of the same session agree. Seq parity is enforced in the Write
  methods: host writes EVEN, sandbox writes ODD.
- **`internal/host/router`** — `RouteInbound(ctx, InboundEvent) ([]RoutingOutcome,
  error)` fans an event out to every wired agent group through an injected
  inbound-writer factory and `Waker`.
- **`internal/host/delivery`** — `Poll(ctx)` reads due outbound messages through an
  injected `OutboundReader` factory, dedups in memory (mirrored into the inbound
  `delivered` table once persistence lands), re-authorizes system actions
  host-side (`authorizeSystemAction`), and enforces destination permission.
- **`internal/host/sweep`** — `Run(ctx)` iterates sessions, probes liveness via an
  injected `Prober`, and kills stuck sandboxes via an injected `Killer`.
- **`internal/host/gateway`** — `FileStore` (durable JSON change store, reloads
  pending on restart), `AuditLog` (append-only JSONL of submit/verdict/decision/
  apply), and two extra deterministic verifiers (`MountAllowlistVerifier`,
  `PackageNameVerifier`) that only ADD rejections ahead of the `AlwaysRequireHuman`
  floor.

The same interfaces accept the durable backends with no caller changes.

## What remains gated

- **Encrypted-SQLite queue binding (RFC-0001).** The host needs a read/write
  inbound opener (`contract.OpenInboundRW`) that the frozen contract does not yet
  expose; the SQLite-gated openers in `internal/host/queue` still return the
  pending-binding error. See the RFC log in [contract.md](contract.md). The
  in-memory queue backends above stand in until it lands.
- **`RunscIsolator` (gVisor) + containerd.** The real isolator and the
  cross-mount live-poll parity check come after the queue binding.

## Future extensions

Agent egress firewalling (beyond the model-proxy allowlist) and a Kata isolation
backend are documented but not built in the skeleton.
