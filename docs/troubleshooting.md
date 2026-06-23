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
         see: https://ironsecco.github.io/ironclaw/troubleshooting/
  [OK  ] build toolchain: go1.23 — control-plane build requires CGO_ENABLED=1 (encrypted SQLite)
  [WARN] model credential: none set — the zero-credential `mock` provider works, but no real model is reachable
         fix: set one of ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, or IRONCLAW_MODEL_GATEWAY_URL on the daemon
         see: https://ironsecco.github.io/ironclaw/troubleshooting/
  [WARN] channel adapters: no channel armed from the environment
         ...
```

## What each `ironctl doctor` check means

| Check | Green | Yellow / Red | Fix it below |
| --- | --- | --- | --- |
| **control-plane API** | `/healthz` reachable at `--addr`. | Daemon not running, wrong `--addr`, or port blocked. | [Daemon unreachable](#daemon-unreachable-connection-refused) |
| **API auth / token** | Bearer token accepted. | Ungated API (set `IRONCLAW_API_TOKEN` for defense-in-depth), token missing, or token rejected (`401`). | [API token rejected](#api-token-missing-or-rejected-401) |
| **readiness** | `/readyz` reports `ready`. | Dependencies still coming up — check the daemon logs if it persists. | [Daemon unreachable](#daemon-unreachable-connection-refused) |
| **sandbox runtime** | gVisor's `runsc` on `PATH`. Honors `IRONCLAW_RUNTIME` / `--runtime`. | Missing runtime (FAIL on Linux; informational off-Linux), or a **relaxed** runtime (docker/runc) with no gVisor syscall isolation. | [gVisor / runtime missing](#sandbox-runtime-runsc-not-found-gvisor) |
| **build toolchain** | Reports the Go version and restates the control-plane's build requirement. | — (informational; the daemon needs `CGO_ENABLED=1` and a C toolchain for the encrypted SQLCipher queues). | [CGO / SQLCipher build errors](#build-fails-with-a-sqlite-cgo-error) |
| **model credential** | A provider key or gateway URL is configured host-side. | None set — the zero-credential `mock` provider still serves chat, but no real model is reachable. Set `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, or `IRONCLAW_MODEL_GATEWAY_URL`. | [No model credentials](#no-model-credentials-and-the-zero-credential-mock-path) |
| **channel adapters** | At least one adapter armed from the environment. | None armed — channels are optional; set e.g. `SLACK_BOT_TOKEN` / `TELEGRAM_BOT_TOKEN` or wire one with `ironctl registry wiring …`. | [Channel adapter not arming](#channel-adapter-not-arming-env-mismatch) |
| **onboard config** | The `0600` token env-file is present and owner-only. | Absent (run `ironctl onboard`), a directory, or readable beyond the owner (`chmod 600`). | [onboard config](#onboard-config-missing-or-too-permissive) |
| **model-proxy socket** | The host model-proxy unix socket accepts a connection. | Socket missing (daemon not started) or present but not accepting connections (restart the control-plane). | [Daemon unreachable](#daemon-unreachable-connection-refused) |

The credential and channel checks read **exactly** the same environment variables
the control-plane consumes on boot (the detectors are shared with `ironctl
onboard`), so a check that says "armed" really will light up at runtime — and only
the *presence* of a secret is ever reported, never its value.

---

## First-run failures

The fixes for the most common errors people hit going from "found the repo" to
"running locally," in the rough order you'll meet them.

### Build fails with a SQLite / cgo error

```
# undefined: sqlite3 …  /  cgo: C compiler "cc" not found  /  exec: "gcc": not found
```

IronClaw builds with **`CGO_ENABLED=1`** — the encrypted-queue binding (SQLCipher)
is compiled via cgo and is **unconditional**. You need a C toolchain:

- **macOS:** `xcode-select --install`
- **Debian / Ubuntu:** `sudo apt-get install build-essential`

Then build with cgo enabled:

```sh
CGO_ENABLED=1 go build -o bin/ ./cmd/controlplane ./cmd/ironctl
```

The SQLCipher C amalgamation is **vendored by the driver**, so you do *not* need a
system `libsqlcipher`. If you see `CGO_ENABLED=0` somewhere in your environment or
CI, unset it. See [Building from source](building.md).

### Daemon unreachable (`connection refused`)

```
[FAIL] control-plane API: dial tcp 127.0.0.1:8787: connect: connection refused
```

The control-plane isn't running (or `--addr` points at the wrong place). Start it:

- **Zero-credential demo:** `docker compose -f docker-compose.demo.yml up --build -d`
- **Local dev (loopback, no gVisor):** `./bin/controlplane --dev --api-addr 127.0.0.1:8787`

Then re-run `ironctl doctor`. If `ironctl` runs on a different host or port, pass
`--addr http://<host>:<port>`. A `readiness: not ready` warning right after start
is normal — give dependencies a moment, then check the daemon logs if it persists.

### Port 8787 already in use

```
listen tcp 127.0.0.1:8787: bind: address already in use
```

Something else (often a previous control-plane that didn't shut down) holds the
port. Find and stop it, or bind elsewhere:

```sh
lsof -i :8787                                   # see what holds the port (macOS/Linux)
./bin/controlplane --dev --api-addr 127.0.0.1:8799   # …or pick another port
```

For the demo compose file, change the published port mapping in
`docker-compose.demo.yml` and point `ironctl --addr` at the new port.

### Docker / Compose won't start the demo

The 5-minute `mock` path needs Docker running and the sandbox image built:

```sh
# Docker Desktop (macOS/Windows) must be running, or dockerd on Linux.
bash container/build.sh                                  # build the sandbox image once (~1–2 min)
docker compose -f docker-compose.demo.yml up --build -d  # start the demo control-plane
docker compose -f docker-compose.demo.yml logs -f        # watch it come up / read errors
```

Common causes: Docker isn't started; the sandbox image wasn't built yet (run
`container/build.sh` first); or an older `docker-compose` v1 — use the
`docker compose` (v2) subcommand. The demo mounts the host Docker socket and runs
the sandbox as a **runc** container — a relaxed, laptop-only posture, *not* the
sealed production seal. Tear it down with
`docker compose -f docker-compose.demo.yml down`.

### Sandbox runtime: `runsc` not found (gVisor)

```
[FAIL] sandbox runtime (runsc): runsc not found on PATH
```

Production sandboxes run on the **Linux control-plane host** behind gVisor.

- **On Linux:** install [gVisor](https://gvisor.dev/docs/user_guide/install/) so
  `runsc` is on `PATH`, or pass `--runtime <bin>` to point at it.
- **Need to run without gVisor right now?** Set `IRONCLAW_RUNTIME=docker` (or pass
  `--runtime`) for the **relaxed runc fallback** — shares the host kernel, so it is
  **not** the hardened isolation posture. Use it for a demo, never for real
  workloads.
- **On macOS / Windows:** this check is informational — the production sandbox is a
  Linux-host concern. Use the Docker demo or a Linux host/VM for real isolation.

### Permission / user-namespace (userns) errors

```
# operation not permitted  /  newuidmap: …  /  failed to create user namespace
```

gVisor's `runsc` and rootless containers need **unprivileged user namespaces** on
the Linux host:

- Ensure the host kernel has user namespaces enabled
  (`sysctl kernel.unprivileged_userns_clone=1` on some distros).
- For rootless setups, install the `uidmap` package so `newuidmap` / `newgidmap`
  are present.
- Confirm `runsc` was installed per the
  [gVisor install guide](https://gvisor.dev/docs/user_guide/install/) and is being
  invoked on a Linux host (not inside an unprivileged nested container that blocks
  userns). The supported production target is a Linux host with containerd + gVisor.

### No model credentials (and the zero-credential `mock` path)

```
[WARN] model credential: none set — the zero-credential `mock` provider works, but no real model is reachable
```

This is **fine for trying IronClaw** — the offline `mock-agent` runs the full
engage → sandbox → reply path with **no key**. To talk to a real model, set one
provider credential **host-side** (it never enters a sandbox) and point your agent
group at that provider:

```sh
export ANTHROPIC_API_KEY=sk-ant-...     # or OPENAI_API_KEY / OPENROUTER_API_KEY,
                                        # or IRONCLAW_MODEL_GATEWAY_URL for a gateway
```

Restart the daemon and re-run `ironctl doctor` — the check flips to green. See the
[Quickstart](quickstart.md#a-working-chat-in-5-minutes-no-credentials).

### Channel adapter not arming (env mismatch)

```
[WARN] channel adapters: no channel armed from the environment
```

Channels are optional. An adapter arms only when the daemon sees the **exact**
environment variable it expects — e.g. `SLACK_BOT_TOKEN`, `TELEGRAM_BOT_TOKEN`
(these are **not** `IRONCLAW_`-prefixed). A frequent mistake is exporting the token
in the `ironctl` shell instead of the **control-plane's** environment — they must
be set where the daemon boots. The [Channel adapters](channels.md) reference lists
every built-in channel and the variable it reads; or wire one explicitly with
`ironctl registry wiring …`.

### API token missing or rejected (401)

```
[FAIL] API auth / token: token rejected (401)
```

The bearer token `ironctl` sends doesn't match what the daemon started with. Export
the same value the control-plane was launched with (or that `ironctl onboard`
minted):

```sh
export IRONCLAW_API_TOKEN=<the token the daemon was started with>
```

A `WARN` that the API is *ungated* (no token required) is expected in `--dev`; set
`IRONCLAW_API_TOKEN` on both the daemon and client for defense-in-depth behind the
mesh. The zero-credential demo uses the fixed loopback token `ironclaw-demo`.

### onboard config missing or too permissive

```
[WARN] onboard config: not present   (or)   readable beyond the owner
```

Run `ironctl onboard` to mint a local API token and write the `0600` env-file. If
the file is readable beyond the owner, tighten it with `chmod 600 <path>`. If a
directory exists where the file should be, remove it and re-run `ironctl onboard`.

---

## Still stuck?

- Re-run `ironctl doctor` after every fix — it's the fastest way to confirm a check
  flipped to green.
- Read the **[FAQ](faq.md)** for the "is it really sandboxed?", "do I need
  credentials?", providers/channels, and licensing questions.
- Search or open a thread in
  [GitHub Discussions](https://github.com/IronSecCo/ironclaw/discussions), or file a
  [bug report](https://github.com/IronSecCo/ironclaw/issues/new/choose).

See also: [Quickstart](quickstart.md) · [FAQ](faq.md) · [Channels](channels.md) ·
[Building from source](building.md).
