<div align="center">

<img src="docs/assets/logo.svg" alt="IronClaw" width="380">

### Security-first, self-hosted AI agents — isolation you can prove, not just promise.

[![CI](https://github.com/nivardsec/ironclaw/actions/workflows/ci.yml/badge.svg)](https://github.com/nivardsec/ironclaw/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/nivardsec/ironclaw?sort=semver)](https://github.com/nivardsec/ironclaw/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/nivardsec/ironclaw.svg)](https://pkg.go.dev/github.com/nivardsec/ironclaw)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

IronClaw is an open-source platform for running personal AI assistants on infrastructure you
control. You talk to them through the chat apps you already use; each assistant runs as a real,
autonomous agent that can read, write, schedule, and reply. What makes it different is the threat
model: it assumes the agent — and the box it runs in — could be compromised at any moment, and
builds hard, provable walls so that even a misbehaving agent can't reach your data or your machine.

> **The security model, in one line:** each sandboxed agent runs with `network=none`, reaches the
> model only through a host proxy, and **cannot change its own configuration** — every capability
> change is held at a gateway for a human decision. The full design is in the
> [architecture overview](docs/architecture.md) and the [threat model](docs/threat-model.md).

<div align="center">

<img src="docs/assets/demo.svg" width="800" alt="Quickstart terminal session: one command installs ironctl and the control-plane; the control-plane starts in dev mode on http://127.0.0.1:8787; a capability change is submitted and HELD at the gateway pending human approval, then approved.">

</div>

## Get running in under two minutes

One command installs the two host binaries (`ironctl` + `ironclaw-controlplane`); in dev mode the
control-plane serves its API at **`http://127.0.0.1:8787`**. From a cold machine, you'll have a
capability change waiting at the security gateway in **under two minutes**:

```sh
# 1. Install — detects your OS/arch and verifies the SHA-256 checksum before installing
curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | sh

# 2. Start the control-plane in dev mode — API base URL: http://127.0.0.1:8787
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
ironclaw-controlplane --dev --api-addr 127.0.0.1:8787 &

# 3. Your first command — submit a change; it is HELD at the gateway for a human decision
ironctl change submit --kind persona --group default --by you
ironctl change pending                       # see it waiting
ironctl change approve <change-id> --by you   # apply it
```

On Windows, install with `irm https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.ps1 | iex`.
Version pinning, system-wide installs, and building from source are all in [Installation](#installation).

## CLI-first and API-first

This is a feature, not a missing dashboard. Every capability is a documented HTTP endpoint **and** an
`ironctl` subcommand, so IronClaw is scriptable, auditable, and CI-friendly from the first command —
with **no public web surface to phish, misconfigure, or leave exposed.** (A private, mesh-only web
console is on the [roadmap](#roadmap) — additive, and never the only way in.)

---

## Table of contents

- [Get running in under two minutes](#get-running-in-under-two-minutes)
- [CLI-first and API-first](#cli-first-and-api-first)
- [Why it's different](#why-its-different)
- [How it works](#how-it-works)
- [Project status](#project-status)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quickstart](#quickstart)
- [Examples](#examples)
- [Usage](#usage)
- [Configuration](#configuration)
- [Development](#development)
- [Repository layout](#repository-layout)
- [Security](#security)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

## Why it's different

| Pillar | What it is | Attack surface it removes |
|--------|------------|----------------------------|
| **Sealed runtime** | The agent ships as a compiled Go binary | Agent self-modification — there's no source inside the box to rewrite |
| **Approved by humans** | Every change to the harness clears a deterministic gateway | Silent setting changes — nothing changes without a human seeing and approving it |
| **Encrypted queues** | Per-session encrypted message queues; read-only inbound | Data theft at rest, and cross-session reads |
| **Sealed sandbox** | gVisor container, no network, host-proxied model calls | Data exfiltration and sandbox escape |
| **Private control panel** | Admin access over a private mesh (Tailscale) only | Remote attacks on the controls |

The throughline: **treat the agent as untrusted, and make the security boundary something you can
verify — not something you take on faith.**

## How it works

Two compiled Go programs that never share memory and talk only through a pair of encrypted SQLite
files per conversation:

```
                    ┌──────────────────────────────────────────────┐
   chat platforms   │            CONTROL-PLANE (host)              │
   ───────────────▶ │  api · gateway · router · delivery · sweep   │
   (Tailscale only) │  keys · channels · modelproxy · isolation    │
                    └───────┬───────────────────────────┬──────────┘
                            │ inbound.db (ro)            │ outbound.db (rw, host reads)
                            ▼ encrypted, per-session     ▲ encrypted, per-session
                    ┌──────────────────────────────────────────────┐
                    │           SANDBOX (gVisor, network=none)      │
                    │   loop · provider · tools · queue             │
                    │   model calls ─▶ host modelproxy unix socket  │
                    └──────────────────────────────────────────────┘
```

- The **control-plane** receives chats, routes them, holds the keys, runs the approval gateway, and
  performs every privileged action on the agent's behalf — after its own checks.
- The **sandbox** — one per conversation, wrapped in gVisor with no network of its own — reads its
  encrypted inbox (read-only), calls the AI model through the host proxy, and writes its encrypted
  outbox. It can *request* a capability change but can never apply one.
- The **frozen contract** (`internal/contract`) is the only package both sides import: typed IDs,
  row shapes, the embedded SQL schema, pinned cipher params, and the gateway protocol.

For the full design, see [`docs/architecture.md`](docs/architecture.md),
[`docs/threat-model.md`](docs/threat-model.md), and the plain-language tour in
[`docs/ironclaw-explained.md`](docs/ironclaw-explained.md).

## Project status

**Pre-alpha.** The architecture is settled and the full control-plane and sandbox pipelines are
implemented and tested. The encrypted-queue binding is now wired:

- **Encrypted-SQLite queue binding** — ✅ wired (**RFC-0001 applied**). `contract.Open*` open
  per-session SQLCipher databases via cgo (`github.com/mutecomm/go-sqlcipher/v4`); a round-trip test
  covers write→read, read-only-write rejection, wrong-key failure, and no-plaintext-on-disk. The
  build now requires `CGO_ENABLED=1` (a C toolchain). `internal/host/queue` uses the live binding;
  in-memory backends remain for `--dev` and tests.
- **Sandbox rootfs provisioning** — ✅ wired via a pluggable provisioner: `isolation` builds the
  hardened OCI spec, provisions the bundle rootfs (with image digest/signature verification against a
  trust policy), and execs `runsc`. A real launch still needs `runsc` and a provisioned/signed image
  present in the environment.
- **Production hardening (Wave 4)** — durable/pluggable master-key custody, a Prometheus `/metrics`
  surface, structured logging, API hardening (TLS, rate-limit, body limits, `/readyz`), host respawn
  + sandbox provider backoff, and model-proxy rate caps/audit/redaction have landed as packages;
  composing them into the daemon entrypoint is the remaining wiring step (see the [roadmap](#roadmap)).

See the [roadmap](#roadmap) for what remains. You can build, test, and run the control-plane today;
a live sandbox launch needs `runsc` plus a provisioned image.

## Prerequisites

| Requirement | For | Notes |
|-------------|-----|-------|
| **Go 1.23+ and a C toolchain** | building everything | `CGO_ENABLED=1` is required — the encrypted-SQLite binding builds via cgo |
| **containerd + gVisor (`runsc`)** | production sandboxing | runtime `io.containerd.runsc.v1`; not needed for `--dev` |
| **Tailscale** | remote admin access | the control-plane API binds to the tailnet IP; no public port |
| **SQLCipher (vendored)** | encrypted queues | the SQLCipher C amalgamation is vendored by the driver; no system lib needed |
| **Anthropic API key** | live model calls | injected host-side into the model proxy, never into the sandbox |

The three external runtime dependencies (gVisor, Tailscale, the encrypted-SQLite binding) are
intentionally **not vendored**. See [`deploy/README.md`](deploy/README.md) for host setup.

## Installation

### Prebuilt binaries (recommended)

One command installs the latest release — `ironctl` and `ironclaw-controlplane`. The script
detects your OS/arch, downloads the matching archive from
[GitHub Releases](https://github.com/nivardsec/ironclaw/releases), and verifies its SHA-256
checksum before installing.

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.ps1 | iex
```

A fresh release is published on every push to `main`, with prebuilt archives for:

| OS | Architectures |
|----|---------------|
| macOS | Intel (`amd64`) · Apple Silicon (`arm64`) |
| Linux | `amd64` · `arm64` |
| Windows | `amd64` |

The installer reads a few environment variables (pass them on the `sh` side of the pipe):

```sh
# Pin a version instead of latest
curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | IRONCLAW_VERSION=v0.1.66 sh

# Install system-wide (a normal user defaults to ~/.local/bin)
curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | sudo sh

# Choose the install directory
curl -fsSL https://raw.githubusercontent.com/nivardsec/ironclaw/main/scripts/install.sh | IRONCLAW_BINDIR="$HOME/bin" sh
```

Then confirm what you installed:

```sh
ironctl --version
```

Prefer to grab files by hand? Download the archive and `SHA256SUMS` for your platform from the
[latest release](https://github.com/nivardsec/ironclaw/releases/latest).

### From source

Requires Go 1.23+ and a C toolchain (`CGO_ENABLED=1` — the encrypted-SQLite binding builds via cgo).

```sh
# Clone
git clone https://github.com/nivardsec/ironclaw.git
cd ironclaw

# Build all binaries
make build            # == go build ./...

# Or install the two host binaries onto your PATH
go build -o /usr/local/bin/ironclaw-controlplane ./cmd/controlplane
go build -o /usr/local/bin/ironctl               ./cmd/ironctl
```

For a full system install — build and install the binaries, provision `/etc/ironclaw`
and `/var/lib/ironclaw`, and enable the service (systemd on Linux, launchd on macOS) —
run [`sudo deploy/install.sh`](deploy/install.sh). It needs root to write under `/etc`
and `/var/lib`. The external runtime dependencies it relies on (containerd + gVisor and
Tailscale) are set up separately — see [`deploy/README.md`](deploy/README.md).

### With Docker (`docker compose`)

Self-host the control-plane in one command. From a clone:

```sh
cp .env.example .env          # fill in ANTHROPIC_API_KEY (optional to boot)
docker compose up -d          # builds locally on first run, or pulls the GHCR image
docker compose logs -f controlplane   # CLAIM the admin token printed once on first run
```

The admin/API token is **minted on first run and printed once** in the logs (there is
no recovery) unless you set `IRONCLAW_API_TOKEN` yourself. The admin API is published
on `127.0.0.1:8787` only — front it with Tailscale for remote access.

Prefer the published image? It is pushed to GitHub Container Registry on every release:

```sh
docker pull ghcr.io/nivardsec/ironclaw-controlplane:latest
# or pin a release: docker pull ghcr.io/nivardsec/ironclaw-controlplane:v0.1.0
```

Set `IRONCLAW_IMAGE` in `.env` to pin that tag for `docker compose`. Every variable the
control-plane reads is documented in [`.env.example`](.env.example). The agent sandboxes
themselves are **not** compose services — the control-plane launches them as gVisor
(`runsc`) children with `network=none`; running real sandboxes needs a runsc-capable
host (see [`deploy/README.md`](deploy/README.md)).

## Quickstart

A fuller local walkthrough — run the control-plane **from source** in dev mode (no gVisor, binds to
loopback) and drive it with the admin CLI:

```sh
# Terminal 1 — start the control-plane in dev mode
export ANTHROPIC_API_KEY=sk-ant-...        # held host-side; never enters the sandbox
export IRONCLAW_API_TOKEN=$(openssl rand -hex 32)
go run ./cmd/controlplane --dev --api-addr 127.0.0.1:8787

# Terminal 2 — talk to the gateway with ironctl
export IRONCLAW_API_TOKEN=<same token as above>

# Submit a capability change — it is HELD pending a human decision (the gateway choke point)
ironctl change submit --kind persona --group default --by alice

# See what's waiting for approval, then approve or reject by id
ironctl change pending
ironctl change approve <change-id> --by alice

# Inspect the append-only audit log
ironctl audit --limit 20
```

Every mutation — persona, enabled tools, packages, wiring, permissions, mounts — flows through this
same gateway. There is no file-edit path that bypasses it.

## Examples

Runnable templates that configure a real agent against a running control-plane live in
[`examples/`](examples/) — each is a directory with a `README.md` and a `setup.sh`:

- [`personal-assistant/`](examples/personal-assistant/) — a private 1:1 assistant on Telegram, plus a walk-through of the mandatory change-approval flow.
- [`channel-triage/`](examples/channel-triage/) — a Slack triage bot that engages only on `@mention`, only for known senders.
- [`multi-agent-team/`](examples/multi-agent-team/) — two agents sharing one channel, separated by engage mode and priority.

## Usage

### `ironclaw-controlplane` — the host daemon

```sh
ironclaw-controlplane \
  --api-addr "$(tailscale ip -4):8787" \            # bind to the tailnet IP (no public port)
  --model-proxy-socket /run/ironclaw/modelproxy.sock \
  --runtime runsc \                                 # container runtime for sandboxes
  --state-dir /var/lib/ironclaw \
  --sweep-interval 60s
```

| Flag | Default | Purpose |
|------|---------|---------|
| `--api-addr` | `127.0.0.1:8787` | control-plane API address; set to the tailnet IP in production |
| `--model-proxy-socket` | `/run/ironclaw/modelproxy.sock` | unix socket bound into each sandbox for model egress |
| `--state-dir` | OS-specific | gateway change store, audit log, keystore |
| `--runtime` | `runsc` | OCI runtime for sandboxes |
| `--bundle-root` | `<state-dir>/bundles` | per-session OCI bundles |
| `--sweep-interval` | `60s` | stale-sandbox / due-message sweep cadence |
| `--dev` | `false` | loopback bind, no gVisor — local development only |

Environment: `ANTHROPIC_API_KEY` (model proxy credential, host-only) and `IRONCLAW_API_TOKEN`
(bearer token required on every API call when set).

### `ironctl` — the admin CLI

A thin client of the control-plane API. `--addr` defaults to `http://127.0.0.1:8787`; the bearer
token comes from `IRONCLAW_API_TOKEN` or `--token`.

```sh
ironctl change submit  --kind <k> --group <g> --by <user>   # k: persona|enabled_tools|packages|wiring|permissions|mounts
ironctl change pending                                       # list changes awaiting a decision
ironctl change history                                       # all changes and their outcomes
ironctl change approve <id> --by <user>
ironctl change reject  <id> --by <user>
ironctl audit [--limit N]                                    # append-only gateway audit log
```

### `sandbox` — the in-sandbox agent

Launched by the control-plane's isolator, not by hand. It receives its session key and queue paths
and runs the reasoning loop. Key flags (`cmd/sandbox`): `--inbound`, `--outbound`, `--key`,
`--workspace`, `--heartbeat`, `--model-socket`, `--model-host`, `--model`.

### Control-plane HTTP API

| Method & path | Purpose |
|---------------|---------|
| `GET  /healthz` | liveness (unauthenticated) |
| `POST /v1/changes` | submit a `ChangeRequest` |
| `GET  /v1/changes/pending` | list pending changes |
| `GET  /v1/changes/history` | list all changes |
| `POST /v1/changes/{id}/decision` | record an approve/reject decision |
| `GET  /v1/audit` | read the audit log |

## Configuration

- **State** lives under `--state-dir`: the durable gateway change store (survives restart), the
  append-only JSONL audit log, and the host keystore.
- **Secrets** are host-only. The Anthropic key is injected into outbound model calls by the host
  `modelproxy`; the sandbox never sees it and has `network=none`. Per-session 256-bit keys are
  generated and held by the host and handed to the sandbox via tmpfs at launch — never via an env
  var, never baked into the image.
- **Mesh.** Bind `--api-addr` to the Tailscale interface and firewall the API port on every other
  interface. See [`deploy/README.md`](deploy/README.md).

## Development

```sh
make build     # go build ./...
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt -w .
```

All tests pass on a stdlib-only tree (the encrypted-SQLite CGo path is gated). The black-box
behavioral suite lives in [`test/parity/`](test/parity) and exercises routing fan-out, engage
modes, session resolution, delivery dedup, the gateway's mandatory-approval flow, and a cross-mount
live-poll spec — over the observable surfaces (the two queues + the API) only.

**The frozen contract.** `internal/contract/**` is the single seam both sides import and is
**frozen**: changing it requires a dated RFC in [`docs/contract.md`](docs/contract.md) and both
CODEOWNERS' approval. Drift here surfaces at runtime as a silent decrypt or routing failure, not a
build error — which is why the freeze is strict. See [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Repository layout

```
ironclaw/
  cmd/
    controlplane/   # host daemon entrypoint
    sandbox/        # in-sandbox agent entrypoint
    ironctl/        # admin CLI
  internal/
    contract/       # FROZEN SEAM — shared types, schema, crypto, gateway protocol
    host/           # control-plane: api gateway isolation router delivery sweep keys channels modelproxy registry scheduling queue
    sandbox/        # sandbox: loop provider tools queue
  api/              # control-plane API reference
  deploy/           # host install script + notes (gVisor, containerd, Tailscale, systemd)
  docs/             # architecture, threat-model, contract (+ RFC log), design-plan, building
  test/parity/      # black-box behavioral suite over the queues + API
```

## Security

IronClaw assumes the sandboxed agent is potentially compromised and designs the boundary so it
cannot escalate. The full threat-and-mitigation table is in
[`docs/threat-model.md`](docs/threat-model.md). Highlights:

- Compiled Go, no interpreter in the sandbox → the agent cannot read or edit its own source.
- All config mutations flow through the mandatory gateway → deterministic verifier chain → human
  approval → idempotent apply. There is no unapproved action path (the legacy `script`-field RCE
  class is designed out).
- Per-session encrypted queues; least-privilege access enforced three ways (Go interface
  segregation, `PRAGMA query_only`, read-only OS bind mount).
- `network=none` sandboxes; model calls only via the host proxy with a destination allowlist.

To report a vulnerability, please open a private security advisory rather than a public issue.

## Roadmap

- [x] Architecture and threat model
- [x] Compiling skeleton: frozen contract, control-plane and sandbox stubs, CI
- [x] Control plane (routing, gateway, isolation spec, key custody, delivery, sweep) on in-memory backends
- [x] Sandbox (agent loop, model provider, queue access, tools)
- [x] Encrypted-SQLite queue binding (RFC-0001) — live encrypted per-session queues
- [x] Cross-mount live-poll integration on the encrypted backend
- [x] Sandbox rootfs provisioning (pluggable) + durable per-group workspace/memory + image trust-policy verification
- [x] Concrete channel adapters: Telegram, Slack, Discord
- [x] Registry admin HTTP API + `ironctl` resource subcommands
- [x] Interactive `ask_user_question` + task-management tools (list/cancel/pause/resume/update)
- [x] End-to-end lifecycle integration test (fake isolator/provider over the real stack)

**Production hardening (Wave 4) — landed as packages, daemon wiring pending:**

- [x] Durable / pluggable master-key custody (file-sealed keystore + KMS seam)
- [x] Prometheus metrics package (`/metrics`) and structured (`slog`) logging
- [x] API hardening: optional TLS, rate-limit, body limits, `/readyz`
- [x] Host respawn crash-loop backoff + sandbox provider backoff/circuit breaker
- [x] Model-proxy rate caps + audit logging + secret redaction
- [ ] Daemon wiring v2 — compose the hardening subsystems into `cmd/controlplane`
- [ ] Real `runsc` launch in a provisioned environment + production deployment units/installer/image

**Future work (Wave 5, design-gated):**

- [ ] Kata isolation backend behind the same hardened `Isolator` interface
- [ ] Egress broker for approved external APIs (relaxes the sealed `network=none` posture)
- [ ] Gateway auto-approval policy + RBAC (the mandatory-human floor stays the default)
- [ ] Agent-to-agent messaging + approval-gated `create_agent` (RFC-0004)

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the contract-freeze rule and the agent-ownership model
(the control-plane and sandbox trees are owned and built separately against the frozen seam).

## License

[MIT](LICENSE) © 2026 nivardsec
