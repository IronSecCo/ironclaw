# Deploy IronClaw on Fly.io

One-command deploy of the IronClaw **control-plane** to [Fly.io](https://fly.io) using
[`fly.toml`](./fly.toml).

> **What this runs.** Fly deploys the trusted IronClaw **control-plane** (approval
> gateway, encrypted SQLCipher per-session queues, host-side model-credential custody,
> API + web console). It does **not** run agent sandboxes — a single Fly Machine has no
> gVisor (`runsc`) and no Docker socket, the same boundary as the
> [hardened Docker Compose path](../docker-compose.prod.yml). For **full agent
> isolation** use a gVisor host ([`deploy/install.sh`](../install.sh)) or a runsc k8s
> node ([`deploy/helm`](../helm/ironclaw)).

## Deploy

```sh
# From a checkout of this repo:
fly launch --copy-config --no-deploy            # creates the app from deploy/fly/fly.toml
fly volume create ironclaw_state --size 1       # 1 GB persistent SQLCipher state
fly secrets set IRONCLAW_API_TOKEN=$(openssl rand -hex 32) \
                ANTHROPIC_API_KEY=sk-ant-...     # secrets — never in fly.toml
fly deploy
fly open /ui/                                    # web console first-run screen
```

Pin the image by digest in `fly.toml` for a reproducible, cosign-verifiable deploy
(`ghcr.io/ironsecco/ironclaw-controlplane@sha256:<digest>`).

## Configure (env)

| Variable | Where | Notes |
|---|---|---|
| `IRONCLAW_API_TOKEN` | `fly secrets set` | admin/console bearer; leave unset to mint+print once to `fly logs` |
| `ANTHROPIC_API_KEY` | `fly secrets set` | (or `OPENAI_API_KEY` / `OPENROUTER_API_KEY`) held host-side |
| `IRONCLAW_DEV` | `[env]` in `fly.toml` | `0` = production; `1` only for a throwaway demo box |

> **First-boot volume ownership.** The image runs as the non-root uid `65532`; a fresh
> Fly volume mounts root-owned. If `fly logs` shows a permission error writing the state
> dir, run `fly ssh console -C "chown -R 65532:65532 /var/lib/ironclaw/state"` once and
> restart the Machine.

See [docs/deployment.md "Path D"](../../docs/deployment.md) for the full guide.
