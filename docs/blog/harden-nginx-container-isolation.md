---
title: "How to harden an nginx container: nginx:1.27-alpine scores 48/100 by default"
description: "nginx:1.27-alpine defaults score 48/100 (grade D): root, full caps, open egress. The ironctl scan --fix flags that harden a proxy to its honest 89/100 grade B."
---

# How to harden an nginx container (nginx:1.27-alpine scores 48/100 by default)

nginx sits at the edge, which is exactly why its container should be the tightest one you run.
A stock `docker run nginx:1.27-alpine` is not that. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **48 of 100, grade D (porous)**. Higher is
safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an A,
and the one dimension it cannot reach is the one a reverse proxy needs by definition (egress).
Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `nginx:1.27-alpine`, the same
> data behind its [isolation scorecard](../scores/nginx.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run nginx:1.27-alpine`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | master process runs as root (uid 0); an escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

For an internet-facing proxy, the **root master process** and **retained capabilities** are the
sharp edges: a request-smuggling or module CVE that lands code execution lands it as root with
`CAP_NET_RAW` and friends. And while a reverse proxy must reach its upstreams, it almost never
needs arbitrary internet **egress**, so the open `bridge` default is attack surface: a
compromised nginx should not be able to call out anywhere it likes.

## Harden it: the exact `--fix` remediation

`ironctl scan my-nginx --fix` prints one remediation per failed dimension, then one hardened
run. For `nginx:1.27-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every capability. If you keep the
  stock image and bind port 80 inside the container you must add `--cap-add=NET_BIND_SERVICE`
  back, which costs 4 of the 20 points. Better: use the `nginx-unprivileged` image and publish a
  high port, so you drop all capabilities and add none back for the full +16.
- **`--user 65532:65532`** (Non-root user, +15): run the workers and the master as non-root.
  The `nginxinc/nginx-unprivileged` image is built for exactly this; a fixed non-root uid is the
  auditable choice.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  proxy, it needs to reach upstreams. Any named or bridge network scores 4 of 15 (a WARN, not a
  fail): egress is possible. This is the one dimension a reverse proxy cannot max out. Contain it
  anyway: attach a user-defined network scoped to just the upstreams, with no default route out.
- **`--read-only --tmpfs /tmp --tmpfs /var/cache/nginx --tmpfs /var/run`** (Read-only rootfs,
  +10): nginx only needs to write its cache and pid; mount those as `tmpfs` and make the rest of
  the root filesystem read-only. Removes the persistence surface.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name nginx -p 8080:80 nginx:1.27-alpine

# After: 89/100, grade B (using the unprivileged image, high port inside)
docker run -d --name nginx-hardened -p 8080:8080 \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only \
  --tmpfs /tmp --tmpfs /var/cache/nginx --tmpfs /var/run \
  nginxinc/nginx-unprivileged:1.27-alpine
```

Rescan: `ironctl scan nginx-hardened` reports `89/100 grade B`. A **41-point swing** with no
custom image build, just the right flags and the unprivileged variant. The only dimension still
short of full marks is egress (4 of 15), because a reverse proxy has to reach its upstreams;
`network=none` would score the last points but break the proxy. That is the honest ceiling for
an internet-facing nginx, and it is a long way from the default D.

## Verify it on your own edge

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-nginx
ironctl scan my-nginx --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can
grade the nginx in your real deployment, not just a bare `docker run`.

## Keep going

- [nginx:1.27-alpine isolation scorecard &rarr;](../scores/nginx.md): the full dimension breakdown.
- [Web servers, ranked by isolation &rarr;](../scores/collections/web-servers.md): how nginx compares to httpd, Caddy, Traefik, and the rest.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
