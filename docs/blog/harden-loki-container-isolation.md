---
title: "How to harden a Loki container: loki:3.3.2 scores 63/100 by default"
description: "grafana/loki:3.3.2 defaults score 63/100 (grade C): full caps, writable rootfs, bridge egress. The exact ironctl scan --fix flags that take a co-located log store to a full 100/100 grade A."
---

# How to harden a Grafana Loki container (and is loki:3.3.2 safe for your logs?)

Loki is where your logs land: application traces, access records, and often the credentials and PII
that leak into log lines nobody meant to keep. A stock `docker run grafana/loki:3.3.2` already runs
as a non-root uid, which is a good start, but it still holds that data behind three weak boundaries.
Graded on IronClaw's seven-dimension containment scale, the default configuration scores **63 of 100,
grade C (partial)**. Higher is safer. Unlike a broker or a proxy, a log store that only its
co-located agent and query reader talk to can close every remaining dimension, including the network.
A few runtime flags take the same image to a full **100 of 100, grade A**. Here are the exact gaps
and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `grafana/loki:3.3.2`, the same data
> behind its [isolation scorecard](../scores/loki.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
grafana/loki:3.3.2`, two fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as 10001 (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

Loki already ships a non-root uid, so the two that should worry you most are **egress** and the
**default capability set**. A log store that can reach arbitrary destinations is one that can quietly
ship your entire log history out the moment a query-parsing or plugin CVE lands code execution. The
retained capabilities and writable rootfs widen and entrench that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-loki --fix` prints one remediation per failed dimension, then one hardened run. For
`grafana/loki:3.3.2`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Loki needs none of
  the default set to serve its API and write chunks on a high port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and
  mount `/loki` as an explicit writable volume. Removes the persistence surface.
- **`--user 65532:65532`** (Non-root user, already PASS): keep the non-root uid and point the data
  and WAL directories at a volume this uid owns.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  store can actually max out. If the only writer and reader are on the same host or pod and reach
  Loki over the loopback of a shared network namespace, cut the NIC entirely. Nothing external can
  connect, and the store cannot phone home.

### When network=none is not honest

If remote agents push logs to Loki over the network (a fleet of Promtail or Grafana Alloy collectors,
for example), you cannot use `--network=none`; the store has to accept those connections. In that
case put it on a user-defined network scoped to just the collectors and the Grafana that queries it,
with no default route out. That holds the network dimension at a WARN (4 of 15) and the honest
ceiling becomes **89 of 100, grade B**, the same as a broker. Use `--network=none` only for the
single-writer, co-located case.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name loki grafana/loki:3.3.2

# After: 100/100, grade A (co-located store, no network needed)
docker run -d --name loki-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v loki-data:/loki \
  --network=none \
  grafana/loki:3.3.2
```

Rescan: `ironctl scan loki-hardened` reports `100/100 grade A`. A **37-point swing** with no custom
image build, just the right flags. Every dimension is closed because a co-located log store does not
need to talk to anything but the app on the other side of its loopback. That is the top grade,
reserved for datastores whose clients live next to them.

## Verify it on your own Loki

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-loki
ironctl scan my-loki --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Loki in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [loki:3.3.2 isolation scorecard &rarr;](../scores/loki.md): the full dimension breakdown.
- [How to harden a Grafana container &rarr;](harden-grafana-container-isolation.md): the dashboard that queries Loki, which caps at grade B because browsers must reach it.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
