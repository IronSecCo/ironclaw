# Scan: audit any container's containment in 10 seconds

`ironctl scan` grades the isolation posture of any running container, any
docker-compose service, or any Kubernetes pod or manifest on a 0 to 100 scale.
It works on your own setups, not just IronClaw's, so you can measure how much
containment you actually have before you trust a sandbox with untrusted code.

It grades the same dimensions IronClaw's own containment benchmark checks, and
it is fail-closed: any posture it cannot determine is scored as insecure, never
silently passed.

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

## Quick start

```bash
# grade a running docker container
ironctl scan my-container

# grade a docker-compose service (pass --service if the file has more than one)
ironctl scan --compose docker-compose.yml --service web

# grade a Kubernetes pod or workload manifest (Deployment, StatefulSet, ...)
ironctl scan --k8s pod.yaml
```

## Output formats

| Flag | What you get |
|---|---|
| (default) | a human-readable scorecard table |
| `--json` | the full report as JSON (schemaVersion 1.0), for pipelines and dashboards |
| `--badge scan.svg` | a self-contained SVG badge you can drop into a README |
| `--md` | a shareable markdown block for a README or blog post |
| `--min-score N` | exit non-zero when the score is below N (a CI gate) |

## What it grades

Each dimension is worth a fixed weight; the weights sum to 100. The heaviest
weights sit on the boundaries whose breach is a full host compromise.

| Dimension | Weight | PASS means |
|---|---|---|
| Non-root user | 15 | the workload runs as a uid that is not 0 |
| Dropped capabilities | 20 | all Linux capabilities are dropped, none re-added |
| Seccomp profile | 15 | a seccomp profile filters the syscall surface |
| Network isolation | 15 | `network=none`: no NIC but loopback, no egress |
| Read-only rootfs | 10 | the root filesystem is read-only |
| No docker.sock exposure | 15 | no Docker or OCI control socket is mounted in |
| No shared host namespaces | 10 | no host PID, IPC, or network namespace sharing |

A `--privileged` container fails capabilities, seccomp, and host namespaces at
once, because privilege is the master escape hatch.

Grades map to bands: A is 90 or above, B is 75 to 89, C is 50 to 74, D is 25 to
49, and F is below 25.

## Why the numbers can differ from what you expect

- Docker applies its default seccomp profile even to unhardened containers, so
  seccomp often passes on a container that fails everything else. Passing
  `--security-opt seccomp=unconfined` turns it red.
- A Kubernetes pod manifest does not carry its NetworkPolicy, so egress is
  graded conservatively (WARN) unless `hostNetwork` makes it strictly worse. A
  hardened pod tops out at a strong B for that reason.
- If a field is absent, scan assumes the insecure value. An auditor that cannot
  see a boundary must never claim the boundary holds.

## Use it as a CI gate

```bash
# fail the build if the sandbox posture regresses below an A
ironctl scan my-sandbox --min-score 90
```

On GitHub, the [scan GitHub Action](scan-action.md) does this for you: it posts
the scorecard as a sticky pull-request comment and fails the check below your
`min-score`, so every PR carries an IronClaw containment grade.

## What a hardened target looks like

An IronClaw `ic-sbx-*` sandbox scores a clean 100:

```
$ ironctl scan ic-sbx-mg-abc123
  score:   100/100  grade A  (hardened)
```

That is the posture IronClaw gives every session by default: non-root, all caps
dropped, seccomp on, `network=none`, read-only rootfs, no control socket, no
shared host namespaces, on a gVisor (runsc) runtime. See
[Security and isolation](security-isolation.md) and the
[containment benchmark](compare/sandbox-containment-benchmark.md) for how that
posture is built and proven.
