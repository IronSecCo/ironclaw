---
title: "How to harden a Traefik container: traefik:v3.2 scores 48/100 by default"
description: "traefik:v3.2 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take an edge reverse proxy to its honest 89/100 grade B."
---

# How to harden a Traefik container (and is traefik:v3.2 safe at your edge?)

Traefik sits at the edge: it terminates TLS, routes every inbound request, and often reads the
Docker or Kubernetes API to discover backends. That makes it both the first thing an attacker
reaches and one of the most privileged processes in a stack, and a stock `docker run traefik:v3.2`
is not the boundary that role deserves. Graded on IronClaw's seven-dimension containment scale, the
default configuration scores **48 of 100, grade D (porous)**. Higher is safer. A few runtime flags
take the same image to **89 of 100, grade B**, one point off an A, and the one dimension it cannot
reach is the one a reverse proxy needs by definition: it exists to accept and forward traffic. Here
are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `traefik:v3.2`, the same data behind
> its [isolation scorecard](../scores/traefik.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
traefik:v3.2`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For an edge proxy, the one that should worry you most is **root**. Traefik parses attacker-controlled
bytes on every request; a routing or TLS CVE that lands code execution in a root container escapes as
root on the host, next to the certificates and the API credentials it uses for service discovery. The
full capability set widens that foothold and the writable rootfs makes it durable. A common Traefik
pattern is mounting `docker.sock` for discovery; do not, and note that this scan would fail the
docker.sock dimension if you did (use the Docker API over a scoped socket-proxy or the
Kubernetes provider instead).

## Harden it: the exact `--fix` remediation

`ironctl scan my-traefik --fix` prints one remediation per failed dimension, then one hardened run.
For `traefik:v3.2`:

- **`--user 65532:65532`** (Non-root user, +15): pin a non-root uid so an escape does not begin as
  host uid 0. Traefik binds 80 and 443 by default; either map them to high host ports, or grant just
  `CAP_NET_BIND_SERVICE` (see below) so an unprivileged uid can bind the low ports.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability, then add back only
  `CAP_NET_BIND_SERVICE` if you must bind 80/443 directly. The scan below reflects dropping the full
  set with ports mapped high; adding one cap back costs 4 points (a WARN, still grade B territory).
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only. Traefik
  runs happily read-only; mount an ACME storage volume if you use its built-in Let's Encrypt.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  proxy, it exists to forward traffic. Any named or bridge network scores 4 of 15 (a WARN, not a
  fail): a connection path exists. Contain it anyway: put Traefik on a user-defined network scoped to
  just the backends it fronts, with no default route out, so a compromised proxy cannot call
  arbitrary internet addresses.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name traefik traefik:v3.2

# After: 89/100, grade B (scoped private network for its backends)
docker run -d --name traefik-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -p 8080:8080 -p 8443:8443 \
  --network=edge-internal \
  traefik:v3.2
```

Rescan: `ironctl scan traefik-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a reverse proxy exists to accept connections; `network=none` would score the last points
but leave nothing able to reach or be reached. That is the honest ceiling for a proxy, and it is a
long way from the default D.

## Verify it on your own Traefik

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-traefik
ironctl scan my-traefik --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Traefik in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [traefik:v3.2 isolation scorecard &rarr;](../scores/traefik.md): the full dimension breakdown.
- [How to harden an nginx container &rarr;](harden-nginx-container-isolation.md): the other popular edge proxy, with the same honest ceiling.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
