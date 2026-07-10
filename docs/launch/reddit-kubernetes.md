# r/kubernetes draft

Audience: cluster operators and platform teams who already know Pod Security
Standards, securityContext, and RuntimeClass. They are skeptical of anything
that sounds like a new admission-controller sales pitch. Lead with the runtime
isolation gap that PSS does not close, the per-pod gVisor angle, and the Helm
chart. Frame the scores directory as evidence, and `ironctl scan --k8s` as
something they can run against their own manifests today.

---

**Title:** Pod Security Standards restrict what a pod requests, not what its kernel shares. We scored 54 popular images on default settings and shipped a per-pod gVisor runtime plus a manifest scanner.

**Body:**

Pod Security Standards and a tight `securityContext` gate what a pod is allowed
to ask for. They do not change the fact that a `runc` pod shares the host kernel,
so a single container escape is a node compromise. If you run agents, CI
runners, or any workload that executes untrusted code, that shared kernel is the
part of the threat model most manifests quietly ignore.

Two things came out of working on this that are useful on their own.

**1. A manifest scanner you can run today.**
`ironctl scan --k8s <manifest>` grades a pod or deployment 0 to 100 across seven
containment dimensions (user, capabilities, seccomp, filesystem, network, PID,
runtime) and prints each failed dimension with the fix. `--fix` rewrites the
`securityContext` and pod spec for you. To show what defaults actually look like,
we scored the 54 most-pulled public images as they ship, no hardening, and put
it in a searchable directory:

https://nivardsec.com/scores

Average default score is 51 out of 100, the most common grade is a D (43 of 54),
and every image reaches 100 once hardened. It is a fast way to calibrate what
"unhardened default" costs you before you argue about it in a design review.

**2. A per-pod gVisor runtime, not a shared sidecar.**
IronClaw is a self-hosted agent runtime where each workload gets its own gVisor
(`runsc`) sandbox: `network=none` by default, a seccomp syscall allowlist, all
Linux capabilities dropped, a non-root user namespace, and a read-only rootfs.
The isolation is per pod, not a cluster-wide trust boundary you hope holds. The
containment boundary ships with a red-team escape harness (Engine socket reach,
host path reads, sibling-container breakout, self-modification, egress) that runs
as a CI gate on every push, so the boundary is tested on every commit rather than
asserted once in a README. The measured gVisor cost is about +13 ms warm and
+39 ms cold per sandbox launch, paid once per launch, not per request.

It installs the way you already install things: there is a Helm chart under
`deploy/helm/ironclaw`, plus Docker, PaaS templates (Fly, Render, Railway), and a
Homebrew formula. AGPLv3 plus commercial, self-hosted, so nothing about your
workloads leaves your cluster.

Repo: https://github.com/IronSecCo/ironclaw

I would value feedback from operators on two things: whether a 0 to 100 posture
score per manifest is useful in an admission or CI gate, and whether per-pod
gVisor via RuntimeClass fits how you actually schedule untrusted work.

---

**Comment-reply seeds:**

- *How is this different from just setting a RuntimeClass to gVisor myself?* It is
  not competing with that, it leans on it. IronClaw wires the per-pod `runsc`
  sandbox plus the seccomp, caps, userns, network, and rootfs posture together
  and ships a red-team harness that keeps the whole boundary honest as a CI gate.
  If you already run gVisor via RuntimeClass, `ironctl scan --k8s` will still tell
  you where the rest of your pod spec leaks.
- *Does the scanner need the runtime?* No. `ironctl scan --k8s` grades any
  manifest standalone and `--fix` rewrites the spec. You can adopt the scanner
  and the CI gate without running IronClaw at all.
- *What about the gVisor performance hit for real workloads?* The measured cost is
  per sandbox launch (about +13 ms warm, +39 ms cold), not per syscall or per
  request, and the harness that produces those numbers is committed so you can
  reproduce it on your own nodes.
- *Is the Helm chart production-shaped or a toy?* It is a real chart under
  `deploy/helm/ironclaw` with the control plane and CLI. If you hit a values gap
  for your cluster, tell me exactly where and it becomes an issue.
