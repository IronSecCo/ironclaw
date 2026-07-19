# Scan: audit any container's containment in 10 seconds

`ironctl scan` grades the isolation posture of any running container, any
docker-compose service, or any Kubernetes pod or manifest on a 0 to 100 scale.
It works on your own setups, not just IronClaw's, so you can measure how much
containment you actually have before you trust a sandbox with untrusted code.

It grades the same dimensions IronClaw's own containment benchmark checks, and
it is fail-closed: any posture it cannot determine is scored as insecure, never
silently passed.

Curious how the images you already pull score? Browse the public
[Container Isolation Scores directory](scores/index.md): the default-config
grade for 150+ of the most-pulled public images, then scan your own in 10 seconds.
See the full ranking, best to worst, on the
[Container Isolation Leaderboard](scores/leaderboard.md).

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

# grade a Dockerfile statically, at authoring/CI time (no daemon, no image pull)
ironctl scan --dockerfile Dockerfile

# force a specific runtime (default is auto-detect)
ironctl scan --runtime podman my-container
```

## Supported runtimes

`ironctl scan` audits any OCI container, not just Docker. It auto-detects the
available runtime (in order: docker, then podman, then nerdctl on your PATH) and
picks the matching adapter; override it with `--runtime`. It grades host-side
inspect data, so probe-masking from inside the container cannot change the score.

| Runtime | How it is graded | Notes |
|---|---|---|
| `docker` | `docker inspect` | the default; also covers Docker-compatible engines |
| `podman` | `podman inspect` | rootless is detected and credited (see below) |
| `nerdctl` / containerd | `nerdctl inspect` | Docker-compatible schema; containerd runtime handlers (for example `io.containerd.runsc.v1`) are recognized |
| compose | `--compose FILE` | grades a service from the file, no runtime needed |
| Kubernetes | `--k8s FILE` | grades a pod or workload manifest, no runtime needed |
| Helm | `--helm CHART` | renders a chart with `helm template` and grades its workloads, no cluster needed |
| Terraform | `--terraform PATH` | grades container workloads in a `terraform show -json` plan/state, no apply needed (see below) |
| AWS ECS | `--ecs PATH` | grades a live `aws ecs describe-task-definition` (or registered) task definition, no AWS call needed (see below) |
| Cloud Run | `--cloudrun PATH` | grades a Google Cloud Run service spec (Knative Service YAML) or a directory of them, no account needed (see below) |
| CloudFormation | `--cloudformation PATH` | grades `AWS::ECS::TaskDefinition` resources in a CloudFormation template (YAML/JSON) or a directory, no AWS call needed (see below) |
| Kustomize | `--kustomize DIR` | renders a kustomization with `kustomize build` (or `kubectl kustomize`) and grades its workloads, no cluster needed (see below) |
| Dockerfile | `--dockerfile FILE` | grades authoring-time posture statically, no daemon and no image pull (see below) |

Selection and binaries:

```bash
ironctl scan --runtime auto CONTAINER      # auto-detect (default)
ironctl scan --runtime podman CONTAINER    # force podman
ironctl scan --podman-bin /usr/bin/podman CONTAINER
```

The runtime is resolved fail-closed: if the selected (or auto-detected) runtime
is not on your PATH or cannot reach a running container, the scan errors with a
clear message instead of returning a misleadingly clean report. `--docker-bin`,
`--podman-bin`, and `--nerdctl-bin` (or the `DOCKER`, `PODMAN`, `NERDCTL`
environment variables) point at a non-default binary.

### Rootless Podman is credited

A rootless container runs inside a user namespace that remaps container-uid 0 to
an unprivileged host uid, so a container-root escape lands as an unprivileged
host user rather than host root. That is a real isolation win, so a rootless
Podman container earns credit on the non-root dimension even when the process
inside the container is uid 0. Rootless mode is detected from `podman info` and,
when present, from the container's user-namespace uid map.

### Hardened runtimes are surfaced, not scored

When a container runs under a recognized strong-isolation runtime (gVisor /
`runsc`, Kata Containers, or Firecracker), the scorecard names it as an
informational line. Scoring stays runtime-agnostic on purpose: a container can
name a hardened runtime and still be misconfigured, so no points are awarded for
the runtime name. The dimension scorers remain the authority on the score.

## Grade a Terraform plan

`--terraform PATH` grades the container workloads a Terraform configuration
declares, before `terraform apply` reaches a cluster or an AWS account. It
consumes `terraform show -json` output, so it is structured and daemon-free:

```bash
terraform show -json plan.tfplan > plan.json
ironctl scan --terraform plan.json
ironctl scan --terraform plan.json --min-score 75   # CI gate
ironctl scan --terraform plan.json --sarif tf.sarif # GitHub code scanning

ironctl scan --terraform ./infra                    # runs `terraform show -json` for you
```

Pass a `terraform show -json` JSON file (a plan or state export) directly, or a
Terraform directory, where `ironctl` runs `terraform show -json` against the
current state for you. It walks the root module and every child module and grades
two workload classes:

| Source | What is graded |
|---|---|
| `kubernetes_*` resources | `kubernetes_pod`, `kubernetes_deployment`, `kubernetes_stateful_set`, `kubernetes_daemon_set`, `kubernetes_job`, `kubernetes_cron_job`, `kubernetes_replication_controller` (and their `_v1` aliases). The provider embeds a pod spec, graded through the same dimension set as `--k8s`. |
| `aws_ecs_task_definition` | each entry in `container_definitions`, with the task-level `network_mode` / `pid_mode` / `ipc_mode` folded in. |

The plan grade is the **weakest** workload: a plan is only as isolated as its
most-exposed container, and every workload's score is listed in the notes.
Network egress depends on a `NetworkPolicy` (Kubernetes) or a security group
(ECS) that a workload spec does not carry, so it is graded conservatively, the
honest static ceiling. Missing tooling or a malformed document fails **open** (a
clear diagnostic and exit 0) so an opt-in CI step never crashes the build; once
workloads are graded, `--min-score` still trips on a low posture.

## Grade an AWS ECS task definition

`--ecs PATH` grades the container contract of an AWS ECS task definition. This is
the **live** counterpart to `--terraform`: Terraform grades an
`aws_ecs_task_definition` expressed in HCL/plan, while `--ecs` grades the
**registered** JSON that most AWS-console, CDK, and Copilot users actually have but
never express as Terraform. No AWS API call is made by the scan itself; you feed it
JSON you already have (or fetch once with the AWS CLI):

```bash
aws ecs describe-task-definition --task-definition webapp > taskdef.json
ironctl scan --ecs taskdef.json
ironctl scan --ecs taskdef.json --min-score 75    # CI gate
ironctl scan --ecs taskdef.json --sarif ecs.sarif # GitHub code scanning

