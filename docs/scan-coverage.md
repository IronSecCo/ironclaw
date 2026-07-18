---
title: "What ironctl scan can grade: containers, Compose, Kubernetes, Helm, Kustomize, Terraform, CloudFormation, Cloud Run, ECS, Nomad, Dockerfiles"
description: "One page, every ironctl scan input mode, grouped by category. A copy-paste command and an isolation grade for a running container, a Compose service, a Kubernetes or Kustomize manifest, a Helm chart, a Terraform plan, a CloudFormation template, a Cloud Run service, an ECS task, a Nomad job, or a Dockerfile."
---

# What `ironctl scan` can grade

One tool, one grade, many inputs. `ironctl scan` reads a container's real isolation posture
and returns a **0&ndash;100 score with a letter grade**, from a running container down to the
infrastructure-as-code that will one day launch it. Every mode below runs read-only, needs no
account, and prints the exact flags that close the gap.

The modes are grouped by **the artifact you already have** &mdash; a live container, an
orchestrator manifest, a cloud runtime spec, or the infrastructure-as-code that defines it.
Jump to your category, then copy one command.

<!-- ---------------------------------------------------------------------------
  SCAN-MODE CARD CONTRACT  (taxonomy-agnostic; add a mode without a redesign)

  Each supported input mode is exactly one <li> in one of the category grids
  below, in this shape:

      - #### :icon-name:{ .lg .middle } <Mode name> { #scan-<slug> }

          ---

          <One line: what it reads and what it grades.>

          ```bash
          ironctl scan <the exact copy-paste command>
          ```

          [:octicons-arrow-right-24: <deep-link label>](<deep doc>)

  To ship a new mode: paste ONE <li> into the grid under the RIGHT category H3
  (Containers & images / Compose & orchestrators / Cloud runtimes /
  Infrastructure-as-Code), and add one row to the "Every mode, one engine"
  table. No template edit, no CSS. The heading id (`#scan-<slug>`) is the
  crawlable anchor; keep it kebab-case and stable. If a new mode does not fit an
  existing category, add a new H3 (a crawlable section) rather than overflowing
  a grid past ~5 cards.
---------------------------------------------------------------------------- -->

### Containers &amp; images { #modes-containers }

The container in front of you, whatever runtime launched it, and the Dockerfile that builds it.

<div class="grid cards" markdown>

-   #### :material-cube-outline:{ .lg .middle } Running container { #scan-container }

    ---

    Audits any live OCI container (Docker, Podman, nerdctl, containerd): user, capabilities,
    seccomp, rootfs, network, privilege, and mounts.

    ```bash
    ironctl scan my-container
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

-   #### :material-cog-sync-outline:{ .lg .middle } Alternate runtimes { #scan-runtime }

    ---

    Same grade for Podman, nerdctl, and containerd. Auto-detected by default; force one with
    `--runtime` when a host runs several.

    ```bash
    ironctl scan --runtime podman my-container
    ```

    [:octicons-arrow-right-24: Supported runtimes](scan.md#supported-runtimes)

-   #### :material-file-code-outline:{ .lg .middle } Dockerfile (static) { #scan-dockerfile }

    ---

    Grades authoring-time posture straight from a `Dockerfile` &mdash; no daemon, no build.
    Catches `USER root`, missing drops, and writable layers in CI.

    ```bash
    ironctl scan --dockerfile Dockerfile
    ```

    [:octicons-arrow-right-24: Grade a Dockerfile statically](scan.md#grade-a-dockerfile-statically)

</div>

### Compose &amp; orchestrators { #modes-orchestrators }

Declared workloads, before you ever run `up`, `apply`, or `job run`. Multi-workload inputs
roll up to the **weakest** workload they produce.

<div class="grid cards" markdown>

-   #### :simple-docker:{ .lg .middle } Docker Compose { #scan-compose }

    ---

    Grades one service in a `docker-compose.yml` from its declared config, before you ever
    run `up`. Point `--service` at the workload you care about.

    ```bash
    ironctl scan --compose docker-compose.yml --service web
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

-   #### :simple-kubernetes:{ .lg .middle } Kubernetes manifest { #scan-k8s }

    ---

    Grades the pod spec in a Kubernetes YAML (`securityContext`, caps, host namespaces) so a
    weak manifest fails review, not production.

    ```bash
    ironctl scan --k8s pod.yaml
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

-   #### :simple-helm:{ .lg .middle } Helm chart { #scan-helm }

    ---

    Renders a chart with `helm template` (directory or `.tgz`) and grades the **weakest**
    workload it produces, so a chart is safe before it is installed.

    ```bash
    ironctl scan --helm ./chart
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

