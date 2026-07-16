---
title: "How to harden a Gitea container: gitea:1.22 scores 48/100 by default"
description: "gitea:1.22 defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take the git server to its honest 89/100 grade B ceiling."
---

# How to harden a Gitea container (and is gitea:1.22 safe for your repositories?)

Gitea hosts your source: git repositories, access tokens, CI webhooks, and the web UI developers push
and pull through all day. A stock `docker run gitea/gitea:1.22` keeps that server behind a boundary
weaker than the code deserves. Graded on IronClaw's seven-dimension containment scale, the default
configuration scores **48 of 100, grade D (porous)**. Higher is safer. A git server exists to be
reached by browsers and git clients, so it cannot take `--network=none` the way a co-located database
can. That sets an honest ceiling of **89 of 100, grade B**, and the flags below reach it. Here are the
exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `gitea:1.22`, the same data behind its
> [isolation scorecard](../scores/gitea.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default
`docker run gitea/gitea:1.22`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The two that should worry you most are **root** and the writable rootfs. A Gitea process that escapes
as root escapes as root on the host, right next to every repository and secret it was serving. A
writable rootfs lets an attacker who lands code execution through a hook, webhook, or web CVE persist
inside the image. The network dimension stays a WARN by design here, because a git server has to accept
connections.

## Harden it: the exact `--fix` remediation

`ironctl scan my-gitea --fix` prints one remediation per failed dimension, then one hardened run. For
`gitea:1.22`:

- **`--user 1000:1000`** (Non-root user, +15): pin the non-root `git` uid so an escape does not begin
  as host uid 0. Point the data directory at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; Gitea needs none of the
  default set to serve HTTP and SSH on high ports.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/data` as an explicit writable volume. Removes the persistence surface.
- **Scoped private network** (Network, held at WARN by design): a git server exists to be reached by
  developers and CI, so `--network=none` would break it. Put Gitea on a user-defined network scoped to
  just its reverse proxy and clients, with no default route out. The network dimension holds at a WARN
  (4 of 15). That is the honest ceiling.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name gitea gitea/gitea:1.22

# After: 89/100, grade B (scoped private network for proxy and clients)
docker run -d --name gitea-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v gitea-data:/data \
  --network=git-internal \
  gitea/gitea:1.22
```

Rescan: `ironctl scan gitea-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a git server exists to be reached by its developers and CI; `network=none` would score the
last points but leave nothing able to connect. That is the honest ceiling for this role, and it is a
long way from the default D.

## Verify it on your own Gitea

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-gitea
ironctl scan my-gitea --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade the
Gitea in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [gitea:1.22 isolation scorecard &rarr;](../scores/gitea.md): the full dimension breakdown.
- [How to harden a Jenkins container &rarr;](harden-jenkins-container-isolation.md): another developer-facing server whose honest ceiling is grade B.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