ironctl scan --ecs ./taskdefs                      # weakest-container rollup over a dir
```

It accepts three input shapes:

| Input | Shape |
|---|---|
| `aws ecs describe-task-definition` output | a top-level `{ "taskDefinition": { "containerDefinitions": [...] } }` wrapper |
| a raw registered / authored task def | `containerDefinitions[]` at the JSON root (CDK / Copilot / hand-written API JSON) |
| a directory | every `*.json` task def in it, graded as one **weakest-container** rollup |

Each `containerDefinitions[]` entry is graded on the same dimensions as every other
source: non-root `user`, `linuxParameters.capabilities.{add,drop}`, `privileged`,
`readonlyRootFilesystem`, seccomp (via `dockerSecurityOptions`), and the task-level
`networkMode` / `pidMode` / `ipcMode`. `networkMode: host` is the worst;
`awsvpc` and `bridge` are egress-capable NICs (not host); ECS applies Docker's
**default** seccomp profile unless a `dockerSecurityOption` disables it, so it is
graded `confined` by default. A host volume whose source path is the Docker control
socket is flagged as a full host-root escape primitive.

The task grade is the **weakest** container: a task is only as isolated as its
most-exposed container, and every container's score is listed in the notes. Because
the shared ECS scorer is the same one the `--terraform` `aws_ecs_task_definition`
path uses, the two entrypoints can never diverge. Network egress depends on a
security group that a task definition does not carry, so it is graded
conservatively, the honest static ceiling. A malformed document fails **open** (a
clear diagnostic and exit 0) so an opt-in CI step never crashes the build; once
containers are graded, `--min-score` still trips on a low posture.

## Grade a CloudFormation template

`--cloudformation PATH` grades the `AWS::ECS::TaskDefinition` resources declared in
an [AWS CloudFormation](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-ecs-taskdefinition.html)
template, in YAML or JSON, straight from the file. It is the infrastructure-as-code
sibling of `--ecs`: `--ecs` grades a **registered** task definition, `--cloudformation`
grades the **template** that will create it, so a weak task fails review before the
stack is ever deployed. No AWS API call is made:

```bash
ironctl scan --cloudformation template.yaml
ironctl scan --cloudformation template.yaml --min-score 75    # CI gate
ironctl scan --cloudformation template.json --sarif cfn.sarif # GitHub code scanning

ironctl scan --cloudformation ./templates                     # weakest-container rollup over a dir
```

It accepts three input shapes:

| Input | Shape |
|---|---|
| a CloudFormation template | YAML or JSON, with one or more `AWS::ECS::TaskDefinition` resources under `Resources` |
| a template with several task defs | every task definition is graded and rolled up to the **weakest** container |
| a directory | every `*.yaml` / `*.yml` / `*.json` / `*.template` file in it, graded as one **weakest-container** rollup |

Each `ContainerDefinitions[]` entry is graded on the exact same dimensions as the
`--ecs` and `--terraform` `aws_ecs_task_definition` paths, through the **shared ECS
scorer**: non-root `User`, `LinuxParameters.Capabilities.{Add,Drop}`, `Privileged`,
`ReadonlyRootFilesystem`, seccomp (via `DockerSecurityOptions`), and the task-level
`NetworkMode` / `PidMode` / `IpcMode`. CloudFormation's PascalCase properties map
onto the same contract, so a template grades **identically** to the registered JSON
of the same task, and the three entrypoints can never diverge.

CloudFormation intrinsics — `!Ref`, `!Sub`, `!GetAtt`, the `Fn::*` family, and their
`{ "Ref": ... }` long forms — cannot be resolved without the deployed stack, so any
graded field they cover is treated as **unset** and graded fail-closed (an unknown
posture is insecure). The scan notes when a template used intrinsics so you can
verify the resolved stack matches. The template grade is the **weakest** container,
with every container's score in the notes. A malformed template fails **open** (a
clear diagnostic and exit 0) so an opt-in CI step never crashes the build; once
containers are graded, `--min-score` still trips on a low posture.

## Grade a Pulumi program

`--pulumi PATH` grades the container workloads a [Pulumi](https://www.pulumi.com/)
program declares — **before** `pulumi up` — straight from the program's own JSON
output. It is the Pulumi sibling of `--terraform`: Terraform grades a
`terraform show -json` plan; Pulumi grades a `pulumi stack export` checkpoint or a
`pulumi preview --json` plan. No cloud credentials and no external binary are
needed — the output is plain JSON you already have:

```bash
pulumi stack export > stack.json
ironctl scan --pulumi stack.json
ironctl scan --pulumi stack.json --min-score 75      # CI gate

pulumi preview --json > preview.json
ironctl scan --pulumi preview.json                   # grade the plan before apply

