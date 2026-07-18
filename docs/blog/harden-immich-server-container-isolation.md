---
title: "How to harden an Immich container: immich-server scores 48/100 by default"
description: "immich-server defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a self-hosted photo server to its honest 89/100 grade B."
---

# How to harden an Immich container (and is immich-server safe on your network?)

Immich is a self-hosted photo and video library: the server holds every image you upload, the machine
learning models that index them, and the tokens your phone uses to sync. A stock
`docker run ghcr.io/immich-app/immich-server` is not the boundary that role deserves. Graded on
IronClaw's seven-dimension containment scale, the default configuration scores
**48 of 100, grade D (porous)**. Higher is safer. A few runtime flags take the same image to
**89 of 100, grade B**, one point off an A, and the one dimension it cannot reach is the one a photo
server needs by definition: your browser and mobile apps must reach its API. Here are the exact gaps
and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of
> `ghcr.io/immich-app/immich-server:v1.123.0`, the same data behind its
> [isolation scorecard](../scores/immich-server.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run immich-server`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**. Immich decodes and transcodes attacker-influenced
media, the photos and videos anyone with an account uploads; an image or codec CVE that lands code
execution in a root container escapes as root on the host, next to your entire library and the
database credentials. The full capability set widens that foothold and the writable rootfs makes it
durable.

## Harden it: the exact `--fix` remediation

`ironctl scan my-immich --fix` prints one remediation per failed dimension, then one hardened run.
For `immich-server`:

- **`--user 1000:1000`** (Non-root user, +15): run as a non-root uid that owns the upload volume so an
  escape does not begin as host uid 0.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; the server binds a
  high port and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/usr/src/app/upload` (and any transcode cache) as explicit writable volumes. Removes the
  persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a photo
  server, your browser and mobile apps must reach it. Any named or bridge network scores 4 of 15 (a
  WARN, not a fail): a connection path exists. Contain it anyway: put Immich on a user-defined network
  scoped to just its Postgres, Redis, machine-learning container, and reverse proxy, with no default
  route out.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name immich-server \
  -v ./library:/usr/src/app/upload \
  ghcr.io/immich-app/immich-server:v1.123.0

# After: 89/100, grade B (scoped private network for its clients and helpers)
docker run -d --name immich-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v immich-upload:/usr/src/app/upload \
  --network=immich-internal \
  ghcr.io/immich-app/immich-server:v1.123.0
```

Rescan reports `89/100 grade B`, a **41-point swing** with no custom image build, just the right
flags. Proven directly on the hardened config with `ironctl scan --compose`:

```
score:   89/100  grade B  (solid, minor gaps)
Non-root user (uid != 0)    [+] PASS  15/15  runs as 1000:1000 (uid != 0)
Dropped capabilities        [+] PASS  20/20  all capabilities dropped, none added back
Read-only root filesystem   [+] PASS  10/10  root filesystem is read-only
Network isolation / egress  [~] WARN  4/15   network=bridge: outbound egress is possible
```

The only dimension still short of full marks is the network (4 of 15), because a photo server exists
to be reached by your devices; `network=none` would score the last points but leave nothing able to
sync. That is the honest ceiling for this role, and it is a long way from the default D. Immich also
needs a co-located Postgres and Redis; harden those too, and each can reach grade A on its own page
since only Immich talks to it.

## Verify it on your own Immich

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-immich
ironctl scan my-immich --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Immich in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [immich-server isolation scorecard &rarr;](../scores/immich-server.md): the full dimension breakdown.
- [How to harden a Postgres container &rarr;](harden-postgres-container-isolation.md): the database Immich stores its metadata in.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
