---
title: "How to harden a Kong container: kong:3.8 scores 63/100 by default"
description: "kong:3.8 defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take an API gateway to its honest 89/100 grade B."
---

# How to harden a Kong container (and is kong:3.8 safe for untrusted workloads?)

An API gateway sits at the edge, in front of everything, which is exactly why its container should be
one of the tightest. A stock `docker run kong:3.8` is closer than most, it already runs as a non-root
user, but graded on IronClaw's seven-dimension containment scale it still scores only **63 of 100,
grade C (partial)**. Higher is safer. A couple of runtime flags take the same image to **89 of 100,
grade B**, one point off an A, and the one dimension it cannot reach is the one a gateway needs by
definition (clients and upstreams must connect through it). Here are the exact gaps and fixes from
the scan data.

> Every number here comes from a read-only `docker inspect` of `kong:3.8`, the same data behind its
> [isolation scorecard](../scores/kong.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run kong:3.8`,
three fail or warn (and, notably, non-root already passes):

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as kong (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Kong's image already did the hardest part, it drops root. The sharpest edges that remain are the
**retained capabilities** and the **writable rootfs**: a plugin or Lua-runtime CVE that lands code
execution lands it with `CAP_NET_RAW` and friends, on the process every request in your stack passes
through, and with a filesystem it can rewrite to persist.

## Harden it: the exact `--fix` remediation

`ironctl scan my-kong --fix` prints one remediation per failed dimension, then one hardened run.
For `kong:3.8`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; add back only what
  the workload provably needs. Kong needs none of the defaults for a standard listen on an
  unprivileged port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount Kong's prefix path (`/usr/local/kong`) as an explicit writable volume. Removes the
  persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  gateway, clients and upstreams must reach it. Any named or bridge network scores 4 of 15 (a WARN,
  not a fail): a connection path exists. This is the one dimension a gateway cannot max out. Contain
  it anyway: attach a user-defined network scoped to just its upstreams, with no default route out,
  so a compromised gateway cannot call arbitrary internet addresses.

No `--user` flag is needed: the image already runs as `kong`.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name kong kong:3.8

# After: 89/100, grade B (scoped private network for clients and upstreams)
docker run -d --name kong-hardened \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v kong-prefix:/usr/local/kong \
  --network=kong-internal \
  kong:3.8
```

Rescan: `ironctl scan kong-hardened` reports `89/100 grade B`. A **26-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a gateway exists to be connected through; `network=none` would score the last points but
leave nothing able to reach the upstreams. That is the honest ceiling for a gateway, and it is a
clear step up from the default C.

## Verify it on your own gateway

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-kong
ironctl scan my-kong --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Kong in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [kong:3.8 isolation scorecard &rarr;](../scores/kong.md): the full dimension breakdown.
- [Web servers and proxies, ranked by isolation &rarr;](../scores/collections/web-servers.md): how Kong compares to nginx, Traefik, Envoy, and the rest.
- [How to harden a Traefik container &rarr;](harden-traefik-container-isolation.md): another edge proxy with the same honest network ceiling, explained the same way.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
