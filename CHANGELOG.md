# Changelog

All notable changes to IronClaw are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The pre-1.0 backend is feature-complete; work in progress targets the 1.0 launch
(packaging, docs, web console, and supply-chain hardening).

### Added

- **Encrypted per-session queues.** SQLCipher-backed inbound/outbound queues with
  interface-segregated access (the sandbox gets read-only inbound + write-only
  outbound), a durable encrypted registry backend, and a per-session key factory.
- **gVisor sandbox isolation.** Hardened OCI spec (`network=none`, all caps
  dropped, `no_new_privs`, non-root userns, read-only rootfs), per-session bundle
  build, and a pluggable rootfs provisioner that pulls + unpacks images host-side.
- **OCI resource limits + seccomp.** cgroup memory/CPU/pids caps (safe defaults,
  overridable via `SandboxSpec`) and a deny-by-default seccomp profile.
- **Sandbox image trust policy.** The provisioner verifies an image's resolved
  digest against a configured trust policy (pinned-digest baseline + pluggable
  signature verifier) before unpacking, failing closed.
- **Host gateway.** Mandatory human-approval floor with a durable change store +
  append-only audit log; capability changes flow only through the gateway.
- **Model egress proxy.** The sandbox's sole network path: a unix-socket proxy
  with a destination allowlist, host-side credential injection, per-session rate
  caps, audit records, and response secret redaction.
- **Channel adapters.** Telegram, Slack, and Discord adapters behind a common
  interface, each redacting its bot token from errors.
- **Control-plane API.** Mesh-bound HTTP API (gateway submit/approve, registry
  admin CRUD, audit/history) with optional bearer auth, TLS, rate limiting, body
  caps, `/readyz`, and `/metrics`.
- **Durable key custody.** File-sealed keystore (master key at `0600` + sealed
  per-session keys) so session keys survive a control-plane restart; a
  `KeySource` seam for external KMS.
- **Resilience.** Sandbox provider exponential backoff + circuit breaker, and a
  host crash-loop respawn backoff that parks a session for human attention after
  a failure ceiling.
- **Observability.** Structured `log/slog` logging (text/JSON, secret-redacting)
  and a Prometheus-format metrics registry.
- **Operations.** Real installer (`deploy/install.sh`), systemd unit + macOS
  launchd plist, and a sandbox container image (`container/`).
- **Tooling.** `ironctl` CLI (change workflow, registry admin, audit).

### Security

- Sandbox is sealed (`network=none`) by design; the model proxy is the only egress.
- Secrets (model credential, API token, bot tokens) live only on the host and are
  redacted from logs and forwarded responses.
- The frozen host↔sandbox contract is RFC-gated to prevent silent decrypt/routing
  drift.

[Unreleased]: https://github.com/nivardsec/ironclaw/commits/main