ironctl scan --pulumi ./stacks                        # weakest-workload rollup over a dir
```

It accepts the shapes a Pulumi user actually has:

| Input | Shape |
|---|---|
| `pulumi stack export` | a checkpoint whose `deployment.resources[]` carry each resource's typed inputs |
| `pulumi preview --json` | a plan whose `steps[].newState` carry the same per-resource shape |
| a directory | every `*.json` file in it, graded as one **weakest-workload** rollup |

Two resource classes are graded, each through the **shared scorer** the matching
input mode already uses — so a program grades **identically** to the equivalent
Terraform / ECS / Kubernetes input of the same workload:

- **Kubernetes** (`kubernetes:*:Pod` / `Deployment` / `StatefulSet` / `DaemonSet` /
  `ReplicaSet` / `ReplicationController` / `Job` / `CronJob`). Pulumi's kubernetes
  inputs **are** the Kubernetes API object, so they map through the same pod-spec
  dimension set as `--k8s` / `--helm`.
- **AWS ECS task definitions** — the classic `aws:ecs/taskDefinition:TaskDefinition`
  (where `containerDefinitions` is a JSON-encoded string, exactly like Terraform)
  and the native `aws-native:ecs:TaskDefinition` (a real array, like the live/CFN
  shape). Both fold into the same ECS scorer as `--ecs` / `--terraform`.

The program grade is the **weakest** workload (a program is only as isolated as its
most-exposed container), with every workload's score in the report notes. As with
`--k8s`/`--helm`, Kubernetes egress depends on a `NetworkPolicy` a pod spec does not
carry, so it is graded conservatively (the honest static ceiling). A malformed file
fails **open** (a clear diagnostic and exit 0) so an opt-in CI step never crashes
the build; once workloads are graded, `--min-score` still trips on a low posture.

## Grade a Google Cloud Run service

`--cloudrun PATH` grades a [Google Cloud Run](https://cloud.google.com/run)
service spec — a Knative `Service` document (`serving.knative.dev/v1`) — before it
reaches a project. Cloud Run specs are plain YAML, so it is structured and
daemon-free, with no `gcloud` login and no external binary:

```bash
gcloud run services describe SVC --format=export > svc.yaml
ironctl scan --cloudrun svc.yaml
ironctl scan --cloudrun svc.yaml --min-score 80   # CI gate
ironctl scan --cloudrun ./run-services            # a directory of service YAMLs
```

Pass a single Knative `Service` YAML (the `gcloud ... --format=export` output, or a
hand-authored spec), a multi-document `---`-separated stream, or a directory of
them (the weakest service governs). Cloud Run's revision template carries a
Kubernetes-shaped pod spec at `spec.template.spec.containers[]`, so it reuses the
same pod-spec dimension set as `--k8s`/`--helm` and then folds in Cloud Run's
**managed-runtime guarantees**:

| Dimension | How Cloud Run is graded |
|---|---|
| Non-root user | graded on the revision's `securityContext.runAsNonRoot` / `runAsUser`; unset means the image default (root), scored fail-closed. Cloud Run runs the container as its configured user. |
| Dropped capabilities | the managed sandbox does not permit privileged mode or added capabilities, so the restricted managed set is credited unless the spec explicitly adds one. |
| Seccomp | every container is sandboxed (gen1 gVisor / gen2 microVM), so the syscall surface is filtered by default — credited as confined unless the spec sets `seccompProfile: Unconfined`. |
| Network isolation | Cloud Run egress is **managed**: allowed by default and restrictable via VPC egress settings, but never `network=none`. Graded as egress-capable (the honest ceiling). |
| Read-only rootfs | graded on `securityContext.readOnlyRootFilesystem`; unset means writable, scored fail-closed. |
| docker.sock | impossible on Cloud Run (no host bind mounts) — passes by construction. |
| Host namespaces | privileged mode and host PID/IPC/network are not permitted — passes by construction. |

Because the platform forbids privileged mode and host namespaces, blocks the
docker socket, and sandboxes every container, a Cloud Run service starts from a
strong floor. What stays your job is running as **non-root** with a **read-only
rootfs**. A fully hardened service tops out at **89/100 (grade B)**: egress is
managed and can never be `network=none`, the same honest ceiling any
egress-capable managed runtime hits. The `gen1` execution environment is surfaced
as gVisor (`runsc`) informationally; scoring stays runtime-agnostic. Load or parse
failures fail **open** (a clear diagnostic and exit 0) so an opt-in CI step never
crashes the build; once services are graded, `--min-score` still trips on a low
posture.

## Grade an Azure container group

`--azure PATH` grades the `Microsoft.ContainerInstance/containerGroups` declared in
an [Azure Resource Manager](https://learn.microsoft.com/azure/container-instances/)
template, an `az container show` / deployment JSON, or a bare containerGroup object.
It completes the big-3 cloud coverage alongside `--cloudrun` (GCP) and `--ecs` (AWS).
Azure output is plain JSON, so it is structured and daemon-free, with no `az` login
and no external binary:

```bash
az container show -g rg -n mygroup > containergroup.json
ironctl scan --azure containergroup.json
ironctl scan --azure arm-template.json --min-score 75   # CI gate
ironctl scan --azure ./deployments                      # a directory of *.json
```

Pass a single ARM template, an `az container show` object, or a directory of JSON
files (the weakest container in the weakest group governs). Each container's
`securityContext` is graded through the **same pod-spec scorer** as `--k8s` /
`--cloudrun`, then Azure Container Instances' **managed-runtime floors** are folded
in:

| Dimension | How ACI is graded |
|---|---|
| Non-root user | graded on `securityContext.runAsUser`; unset means the image default (root), scored fail-closed. |
| Dropped capabilities | graded on `securityContext.capabilities.{add,drop}`. ACI **lets a container add capabilities**, so cap-drop is NOT credited by the platform — express `capabilities.drop: [ALL]` to earn it. |
| Seccomp | the managed runtime applies a default profile — credited as confined unless overridden; a custom `seccompProfile` string counts as applied. |
| Network isolation | ACI egress is **managed** (public or private IP), never `network=none`. Graded as egress-capable (the honest ceiling). |
| Read-only rootfs | **not expressible** — ACI's `securityContext` has no read-only-rootfs field, so this dimension is always graded fail-closed. |
| docker.sock | impossible on ACI (no host bind mounts) — passes by construction. |
| Host namespaces | each container group runs in a dedicated **Hyper-V-isolated VM**; host PID/IPC/network are unreachable — passes by construction. Privileged is not permitted on the Standard SKU. |

Because the platform blocks host namespaces and the docker socket and forbids
privileged (unless the spec explicitly sets it), an ACI container starts from a
strong floor. What stays your job is running as **non-root** and dropping
**capabilities**. A fully hardened ACI container tops out at **79/100 (grade B)** —
one dimension (read-only rootfs, 10 pts) below Cloud Run's 89/B, because ACI cannot
express a read-only root filesystem. ARM expressions (`"[parameters(...)]"` /
`"[variables(...)]"`) cannot be resolved without the deployment, so any graded field
they cover is treated as **unset** and graded fail-closed; the scan notes when a
template used them. The group grade is the **weakest** container, with every
container's score in the notes. Load or parse failures fail **open** (a clear
diagnostic and exit 0) so an opt-in CI step never crashes the build; once containers
are graded, `--min-score` still trips on a low posture.

## Grade an AWS App Runner service

`--app-runner PATH` grades an [AWS App Runner](https://docs.aws.amazon.com/apprunner/)
service straight from its `aws apprunner describe-service` JSON, a raw `Service`
object, or a directory of them. It rounds out the AWS coverage alongside `--ecs`
(task definitions) and joins the managed-runtime family with `--cloudrun` (GCP) and
`--azure` (ACI). App Runner output is plain JSON, so it is structured and
daemon-free, with no AWS call and no external binary:

```bash
aws apprunner describe-service --service-arn "$ARN" > service.json
ironctl scan --app-runner service.json
ironctl scan --app-runner service.json --min-score 40   # CI gate
ironctl scan --app-runner ./services                    # a directory of *.json
```

App Runner runs every service on **AWS Fargate (a Firecracker microVM)**, graded
through the **same managed-runtime model** as `--cloudrun` / `--azure`:

| Dimension | How App Runner is graded |
|---|---|
| Non-root user | **not expressible** — App Runner's service config has no user field; it respects the image `USER`, which the config cannot override. Graded fail-closed. |
| Dropped capabilities | Fargate **retains Docker's default capability set** and App Runner gives no knob to drop it — unlike Cloud Run, capabilities are NOT credited as dropped. |
| Seccomp | Fargate applies Docker's default profile — credited as confined. |
| Network isolation | App Runner egress is **managed** (public by default, or via a VPC connector), never `network=none`. Graded as egress-capable. |
| Read-only rootfs | **not expressible** — App Runner has no read-only-rootfs field (like ACI). Graded fail-closed. |
| docker.sock | impossible on App Runner (no host bind mounts) — passes by construction. |
| Host namespaces | each service runs in a dedicated **Firecracker microVM**; host PID/IPC/network are unreachable and privileged mode is not permitted — passes by construction. |

App Runner's service configuration exposes **no container `securityContext` at all**,
so three dimensions (non-root, capabilities, read-only rootfs) cannot be hardened and
are graded fail-closed. A fully hardened App Runner service therefore tops out at
**48/100 (grade D)** on the managed-runtime floors alone — honestly lower than Cloud
Run's 89/B and ACI's 79/B, because App Runner buys you strong microVM isolation but
almost no config-expressible hardening surface. The Firecracker boundary is surfaced
as an informational note but awards no points (scoring is runtime-agnostic). The
deployment grade is the **weakest** service, with every service's score in the notes.
Load or parse failures fail **open** (a clear diagnostic and exit 0) so an opt-in CI
step never crashes the build; once a service is graded, `--min-score` still trips on a
low posture.

## Grade an Azure Bicep template

`--bicep PATH` compiles an [Azure Bicep](https://learn.microsoft.com/azure/azure-resource-manager/bicep/)
template to ARM and grades the `Microsoft.ContainerInstance/containerGroups` it
declares. Bicep transpiles **1:1** to an ARM deployment template, so once compiled it
is graded through the **exact same** managed-runtime path as `--azure`. Point it at a
`.bicep` file or a directory of them:

```bash
ironctl scan --bicep main.bicep
ironctl scan --bicep main.bicep --min-score 75   # CI gate
ironctl scan --bicep ./infra                     # a directory of *.bicep
```

Compilation prefers the standalone `bicep build --stdout` and falls back to
`az bicep build` (the Bicep compiler shipped as an Azure CLI extension) when the
standalone binary is absent — pass `--bicep-bin` / `--az-bin` to override. It is
offline and daemon-free: the compiler transpiles locally with no Azure login. Because
the input compiles to the same ARM `containerGroups` document `--azure` grades, the
scoring is identical: each container's `securityContext` runs through the shared
pod-spec scorer, ACI's managed-runtime floors are folded in, and a fully hardened ACI
container tops out at **79/100 (grade B)** — see [Grade an Azure container
group](#grade-an-azure-container-group) for the per-dimension breakdown. The template
grade is the **weakest** container across the compiled groups. ARM expressions that
survive compilation (`"[parameters(...)]"`) are graded fail-closed and noted. A
compile or parse failure (the `bicep`/`az` binary is absent or the template is
malformed) fails **open** — a clear diagnostic and exit 0 — so an opt-in CI step never
crashes the build; once containers are graded, `--min-score` still trips on a low
posture.

## Grade an AWS CDK app

`--cdk PATH` grades an [AWS CDK](https://docs.aws.amazon.com/cdk/) app by way of the
CloudFormation template its `cdk synth` step emits. The CDK is a program
(TypeScript/Python/Go/...) that **synthesizes** standard CloudFormation, so once
synthesized it is graded through the **exact same** shared ECS scorer as
`--cloudformation`. Point it at a CDK app directory (one containing `cdk.json`), a
pre-synthesized template file, or a synthesized `cdk.out` cloud assembly:

```bash
ironctl scan --cdk ./my-cdk-app                  # synthesize the app, then grade
ironctl scan --cdk ./my-cdk-app --min-score 75   # CI gate
ironctl scan --cdk cdk.out/MyStack.template.json # a pre-synthesized template
ironctl scan --cdk ./cdk.out                     # a synthesized cloud assembly
```

A CDK app directory is synthesized with `cdk synth` (pass `--cdk-bin` to override the
binary); when the `aws-cdk` binary is absent it falls back to an already-synthesized
`cdk.out` inside the app, so a CI that checks in its cloud assembly still grades with no
Node install. It is offline and daemon-free: synthesis is local, no AWS login. Because
the synthesized output is the same `AWS::ECS::TaskDefinition` CloudFormation
`--cloudformation` grades, the scoring is identical: each container definition runs
through the shared ECS scorer, and a fully hardened task definition tops out at
**89/100 (grade B)** — see [Grade a CloudFormation
template](#grade-a-cloudformation-template) for the per-dimension breakdown. The app
grade is the **weakest** container across every synthesized template. Unresolved CDK
tokens / CloudFormation intrinsics (`!Ref`/`Fn::...`/`${Token[...]}`) are graded
fail-closed and noted. A synth or parse failure (the `cdk` binary is absent with no
`cdk.out`, or the app is malformed) fails **open** — a clear diagnostic and exit 0 — so
an opt-in CI step never crashes the build; once containers are graded, `--min-score`
still trips on a low posture.

## Grade an AWS SAM template

`--sam PATH` grades the container workloads declared in an [AWS SAM](https://aws.amazon.com/serverless/sam/)
(Serverless Application Model) template. A SAM template carries a
`Transform: AWS::Serverless-*` header and, when deployed, is expanded to
CloudFormation — but SAM is a strict **superset** of CloudFormation: any resource that
is not an `AWS::Serverless::*` type (an `AWS::ECS::TaskDefinition`, for instance) passes
through the transform **unchanged**, in ordinary CloudFormation form. So an ECS/Fargate
task definition in a SAM app is graded through the **exact same** shared ECS scorer as
`--cloudformation`, with no `sam build`/transform step required. Point it at a SAM
template file or a directory of them:

```bash
ironctl scan --sam ./template.yaml                 # grade the SAM template's ECS task defs
ironctl scan --sam ./template.yaml --min-score 75  # CI gate
ironctl scan --sam ./sam                            # a directory of SAM templates (weakest wins)
ironctl scan --sam ./template.yaml --sarif scan.sarif  # upload to code-scanning
```

It is offline and daemon-free: the template is parsed on disk, with no AWS login and no
deploy. Because a SAM template's ECS task definitions are already native
CloudFormation, the scoring is identical to `--cloudformation`: each container
definition runs through the shared ECS scorer, and a fully hardened task definition
tops out at **89/100 (grade B)** — see [Grade a CloudFormation
template](#grade-a-cloudformation-template) for the per-dimension breakdown. The
template grade is the **weakest** container across every task definition. Unresolved
CloudFormation intrinsics (`!Ref`/`!Sub`/`Fn::...`) are graded fail-closed and noted.

`AWS::Serverless::Function` resources — including `PackageType: Image`
Lambda-container functions — run on the managed Lambda runtime, which exposes none of
the ECS task-definition isolation fields (privileged, user, read-only rootfs, host
namespaces) this scorer grades, so they are not scored; the real, user-controllable
container-isolation surface in a SAM app is its ECS/Fargate task definitions. A load or
parse failure (an unreadable path or a malformed template) fails **open** — a clear
diagnostic and exit 0 — so an opt-in CI step never crashes the build; once containers
are graded, `--min-score` still trips on a low posture.

## Grade a kustomization

`--kustomize DIR` renders a [Kustomize](https://kustomize.io/) directory with
`kustomize build` (falling back to `kubectl kustomize` when the standalone binary
is absent) and grades the isolation posture of the flattened workloads. It is
offline and daemon-free: overlays are flattened locally, with no cluster and no
`kubectl apply`:

```bash
ironctl scan --kustomize ./overlays/prod
ironctl scan --kustomize ./overlays/prod --min-score 80   # CI gate
ironctl scan --kustomize ./base --sarif scan.sarif        # upload to code-scanning
```

Kustomize flattens the base plus every overlay patch into the same multi-document
manifest stream that `--k8s`/`--helm` grade, so `--kustomize` reuses the exact
pod-spec dimension set and the same weakest-link rollup: the **build grade is the
weakest rendered workload** (a kustomization is only as isolated as its
most-exposed pod), and every workload's score is listed in the report notes. This
grades the manifests **after** overlay patches apply — the same YAML the cluster
would receive — so a `securityContext` a production overlay strips (or one it
adds) is reflected in the grade.

As with `--helm`, network egress depends on a `NetworkPolicy` that a pod spec does
not carry, so it is graded conservatively (the honest static ceiling). Render
failures (neither `kustomize` nor `kubectl` on your PATH, or a `build` error) fail
**open** — a clear diagnostic and exit 0 — so an opt-in CI step never crashes the
build; once the kustomization renders, `--min-score` still trips on a low posture.
Point at a non-default renderer with `--kustomize-bin` / `--kubectl-bin` (or the
`KUSTOMIZE` / `KUBECTL` environment variables).

## Grade a Kubernetes Admission Review

`--k8s-admission FILE` moves scan from a static report to an **in-cluster
enforcement gate**. It reads a Kubernetes [`admission.k8s.io/v1`
AdmissionReview](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
request &mdash; the exact JSON body the API server POSTs to a validating webhook
&mdash; grades the admitted workload through the **same pod-spec scorer as
`--k8s`**, and gates admission on `--min-score`. Pass `-` to read the request from
stdin (the webhook shape):

```bash
# Local / CI: inspect what a webhook WOULD decide (scorecard + exit gate)
ironctl scan --k8s-admission review.json --min-score 80

