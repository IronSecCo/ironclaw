# Try IronClaw with zero install (GitHub Codespaces)

This `.devcontainer/` lets a visitor go from **Open in Codespaces** to a **running,
sandboxed IronClaw agent** in the browser with no local install and no API key.

## What you get

Open the repo in a Codespace ([one-click button in the README](../README.md#want-to-see-it-work-first--no-api-key-no-signup)).
On first create it:

1. Installs `jq` + `curl` and a nested Docker daemon (`docker-in-docker`).
2. Runs [`examples/hello-ironclaw/run.sh --keep`](../examples/hello-ironclaw/README.md),
   which builds the sandbox image and the demo control-plane, then **asserts** a real
   chat -> per-session sandbox -> encrypted queue -> reply round-trip with the offline
   `mock` provider.
3. Leaves the demo running on port **8787**, which Codespaces forwards to your browser.

Open the forwarded **8787** port, add **`/ui/`** to the URL, pick **Mock Agent (offline)**
in the Chat tab, and say hi. If prompted for a token, paste **`ironclaw-demo`**.

Then watch containment for real:

```sh
examples/live-containment/run.sh
```

A fully jailbroken agent tries three escapes (network exfil, host-filesystem breakout,
host takeover via the Docker socket) and each is **denied**.

## Why docker-in-docker (not the host socket)

The demo launches each agent sandbox as a **sibling container** and binds the session's
encrypted-queue + key files at the **same absolute path** inside both the control-plane
and the sandbox (`IRONCLAW_DOCKER_BINDS` in
[`docker-compose.demo.yml`](../docker-compose.demo.yml)). That path alignment is
load-bearing for the queue handshake, and it only holds when the Docker daemon shares
this container's filesystem — which `docker-in-docker` provides. A mounted host Docker
socket (docker-outside-of-docker) would resolve `${PWD}` against a **different**
filesystem and break the handshake, so we deliberately do not use it here.

## Security note — this is the demo posture, not production

The Codespace runs the **same relaxed demo posture** as the local zero-credential demo:
no gVisor, `runc` (shared host kernel), the control-plane runs as root and reaches the
nested Docker daemon. What stays intact even here:

- **`network=none`** on every sandbox sibling (no egress; the
  [`live-containment`](../examples/live-containment/README.md) demo proves it),
- the **mandatory human-approval gateway**,
- the **encrypted per-session queues**, and
- **host-side model-credential custody** (no key ever enters a sandbox).

This is a trial box, not a deployment. The hardened production posture (gVisor +
`network=none` + host model-proxy) is the default
[`docker-compose.yml`](../docker-compose.yml) / [deployment guide](../docs/deployment.md).

## Known constraints

- **First create is ~2-3 min** while the sandbox and control-plane images build. This is
  prebuild-cacheable.
- **gVisor is not available in Codespaces**, so the sandbox seal is `runc`, exactly like
  the local laptop demo. `network=none`, the approval gateway, and the encrypted queues
  are unchanged, so the containment story (what the `live-containment` demo shows) still
  holds; only the kernel-isolation layer is relaxed. For a true gVisor seal, run on a
  Linux host per the [deployment guide](../docs/deployment.md).
- The demo control-plane binds **loopback only** (`127.0.0.1:8787`); Codespaces forwards
  that to you privately. No public `0.0.0.0` port is opened inside the box.

## Files

| File | Role |
|------|------|
| `devcontainer.json` | image, `docker-in-docker` + Go features, port 8787, lifecycle hooks |
| `post-create.sh` | the single post-create command: build + run the demo end to end, leave it up |
| `welcome.sh` | post-attach banner (how to open the console / run live-containment) |
