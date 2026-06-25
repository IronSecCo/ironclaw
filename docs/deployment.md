# Production deployment & self-hosting

The [quickstart](quickstart.md) gets you a working agent in minutes. This guide takes
you the rest of the way: a **durable, secured, self-hosted** IronClaw you can put real
traffic and real credentials in front of.

Read [Security & Trust](security.md) first — it explains *why* the boundaries below
exist. This page is the *how*.

## Pick a deployment path

There are three supported ways to run the control-plane in production. They differ in
**how much sandbox isolation you get**, because that depends on the host kernel.

| | Bare-host (systemd / launchd) | Hardened Docker Compose | Kubernetes (Helm) |
|---|---|---|---|
| Installer | [`deploy/install.sh`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/install.sh) | [`deploy/docker-compose.prod.yml`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/docker-compose.prod.yml) | [`deploy/helm/ironclaw`](https://github.com/IronSecCo/ironclaw/tree/main/deploy/helm/ironclaw) |
| Sandbox isolation | **Full gVisor (`runsc`)** — `network=none`, all caps dropped, read-only rootfs, user-namespaced | Control-plane only; sandboxes need a runsc host | Control-plane only; full gVisor needs a runsc-capable node |
| Best for | The real production posture: agents executing tool calls under gVisor | Hardened control-plane, gateway, console, channels on any Docker host | Self-hosters who run their infra on k8s |
| Host requirements | Linux + containerd + gVisor (or macOS/launchd for the control-plane) | Any host with Docker + Compose | A Kubernetes cluster (1.25+) + Helm |

!!! important "Where the sandboxes run"
    IronClaw's security value is that **agent code runs inside a gVisor sandbox** with
    no network. gVisor sandboxes are launched by the control-plane directly on the host
    via containerd — they are **not** Docker Compose services and never share the
    control-plane's network. Running gVisor sandboxes from *inside* the Compose
    control-plane container needs a containerd+runsc host and more privilege than the
    hardened Compose grants. So:

    - For **full sandbox isolation**, run the control-plane on a **gVisor-capable host**
      with the systemd path below.
    - The **hardened Compose** runs and locks down the *trusted control-plane itself*
      (the mandatory approval gateway, the encrypted per-session queues, host-side
      credential custody, the console and channels). On a host that also has runsc, the
      sandboxes it launches still get the full gVisor seal.

---

## Path A — Bare-host install (full gVisor isolation)

This is the production posture from [`deploy/README.md`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/README.md).
The control-plane runs as a host service; every agent sandbox runs under gVisor.

### 1. Provision the host

Install the three external dependencies that are intentionally **not** vendored:

- **containerd + gVisor (`runsc`)** — the `io.containerd.runsc.v1` runtime. Every
  sandbox runs under it with `network=none`, all caps dropped, `no_new_privs`, a
  user namespace, and a read-only rootfs.
- **Tailscale (recommended)** — the control-plane API has **no public port**; bind it
  to the tailnet IP and reach it over the mesh.
- A C toolchain + Go 1.23+ if you build from source (the encrypted-SQLite binding is cgo).

### 2. Install + enable the service

```sh
sudo deploy/install.sh
```

The installer is idempotent: it builds `ironclaw-controlplane` and `ironctl`, installs
them under `/usr/local/bin`, provisions `/etc/ironclaw/ironclaw.env` (mode `0600`,
**minting an API token on first run and preserving it on re-install**), and installs +
enables the host service — [`ironclaw.service`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/ironclaw.service)
(systemd) or [`io.ironclaw.controlplane.plist`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/io.ironclaw.controlplane.plist)
(launchd). Tunables (`IRONCLAW_API_ADDR`, `IRONCLAW_STATE_DIR`, `ANTHROPIC_API_KEY`, …)
go in that env file; the unit files stay static and hold no secrets.

### 3. Pin and pre-pull the sandbox image

The sandbox is a separate, pinned image. Build it and pin the digest:

```sh
container/build.sh ghcr.io/your-org/ironclaw-sandbox:1.0   # prints the RepoDigest to pin
ctr -n ironclaw images pull ghcr.io/your-org/ironclaw-sandbox@sha256:<digest>
```

Pin that digest in `IRONCLAW_SANDBOX_IMAGE` and the provisioner trust policy
(`PinnedDigestPolicy`). The image is pulled and unpacked **once** host-side (the sandbox
is `network=none`) into a shared read-only rootfs reused across sessions.

### 4. Per-group persistent storage (optional)

Each agent group can be given durable storage. Lay out the host dirs and chown them to
the sandbox's mapped non-root uid (`65532`):

```sh
install -d -m 0700 -o 65532 -g 65532 \
  /var/lib/ironclaw/groups/<group>/workspace \
  /var/lib/ironclaw/groups/<group>/memory
install -d -m 0755 /var/lib/ironclaw/shared    # global, read-only to sandboxes
```

`/workspace` and `/memory` mount `rw` (with `nosuid,nodev,noexec`); `/shared` is `ro`.

---

## Path B — Hardened Docker Compose

Use [`deploy/docker-compose.prod.yml`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/docker-compose.prod.yml)
to run a locked-down control-plane behind a TLS-terminating reverse proxy on any Docker
host. It differs from the one-command root [`docker-compose.yml`](https://github.com/IronSecCo/ironclaw/blob/main/docker-compose.yml)
(which publishes the API on host loopback for evaluation) in three ways:

- the control-plane is **not published to the host** — only Caddy is, and it proxies
  over a private network;
- the container is **locked down** (read-only rootfs, all caps dropped,
  `no-new-privileges`, pid + CPU + memory ceilings, non-root uid `65532`);
- the image is **pinned by digest**, not `:latest`.

### 1. Configure secrets

```sh
cp deploy/.env.prod.example deploy/.env.prod
chmod 600 deploy/.env.prod
$EDITOR deploy/.env.prod
```

`deploy/.env.prod` is **git-ignored** and never baked into the image — secrets arrive
at runtime via the env-file only. The variable names mirror
[`.env.example`](https://github.com/IronSecCo/ironclaw/blob/main/.env.example) (the
daemon reads the same env): `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` /
`OPENROUTER_API_KEY` for the host-side model credential, `SLACK_BOT_TOKEN` /
`DISCORD_BOT_TOKEN` / `TELEGRAM_BOT_TOKEN` / `IRONCLAW_TEAMS_WEBHOOK_URL` /
`IRONCLAW_SIGNAL_*` for channels, and `IRONCLAW_API_TOKEN` for the admin bearer.

!!! tip "Set the admin token yourself"
    In production set `IRONCLAW_API_TOKEN` (`openssl rand -hex 32`) from your secrets
    manager. If you leave it blank the control-plane mints one and prints it **once**
    in the logs — there is no recovery. Either way the model credential is held
    host-side and **never enters a sandbox**.

### 2. Pin the image by digest

```sh
docker buildx imagetools inspect ghcr.io/ironsecco/ironclaw-controlplane:v0.1.0
```

Put the resolved `ghcr.io/ironsecco/ironclaw-controlplane@sha256:<digest>` in
`IRONCLAW_IMAGE`. The image is published on every release, signed with cosign, and
attested on the index digest — verify before pinning:

```sh
cosign verify ghcr.io/ironsecco/ironclaw-controlplane@sha256:<digest> \
  --certificate-identity-regexp '^https://github.com/IronSecCo/ironclaw' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

### 3. Bring it up

```sh
docker compose -f deploy/docker-compose.prod.yml up -d
docker compose -f deploy/docker-compose.prod.yml logs -f controlplane
```

The control-plane comes up `healthy` (the `/healthz` probe), locked down: read-only
rootfs, `CapDrop=ALL`, `no-new-privileges`, non-root, with a tmpfs for the model-proxy
socket. Caddy fronts it on `:443`/`:80`.

---

## Path C — Kubernetes (Helm)

For self-hosters who already run on Kubernetes, the
[`deploy/helm/ironclaw`](https://github.com/IronSecCo/ironclaw/tree/main/deploy/helm/ironclaw)
chart deploys the control-plane with the **same hardening as the Compose path**:
non-root uid `65532`, read-only rootfs, **all capabilities dropped**,
`allowPrivilegeEscalation: false` (`no-new-privileges`), `RuntimeDefault` seccomp,
resource ceilings, and durable encrypted (SQLCipher) state on a PersistentVolume. Like
Compose, this runs and locks down the **trusted control-plane only** — agent sandboxes
run under gVisor on the host and are never Kubernetes Pods (see the box above); for full
sandbox isolation, schedule the control-plane onto a **runsc-capable node**.

### 1. Install

```sh
helm install ironclaw ./deploy/helm/ironclaw \
  --namespace ironclaw --create-namespace \
  --set secrets.apiToken="$(openssl rand -hex 32)" \
  --set secrets.anthropicApiKey="sk-ant-…"
```

The control-plane is `ClusterIP` only (not exposed outside the cluster). Try it with a
port-forward:

```sh
kubectl -n ironclaw port-forward svc/ironclaw 8787:8787
curl -fsS http://127.0.0.1:8787/healthz
```

!!! tip "Bring your own Secret"
    Putting secrets in `--set` writes them into the Helm release. In production, create a
    Secret (keys = daemon env names like `IRONCLAW_API_TOKEN`, `ANTHROPIC_API_KEY`) from
    your secrets manager and pass `--set secrets.existingSecret=<name>`. If
    `IRONCLAW_API_TOKEN` is unset the control-plane mints one and prints it **once** in
    the Pod logs (`kubectl logs deploy/ironclaw`) — no recovery.

### 2. Pin a verified image digest

Mirror the Compose digest-pinning. Resolve and `cosign verify` the digest (as in Path B),
then:

```sh
helm upgrade --install ironclaw ./deploy/helm/ironclaw -n ironclaw \
  --set image.digest=sha256:<digest>
```

### 3. TLS termination via Ingress

The control-plane serves **plain HTTP** — terminate TLS at the Ingress (cert-manager or
your controller), exactly as Caddy/nginx do for the other paths:

```sh
helm upgrade --install ironclaw ./deploy/helm/ironclaw -n ironclaw \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=ironclaw.example.com \
  --set ingress.tls[0].secretName=ironclaw-tls \
  --set ingress.tls[0].hosts[0]=ironclaw.example.com
```

### Persistence, PID limits & network policy

- **State** lives on the `state` PVC (`persistence.size`, default `1Gi`; or
  `persistence.existingClaim`). It holds the encryption keys and admin token — back it up
  **encrypted** (see [Backups & restore](#backups-restore)).
- **PID ceiling:** Kubernetes has no per-container `pids` field (the Compose path sets
  one), so enforce it at the node (`--pod-max-pids`) or with a namespace `LimitRange`.
- **NetworkPolicy:** set `networkPolicy.enabled=true` (with an enforcing CNI) to restrict
  ingress to the Pod, mirroring the Compose "private network" posture.

Full value reference and validation commands are in the chart
[README](https://github.com/IronSecCo/ironclaw/blob/main/deploy/helm/ironclaw/README.md).
The chart is validated with `helm lint` and `helm template` rendering against the
Kubernetes schema; a live-cluster apply is left to the operator.

---

## Reverse proxy & TLS termination

The control-plane serves **plain HTTP** — its security boundary is the network (mesh or
private bridge) plus the bearer token, not its own TLS. Terminate TLS at the edge.

The Compose ships [`deploy/Caddyfile`](https://github.com/IronSecCo/ironclaw/blob/main/deploy/Caddyfile):
set `IRONCLAW_DOMAIN` to your FQDN and Caddy provisions a Let's Encrypt certificate
automatically (or a local self-signed CA for a bare host/IP). It also sets HSTS and
basic hardening headers and can hide `/metrics` from the public edge.

Prefer nginx? Terminate TLS and proxy to the control-plane:

```nginx
server {
    listen 443 ssl;
    server_name ironclaw.example.com;
    ssl_certificate     /etc/letsencrypt/live/ironclaw.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ironclaw.example.com/privkey.pem;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://127.0.0.1:8787;   # or the control-plane's private address
        proxy_set_header Host              $host;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Bind-address guidance

- **Never expose `8787` to the internet raw.** The default bind is `127.0.0.1:8787`.
- **Best:** bind to a **Tailscale tailnet IP** (`--api-addr "$(tailscale ip -4):8787"`)
  so there is no public port at all, and drop inbound to the API port on every
  interface except the tailnet one with a host firewall.
- **Compose:** the hardened file publishes only the reverse proxy; the control-plane
  stays on the private bridge. Restrict the proxy further by binding it to a specific
  host/tailnet IP (e.g. `"${TAILNET_IP}:443:443"`).

---

## Backups & restore

All durable state lives in **one** place — the state directory (`/var/lib/ironclaw` on a
bare host, the `ironclaw-state` volume in Compose). It holds the **encrypted (SQLCipher)
per-session queues**, the gateway change store, the append-only audit log, the sealed
per-session keys + file master key, and the minted admin token. Losing it loses the
admin token and all session state.

**Back up** (stop briefly or snapshot for a consistent copy):

```sh
# Compose: archive the named volume
docker run --rm -v ironclaw_ironclaw-state:/state -v "$PWD":/backup alpine \
  tar czf /backup/ironclaw-state-$(date +%F).tgz -C /state .

# Bare host
sudo systemctl stop ironclaw
sudo tar czf ironclaw-state-$(date +%F).tgz -C /var/lib/ironclaw .
sudo systemctl start ironclaw
```

The archive contains the **encryption keys**, so it is as sensitive as the live state —
encrypt it at rest and store it in your secrets/backup vault, not next to the host.

**Restore:** stop the stack, extract the archive back into the state dir/volume
(preserving ownership `65532:65532` and `0700` perms), and start it. The admin token and
all queues come back intact.

---

## Upgrades

IronClaw is alpha and **not** bound by backward compatibility yet — read the release
notes before upgrading. The state directory is the durable contract; the binary/image is
replaceable.

```sh
# Compose: resolve + verify the new digest, update IRONCLAW_IMAGE in deploy/.env.prod, then
docker compose -f deploy/docker-compose.prod.yml pull
docker compose -f deploy/docker-compose.prod.yml up -d   # recreates with the new image

# Bare host: pull the new source/release and re-run the idempotent installer
git pull && sudo deploy/install.sh   # preserves the env file + token, restarts the service
```

Take a fresh backup before upgrading. Roll back by pinning the previous digest (Compose)
or re-installing the previous tag, then restoring the pre-upgrade backup if state changed.

---

## Observability

- **Liveness / readiness:** `GET /healthz` and `GET /readyz` are **unauthenticated** by
  design (so probes need no credential) and exempt from rate limiting. The Compose
  healthcheck uses `/healthz`.
- **Metrics:** `GET /metrics` exposes Prometheus counters and histograms (gateway
  decisions, model-proxy egress + redactions, queue activity). It is served on the API
  address and is **bearer-gated** — scrape it with the admin token, ideally over the
  private network:

  ```yaml
  scrape_configs:
    - job_name: ironclaw
      metrics_path: /metrics
      authorization:
        credentials: <IRONCLAW_API_TOKEN>
      static_configs:
        - targets: ["controlplane:8787"]
  ```

  Or keep it off the public edge entirely (the Caddyfile shows how) and scrape the
  private address.
- **Logs:** set `IRONCLAW_LOG_FORMAT=json` for shippers (the default in the Compose).
  The logger **redacts secrets** — model keys and tokens never appear in logs.
- **Audit:** the gateway writes an append-only audit log under the state dir; surface it
  with `ironctl … audit` or the console's audit view.

---

## Hardening checklist

- [ ] Control-plane **not** published to the internet — mesh/tailnet bind or private
      network behind a reverse proxy.
- [ ] TLS terminated at the edge (Caddy/nginx); HSTS on.
- [ ] `IRONCLAW_API_TOKEN` set from a secrets manager (not minted-and-printed).
- [ ] Image **pinned by digest** and cosign-verified.
- [ ] Secrets only in the `0600` env-file — never baked into images, never committed.
- [ ] `IRONCLAW_DEV=0` (the dev seed is unauthenticated — local only).
- [ ] State dir/volume backed up **encrypted**, restore tested.
- [ ] Sandboxes run under **gVisor** on the host (Path A) — verify `runtime: runsc` in
      the startup log, not `docker`/`runc`.
- [ ] Metrics scraped over the private network with the bearer token.
- [ ] Resource + pid ceilings set (the hardened Compose sets these).

See [Security & Trust](security.md) and the [threat model](threat-model.md) for the
boundaries each of these protects.