-   #### :material-layers-triple-outline:{ .lg .middle } Kustomize overlay { #scan-kustomize }

    ---

    Renders an overlay with `kustomize build` (or `kubectl kustomize`) and grades the
    **weakest** workload it produces, so a patched overlay is graded before it hits a cluster.

    ```bash
    ironctl scan --kustomize ./overlays/prod
    ```

    [:octicons-arrow-right-24: Grade a kustomization](scan.md#grade-a-kustomization)

-   #### :simple-nomad:{ .lg .middle } Nomad job { #scan-nomad }

    ---

    Grades the docker-driver tasks in a HashiCorp Nomad job spec and rolls up to the weakest
    task, so a job is graded before `nomad job run`.

    ```bash
    ironctl scan --nomad job.nomad
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

</div>

### Cloud runtimes { #modes-cloud }

Managed container runtimes, graded from their service or task spec &mdash; no cloud account,
no API call.

<div class="grid cards" markdown>

-   #### :simple-googlecloud:{ .lg .middle } Google Cloud Run { #scan-cloudrun }

    ---

    Grades a Cloud Run service's container config straight from its Knative Service YAML
    (`gcloud run services describe --format=export`), no Google account needed.

    ```bash
    ironctl scan --cloudrun svc.yaml
    ```

    [:octicons-arrow-right-24: Grade a Cloud Run service](scan.md#grade-a-google-cloud-run-service)

-   #### :fontawesome-brands-aws:{ .lg .middle } Amazon ECS { #scan-ecs }

    ---

    Grades an ECS task definition's container contract from a live or registered
    `describe-task-definition`, no AWS call needed. Rolls up to the weakest container.

    ```bash
    ironctl scan --ecs taskdef.json
    ```

    [:octicons-arrow-right-24: Grade an ECS task definition](scan.md#grade-an-aws-ecs-task-definition)

-   #### :material-microsoft-azure:{ .lg .middle } Azure Container Instances { #scan-azure }

    ---

    Grades the `Microsoft.ContainerInstance/containerGroups` in an ARM template or
    `az container show` JSON with the managed-runtime model, rolling up to the **weakest**
    container in the group.

    ```bash
    ironctl scan --azure containergroup.json
    ```

    [:octicons-arrow-right-24: Grade an Azure container group](scan.md#grade-an-azure-container-group)

-   #### :fontawesome-brands-aws:{ .lg .middle } AWS App Runner { #scan-app-runner }

    ---

    Grades an App Runner service from its `aws apprunner describe-service` JSON with the
    managed-runtime model. App Runner exposes no `securityContext`, so it grades honestly
    on its Fargate/Firecracker floors.

    ```bash
    ironctl scan --app-runner service.json
    ```

    [:octicons-arrow-right-24: Grade an App Runner service](scan.md#grade-an-aws-app-runner-service)

</div>

### Infrastructure-as-Code { #modes-iac }

The code that will one day launch the container, graded before it deploys. Container workloads
roll up to the **weakest** one the code defines.

<div class="grid cards" markdown>

-   #### :simple-terraform:{ .lg .middle } Terraform plan { #scan-terraform }

    ---

    Grades container workloads in a `terraform show -json` plan or state (Kubernetes and
    ECS task resources), or point it at a directory and it runs the plan for you.

    ```bash
    ironctl scan --terraform plan.json
    ```

    [:octicons-arrow-right-24: Grade a Terraform plan](scan.md#grade-a-terraform-plan)

-   #### :material-aws:{ .lg .middle } CloudFormation template { #scan-cloudformation }

    ---

    Grades every `AWS::ECS::TaskDefinition` in a CloudFormation template (YAML or JSON) and
    rolls up to the weakest, so a template is graded before the stack deploys.

    ```bash
    ironctl scan --cloudformation template.yaml
    ```

    [:octicons-arrow-right-24: Grade a CloudFormation template](scan.md#grade-a-cloudformation-template)

-   #### :simple-pulumi:{ .lg .middle } Pulumi program { #scan-pulumi }

    ---

    Grades the container workloads in a `pulumi stack export` or `pulumi preview --json`
    (Kubernetes and ECS resources) with the same scorers as `--k8s` and `--ecs`, rolling up
    to the **weakest** workload, so a program is graded before `pulumi up`.

    ```bash
    ironctl scan --pulumi stack.json
    ```

    [:octicons-arrow-right-24: Grade a Pulumi program](scan.md#grade-a-pulumi-program)

-   #### :material-microsoft-azure:{ .lg .middle } Azure Bicep template { #scan-bicep }

    ---

    Compiles a `.bicep` file (or a directory of them) to ARM with `bicep build` and grades the
    `Microsoft.ContainerInstance/containerGroups` it declares, reusing the `--azure` ACI path
    and rolling up to the **weakest** container, so a template is graded before `az deployment`.

    ```bash
    ironctl scan --bicep main.bicep
    ```

    [:octicons-arrow-right-24: Grade an Azure Bicep template](scan.md#grade-an-azure-bicep-template)

-   #### :material-aws:{ .lg .middle } AWS CDK app { #scan-cdk }

    ---

    Synthesizes a CDK app with `cdk synth` (or grades a pre-synthesized template / `cdk.out`)
    and grades every `AWS::ECS::TaskDefinition` it emits, reusing the `--cloudformation` scorer
    and rolling up to the **weakest** container, so an app is graded before `cdk deploy`.

    ```bash
    ironctl scan --cdk ./my-cdk-app
    ```

    [:octicons-arrow-right-24: Grade an AWS CDK app](scan.md#grade-an-aws-cdk-app)

</div>

## Every mode, one engine

Every card above feeds the **same seven-dimension scorer**. The input changes; the grade,
the letter, and the `--fix` remediation do not. That means a Compose file, a Helm chart, and
the container they eventually produce are all measured on one comparable scale.

| Category | Input | Flag | What it reads | Deep dive |
|----------|-------|------|---------------|-----------|
| Containers &amp; images | [Running container](#scan-container) | *(positional)* | live `docker inspect` of any OCI container | [Scan reference](scan.md) |
| Containers &amp; images | [Alternate runtimes](#scan-runtime) | `--runtime` | Podman / nerdctl / containerd | [Supported runtimes](scan.md#supported-runtimes) |
| Containers &amp; images | [Dockerfile](#scan-dockerfile) | `--dockerfile` | authoring-time posture, no daemon | [Grade a Dockerfile statically](scan.md#grade-a-dockerfile-statically) |
| Compose &amp; orchestrators | [Docker Compose](#scan-compose) | `--compose` `--service` | a service's declared config | [Scan reference](scan.md) |
| Compose &amp; orchestrators | [Kubernetes manifest](#scan-k8s) | `--k8s` | a pod spec's `securityContext` | [Scan reference](scan.md) |
| Compose &amp; orchestrators | [Helm chart](#scan-helm) | `--helm` | weakest workload from `helm template` | [Scan reference](scan.md) |
| Compose &amp; orchestrators | [Kustomize overlay](#scan-kustomize) | `--kustomize` | weakest workload from `kustomize build` | [Grade a kustomization](scan.md#grade-a-kustomization) |
| Compose &amp; orchestrators | [Nomad job](#scan-nomad) | `--nomad` | docker-driver tasks in a job spec | [Scan reference](scan.md) |
| Cloud runtimes | [Cloud Run](#scan-cloudrun) | `--cloudrun` | a Knative Service's container config | [Grade a Cloud Run service](scan.md#grade-a-google-cloud-run-service) |
| Cloud runtimes | [Amazon ECS](#scan-ecs) | `--ecs` | a task definition's container contract | [Grade an ECS task definition](scan.md#grade-an-aws-ecs-task-definition) |
| Cloud runtimes | [Azure Container Instances](#scan-azure) | `--azure` | weakest container in an ARM `containerGroups` | [Grade an Azure container group](scan.md#grade-an-azure-container-group) |
| Cloud runtimes | [AWS App Runner](#scan-app-runner) | `--app-runner` | a service's managed-runtime posture | [Grade an App Runner service](scan.md#grade-an-aws-app-runner-service) |
| Infrastructure-as-Code | [Terraform plan](#scan-terraform) | `--terraform` | container workloads in a plan/state | [Grade a Terraform plan](scan.md#grade-a-terraform-plan) |
| Infrastructure-as-Code | [CloudFormation](#scan-cloudformation) | `--cloudformation` | ECS task defs in a CFN template | [Grade a CloudFormation template](scan.md#grade-a-cloudformation-template) |
| Infrastructure-as-Code | [Pulumi program](#scan-pulumi) | `--pulumi` | K8s &amp; ECS workloads in stack-export / preview JSON | [Grade a Pulumi program](scan.md#grade-a-pulumi-program) |
| Infrastructure-as-Code | [Azure Bicep template](#scan-bicep) | `--bicep` | weakest `containerGroups` container, compiled to ARM | [Grade an Azure Bicep template](scan.md#grade-an-azure-bicep-template) |
| Infrastructure-as-Code | [AWS CDK app](#scan-cdk) | `--cdk` | weakest ECS container, synthesized to CloudFormation | [Grade an AWS CDK app](scan.md#grade-an-aws-cdk-app) |

Every mode also emits the machine-readable outputs: a [SARIF log](scan.md#github-code-scanning-security-tab)
for GitHub code scanning, a [shields.io badge](scan.md#sandbox-isolation-score-badge), and
`--fix` [remediation](scan.md#fix-it-do-not-just-grade-it).

## Start with what you have

```bash
# install (Homebrew)
brew install ironsecco/ironclaw/ironclaw

# grade the surface in front of you, then print the exact hardening flags
ironctl scan my-container
ironctl scan my-container --fix
```

## Keep going

- [Scan any container in 10 seconds &rarr;](scan.md): the full `ironctl scan` reference for every flag above.
- [Scan in CI &rarr;](scan-action.md): the same engine as a GitHub Action that grades every pull request.
- [Container hardening guides &rarr;](blog/hardening-guides.md): real before/after grades and the flags that close the gap, per image.
- [Container Isolation Scores &rarr;](scores/index.md): default-config grades for the most-pulled public images.
