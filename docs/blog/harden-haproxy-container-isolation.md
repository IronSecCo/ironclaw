---
title: "How to harden a HAProxy container: haproxy:3.1-alpine scores 63/100 by default"
description: "haproxy:3.1-alpine defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take a load balancer to its honest 89/100 grade B."
---

# How to harden a HAProxy container (and is haproxy:3.1-alpine safe at your edge?)

HAProxy sits in the request path: it terminates TLS, load-balances every inbound connection, and is
the first thing an attacker reaches. A stock `docker run haproxy:3.1-alpine` is better than most edge
images out of the box, but it is not yet the boundary that role deserves. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **63 of 100, grade C (partial)**.
Higher is safer. A few runtime flags take the same image to **89 of 100, grade B**, one point off an
A, and the one dimension it cannot reach is the one a load balancer needs by definition: it exists to
accept and forward traffic. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `haproxy:3.1-alpine`, the same data
> behind its [isolation scorecard](../scores/haproxy.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
haproxy:3.1-alpine`, two fail and one warns. It already runs as a non-root user, which is why it
starts a full grade above most edge images:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as haproxy (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

HAProxy already gets the hardest dimension right by dropping to a non-root uid. The two that remain are
the **full capability set** and the **writable rootfs**. HAProxy parses attacker-controlled bytes on
every request; a routing or TLS CVE that lands code execution keeps CAP_NET_RAW to craft raw packets
and a writable rootfs to persist, unless you take them away. Neither is needed to forward traffic.

## Harden it: the exact `--fix` remediation

`ironctl scan my-haproxy --fix` prints one remediation per failed dimension, then one hardened run.
For `haproxy:3.1-alpine`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability, then add back only
  `CAP_NET_BIND_SERVICE` if you must bind 80/443 directly. The scan below reflects dropping the full
  set with ports mapped high; adding one cap back costs 4 points (a WARN, still grade B territory).
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only. HAProxy
  runs happily read-only; mount a writable volume only if you use its runtime state or maps.
- **`--user 99:99`** (Non-root user, already +15): the image already drops to the `haproxy` uid, so
  this dimension is closed by default. Keep it pinned if you override the entrypoint.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a load
  balancer, it exists to forward traffic. Any named or bridge network scores 4 of 15 (a WARN, not a
  fail): a connection path exists. Contain it anyway: put HAProxy on a user-defined network scoped to
  just the backends it fronts, with no default route out, so a compromised proxy cannot call arbitrary
  internet addresses.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name haproxy haproxy:3.1-alpine

# After: 89/100, grade B (scoped private network for its backends)
docker run -d --name haproxy-hardened \
  --user 99:99 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -p 8080:8080 -p 8443:8443 \
  --network=edge-internal \
  haproxy:3.1-alpine
```

Rescan: `ironctl scan haproxy-hardened` reports `89/100 grade B`. A **26-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a load balancer exists to accept connections; `network=none` would score the last points
but leave nothing able to reach or be reached. That is the honest ceiling for a proxy, and a solid
grade B.

## Verify it on your own HAProxy

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-haproxy
ironctl scan my-haproxy --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the HAProxy in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [haproxy isolation scorecard &rarr;](../scores/haproxy.md): the full dimension breakdown.
- [How to harden a Traefik container &rarr;](harden-traefik-container-isolation.md): the other popular edge proxy, with the same honest ceiling.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