# Webhook backend: emit the AdmissionReview response the API server expects
ironctl scan --k8s-admission - --admission-response --min-score 80 < review.json
```

By default it prints the familiar scorecard (table, or `--json`/`--md`/`--sarif`)
and exits non-zero below `--min-score`. With `--admission-response` it instead
emits an `admission.k8s.io/v1` AdmissionReview **response** to stdout &mdash;
`response.allowed` set from the grade, the request `uid` echoed (the API server
rejects a mismatch), and a `403` status message on deny &mdash; so a thin
`ValidatingWebhookConfiguration` backend can serve stdout verbatim as its HTTP
response body. Because the object under admission is fed straight to the `--k8s`
scorer, the admission decision is **identical** to what `ironctl scan --k8s` would
report for the same manifest (parity is unit-tested).

Unlike the fail-**open** batch modes (`--helm`, `--terraform`, `--kustomize`), an
admission gate is fail-**closed**: an unreadable or unparseable review, a missing
request, or an object with nothing to grade **denies** admission and exits
non-zero (the deny JSON is still written first in `--admission-response` mode, so
the webhook always returns a valid body). A gate must never silently admit what it
could not inspect. As with `--k8s`, network egress depends on a `NetworkPolicy`
not visible in the pod spec, so the honest static ceiling applies &mdash; size
`--min-score` accordingly (a fully hardened pod tops out around a strong **B**).

## Generate an admission policy from the findings

`--emit-policy=kyverno|gatekeeper` moves scan from *grading* a workload to
*generating the guardrail*. Point it at a Kubernetes manifest and instead of a
scorecard it emits ready-to-apply admission-policy YAML that **blocks the exact
controls the manifest failed** &mdash; the delta between its grade and a hardened
**100/A**. It reuses the **same pod-spec scorer** as `--k8s` / `--k8s-admission`,
so nothing is re-derived: one failed dimension becomes one policy rule.

```bash
# Kyverno: one ClusterPolicy, one Enforce rule per failed control
ironctl scan --k8s pod.yaml --emit-policy=kyverno | kubectl apply -f -

