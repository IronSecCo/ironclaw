# IronClaw control-plane Helm chart

Deploy the **hardened IronClaw control-plane** on Kubernetes. This chart mirrors the
security posture of the [hardened Docker Compose](../../docker-compose.prod.yml):
non-root uid `65532`, read-only rootfs, **all Linux capabilities dropped**,
`no-new-privileges` (`allowPrivilegeEscalation: false`), `RuntimeDefault` seccomp,
resource ceilings, and durable encrypted (SQLCipher) state on a PersistentVolume.

> **Where the sandboxes run.** This chart deploys the **trusted control-plane only** —
> the mandatory approval gateway, the encrypted per-session queues, host-side model
> credential custody, the web console and channel adapters. IronClaw's agent sandboxes
> run under **gVisor (`runsc`)**, launched by the control-plane directly on a
> runsc-capable host; they are **not** Kubernetes Pods and never share this Pod's
> network. For full sandbox isolation the control-plane must run on a gVisor-capable
> node. See [docs/deployment.md](../../../docs/deployment.md).

## Prerequisites

- Kubernetes 1.25+ and Helm 3.8+ (validated with Helm 4).
- A default StorageClass (or set `persistence.storageClass` / `persistence.existingClaim`).
- An Ingress controller + cert-manager if you want TLS termination at the edge.

## Install

```sh
# From a checkout of the repo:
helm install ironclaw ./deploy/helm/ironclaw \
  --namespace ironclaw --create-namespace \
  --set secrets.apiToken="$(openssl rand -hex 32)" \
  --set secrets.anthropicApiKey="sk-ant-…"
```

Then reach it (ClusterIP by default — not exposed outside the cluster):

```sh
kubectl -n ironclaw port-forward svc/ironclaw 8787:8787
curl -fsS http://127.0.0.1:8787/healthz
```

### Production: pin a verified image digest

Don't ship a floating tag. Resolve and **cosign-verify** the digest, then pin it:

```sh
docker buildx imagetools inspect ghcr.io/ironsecco/ironclaw-controlplane:v0.1.0
cosign verify ghcr.io/ironsecco/ironclaw-controlplane@sha256:<digest> \
  --certificate-identity-regexp '^https://github.com/IronSecCo/ironclaw' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

helm upgrade --install ironclaw ./deploy/helm/ironclaw -n ironclaw \
  --set image.digest=sha256:<digest>
```

### Production: secrets from your own Secret

Avoid putting secrets in Helm values. Create a Secret (or have your secrets operator
sync one) whose keys are the daemon env names, then point the chart at it:

```sh
kubectl -n ironclaw create secret generic ironclaw-creds \
  --from-literal=IRONCLAW_API_TOKEN="$(openssl rand -hex 32)" \
  --from-literal=ANTHROPIC_API_KEY="sk-ant-…"

helm upgrade --install ironclaw ./deploy/helm/ironclaw -n ironclaw \
  --set secrets.existingSecret=ironclaw-creds
```

If you leave `IRONCLAW_API_TOKEN` unset, the control-plane **mints one on first run and
prints it once** in the Pod logs (no recovery) — grab it from
`kubectl logs deploy/ironclaw` and set it explicitly.

### Expose with TLS at the Ingress

The control-plane serves **plain HTTP** — terminate TLS at the Ingress.

```sh
helm upgrade --install ironclaw ./deploy/helm/ironclaw -n ironclaw \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=ironclaw.example.com \
  --set 'ingress.annotations.cert-manager\.io/cluster-issuer=letsencrypt-prod' \
  --set ingress.tls[0].secretName=ironclaw-tls \
  --set ingress.tls[0].hosts[0]=ironclaw.example.com
```

## Key values

| Key | Default | Notes |
|---|---|---|
| `image.repository` | `ghcr.io/ironsecco/ironclaw-controlplane` | |
| `image.tag` | `""` → `.Chart.appVersion` | |
| `image.digest` | `""` | Pin a cosign-verified `sha256:…` in production (wins over `tag`). |
| `replicaCount` | `1` | Single-writer; do not scale active-active. |
| `config.dev` | `"0"` | **MUST stay `0`** in production (`1` seeds an unauthenticated owner). |
| `config.logFormat` | `"json"` | Logger redacts secrets regardless. |
| `secrets.existingSecret` | `""` | Prefer this over inline `secrets.*`. |
| `secrets.apiToken` | `""` | Admin bearer; minted+printed once if empty. |
| `persistence.enabled` | `true` | Durable encrypted state; back it up **encrypted**. |
| `persistence.size` | `1Gi` | |
| `persistence.existingClaim` | `""` | Bring your own PVC. |
| `resources.limits` | `cpu: 2, memory: 1Gi` | |
| `ingress.enabled` | `false` | Terminate TLS here. |
| `networkPolicy.enabled` | `false` | Restrict ingress to the Pod (needs an enforcing CNI). |
| `service.type` / `service.port` | `ClusterIP` / `8787` | |

The hardening `securityContext` / `podSecurityContext` blocks are intentionally locked
down — **do not relax them**. They are part of IronClaw's security promise.

## Persistence & backups

All durable state — encrypted SQLCipher queues, gateway change store, append-only audit
log, **sealed encryption keys**, and the minted admin token — lives on the `state` PVC.
The volume holds the encryption keys, so **back it up encrypted** and test restore. See
the Backups & restore section of [docs/deployment.md](../../../docs/deployment.md).

## Pod PID limits

Kubernetes has no per-container `pids` field (unlike the Compose `pids` limit). Enforce a
PID ceiling at the node (kubelet `--pod-max-pids`) or with a namespace `LimitRange`.

## Validate locally (no cluster needed)

```sh
helm lint deploy/helm/ironclaw
helm template ironclaw deploy/helm/ironclaw | kubeconform -strict -summary -
```
