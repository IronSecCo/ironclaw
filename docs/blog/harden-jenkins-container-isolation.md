---
title: "How to harden a Jenkins container: jenkins/jenkins scores 48/100 by default"
description: "jenkins/jenkins:lts defaults score 48/100 (grade D): root, full caps, writable rootfs. The exact ironctl scan --fix flags that take a CI server to its honest 89/100 grade B."
---

# How to harden a Jenkins container (and is jenkins/jenkins:lts safe to run builds?)

Jenkins is one of the most attacked services in a stack: it holds deploy credentials, cloud keys, and
source, and it runs arbitrary build steps by design. A stock `docker run jenkins/jenkins:lts` is not
the boundary that role deserves. Graded on IronClaw's seven-dimension containment scale, the default
configuration scores **48 of 100, grade D (porous)**. Higher is safer. A few runtime flags take the
same image to **89 of 100, grade B**, one point off an A, and the one dimension it cannot reach is the
one a CI server needs by definition: agents and browsers must reach it. Here are the exact gaps and
fixes from the scan data.

> Every number here comes from a read-only `docker inspect` of `jenkins/jenkins:lts`, the same data
> behind its [isolation scorecard](../scores/jenkins.md). No workload is executed.
> [How scoring works &rarr;](../scan.md)

## Where the default configuration leaks

`ironctl scan` grades seven independent containment boundaries. On a default `docker run
jenkins/jenkins:lts`, three fail and one warns:

| Dimension | Verdict | Score | What the scan found |
|-----------|:-------:|------:|---------------------|
| Non-root user (uid != 0) | ❌ FAIL | 0/15 | runs as root (uid 0); a container escape starts with host-uid 0 |
| Dropped capabilities | ❌ FAIL | 4/20 | default capability set retained (CAP_NET_RAW, CAP_MKNOD, and more) |
| Seccomp profile | ✅ PASS | 15/15 | seccomp profile active |
| Network isolation / egress | ⚠️ WARN | 4/15 | network=bridge: outbound egress is possible |
| Read-only root filesystem | ❌ FAIL | 0/10 | root filesystem is writable |
| No docker.sock exposure | ✅ PASS | 15/15 | no control socket mounted |
| No shared host namespaces | ✅ PASS | 10/10 | no host PID/IPC/network sharing |

The one that should worry you most is **root**, and Jenkins makes it worse than most. A plugin CVE, a
malicious pipeline, or a poisoned dependency that lands code execution in a root container escapes as
root on the host, next to the credentials store Jenkins guards. A very common Jenkins pattern is
mounting `docker.sock` so pipelines can build images; do not, and note that this scan would fail the
docker.sock dimension if you did. Mounting the host Docker socket hands a build step full control of
the host. Use a rootless builder or a scoped socket-proxy instead. The full capability set and
writable rootfs widen and entrench any foothold.

## Harden it: the exact `--fix` remediation

`ironctl scan my-jenkins --fix` prints one remediation per failed dimension, then one hardened run.
For `jenkins/jenkins:lts`:

- **`--user 1000:1000`** (Non-root user, +15): pin the non-root `jenkins` uid so an escape does not
  begin as host uid 0. Point `JENKINS_HOME` at a volume this uid owns.
- **`--cap-drop=ALL`** (Dropped capabilities, +16): drop every Linux capability; the Jenkins
  controller serves its UI and agent port on high ports and needs none of the default set.
- **`--read-only --tmpfs /tmp`** (Read-only rootfs, +10): make the root filesystem read-only and mount
  `JENKINS_HOME` as an explicit writable volume. Removes the persistence surface.
- **Scoped network** (Network isolation): `--network=none` scores the full 15 but is wrong for a CI
  server, agents and browsers must reach it. Any named or bridge network scores 4 of 15 (a WARN, not a
  fail): a connection path exists. Contain it anyway: put the controller on a user-defined network
  scoped to just its agents and reverse proxy, with no default route out, so a compromised build
  cannot call arbitrary internet addresses.

## Before and after

```bash
# Before: 48/100, grade D
docker run -d --name jenkins jenkins/jenkins:lts

# After: 89/100, grade B (scoped private network for agents and proxy)
docker run -d --name jenkins-hardened \
  --user 1000:1000 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  -v jenkins-home:/var/jenkins_home \
  --network=ci-internal \
  jenkins/jenkins:lts
```

Rescan: `ironctl scan jenkins-hardened` reports `89/100 grade B`. A **41-point swing** with no custom
image build, just the right flags. The only dimension still short of full marks is the network (4 of
15), because a CI server exists to be reached by its agents and users; `network=none` would score the
last points but leave nothing able to connect. That is the honest ceiling for this role, and it is a
long way from the default D.

## Verify it on your own Jenkins

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade your running container, then print the fixes
ironctl scan my-jenkins
ironctl scan my-jenkins --fix
```

`ironctl scan` also reads a `docker-compose.yml` service or a Kubernetes manifest, so you can grade
the Jenkins in your stack, not just a bare `docker run`.

## Keep going

- [All hardening guides &rarr;](hardening-guides.md): every harden-a-container walkthrough, with grade deltas.
- [jenkins/jenkins isolation scorecard &rarr;](../scores/jenkins.md): the full dimension breakdown.
- [How to harden a SonarQube container &rarr;](harden-sonarqube-container-isolation.md): the code-quality server that sits next to Jenkins in most CI stacks.
- [Scan any container in 10 seconds &rarr;](../scan.md): the full `ironctl scan` reference.
- [Run untrusted code in a real sandbox &rarr;](../index.md): IronClaw wraps every AI-agent session in a gVisor/Kata boundary with `network=none` by default.
