---
title: "How to harden a pgAdmin container: pgadmin4 scores 63/100 by default"
description: "pgadmin4 defaults score 63/100 (grade C): full caps, writable rootfs. The exact ironctl scan --fix flags that take a database admin console to its honest 89/100 grade B."
---

# How to harden a pgAdmin container (and is pgadmin4 safe on your network?)

pgAdmin is a web console with a saved connection to your production Postgres, which means it holds the
credentials to your database behind a browser login. A stock `docker run dpage/pgadmin4` starts better
than most, but it is not the boundary that role deserves. Graded on IronClaw's seven-dimension
containment scale, the default configuration scores **63 of 100, grade C (partial)**. Higher is safer.
The image already runs non-root, which is why it starts at C not D; a few more runtime flags take it to
**89 of 100, grade B**, one point off an A, and the one dimension it cannot reach is the one an admin
console needs by definition: your browser must reach its UI. Here are the exact gaps and fixes from the
scan data.

> Every number here comes from a read-only `docker inspect` of `dpage/pgadmin4:8`, the same data
> behind its [isolation scorecard](../scores/pgadmin4.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run dpage/pgadmin4`, two fail and one warns, and it already earns the non-root point most
images miss:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ✅ PASS | 15/15 | runs as pgadmin (uid != 0) |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

pgAdmin starts ahead by running as the non-root `pgadmin` user, so the container-escape-as-host-root
path is already closed. The two gaps left are the **full capability set**, which widens any foothold,
and the **writable rootfs**, which lets an attacker who reaches the process persist. For a console that
stores database credentials, both are worth closing.

## Harden it: the exact `--fix` remediation

`ironctl scan my-pgadmin --fix` prints one remediation per failed dimension, then one hardened run.
For `pgadmin4`:

- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; pgAdmin serves its web
  UI on a high port and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/var/lib/pgadmin` as an explicit writable volume the `pgadmin` uid owns. Removes the persistence
  surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for an admin
  console, your browser must reach it. Any named or bridge network scores 4 of 15 (a WARN, not a fail):
  a connection path exists. Contain it anyway: put pgAdmin on a user-defined network scoped to just the
  Postgres instances it manages and a reverse proxy, with no default route out.

## Before and after

```bash
# Before: 63/100, grade C
docker run -d --name pgadmin \
  -e PGADMIN_DEFAULT_EMAIL=admin@example.com \
  -e PGADMIN_DEFAULT_PASSWORD=secret \
  dpage/pgadmin4:8

# After: 89/100, grade B (scoped private network for its browser and databases)
docker run -d --name pgadmin-hardened \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v pgadmin-data:/var/lib/pgadmin \
  --network=db-internal \
  -e PGADMIN_DEFAULT_EMAIL=admin@example.com \
  -e PGADMIN_DEFAULT_PASSWORD=secret \
  dpage/pgadmin4:8
```

Rescan reports `89/100 grade B`, a **26-point swing** with no custom image build, just the right flags.
Proven directly on the hardened config with `ironctl scan --compose`:

```
score:   89/100  grade B  (solid, minor gaps)
Non-root user (uid != 0)    [+] PASS  15/15  runs as 1000:1000 (uid != 0)
Dropped capabilities        [+] PASS  20/20  all capabilities dropped, none added back
Read-only root filesystem   [+] PASS  10/10  root filesystem is read-only
Network isolation / egress  [~] WARN  4/15   network=bridge: outbound egress is possible
```

The only dimension still short of full marks is the network (4 of 15), because an admin console exists
to be reached by a browser; `network=none` would score the last points but leave nothing able to log
in. That is the honest ceiling for this role, and it is a clear step up from the default C.

## Verify it on your own pgAdmin

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-pgadmin
ironctl scan my-pgadmin --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the pgAdmin in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [pgadmin4 isolation scorecard &rarr;](../scores/pgadmin4.md): the full dimension breakdown.
- [How to harden a Postgres container &rarr;](harden-postgres-container-isolation.md): the database pgAdmin connects to; take it to grade A.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
