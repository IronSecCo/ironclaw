---
title: "How to harden a SonarQube container: sonarqube scores 48/100 by default"
description: "sonarqube defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a code-quality server to its honest 89/100 grade B."
---

# How to harden a SonarQube container (and is sonarqube safe in your CI stack?)

SonarQube reads every line of your source on every scan and stores the findings, the tokens CI uses to
push results, and often a copy of your code smells and secrets. A stock `docker run sonarqube` is not
the boundary that role deserves. Graded on IronClaw's seven-dimension containment scale, the default
configuration scores **48 of 100, grade D (porous)**. Higher is safer. A few runtime flags take the
same image to **89 of 100, grade B**, one point off an A, and the one dimension it cannot reach is the
one a code-quality server needs by definition: scanners and browsers must reach its API and UI. Here
are the exact gaps and fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `sonarqube`, the same data behind its
> [isolation scorecard](../scores/sonarqube.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run sonarqube`,
three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**. SonarQube parses attacker-adjacent input, the source
under analysis, on every scan; a parser or plugin CVE that lands code execution in a root container
escapes as root on the host, next to the analysis tokens and the database credentials it holds. The
full capability set widens that foothold and the writable rootfs makes it durable. SonarQube also
needs a co-located database (Postgres); harden that too, and it can reach grade A on its own page since
only SonarQube talks to it.

## Harden it: the exact `--fix` remediation

`ironctl scan my-sonarqube --fix` prints one remediation per failed dimension, then one hardened run.
For `sonarqube`:

- **`--user 1000:1000`** (Non-root user, +15): pin the non-root `sonarqube` uid so an escape does not
  begin as host uid 0. Point the data, logs, and extensions directories at volumes this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; SonarQube serves its
  UI and API on a high port and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `/opt/sonarqube/data`, `/opt/sonarqube/logs`, and `/opt/sonarqube/extensions` as explicit writable
  volumes. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a
  code-quality server, scanners and browsers must reach it. Any named or bridge network scores 4 of 15
  (a WARN, not a fail): a connection path exists. Contain it anyway: put SonarQube on a user-defined
  network scoped to just its database, CI runners, and reverse proxy, with no default route out.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name sonarqube sonarqube:community

# After: 89/100, grade B (scoped private network for CI and its database)
docker run -d --name sonarqube-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v sonarqube-data:/opt/sonarqube/data \
  -v sonarqube-logs:/opt/sonarqube/logs \
  --network=ci-internal \
  sonarqube:community
```

Rescan: `ironctl scan sonarqube-hardened` reports `89/100 grade B`. A **41-point swing** with no
custom image build, just the right flags. The only dimension still short of full marks is the network
(4 of 15), because a code-quality server exists to be reached by its scanners and users;
`network=none` would score the last points but leave nothing able to connect. That is the honest
ceiling for this role, and it is a long way from the default D.

> SonarQube also needs `vm.max_map_count` raised on the host for its embedded search engine. That is a
> host sysctl, not a container capability, and it does not affect the containment grade.

## Verify it on your own SonarQube

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-sonarqube
ironctl scan my-sonarqube --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the SonarQube in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [sonarqube isolation scorecard &rarr;](../scores/sonarqube.md): the full dimension breakdown.
- [How to harden a Jenkins container &rarr;](harden-jenkins-container-isolation.md): the CI server that usually drives SonarQube scans.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