# Gatekeeper: one ConstraintTemplate (Rego) + Constraint per failed control
ironctl scan --k8s pod.yaml --emit-policy=gatekeeper | kubectl apply -f -
```

| Scorer dimension | Enforced control | Kyverno rule | Gatekeeper template |
| --- | --- | --- | --- |
| Non-root user | `runAsNonRoot: true` | `require-run-as-non-root` | `K8sRequireRunAsNonRoot` |
| Dropped capabilities | `capabilities.drop: [ALL]`, not privileged | `require-drop-all-capabilities` | `K8sRequireDropAllCaps` |
| Seccomp profile | `seccompProfile.type: RuntimeDefault\|Localhost` | `require-seccomp` | `K8sRequireSeccomp` |
| Read-only rootfs | `readOnlyRootFilesystem: true` | `require-readonly-rootfs` | `K8sRequireReadOnlyRootFs` |
| Network isolation | `hostNetwork: false` | `disallow-host-network` | `K8sDisallowHostNetwork` |
| Host namespaces | `hostPID: false`, `hostIPC: false` | `disallow-host-namespaces` | `K8sDisallowHostNamespaces` |
| No docker.sock | no runtime-socket hostPath mount | `disallow-runtime-socket-mounts` | `K8sDisallowRuntimeSocket` |

The Kyverno policy matches `Pod` (Kyverno auto-generates the matching rules for
Deployments, StatefulSets, and the other pod controllers) with
`validate.failureAction: Enforce`, so a violating workload is rejected at admission
time. For a multi-document manifest a control is enforced when **any** workload
fails it &mdash; the emitted set is the union of every workload's gaps. As with
`--k8s`, full egress lockdown (`network=none`) needs a `NetworkPolicy`, which
admission control cannot express, so the `hostNetwork` guardrail carries a note to
that effect. This turns the scan into a policy-generation tool: grade once, then
enforce the delta cluster-wide so the gap can never reopen.

## Grade a Dockerfile statically

`--dockerfile FILE` grades a Dockerfile with no daemon and no image pull, so it
runs at authoring time and in CI on a pull request, before anything is built. It
opens the shift-left surface: catch a leaked credential, an unpinned base, or a
root default in review instead of in production.

```bash
ironctl scan --dockerfile Dockerfile
ironctl scan --dockerfile Dockerfile --min-score 80   # CI gate
ironctl scan --dockerfile Dockerfile --sarif df.sarif # GitHub code scanning
```

It grades a different, authoring-time dimension set, the postures a Dockerfile
author actually controls:

| Dimension | Weight | What earns the points |
|---|---|---|
| Non-root USER | 25 | the final stage sets a non-root `USER` (a root default means every runtime escape starts as uid 0) |
| Pinned base image | 20 | `FROM image@sha256:...` (a mutable tag scores partial; `:latest` fails) |
| No secrets in ENV/ARG | 20 | no secret-like literal is baked into an `ENV`/`ARG` value |
| COPY over remote/opaque ADD | 12 | no remote `ADD` (network fetch into a layer) or archive-extracting `ADD`; use `COPY` |
| No world-writable files | 10 | no `chmod 777` / `o+w` |
| HEALTHCHECK defined | 8 | a `HEALTHCHECK` so an orchestrator can spot a wedged container |
| Layer / cache hygiene | 5 | package installs prune their cache in the same layer |

### The honest static ceiling

A static scan cannot see runtime hardening. Dropped capabilities, seccomp,
`network=none`, a read-only rootfs, the docker.sock mount, and shared host
namespaces are all set at `docker run` or orchestration time and are simply not
expressible in a Dockerfile, so this mode does not grade them and never fakes a
pass for them. A perfect 100/A Dockerfile is necessary but not sufficient: it
still needs a runtime scan (`ironctl scan <container>`) to grade the isolation
the file cannot express. Every Dockerfile scorecard prints this reminder.

Multi-stage builds are supported: the final stage (the shipped image) is graded
for its `USER`, base pin, and `HEALTHCHECK`, while secret and remote-fetch checks
run across every stage. Full multi-stage dataflow analysis is out of scope; this
mode grades containment posture, not general Dockerfile linting.

## Use as a pre-commit hook

The Dockerfile mode ships as a [pre-commit](https://pre-commit.com) hook, so every
Dockerfile is graded automatically on commit, with no daemon and no image pull.
Add this to any repo's `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/IronSecCo/ironclaw
    rev: v0.1.x                     # pin a released tag
    hooks:
      - id: ironclaw-scan-dockerfile
        args: [--min-score=80]      # optional: fail the commit below grade B
