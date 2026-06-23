# Troubleshooting

> _"I followed the steps, it didn't work, and I don't know why."_

`ironctl doctor` exists to answer that. It runs a set of **read-only** preflight
checks against your environment and the control-plane and prints a `pass / warn /
fail` report — each line with a one-line remediation and a docs link. It never
prints secret values (presence only) and exits non-zero if any check **FAILs**, so
it doubles as a scriptable health gate.

```sh
ironctl doctor
# or point at a non-default daemon / runtime:
ironctl --addr http://127.0.0.1:8787 doctor --runtime runsc \
  --model-proxy-socket /run/ironclaw/modelproxy.sock
```

Example output from a fresh, not-yet-started checkout:

```
ironctl doctor — diagnostics
  [FAIL] control-plane API: dial tcp 127.0.0.1:8787: connect: connection refused
         fix: is the daemon running? check --addr and that the port is reachable
         see: https://ironsecco.github.io/ironclaw/quickstart/
  [OK  ] build toolchain: go1.23 — control-plane build requires CGO_ENABLED=1 (encrypted SQLite)
  [WARN] model credential: none set — the zero-credential `mock` provider works, but no real model is reachable
         fix: set one of ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, or IRONCLAW_MODEL_GATEWAY_URL on the daemon
         see: https://ironsecco.github.io/ironclaw/quickstart/
  [WARN] channel adapters: no channel armed from the environment
         ...
```

## What each check means

| Check | Green | Yellow / Red |
| --- | --- | --- |
| **control-plane API** | `/healthz` reachable at `--addr`. | Daemon not running, wrong `--addr`, or port blocked. |
| **API auth / token** | Bearer token accepted. | Ungated API (set `IRONCLAW_API_TOKEN` for defense-in-depth), token missing, or token rejected (`401`). |
| **readiness** | `/readyz` reports `ready`. | Dependencies still coming up — check the daemon logs if it persists. |
| **sandbox runtime** | gVisor's `runsc` on `PATH`. Honors `IRONCLAW_RUNTIME` / `--runtime`. | Missing runtime (FAIL on Linux; informational off-Linux), or a **relaxed** runtime (docker/runc) with no gVisor syscall isolation. |
| **build toolchain** | Reports the Go version and restates the control-plane's build requirement. | — (informational; the daemon needs `CGO_ENABLED=1` and a C toolchain for the encrypted SQLCipher queues). |
| **model credential** | A provider key or gateway URL is configured host-side. | None set — the zero-credential `mock` provider still serves chat, but no real model is reachable. Set `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, or `IRONCLAW_MODEL_GATEWAY_URL`. |
| **channel adapters** | At least one adapter armed from the environment. | None armed — channels are optional; set e.g. `SLACK_BOT_TOKEN` / `TELEGRAM_BOT_TOKEN` or wire one with `ironctl registry wiring …`. |
| **onboard config** | The `0600` token env-file is present and owner-only. | Absent (run `ironctl onboard`), a directory, or readable beyond the owner (`chmod 600`). |
| **model-proxy socket** | The host model-proxy unix socket accepts a connection. | Socket missing (daemon not started) or present but not accepting connections (restart the control-plane). |

The credential and channel checks read **exactly** the same environment variables
the control-plane consumes on boot (the detectors are shared with `ironctl
onboard`), so a check that says "armed" really will light up at runtime — and only
the *presence* of a secret is ever reported, never its value.

## Common fixes

- **`control-plane API: connection refused`** — start the daemon (`./bin/controlplane
  --dev` for a loopback dev run, or your service unit), then re-run `ironctl doctor`.
- **`sandbox runtime: runsc not found` (Linux)** — install
  [gVisor](https://gvisor.dev/docs/user_guide/install/), or set `IRONCLAW_RUNTIME=docker`
  for the relaxed runc fallback on hosts without gVisor (not the hardened posture).
- **`model credential: none set`** — fine for the `mock` demo; for a real model,
  export a provider key host-side and point your agent group at that provider.
- **`onboard config: not present`** — run `ironctl onboard` to mint a local API
  token and write the `0600` config file.

See also: [Quickstart](quickstart.md) · [Channels](channels.md) ·
[Building from source](building.md).
