---
title: "How to harden an Envoy container: envoy:v1.32-latest scores 48/100 by default"
description: "envoyproxy/envoy:v1.32-latest defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a service proxy to its honest 89/100 grade B."
---

# How to harden an Envoy container (and is envoy:v1.32-latest safe for untrusted workloads?)

A service proxy carries every request between your services, which is exactly why its container
should be one of the tightest. A stock `docker run envoyproxy/envoy:v1.32-latest` is not: graded on
IronClaw's seven-dimension containment scale it scores **48 of 100, grade D (porous)**. Higher is
safer. A short list of runtime flags takes the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one a proxy needs by definition (it exists to accept
and forward traffic). Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `envoyproxy/envoy:v1.32-latest`, the
> same data behind its [isolation scorecard](../scores/envoy.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
envoyproxy/envoy:v1.32-latest`, four fail or warn:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The sharpest edges are **root**, the **retained capabilities**, and the **writable rootfs**: a
config-parser or filter CVE that lands code execution lands it as uid 0, with `CAP_NET_RAW` and
friends, on the process every request in your mesh passes through, and with a filesystem it can
rewrite to persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-envoy --fix` prints one remediation per failed dimension, then one hardened run.
For `envoyproxy/envoy:v1.32-latest`:

- **`--user 1000:1000`** (Non-root, +15): run the process as an unprivileged uid. Envoy binds
  unprivileged ports fine as a non-root user.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Envoy needs none of the defaults for a standard listen on an
  unprivileged port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only; Envoy
  reads a static config and writes nothing to root. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  proxy, it exists to accept and forward traffic. Any named or bridge network scores 4 of 15 (a WARN,
  not a fail): a connection path exists. This is the one dimension a proxy cannot max out. Contain it
  anyway: attach a user-defined network scoped to just its upstreams, with no default route out, so a
  compromised proxy cannot call arbitrary internet addresses.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name envoy envoyproxy/envoy:v1.32-latest

# After: 89/100, grade B (scoped private mesh network)
docker run -d --name envoy-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=mesh-internal \
  envoyproxy/envoy:v1.32-latest
```

Rescan: `ironctl scan envoy-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a proxy exists to forward traffic; `network=none` would score the last points but leave
nothing able to reach it. That is the honest ceiling for a proxy, and it is a clear step up from the
default D.

## Verify it on your own proxy

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-envoy
ironctl scan my-envoy --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Envoy in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [envoy:v1.32-latest isolation scorecard &rarr;](../scores/envoy.md): the full dimension breakdown.
- [Web servers and proxies, ranked by isolation &rarr;](../scores/collections/web-servers.md): how Envoy compares to nginx, Traefik, Kong, and the rest.
- [How to harden an nginx container &rarr;](harden-nginx-container-isolation.md): another proxy with the same honest network ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