```

Then:

```bash
pre-commit install                                   # run on every commit
pre-commit run ironclaw-scan-dockerfile --all-files  # grade every Dockerfile now
```

The hook uses `language: golang`, so pre-commit builds the `ironctl` binary from
source in an isolated environment the first time it runs. No separate install step
is required, and there is nothing to keep on your `PATH`.

`args: [--min-score=N]` turns the hook into a gate: the commit fails if any matched
Dockerfile scores below `N` on the 0 to 100 scale (the grade bands are A >= 90,
B >= 75, C >= 50, D >= 25, F below). Omit `args` to run informationally: the
scorecard still prints on every commit, but a low score never blocks it. When
several Dockerfiles are staged, the hook grades each and fails on the first that
falls below the threshold, naming it.

By default the hook matches `Dockerfile`, `Dockerfile.*`, and `*.Dockerfile`
anywhere in the tree. Override `files` in your config to narrow or widen that.

IronClaw dogfoods this hook on its own repository. See its
[`.pre-commit-config.yaml`](https://github.com/IronSecCo/ironclaw/blob/main/.pre-commit-config.yaml),
which gates the three container images it ships at `--min-score=80`.

## Output formats

| Flag | What you get |
|---|---|
| (default) | a human-readable scorecard table |
| `--json` | the full report as JSON (schemaVersion 1.0), for pipelines and dashboards |
| `--fix` | print the concrete remediation for every failed dimension, plus a copy-pasteable hardened config (`--remediate` is an alias) |
| `--badge scan.svg` | a self-contained SVG badge you can drop into a README |
| `--badge-json badge.json` | a [shields.io endpoint](https://shields.io/badges/endpoint-badge) JSON file for a live, self-updating README badge |
| `--sarif scan.sarif` | a SARIF 2.1.0 log you can upload to GitHub code scanning (findings land in the repo Security tab) |
| `--md` | a shareable markdown block for a README or blog post |
| `--compare A B` | grade two containers and print a side-by-side isolation-posture diff |
| `--min-score N` | exit non-zero when the score is below N (a CI gate) |

## Compare two containers

`--compare` grades two running containers and prints a side-by-side diff: each of
the seven dimensions with both scores, a per-dimension winner marker, the overall
grade and point delta, and a one-line verdict naming the more hardened target and
why. It reuses the same adapters and scorer as a single scan, so the numbers match
what `ironctl scan <container>` reports for each side.

Use it to pick a base image (alpine vs distroless), to prove a hardening change
moved the needle, or to generate the data behind an "X vs Y container isolation"
comparison page.

```bash
ironctl scan --compare my-hardened-ctr my-default-ctr
```

```
IronClaw containment scan: comparison

  A: my-hardened-ctr (docker)  90/100 grade A
  B: my-default-ctr (docker)  40/100 grade F

