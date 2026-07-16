---
title: "What ironctl scan can grade: containers, Compose, Kubernetes, Helm, Terraform, Nomad, Dockerfiles"
description: "One page, every ironctl scan input mode. A copy-paste command and an isolation grade for a running container, a Compose service, a Kubernetes manifest, a Helm chart, a Terraform plan, a Nomad job, or a Dockerfile."
---

# What `ironctl scan` can grade

One tool, one grade, many inputs. `ironctl scan` reads a container's real isolation posture
and returns a **0&ndash;100 score with a letter grade**, from a running container down to the
infrastructure-as-code that will one day launch it. Every mode below runs read-only, needs no
account, and prints the exact flags that close the gap.

Pick the surface you have. Each card is one copy-paste command.

<!-- ---------------------------------------------------------------------------
  SCAN-MODE CARD CONTRACT  (taxonomy-agnostic; add a mode without a redesign)

  Each supported input mode is exactly one <li> in the grid below, in this shape:

      - :icon-name:{ .lg .middle } __<Mode name>__ { #scan-<slug> }

          ---

          <One line: what it reads and what it grades.>

          ```bash
          ironctl scan <the exact copy-paste command>
          ```

          [:octicons-arrow-right-24: <deep-link label>](<deep doc>)

  To ship a new mode (e.g. Cloud Run, ECS): paste one <li> here and one row in
  the "Every mode, one engine" table. No template edit, no CSS. The heading id
  (`#scan-<slug>`) is the crawlable anchor; keep it kebab-case and stable.
---------------------------------------------------------------------------- -->

<div class="grid cards" markdown>

-   #### :material-cube-outline:{ .lg .middle } Running container { #scan-container }

    ---

    Audits any live OCI container (Docker, Podman, nerdctl, containerd): user, capabilities,
    seccomp, rootfs, network, privilege, and mounts.

    ```bash
    ironctl scan my-container
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

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

-   #### :simple-terraform:{ .lg .middle } Terraform plan { #scan-terraform }

    ---

    Grades container workloads in a `terraform show -json` plan or state (Kubernetes and
    ECS task resources), or point it at a directory and it runs the plan for you.

    ```bash
    ironctl scan --terraform plan.json
    ```

    [:octicons-arrow-right-24: Grade a Terraform plan](scan.md#grade-a-terraform-plan)

-   #### :material-aws:{ .lg .middle } CloudFormation { #scan-cloudformation }

    ---

    Grades the `AWS::ECS::TaskDefinition` resources in a CloudFormation template (YAML or
    JSON) with the same ECS scorer as `--ecs`, rolling up to the **weakest** container.

    ```bash
    ironctl scan --cloudformation template.yaml
    ```

    [:octicons-arrow-right-24: Grade a CloudFormation template](scan.md#grade-a-cloudformation-template)

-   #### :simple-nomad:{ .lg .middle } Nomad job { #scan-nomad }

    ---

    Grades the docker-driver tasks in a HashiCorp Nomad job spec and rolls up to the weakest
    task, so a job is graded before `nomad job run`.

    ```bash
    ironctl scan --nomad job.nomad
    ```

    [:octicons-arrow-right-24: Scan reference](scan.md)

-   #### :material-file-code-outline:{ .lg .middle } Dockerfile (static) { #scan-dockerfile }

    ---

    Grades authoring-time posture straight from a `Dockerfile` &mdash; no daemon, no build.
    Catches `USER root`, missing drops, and writable layers in CI.

    ```bash
    ironctl scan --dockerfile Dockerfile
    ```

    [:octicons-arrow-right-24: Grade a Dockerfile statically](scan.md#grade-a-dockerfile-statically)

-   #### :material-cog-sync-outline:{ .lg .middle } Alternate runtimes { #scan-runtime }

    ---

    Same grade for Podman, nerdctl, and containerd. Auto-detected by default; force one with
    `--runtime` when a host runs several.

    ```bash
    ironctl scan --runtime podman my-container
    ```

    [:octicons-arrow-right-24: Supported runtimes](scan.md#supported-runtimes)

</div>

## Every mode, one engine

Every card above feeds the **same seven-dimension scorer**. The input changes; the grade,
the letter, and the `--fix` remediation do not. That means a Compose file, a Helm chart, and
the container they eventually produce are all measured on one comparable scale.

| Input | Flag | What it reads | Deep dive |
|-------|------|---------------|-----------|
| [Running container](#scan-container) | *(positional)* | live `docker inspect` of any OCI container | [Scan reference](scan.md) |
| [Docker Compose](#scan-compose) | `--compose` `--service` | a service's declared config | [Scan reference](scan.md) |
| [Kubernetes manifest](#scan-k8s) | `--k8s` | a pod spec's `securityContext` | [Scan reference](scan.md) |
| [Helm chart](#scan-helm) | `--helm` | weakest workload from `helm template` | [Scan reference](scan.md) |
| [Terraform plan](#scan-terraform) | `--terraform` | container workloads in a plan/state | [Grade a Terraform plan](scan.md#grade-a-terraform-plan) |
| [CloudFormation](#scan-cloudformation) | `--cloudformation` | `AWS::ECS::TaskDefinition` in a template | [Grade a CloudFormation template](scan.md#grade-a-cloudformation-template) |
| [Nomad job](#scan-nomad) | `--nomad` | docker-driver tasks in a job spec | [Scan reference](scan.md) |
| [Dockerfile](#scan-dockerfile) | `--dockerfile` | authoring-time posture, no daemon | [Grade a Dockerfile statically](scan.md#grade-a-dockerfile-statically) |
| [Alternate runtimes](#scan-runtime) | `--runtime` | Podman / nerdctl / containerd | [Supported runtimes](scan.md#supported-runtimes) |

Every mode also emits the machine-readable outputs: a [SARIF log](scan.md#github-code-scanning-security-tab)
for GitHub code scanning, a [shields.io badge](scan.md#sandbox-isolation-score-badge), and
`--fix` [remediation](scan.md#fix-it-do-not-just-grade-it).

## On the roadmap

Two more input modes are in progress and will drop into the grid above as they ship &mdash; the
card contract is built so they add without a redesign:

- **Google Cloud Run** &mdash; grade a service's container config from its revision spec.
- **Amazon ECS** &mdash; grade a task definition directly (already reachable today via a
  [Terraform plan](#scan-terraform) that defines `aws_ecs_task_definition`).

We list these as *planned*, not shipped. When they land, this line moves up into a card.

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
