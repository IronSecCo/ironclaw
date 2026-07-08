---
title: "Audit your sandbox in 10 seconds with ironctl scan"
description: "ironctl scan grades the containment posture of any running container, docker-compose service, or Kubernetes pod on a 0 to 100 scale. It works on your own setups, not just IronClaw's. One command tells you how much isolation you actually have before you hand a sandbox to untrusted code."
---

# Audit your sandbox in 10 seconds with ironctl scan

If you run AI agents, build agents, or hand any untrusted code to a container,
you are trusting that container to hold. The uncomfortable question is: how much
containment do you actually have? Most people cannot answer with a number. They
have a `docker run` line, a compose file, or a Kubernetes manifest, and a hope
that it is tight enough.

`ironctl scan` answers with a number. Point it at a running container, a
compose service, or a pod manifest, and in about ten seconds it grades the
isolation posture on a 0 to 100 scale, with a per-dimension breakdown of exactly
where the boundary leaks.

It runs on your own setups, not only on IronClaw's. You do not need to adopt
anything to use it. It is a standalone audit tool.

## One command

```bash
ironctl scan my-container
```

That is the whole thing. No config, no agent, no daemon. It reads the container's
runtime configuration and grades it.

## What a weak container looks like

Here is a container started the way a lot of quickstart guides tell you to: as
root, with the default capability set, on a bridge network, with `docker.sock`
mounted in for convenience.

```
$ ironctl scan my-container
IronClaw containment scan
  target:  my-container (docker)
  runtime: runc
  score:   23/100  grade F  (wide open)

DIMENSION                   VERDICT   SCORE  DETAIL
Non-root user (uid != 0)    [x] FAIL  0/15   runs as root (user "0"); a container escape starts with host-uid 0
Dropped capabilities        [x] FAIL  4/20   default capability set retained (includes CAP_NET_RAW, CAP_MKNOD)
Seccomp profile             [+] PASS  15/15  seccomp profile active (syscall surface filtered)
Network isolation / egress  [~] WARN  4/15   network=bridge: outbound egress is possible; prefer network=none
Read-only root filesystem   [x] FAIL  0/10   root filesystem is writable: tamper/persistence surface
No docker.sock exposure     [x] FAIL  0/15   docker.sock is mounted: trivial host-root escape
No shared host namespaces   [x] FAIL  0/10   shares host namespace(s): PID
```

Twenty three out of a hundred, grade F. Every line tells you what specifically is
open and why it matters. `docker.sock` mounted in is a one-line escape to host
root. Running as uid 0 means a container escape starts with host-uid 0. This is
not a lecture, it is a checklist with the boxes already ticked for you.

## What a hardened target looks like

Now the same scan against an IronClaw session sandbox, one of the `ic-sbx-*`
containers IronClaw hands every agent by default:

```
$ ironctl scan ic-sbx-mg-abc123
  score:   100/100  grade A  (hardened)
```

A clean hundred. Non-root, all capabilities dropped, seccomp on, `network=none`,
read-only root filesystem, no control socket, no shared host namespaces, on a
gVisor (runsc) runtime. That is the posture IronClaw builds for every session so
you do not have to assemble it by hand and hope you got all seven dimensions
right.

The gap between 23 and 100 is the point. Both are "a container." Only one of them
is a boundary you would trust with code you assume is hostile.

## It is fail-closed

An auditor that guesses "probably fine" on the things it cannot see is worse than
useless, because it hands you false confidence. `ironctl scan` does the opposite.
If it cannot determine a dimension, it scores that dimension as insecure. A
boundary you cannot observe is a boundary you must not claim holds.

That is why the numbers sometimes surprise people. Docker applies its default
seccomp profile even to otherwise wide-open containers, so seccomp can pass on a
container that fails everything else. A Kubernetes pod manifest does not carry its
NetworkPolicy, so egress is graded conservatively unless `hostNetwork` makes it
strictly worse. Absent fields are read as insecure, never waved through.

## The seven dimensions

Each dimension carries a fixed weight; the weights sum to 100. The heaviest
weights sit on the boundaries whose breach is a full host compromise.

| Dimension | Weight | PASS means |
|---|---|---|
| Non-root user | 15 | the workload runs as a uid that is not 0 |
| Dropped capabilities | 20 | all Linux capabilities dropped, none re-added |
| Seccomp profile | 15 | a seccomp profile filters the syscall surface |
| Network isolation | 15 | `network=none`: no NIC but loopback, no egress |
| Read-only rootfs | 10 | the root filesystem is read-only |
| No docker.sock exposure | 15 | no Docker or OCI control socket mounted in |
| No shared host namespaces | 10 | no host PID, IPC, or network namespace sharing |

Grades map to bands: A is 90 or above, B is 75 to 89, C is 50 to 74, D is 25 to
49, and F is below 25.

## Scan compose files and Kubernetes too

```bash
# a docker-compose service (pass --service if the file has more than one)
ironctl scan --compose docker-compose.yml --service web

# a Kubernetes pod or workload manifest (Deployment, StatefulSet, ...)
ironctl scan --k8s pod.yaml
```

Same scale, same fail-closed grading, so you can compare a compose stack against a
pod against a live container on one ruler.

## Put the grade in your README

`ironctl scan --badge scan.svg` writes a self-contained SVG badge you can drop
straight into a README, so your containment posture is visible to anyone reading
your repo:

```bash
ironctl scan my-sandbox --badge scan.svg
```

Prefer a table? `--md` prints a shareable markdown block:

```bash
ironctl scan my-sandbox --md
```

```
### IronClaw containment scan: `my-sandbox` scored **100/100 (grade A)**

| Dimension | Verdict | Score |
|---|---|---|
| Non-root user (uid != 0) | PASS | 15/15 |
| Dropped capabilities | PASS | 20/20 |
| Seccomp profile | PASS | 15/15 |
| Network isolation / egress | PASS | 15/15 |
| Read-only root filesystem | PASS | 10/10 |
| No docker.sock exposure | PASS | 15/15 |
| No shared host namespaces | PASS | 10/10 |
```

## Wire it into CI

`--min-score N` exits non-zero when the score is below N, so a posture regression
fails the build instead of shipping quietly:

```bash
# fail the build if the sandbox drops below an A
ironctl scan my-sandbox --min-score 90
```

Now "our sandbox is hardened" is a checked fact on every push, not a line in a
design doc that drifted out of date six months ago.

## Try it on your own container right now

```bash
brew install ironsecco/ironclaw/ironclaw
ironctl scan <your-container>
```

Run it against the tightest box you have. If it comes back an A, good, you have
earned your confidence. If it comes back an F, you just found out for free,
before untrusted code did.

For every flag and dimension in detail, see the
[scan reference](../scan.md). To see how IronClaw builds a 100 out of 100 posture
for every agent session, see [Security and isolation](../security-isolation.md)
and the reproducible [containment benchmark](containment-benchmark-docker-gvisor-e2b-daytona.md).
