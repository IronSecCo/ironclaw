# Deploy IronClaw on Railway

One-click-ish deploy of the IronClaw **control-plane** to [Railway](https://railway.com).

[![Deploy on Railway](https://railway.com/button.svg)](https://railway.com/new)

> **What this runs.** Railway deploys the trusted IronClaw **control-plane** — the
> mandatory approval gateway, the encrypted SQLCipher per-session queues, host-side
> model-credential custody, and the API + web console. It does **not** run agent
> sandboxes: a single Railway container has no gVisor (`runsc`) and no Docker socket,
> so the agent execution sandbox cannot launch here — the same boundary as the
> [hardened Docker Compose path](../docker-compose.prod.yml). For **full agent
> isolation** use a gVisor host ([`deploy/install.sh`](../install.sh)) or a runsc k8s
> node ([`deploy/helm`](../helm/ironclaw)). This path gets you a hardened control-plane
> and the console first-run screen with zero local tooling.

## Deploy

You have two source options; both reach the same running control-plane.

**A. Prebuilt image (no build, fastest).** In the Railway dashboard:
*New Project → Deploy a Docker Image →* `ghcr.io/ironsecco/ironclaw-controlplane:latest`
(pin a `@sha256:<digest>` for a reproducible, cosign-verifiable deploy).

**B. Build from this repo.** *New Project → Deploy from GitHub repo →* select your fork.
Set the service **Config-as-code** path to `deploy/railway/railway.json` (root directory
= repo root). Railway builds the control-plane from
[`container/controlplane.Dockerfile`](../../container/controlplane.Dockerfile) (CGO +
SQLCipher).

## Configure (required)

1. **Networking → target port `8787`.** The control-plane listens on `8787`; set the
   service's exposed/target port to `8787` so Railway's edge proxies `443 → 8787`.
2. **Variables:**

   | Variable | Value | Notes |
   |---|---|---|
   | `IRONCLAW_API_ADDR` | `0.0.0.0:8787` | bind address inside the container |
   | `IRONCLAW_LOG_FORMAT` | `json` | structured logs |
   | `IRONCLAW_STATE_DIR` | `/var/lib/ironclaw/state` | durable state path |
   | `IRONCLAW_DEV` | `0` | `0` = production; `1` only for a throwaway demo box |
   | `IRONCLAW_API_TOKEN` | `openssl rand -hex 32` | admin/console bearer; leave unset to mint+print once to the logs |
   | `ANTHROPIC_API_KEY` | `sk-ant-…` | (or `OPENAI_API_KEY` / `OPENROUTER_API_KEY`) held host-side, never in a sandbox |

3. **Volume.** Attach a Railway **Volume** mounted at `/var/lib/ironclaw/state` so the
   encrypted SQLCipher queues, the gateway change store, the audit log, sealed keys, and
   the minted admin token survive restarts. Losing this loses the token and all state.

   > **First-boot ownership.** The image runs as the non-root uid `65532`; a fresh
   > Railway volume mounts root-owned. If the first boot logs a permission error writing
   > the state dir, open the service shell and run
   > `chown -R 65532:65532 /var/lib/ironclaw/state`, then redeploy.

## Verify

Open the service URL at `/ui/` — you should land on the IronClaw web console first-run
screen. `GET /healthz` returns `ok` once the control-plane is up.

## One-click template (follow-up)

A curated, hosted Railway **template** (volume + env pre-wired so the button is truly
one-click) must be registered in a Railway account; that registration and end-to-end
verification are tracked as a QA follow-up to IRO-229. Until then, the button above opens
Railway's New-Project flow and you complete the **Configure** steps above.