DIMENSION                   A          B         WINNER
Non-root user (uid != 0)    [+] 15/15  [x] 0/15  < A
Dropped capabilities        [+] 20/20  [x] 4/20  < A
...
OVERALL                     90/100 A   40/100 F  A (+50)

  Verdict: A (`my-hardened-ctr`) is more hardened: 90/100 (grade A) vs 40/100
  (grade F), a 50-point lead; it leads on Dropped capabilities, ...
```

Add `--json` for the machine-readable diff (each side is a full scan report under
`a`/`b`, plus a `dimensions` delta array, `scoreDelta`, `winner`, and `verdict`),
or `--md` for a markdown table you can drop straight into a blog post or README.
The two targets must be distinct; either target failing to inspect fails the whole
compare (fail-closed) rather than printing a half diff.

## Sandbox Isolation Score badge

Show your container's containment grade in your README, the same way a coverage or
build badge advertises code health. Two ways to do it, both free and static (no
server scans your image on every badge hit):

**1. A pinned static badge (zero infrastructure).** Point a plain
[shields.io static badge](https://shields.io/badges/static-badge) at your grade:

```markdown
![Sandbox Isolation](https://img.shields.io/badge/sandbox%20isolation-100%2F100%20A-3fb950)
```

**2. A live badge that updates itself.** Commit a small JSON file to your repo and
let shields.io render it. Regenerate the file whenever your container config
changes and the badge follows along:

```bash
# grade your container (or --compose FILE / --k8s FILE) and write the badge file
ironctl scan my-container --badge-json .ironclaw/sandbox-isolation.json
git add .ironclaw/sandbox-isolation.json && git commit -m "chore: refresh sandbox isolation badge"
```

Then embed it, swapping in your `OWNER/REPO/BRANCH/PATH`:

```markdown
[![Sandbox Isolation Score](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/OWNER/REPO/main/.ironclaw/sandbox-isolation.json)](https://ironsecco.github.io/ironclaw/scan/)
```

The score is pinned in the committed file at generation time, so a badge hit never
triggers a scan of a remote target. Grade to color follows the scorecard palette:
A is green, B and C are amber, D and F are red.

IronClaw dogfoods its own badge in the project README, generated from the sandbox
reference posture the isolation launcher applies to every session
(`.ironclaw/sandbox-posture.yml`, graded 100/100 A).

For a step-by-step walkthrough, including hosting the file, wiring it into CI, and the
reasoning behind the committed-file design, see the blog post
[Add a live Sandbox Isolation Score badge to your repo](blog/add-a-sandbox-isolation-score-badge-to-your-repo.md).

Want to see how your grade stacks up? Compare it against the
[Container Isolation Scores directory](scores/index.md), where 150+ of the most-pulled
public images are graded in their default configuration, or the ranked
[leaderboard](scores/leaderboard.md).

## GitHub code scanning (Security tab)

Emit [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
and upload it so every failed isolation dimension appears in your repo's
**Security > Code scanning** tab, right next to the findings from CodeQL and any
other scanner, with no IronClaw account required:

```bash
ironctl scan --compose docker-compose.yml --sarif ironclaw-scan.sarif
```

- One SARIF **rule** per graded dimension (non-root user, dropped capabilities,
  seccomp, network isolation, read-only rootfs, docker.sock, host namespaces),
  each carrying the concrete remediation as `help` text.
- One SARIF **result** per FAILED dimension, at `error` or `warning` level from
  the dimension's severity, anchored at the scanned config file (with a line
  region when derivable). A clean 100/A target emits **zero** results.
- A stable `partialFingerprints` value per (rule, file) so GitHub dedupes the
  same finding across runs instead of re-alerting.

The easiest way to upload is the [scan Action](scan-action.md) with
`upload-sarif: true`; it runs the scan and hands the SARIF to
`github/codeql-action/upload-sarif` for you. To upload from your own workflow,
grant `permissions: security-events: write` and call the same action:

```yaml
permissions:
  security-events: write   # required to upload SARIF
steps:
  - uses: github/codeql-action/upload-sarif@v3
    with:
      sarif_file: ironclaw-scan.sarif
      category: ironclaw-scan
```

SARIF emit is fail-open: if writing the file ever fails, the scan itself (and its
`--min-score` gate) is unaffected.

## Fix it, do not just grade it

`--fix` turns the audit into a prescription. For every dimension that did not
pass, it prints the exact config to set for the source you scanned (docker
flags, a compose service patch, or a Kubernetes securityContext), then assembles
one copy-pasteable hardened artifact that scores A when applied. It is
fail-closed and deterministic, and `--json` carries the same remediation under a
`remediation` key.

```
$ ironctl scan my-container --fix
  score:   23/100  grade F  (wide open)
  ... scorecard table ...

Remediation (6 dimension(s) to harden, my-container currently 23/100 grade F):

  [user.nonroot] Non-root user (uid != 0) (FAIL)
      fix: --user 65532:65532
      why: Pin a non-root uid so a container escape does not begin as host uid 0.
  [caps.dropped] Dropped capabilities (FAIL)
      fix: --cap-drop=ALL
      why: Drop every Linux capability; add back only what the workload provably needs.
  [docker.sock] No docker.sock exposure (FAIL)
      fix: remove the -v /var/run/docker.sock:... bind mount
      why: Mounting the container-runtime socket is a one-command host-root escape.
  ... one entry per failed dimension ...

Copy-pasteable hardened docker run (scores A/100 when applied):

docker run -d --name ic-hardened \
  --user 65532:65532 \
  --cap-drop=ALL \
  --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp \
  --network=none \
  nginx:alpine
# intentionally dropped from the original run: the docker.sock bind mount (host-root escape), --pid=host
```

Run that command and rescan: `ironctl scan ic-hardened` reports `100/100 grade
A`. For a compose service the snippet is a minimal patch to merge into the file;
for a Kubernetes manifest it is the pod-spec and container `securityContext`
fields to set (plus a note to add a default-deny egress NetworkPolicy, which the
pod spec cannot express).

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

## Compare with real scan data

Head-to-head reads that answer common questions using the same seven-dimension
grade this page describes:

- [Alpine vs Debian vs Ubuntu](blog/alpine-vs-debian-vs-ubuntu-container-isolation.md):
  does the base image change isolation? (Spoiler: all seven base images tie at 48.)
- [Docker default vs hardened](blog/docker-default-vs-hardened-container-isolation.md):
  the 48-point jump from a D to an A, flag by flag, across 151 images.
- [gVisor vs runc](blog/gvisor-vs-runc-container-isolation-compared.md): why they
  score the same on a config scan yet block a different number of live escape attempts.

Per-image hardening walkthroughs, default grade to hardened, with the exact `--fix` flags:

- [Harden a Postgres container](blog/harden-postgres-container-isolation.md): `postgres:17-alpine`, 48 to 100.
- [Harden a Redis container](blog/harden-redis-container-isolation.md): `redis:7-alpine`, 48 to 100.
- [Run untrusted Node.js code safely](blog/run-untrusted-nodejs-code-safely.md): `node:22-alpine`, 48 to 100.
- [Harden an nginx container](blog/harden-nginx-container-isolation.md): `nginx:1.27-alpine`, 48 to 89 (the honest proxy ceiling).
