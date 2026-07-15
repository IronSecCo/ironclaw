# Deploy IronClaw on Render

One-click deploy of the IronClaw **control-plane** to [Render](https://render.com) using
the [`render.yaml`](./render.yaml) Blueprint.

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/IronSecCo/ironclaw&path=deploy/render/render.yaml)

> **What this runs.** Render deploys the trusted IronClaw **control-plane** (approval
> gateway, encrypted SQLCipher per-session queues, host-side model-credential custody,
> API + web console). It does **not** run agent sandboxes — a single Render instance has
> no gVisor (`runsc`) and no Docker socket, the same boundary as the
> [hardened Docker Compose path](../docker-compose.prod.yml). For **full agent
> isolation** use a gVisor host ([`deploy/install.sh`](../install.sh)) or a runsc k8s
> node ([`deploy/helm`](../helm/ironclaw)).

## Deploy

Click the button (it reads `deploy/render/render.yaml` from your fork), or in the Render
dashboard: **New → Blueprint →** point at this repo. Render provisions the service + a
1 GB persistent disk and prompts for the secret env vars.

The Blueprint pulls the prebuilt image `ghcr.io/ironsecco/ironclaw-controlplane:latest`
(pin a `@sha256:<digest>` for a reproducible, cosign-verifiable deploy).

> **Persistent disks require a paid instance type** (not available on the free tier). The
> Blueprint defaults to the `starter` plan for this reason.

## Configure (env)

Render prompts for these at deploy time (`sync: false`, never stored in Git):

| Variable | Notes |
|---|---|
| `IRONCLAW_API_TOKEN` | admin/console bearer; leave unset to mint+print once to the Render log stream |
| `ANTHROPIC_API_KEY` | (or `OPENAI_API_KEY` / `OPENROUTER_API_KEY`) held host-side, never in a sandbox |

`IRONCLAW_DEV` defaults to `0` (production). Set to `1` only for a throwaway demo box.

> **First-boot disk ownership.** The image runs as the non-root uid `65532`; a fresh
> Render disk mounts root-owned. If the first boot logs a permission error writing the
> state dir, open a shell on the instance and run
> `chown -R 65532:65532 /var/lib/ironclaw/state`, then redeploy.

See [docs/deployment.md "Path D"](../../docs/deployment.md) for the full guide.
