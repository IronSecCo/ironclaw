# hello-ironclaw — the zero-credential end-to-end check

The fastest way to prove IronClaw actually works on your machine: one command brings
up the offline demo control-plane, sends a chat message through the **real secured
path** — engage → per-session Docker sandbox → encrypted queue → delivery — and
**asserts the agent's reply comes back**. No model key, no channel tokens, no gVisor.

This is also IronClaw's user-facing smoke test: the same script runs in CI
([`.github/workflows/example-smoke.yml`](../../.github/workflows/example-smoke.yml))
so a regression in the demo compose file, the HTTP routes, or the sandbox runtime
fails a build instead of silently breaking a first-timer's first five minutes.

## Requirements

- **Docker** (Docker Desktop on macOS/Windows is fine) — the agent runs in a real
  per-conversation sandbox container.
- [`jq`](https://jqlang.github.io/jq/) and `curl`.

## Run it

```sh
# from the repo root
examples/hello-ironclaw/run.sh
```

The first run builds the sandbox image (`ironclaw-sandbox:latest`, ~1–2 min) and the
demo control-plane image, then brings the demo up, runs the check, and tears it down.

### Expected output

```text
==> building the sandbox image (ironclaw-sandbox:latest) — first run is ~1-2 min
==> starting the offline demo control-plane (docker compose -f docker-compose.demo.yml up)
==> waiting for the control-plane to be ready....
==> sending a chat message to 'mock-agent': "hello from hello-ironclaw 12345"
==> waiting for the agent's reply (real sandbox launch + encrypted queue round-trip)...
    agent replied: mock-agent received: hello from hello-ironclaw 12345

PASS ✅  IronClaw is working end-to-end with zero credentials.
        message -> engage -> sandboxed mock-agent -> encrypted queue -> reply.
```

The script exits `0` on `PASS` and non-zero (printing the last lines of the
control-plane logs) if the reply never arrives or doesn't match — so it is safe to
use as a CI gate or a local sanity check.

### Flags

| Flag | Effect |
|------|--------|
| _(none)_ | build → `up` → check → tear down |
| `--keep` | leave the demo control-plane running afterwards (chat with it in the browser at `http://127.0.0.1:8787/ui/`) |
| `--attach` | don't manage Docker; run the check against an already-running control-plane (set `IRONCLAW_ADDR` / `IRONCLAW_API_TOKEN`) |

`SKIP_BUILD=1` skips the sandbox image build when you already have
`ironclaw-sandbox:latest`.

## What this proves (and what it relaxes)

The reply is `mock-agent received: <your text>` — a deterministic echo from the
offline [`mock` provider](../../internal/sandbox/provider/mock.go), which makes **no
network call at all**. That the echo returns proves the load-bearing machinery is
intact end-to-end: a real sandbox container launched, the inbound/outbound
**SQLCipher-encrypted queues** handshook, and the delivery loop routed the reply
back out.

The demo deliberately relaxes the **sandbox seal** to run without gVisor on a stock
laptop: the control-plane runs as root, mounts the host Docker socket, and uses
**runc (shared host kernel)**, not gVisor. The mandatory approval gateway, the
encrypted per-session queues, and host-side model-credential custody are
**unchanged**. Don't run the demo compose outside a local trial — the hardened
production posture is the default `docker-compose.yml`
([deployment](../../docs/deployment.md)).

## Swap in a real model

The mock echo is what makes this credential-free. To get a real answer instead:

1. Set a provider key host-side on the control-plane, e.g. `ANTHROPIC_API_KEY`,
   `OPENAI_API_KEY`, or `OPENROUTER_API_KEY` (also works with a local
   Ollama/LM Studio/vLLM endpoint — see [providers](../../docs/quickstart.md)).
2. Point an agent group at that provider and send to its id instead of `mock-agent`:

   ```sh
   IRONCLAW_DEMO_AGENT=my-agent examples/hello-ironclaw/run.sh --attach
   ```

   (Use `--attach` against your own hardened control-plane rather than the demo
   compose, and the reply assertion will need adjusting — a real model won't echo.)

The supported production path (gVisor sandboxes + the host model-proxy) is in
[deployment](../../docs/deployment.md).

## See also

- The scenario recipes ([`scheduled-report/`](../scheduled-report/),
  [`webhook-responder/`](../webhook-responder/), [`slack-triage/`](../slack-triage/))
  show specific agent behaviours over the same offline `mock` provider.
- [`docs/quickstart.md`](../../docs/quickstart.md) — the manual browser/curl version
  of this flow.
