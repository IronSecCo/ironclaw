---
title: "How to harden a TimescaleDB container: timescaledb scores 48/100 by default"
description: "timescale/timescaledb defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a co-located time-series database to a full 100/100 grade A."
---

# How to harden a TimescaleDB container (and is timescale/timescaledb safe for your metrics?)

TimescaleDB holds your time-series of record: sensor readings, financial ticks, application metrics,
the append-only history an attacker would love to tamper with or exfiltrate. Built on Postgres, a
stock `docker run timescale/timescaledb` inherits the same porous defaults. Graded on IronClaw's
seven-dimension containment scale, the default configuration scores **48 of 100, grade D (porous)**.
Higher is safer. Unlike a broker or a proxy, a database that only its co-located application talks to
can close every dimension, including the network. A few runtime flags take the same image to a full
**100 of 100, grade A**. Here are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `timescale/timescaledb:2.17.2-pg17`,
> the same data behind its [isolation scorecard](../scores/timescaledb.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
timescale/timescaledb:2.17.2-pg17`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and **egress**. A Postgres process that escapes as
root escapes as root on the host, next to the very store files it was holding. And a database that can
reach arbitrary destinations is one that can quietly ship your entire history out the moment a SQL
parsing or extension CVE lands code execution. The default capability set and writable rootfs widen
and entrench that foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-timescaledb --fix` prints one remediation per failed dimension, then one hardened
run. For `timescale/timescaledb:2.17.2-pg17`:

- **`--user 999:999`** (Non-root user, +15): pin the non-root `postgres` uid so an escape does not
  begin as host uid 0. Point `/var/lib/postgresql/data` at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Postgres needs none
  of the default set to serve on its port.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  the data directory as an explicit writable volume. Removes the persistence surface.
- **`--network=none`** (Network isolation, +11 to the full 15): this is the dimension a co-located
  store can actually max out. If the only client is an application on the same host or pod reaching
  TimescaleDB over the loopback of a shared network namespace, cut the NIC entirely. Nothing external
  can connect, and the database cannot phone home.

### When network=none is not honest

If remote clients connect over the network, or you run streaming replication to standby replicas, you
cannot use `--network=none`; the store has to accept those connections. In that case put it on a
user-defined network scoped to just its clients and replicas, with no default route out. That holds
the network dimension at a WARN (4 of 15) and the honest ceiling becomes **89 of 100, grade B**, the
same as a broker. Use `--network=none` only for the single-instance, co-located case.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name timescaledb -e POSTGRES_PASSWORD=x timescale/timescaledb:2.17.2-pg17

# After: 100/100, grade A (co-located app, no network needed)
docker run -d --name timescaledb-hardened \
  --user 999:999 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -e POSTGRES_PASSWORD=x \
  -v timescaledb-data:/var/lib/postgresql/data \
  --network=none \
  timescale/timescaledb:2.17.2-pg17
```

Rescan: `ironctl scan timescaledb-hardened` reports `100/100 grade A`. A **52-point swing** with no
custom image build, just the right flags. Every dimension is closed because a co-located time-series
database does not need to talk to anything but the app on the other side of its loopback. That is the
top grade, reserved for datastores whose clients live next to them.

## Verify it on your own TimescaleDB

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-timescaledb
ironctl scan my-timescaledb --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the TimescaleDB in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [timescaledb isolation scorecard &rarr;](../scores/timescaledb.md): the full dimension breakdown.
- [How to harden a Postgres container &rarr;](harden-postgres-container-isolation.md): the base image TimescaleDB extends, with the same grade-A path.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
