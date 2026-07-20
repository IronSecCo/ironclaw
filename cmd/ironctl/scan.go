package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/scan"
	"github.com/IronSecCo/ironclaw/internal/version"
)

// cmdScan implements `ironctl scan` — a containment self-audit that grades the
// isolation posture of ANY running container, docker-compose service, or
// Kubernetes pod/manifest 0-100. It is a LOCAL, read-only command: it inspects
// configuration (docker inspect / a compose or k8s file) and never talks to the
// control-plane API, so it works on a user's own setups, not just IronClaw's.
//
// Fail-closed: any dimension it cannot determine is graded as insecure.
//
// Usage:
//
//	ironctl scan <container>                 grade a running docker container
//	ironctl scan --compose FILE [--service N]  grade a compose service
//	ironctl scan --k8s FILE                    grade a pod/workload manifest
//	  [--json] [--badge scan.svg] [--sarif scan.sarif] [--md] [--min-score N]
func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the scorecard as JSON")
	badge := fs.String("badge", "", "write a shareable SVG badge to this path")
	badgeJSON := fs.String("badge-json", "", "write a shields.io endpoint JSON badge to this path (embed a live README badge)")
	badgeMd := fs.String("badge-md", "", "write a copy-paste README badge Markdown snippet to this path (grade badge linked to the receipt card)")
	sarif := fs.String("sarif", "", "write a SARIF 2.1.0 log to this path (GitHub code-scanning upload)")
	md := fs.Bool("md", false, "print a shareable markdown block (README/blog section)")
	share := fs.Bool("share", false, "print a shareable scan receipt: grade badge + per-dim breakdown + a link to a hosted receipt page and install CTA (offline; no network)")
	fix := fs.Bool("fix", false, "emit concrete remediation config for each failed dimension")
	remediate := fs.Bool("remediate", false, "alias for --fix")
	compareFlag := fs.Bool("compare", false, "compare two containers side-by-side (takes two positional targets)")
	compose := fs.String("compose", "", "grade a service in this docker-compose file")
	service := fs.String("service", "", "compose service name (required if the file has >1 service)")
	k8s := fs.String("k8s", "", "grade the first container in this Kubernetes pod/workload manifest")
	k8sAdmission := fs.String("k8s-admission", "", "grade the workload carried in a Kubernetes admission.k8s.io/v1 AdmissionReview JSON (webhook backend); '-' reads stdin")
	admissionResponse := fs.Bool("admission-response", false, "with --k8s-admission, emit an admission.k8s.io/v1 AdmissionReview response JSON (allow/deny) to stdout instead of the scorecard")
	emitPolicy := fs.String("emit-policy", "", "instead of a scorecard, emit admission-policy YAML (engine: kyverno|gatekeeper|vap) that BLOCKS the controls the scanned --k8s manifest failed (the delta to 100/A); vap is a controller-free native ValidatingAdmissionPolicy")
	check := fs.Bool("check", false, "policy-as-code CI gate: evaluate the --k8s manifest AGAINST the rules --emit-policy would generate and exit non-zero on any violation (no cluster, no controller). Pairs with --md/--json/--sarif for diagnostics")
	helm := fs.String("helm", "", "render a Helm chart (dir or .tgz) with `helm template` and grade the isolation posture of its workloads")
	helmBin := fs.String("helm-bin", envOrDefault("HELM", "helm"), "helm binary used to render the chart")
	terraform := fs.String("terraform", "", "grade container workloads in a `terraform show -json` file (plan.json/state.json) or a Terraform dir")
	terraformBin := fs.String("terraform-bin", envOrDefault("TERRAFORM", "terraform"), "terraform binary used to run `terraform show -json` on a dir/binary plan")
	nomad := fs.String("nomad", "", "grade docker-driver tasks in a HashiCorp Nomad job spec (job.json, or a .hcl/.nomad file rendered via `nomad job run -output`)")
	nomadBin := fs.String("nomad-bin", envOrDefault("NOMAD", "nomad"), "nomad binary used to render an HCL job to JSON (`nomad job run -output`)")
	ecs := fs.String("ecs", "", "grade an AWS ECS task definition: `aws ecs describe-task-definition` JSON, a raw registered task-def JSON, or a directory of task-def JSON files")
	cloudrun := fs.String("cloudrun", "", "grade Google Cloud Run service specs: a Knative Service YAML (`gcloud run services describe SVC --format=export`), or a directory of them")
	cloudformation := fs.String("cloudformation", "", "grade AWS::ECS::TaskDefinition resources in a CloudFormation template (YAML or JSON), or a directory of them")
	sam := fs.String("sam", "", "grade the AWS::ECS::TaskDefinition resources declared in an AWS SAM template (Transform: AWS::Serverless-*, YAML or JSON), or a directory of them")
	pulumi := fs.String("pulumi", "", "grade container workloads in a Pulumi `pulumi stack export` / `pulumi preview --json` file (or a directory of them): kubernetes:* workloads and aws ECS task definitions")
	azure := fs.String("azure", "", "grade Microsoft.ContainerInstance/containerGroups in an Azure ARM template / `az container show` JSON, or a directory of them")
	appRunner := fs.String("app-runner", "", "grade an AWS App Runner service: `aws apprunner describe-service` JSON, a raw Service object, or a directory of them")
	bicep := fs.String("bicep", "", "compile an Azure Bicep template (.bicep file or a directory of them) to ARM and grade the Microsoft.ContainerInstance/containerGroups it declares")
	bicepBin := fs.String("bicep-bin", envOrDefault("BICEP", "bicep"), "standalone bicep binary used to compile a .bicep to ARM (`bicep build --stdout`); falls back to `az bicep build` if absent")
	azBin := fs.String("az-bin", envOrDefault("AZ", "az"), "azure-cli binary used for the `az bicep build` fallback when the standalone bicep binary is absent")
	cdk := fs.String("cdk", "", "grade an AWS CDK app: a CDK app dir (synthesized with `cdk synth`) OR a pre-synthesized CloudFormation template file/dir; grades the AWS::ECS::TaskDefinition resources it emits")
	cdkBin := fs.String("cdk-bin", envOrDefault("CDK", "cdk"), "aws-cdk binary used to run `cdk synth` on a CDK app directory (falls back to `cdk.out` templates if the app is already synthesized)")
	kustomize := fs.String("kustomize", "", "render a kustomization directory with `kustomize build` (or `kubectl kustomize`) and grade the isolation posture of its workloads")
	kustomizeBin := fs.String("kustomize-bin", envOrDefault("KUSTOMIZE", "kustomize"), "kustomize binary used to render the directory (falls back to `kubectl kustomize` if absent)")
	kubectlBin := fs.String("kubectl-bin", envOrDefault("KUBECTL", "kubectl"), "kubectl binary used for `kubectl kustomize` when the kustomize binary is absent")
	openshift := fs.String("openshift", "", "grade OpenShift workloads (DeploymentConfig, Deployment, Pod) in a manifest file (`oc get -o yaml`) or a directory of them")
	dockerfile := fs.Bool("dockerfile", false, "statically grade the positional Dockerfile(s) authoring-time posture (daemon-free)")
	runtime := fs.String("runtime", envOrDefault("IRONCTL_SCAN_RUNTIME", "auto"), "container runtime: auto|docker|podman|nerdctl")
	dockerBin := fs.String("docker-bin", envOrDefault("DOCKER", "docker"), "docker binary used for `docker inspect`")
	podmanBin := fs.String("podman-bin", envOrDefault("PODMAN", "podman"), "podman binary used for `podman inspect`")
	nerdctlBin := fs.String("nerdctl-bin", envOrDefault("NERDCTL", "nerdctl"), "nerdctl binary used for `nerdctl inspect`")
	minScore := fs.Int("min-score", 0, "exit non-zero if the score is below this threshold (CI gate)")
	fs.Usage = func() { scanUsage(os.Stdout) }
	// Go's flag package stops at the first positional, so `scan <target> --json`
	// would silently drop the flags after the target. Re-parse around each
	// positional so flags may appear anywhere (interspersed-flag handling).
	var positional []string
	rest := args
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}

	// --compare A B: grade two live containers and print a side-by-side diff.
	// Reuses the same adapters and scorer as a single scan; only the presentation
	// differs, so it returns early with its own renderers.
	if *compareFlag {
		return runCompare(compareArgs{
			targets: positional,
			runtime: *runtime,
			bins:    runtimeBins{docker: *dockerBin, podman: *podmanBin, nerdctl: *nerdctlBin},
			asJSON:  *asJSON,
			md:      *md,
		})
	}

	// --dockerfile: static, daemon-free posture grading of one or more Dockerfiles
	// passed as positionals. It grades a DIFFERENT, authoring-time dimension set
	// (see internal/host/scan/dockerfile.go) and uses its own scorer + SARIF
	// renderer; the runtime --fix/--compare paths do not apply. Taking the files as
	// positionals (not a --dockerfile=FILE value) lets a pre-commit hook append its
	// matched filenames after fixed --min-score/--json args (IRO-494).
	if *dockerfile {
		if len(positional) == 0 {
			scanUsage(os.Stderr)
			return fmt.Errorf("scan --dockerfile needs at least one Dockerfile path")
		}
		return runDockerfileScan(dockerfileArgs{
			paths:     positional,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --helm: render a chart locally (`helm template`, no cluster, daemon-free)
	// and grade the isolation posture of the rendered workloads, reusing the k8s
	// dimension set. It renders (I/O) here, then defers to the pure
	// SpecsFromK8sStream + AggregateHelm scorer. Fail-OPEN on a render failure
	// (helm absent / template error) so an opt-in CI step never crashes the build;
	// the --min-score gate still applies once the chart renders.
	if *helm != "" {
		return runHelmScan(helmArgs{
			chart:     *helm,
			helmBin:   *helmBin,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --terraform: consume `terraform show -json` (plan.json/state.json) and grade
	// the container workloads it declares (kubernetes_* pods/workloads +
	// aws_ecs_task_definition), reusing the k8s/ECS dimension mapping. It loads
	// JSON here (reading a file, or running `terraform show -json` on a dir) then
	// defers to the pure SpecsFromTerraform + AggregateTerraform scorer. Fail-OPEN
	// on a load/parse failure; --min-score still gates once workloads are graded.
	if *terraform != "" {
		return runTerraformScan(terraformArgs{
			path:         *terraform,
			terraformBin: *terraformBin,
			asJSON:       *asJSON,
			md:           *md,
			share:        *share,
			badge:        *badge,
			badgeJSON:    *badgeJSON,
			sarif:        *sarif,
			minScore:     *minScore,
		})
	}

	// --nomad: consume a Nomad job spec (JSON directly, or a .hcl/.nomad file
	// rendered to JSON via `nomad job run -output`) and grade the docker-driver
	// tasks it declares, reusing the same docker/compose dimension mapping and
	// weakest-link aggregate as --helm/--terraform. It loads JSON here then defers
	// to the pure SpecsFromNomad + AggregateNomad scorer. Fail-OPEN on a
	// load/parse failure; --min-score still gates once tasks are graded.
	if *nomad != "" {
		return runNomadScan(nomadArgs{
			path:      *nomad,
			nomadBin:  *nomadBin,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --ecs: consume an AWS ECS task definition (describe-task-definition JSON, a
	// raw registered task def, or a directory of task-def JSON files) and grade the
	// container contract, reusing the SHARED ECS scorer that the --terraform
	// aws_ecs_task_definition path also uses. It loads JSON here then defers to the
	// pure SpecsFromECS + AggregateECS scorer. Fail-OPEN on a load/parse failure;
	// --min-score still gates once containers are graded.
	if *ecs != "" {
		return runECSScan(ecsArgs{
			path:      *ecs,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --cloudrun: consume a Google Cloud Run service spec (a Knative Service YAML
	// as emitted by `gcloud run services describe --format=export`, or a directory
	// of them) and grade its revision containers, reusing the same pod-spec scorer
	// as --k8s/--helm plus Cloud Run's managed-runtime guarantees. It loads YAML
	// here (no external binary — Cloud Run specs are plain YAML) then defers to the
	// pure SpecsFromCloudRun + AggregateCloudRun scorer. Fail-OPEN on a load/parse
	// failure; --min-score still gates once services are graded.
	if *cloudrun != "" {
		return runCloudRunScan(cloudRunScanArgs{
			path:      *cloudrun,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --cloudformation: consume an AWS CloudFormation template (YAML or JSON, or a
	// directory of them) and grade the AWS::ECS::TaskDefinition resources it
	// declares, reusing the SHARED ECS scorer that the --ecs and --terraform
	// aws_ecs_task_definition paths also use. It reads the template here (no external
	// binary — CFN templates are plain YAML/JSON) then defers to the pure
	// SpecsFromCloudFormation + AggregateCloudFormation scorer. Fail-OPEN on a
	// load/parse failure; --min-score still gates once containers are graded.
	if *cloudformation != "" {
		return runCloudFormationScan(cloudFormationScanArgs{
			path:      *cloudformation,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --pulumi: consume Pulumi program output (`pulumi stack export` checkpoint or
	// `pulumi preview --json`, or a directory of them) and grade the container
	// workloads it declares — kubernetes:* pods/workloads and aws ECS task
	// definitions — reusing the SHARED k8s and ECS scorers that the --k8s/--helm and
	// --ecs/--terraform paths also use. Pulumi output is plain JSON, so there is no
	// external binary to shell out to. It reads the file(s) here then defers to the
	// pure SpecsFromPulumi + AggregatePulumi scorer (the same weakest-link rollup as
	// --terraform). Fail-OPEN on a load/parse failure so an opt-in CI/Action step
	// never crashes the build; --min-score still gates once workloads are graded.
	if *pulumi != "" {
		return runPulumiScan(pulumiScanArgs{
			path:      *pulumi,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --azure: consume an Azure Container Instances definition (an ARM template,
	// an `az container show`/deployment JSON, or a directory of them) and grade the
	// Microsoft.ContainerInstance/containerGroups it declares, reusing the SAME
	// pod-spec scorer as --k8s/--cloudrun plus ACI's managed-runtime floors. It
	// reads the JSON here (no external binary) then defers to the pure
	// SpecsFromAzure + AggregateAzure scorer. Fail-OPEN on a load/parse failure;
	// --min-score still gates once containers are graded. Unresolvable ARM
	// expressions ("[...]") are graded fail-closed and noted on the report.
	if *azure != "" {
		return runAzureScan(azureScanArgs{
			path:      *azure,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --app-runner: consume an AWS App Runner service (an `aws apprunner
	// describe-service` JSON, a raw Service object, or a directory of them) and grade
	// it, reusing the SAME pod-spec scorer as --k8s/--cloudrun/--azure plus App
	// Runner's managed-runtime floors. App Runner exposes no securityContext, so the
	// user-hardenable dimensions grade fail-closed and a service tops out at 48/100
	// (D) on the managed floors alone. It reads the JSON here (no external binary)
	// then defers to the pure SpecsFromAppRunner + AggregateAppRunner scorer.
	// Fail-OPEN on a load/parse failure; --min-score still gates once a service is
	// graded.
	if *appRunner != "" {
		return runAppRunnerScan(appRunnerScanArgs{
			path:      *appRunner,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --bicep: compile an Azure Bicep template (a .bicep file or a directory of
	// them) to ARM JSON with `bicep build --stdout` (or `az bicep build` when the
	// standalone binary is absent), then grade the
	// Microsoft.ContainerInstance/containerGroups it declares by reusing the SAME
	// ACI managed-runtime path as --azure. It compiles (I/O) here, then defers to the
	// pure SpecsFromBicepARM + AggregateBicep scorer. Fail-OPEN on a compile/parse
	// failure (bicep/az absent or a bad template) so an opt-in CI step never crashes
	// the build; --min-score still gates once containers are graded. Unresolvable ARM
	// expressions ("[...]") are graded fail-closed and noted on the report.
	if *bicep != "" {
		return runBicepScan(bicepScanArgs{
			path:      *bicep,
			bicepBin:  *bicepBin,
			azBin:     *azBin,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --cdk: grade an AWS CDK app. The CDK is a program that SYNTHESIZES a standard
	// CloudFormation template, so this is a thin front-door over --cloudformation: if
	// the path is a CDK app dir it is synthesized with `cdk synth` (I/O here), and a
	// pre-synthesized template file/dir (or a synthesized `cdk.out`) is graded directly.
	// Either way the emitted CloudFormation is decoded by the SAME shared ECS scorer as
	// --cloudformation/--ecs/--terraform (SpecsFromCDK -> SpecsFromCloudFormation). It
	// fails OPEN on a synth/parse failure (cdk absent or a bad app) so an opt-in CI step
	// never crashes the build; --min-score still gates once containers are graded.
	// Unresolvable CDK tokens / CFN intrinsics are graded fail-closed and noted.
	if *cdk != "" {
		return runCDKScan(cdkScanArgs{
			path:      *cdk,
			cdkBin:    *cdkBin,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --sam: consume an AWS SAM template (Transform: AWS::Serverless-*) and grade the
	// AWS::ECS::TaskDefinition resources it declares. SAM is a CloudFormation superset:
	// raw (non-AWS::Serverless::*) resources are already native CFN, so this is a thin
	// front-door over --cloudformation with no `sam` transform step required to reach
	// the ECS task defs. The template is decoded by the SAME shared ECS scorer as
	// --cloudformation/--cdk/--ecs/--terraform (SpecsFromSAM -> SpecsFromCloudFormation).
	// It fails OPEN on a load/parse failure so an opt-in CI step never crashes the build;
	// --min-score still gates once containers are graded. Unresolvable CFN intrinsics are
	// graded fail-closed and noted.
	if *sam != "" {
		return runSAMScan(samScanArgs{
			path:      *sam,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --kustomize: render a kustomization locally (`kustomize build`, or
	// `kubectl kustomize` when the standalone binary is absent — no cluster,
	// daemon-free) and grade the isolation posture of the flattened workloads,
	// reusing the k8s dimension set. It renders (I/O) here, then defers to the pure
	// SpecsFromKustomize + AggregateKustomize scorer (the same weakest-link rollup
	// as --helm). Fail-OPEN on a render failure (kustomize/kubectl absent or build
	// error) so an opt-in CI step never crashes the build; --min-score still gates
	// once the kustomization renders.
	if *kustomize != "" {
		return runKustomizeScan(kustomizeArgs{
			dir:          *kustomize,
			kustomizeBin: *kustomizeBin,
			kubectlBin:   *kubectlBin,
			asJSON:       *asJSON,
			md:           *md,
			share:        *share,
			badge:        *badge,
			badgeJSON:    *badgeJSON,
			sarif:        *sarif,
			minScore:     *minScore,
		})
	}

	// --openshift: consume an OpenShift manifest set (an `oc get -o yaml` export, a
	// raw manifest file, or a directory of them) and grade its workloads. An
	// OpenShift DeploymentConfig embeds a standard k8s PodSpec at spec.template.spec,
	// so this reuses the SAME pod-spec scorer as --k8s/--kustomize with the same
	// weakest-link aggregate; plain Deployment/Pod docs in the same stream grade too
	// and OpenShift-only kinds (Route, ImageStream, …) are skipped. Manifests are
	// plain YAML, so there is no external binary to shell out to. It reads the
	// file(s) here then defers to the pure SpecsFromOpenShift + AggregateOpenShift
	// scorer. Fail-OPEN on a load/parse failure so an opt-in CI/Action step never
	// crashes the build; --min-score still gates once workloads are graded.
	if *openshift != "" {
		return runOpenShiftScan(openShiftScanArgs{
			path:      *openshift,
			asJSON:    *asJSON,
			md:        *md,
			share:     *share,
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
	}

	// --k8s-admission: grade the workload carried in a Kubernetes AdmissionReview
	// request and gate admission on --min-score, reusing the SAME pod-spec scorer as
	// --k8s. This turns `ironctl scan` from a static report into an in-cluster
	// ENFORCEMENT gate: a thin ValidatingWebhook wrapper pipes the API server's
	// request body to `scan --k8s-admission - --admission-response --min-score N`
	// and returns stdout verbatim. Unlike the fail-OPEN batch modes, it is
	// fail-CLOSED (unparseable input DENIES, never silently allows).
	if *k8sAdmission != "" {
		return runK8sAdmissionScan(k8sAdmissionArgs{
			path:         *k8sAdmission,
			emitResponse: *admissionResponse,
			asJSON:       *asJSON,
			md:           *md,
			share:        *share,
			badge:        *badge,
			badgeJSON:    *badgeJSON,
			sarif:        *sarif,
			minScore:     *minScore,
		})
	}

	// --check: the enforce-in-place dual of --emit-policy. Instead of EMITTING
	// guardrail YAML, evaluate the scanned Kubernetes manifest AGAINST the rules
	// --emit-policy would generate and exit non-zero on any violation — a
	// self-contained policy-as-code CI gate that needs no cluster and no admission
	// controller. It reuses the SAME dim->rule map as --emit-policy (checkRules /
	// policyRules share keys), so generate and enforce never diverge.
	if *check {
		return runCheck(checkArgs{k8s: *k8s, positional: positional, asJSON: *asJSON, md: *md, sarif: *sarif})
	}

	// --emit-policy: turn scan findings into a guardrail. Instead of a scorecard,
	// emit ready-to-apply Kyverno/Gatekeeper admission-policy YAML that BLOCKS the
	// exact containment controls the scanned Kubernetes manifest failed — the delta
	// between its grade and 100/A, one failed dimension -> one policy rule. It reuses
	// the SAME pod-spec scorer as --k8s / --k8s-admission (no controls re-derived).
	if *emitPolicy != "" {
		return runEmitPolicy(emitPolicyArgs{engine: *emitPolicy, k8s: *k8s, positional: positional})
	}

	// Resolve the source and build a normalized Spec. configFile/configRaw carry
	// the scanned file (compose/k8s) so SARIF results anchor at the config; both
	// stay empty for a live-container scan (no file to point at).
	var (
		spec       scan.Spec
		err        error
		configFile string
		configRaw  []byte
	)
	switch {
	case *compose != "":
		raw, rerr := os.ReadFile(*compose)
		if rerr != nil {
			return fmt.Errorf("read compose file: %w", rerr)
		}
		configFile, configRaw = *compose, raw
		spec, err = scan.SpecFromCompose(raw, *service)
	case *k8s != "":
		raw, rerr := os.ReadFile(*k8s)
		if rerr != nil {
			return fmt.Errorf("read manifest: %w", rerr)
		}
		configFile, configRaw = *k8s, raw
		spec, err = scan.SpecFromK8s(raw)
	default:
		if len(positional) < 1 {
			scanUsage(os.Stderr)
			return fmt.Errorf("scan needs a target: a container name/id, or --compose/--k8s FILE")
		}
		if len(positional) > 1 {
			return fmt.Errorf("scan grades one target at a time; got %d: %s", len(positional), strings.Join(positional, " "))
		}
		bins := runtimeBins{docker: *dockerBin, podman: *podmanBin, nerdctl: *nerdctlBin}
		spec, err = containerSpec(*runtime, bins, positional[0])
	}
	if err != nil {
		return err
	}

	report := scan.Score(spec)
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	// --fix (a.k.a. --remediate): compute the prescriptive plan up front so it can
	// ride along in either the JSON or the table output.
	var plan *scan.RemediationPlan
	if *fix || *remediate {
		p := scan.Remediate(spec, report)
		plan = &p
	}

	// Emit the requested representations. Table is the default; --json swaps it.
	if *asJSON {
		if err := scan.RenderJSON(os.Stdout, report, plan); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
		if plan != nil {
			scan.RenderPlan(os.Stdout, *plan)
		}
	}
	if *md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if *share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if *badge != "" {
		if err := os.WriteFile(*badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !*asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", *badge)
		}
	}
	if *badgeJSON != "" {
		if err := os.WriteFile(*badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !*asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", *badgeJSON)
		}
	}
	if *badgeMd != "" {
		if err := os.WriteFile(*badgeMd, []byte(scan.RenderBadgeSnippet(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-md: %w", err)
		}
		if !*asJSON {
			fmt.Fprintf(os.Stdout, "  wrote README badge snippet: %s\n", *badgeMd)
		}
	}

	if *sarif != "" {
		// SARIF is a best-effort side artifact: an emit error must never block the
		// scan itself (fail-open), and never affects the min-score gate below.
		opts := scan.SARIFOptions{ConfigFile: configFile}
		if configRaw != nil {
			opts.AnchorLine = scan.AnchorLine(configRaw, spec.Target)
		}
		if err := writeSARIF(*sarif, report, spec, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !*asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", *sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold.
	if *minScore > 0 && report.Score < *minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, *minScore)
	}
	return nil
}

// compareArgs carries the resolved inputs for a `scan --compare` run.
type compareArgs struct {
	targets []string
	runtime string
	bins    runtimeBins
	asJSON  bool
	md      bool
}

// runCompare grades exactly two live containers and prints a side-by-side diff.
// It reuses containerSpec (the same docker/podman/nerdctl adapters as a single
// scan) and scan.Score, then defers to the comparison renderers. It fails closed:
// if either target cannot be inspected, the whole compare errors rather than
// printing a half diff.
func runCompare(a compareArgs) error {
	if len(a.targets) != 2 {
		scanUsage(os.Stderr)
		return fmt.Errorf("scan --compare needs exactly two targets; got %d", len(a.targets))
	}
	if a.targets[0] == a.targets[1] {
		return fmt.Errorf("scan --compare needs two distinct targets; both are %q", a.targets[0])
	}

	reports := make([]scan.Report, 2)
	for i, target := range a.targets {
		spec, err := containerSpec(a.runtime, a.bins, target)
		if err != nil {
			return fmt.Errorf("scan target %q: %w", target, err)
		}
		r := scan.Score(spec)
		r.Version = version.String()
		r.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
		reports[i] = r
	}

	cmp := scan.Compare(reports[0], reports[1])
	switch {
	case a.asJSON:
		if err := scan.RenderComparisonJSON(os.Stdout, cmp); err != nil {
			return err
		}
	default:
		scan.RenderComparisonTable(os.Stdout, cmp)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderComparisonMarkdown(cmp))
	}
	return nil
}

// dockerfileArgs carries the resolved inputs for a `scan --dockerfile` run. paths
// holds one or more Dockerfile paths (a pre-commit hook appends every matched
// file), each graded independently.
type dockerfileArgs struct {
	paths     []string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runDockerfileScan grades each Dockerfile's authoring-time posture statically
// (no daemon, no pull) and emits the requested representations. It reuses the
// shared Report/table/json/md/badge paths and the Dockerfile-specific SARIF
// renderer, and honors --min-score as a CI gate exactly like the live modes: the
// gate trips (non-zero exit) if ANY graded file falls below the threshold, so a
// pre-commit hook fails the commit on the first porous Dockerfile. The single
// -artifact flags (--badge/--badge-json/--sarif) require exactly one path.
func runDockerfileScan(a dockerfileArgs) error {
	if len(a.paths) > 1 && (a.badge != "" || a.badgeJSON != "" || a.sarif != "") {
		return fmt.Errorf("--badge/--badge-json/--sarif write a single artifact; pass one Dockerfile at a time with them (got %d)", len(a.paths))
	}
	var failed []string
	for _, path := range a.paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Dockerfile: %w", err)
		}
		spec, err := scan.SpecFromDockerfile(raw, path)
		if err != nil {
			return err
		}
		report := scan.ScoreDockerfile(spec)
		report.Version = version.String()
		report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

		if a.asJSON {
			if err := scan.RenderJSON(os.Stdout, report); err != nil {
				return err
			}
		} else {
			scan.RenderTable(os.Stdout, report)
		}
		if a.md {
			fmt.Fprintln(os.Stdout)
			fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
		}
		if a.share {
			fmt.Fprintln(os.Stdout)
			fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
		}
		if a.badge != "" {
			if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
				return fmt.Errorf("write badge: %w", err)
			}
			if !a.asJSON {
				fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
			}
		}
		if a.badgeJSON != "" {
			if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
				return fmt.Errorf("write badge-json: %w", err)
			}
			if !a.asJSON {
				fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
			}
		}
		if a.sarif != "" {
			// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
			opts := scan.SARIFOptions{ConfigFile: path, AnchorLine: scan.AnchorLine(raw, "FROM")}
			if err := writeDockerfileSARIF(a.sarif, report, opts); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
			} else if !a.asJSON {
				fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
			}
		}
		if a.minScore > 0 && report.Score < a.minScore {
			failed = append(failed, fmt.Sprintf("%s (%d/100)", path, report.Score))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("Dockerfile posture below the required %d: %s", a.minScore, strings.Join(failed, ", "))
	}
	return nil
}

// helmArgs carries the resolved inputs for a `scan --helm` run.
type helmArgs struct {
	chart     string
	helmBin   string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runHelmScan renders a Helm chart with `helm template` (no cluster, daemon-free)
// and grades the isolation posture of its rendered workloads, reusing the k8s
// scorer. It fails OPEN on a render failure (helm binary absent, or `helm
// template` errors): it prints a clear diagnostic to stderr and returns nil so
// an opt-in CI/Action step never crashes the build over tooling or chart-render
// issues. Once the chart renders, scoring and the --min-score CI gate apply
// exactly like --k8s (a genuinely low posture still fails the gate).
func runHelmScan(a helmArgs) error {
	rendered, err := renderHelmChart(a.helmBin, a.chart)
	if err != nil {
		// Fail-open: surface the reason, do not error out.
		fmt.Fprintf(os.Stderr, "  warning: scan --helm could not render %q (scan skipped, exit 0): %v\n", a.chart, err)
		return nil
	}

	specs, err := scan.SpecsFromK8sStream(rendered)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --helm could not parse rendered chart %q (scan skipped, exit 0): %v\n", a.chart, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateHelm(specs, filepath.Base(strings.TrimRight(a.chart, "/")))
	if err != nil {
		// A chart that renders no gradeable workload is a fail-open skip, not a
		// crash: nothing to grade is not the same as a low score.
		fmt.Fprintf(os.Stderr, "  warning: scan --helm found nothing to grade in %q (scan skipped, exit 0): %v\n", a.chart, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		// Anchor at the chart with a logical location naming the weakest workload
		// (the rendered stream has no stable source file/line to point at).
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (render succeeded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// renderHelmChart runs `helm template` against a local chart (unpacked dir or
// .tgz) and returns the rendered multi-document manifest stream. It is offline
// and daemon-free: no cluster connection, no network beyond helm's own local
// dependency resolution. A fixed release name keeps output deterministic.
func renderHelmChart(helmBin, chart string) ([]byte, error) {
	if _, err := exec.LookPath(helmBin); err != nil {
		return nil, fmt.Errorf("helm binary %q not found on PATH (install helm or pass --helm-bin): %w", helmBin, err)
	}
	cmd := exec.Command(helmBin, "template", "ironclaw-scan", chart)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("helm template failed: %s", msg)
	}
	return stdout.Bytes(), nil
}

// kustomizeArgs carries the resolved inputs for a `scan --kustomize` run.
type kustomizeArgs struct {
	dir          string
	kustomizeBin string
	kubectlBin   string
	asJSON       bool
	md           bool
	share        bool
	badge        string
	badgeJSON    string
	sarif        string
	minScore     int
}

// runKustomizeScan renders a kustomization directory with `kustomize build`
// (falling back to `kubectl kustomize` when the standalone binary is absent — no
// cluster, daemon-free) and grades the isolation posture of its flattened
// workloads, reusing the k8s scorer. It fails OPEN on a render failure (neither
// binary present, or the build errors): it prints a clear diagnostic to stderr
// and returns nil so an opt-in CI/Action step never crashes the build over
// tooling or overlay issues. Once the kustomization renders, scoring and the
// --min-score CI gate apply exactly like --helm (a genuinely low posture still
// fails the gate).
func runKustomizeScan(a kustomizeArgs) error {
	rendered, err := renderKustomize(a.kustomizeBin, a.kubectlBin, a.dir)
	if err != nil {
		// Fail-open: surface the reason, do not error out.
		fmt.Fprintf(os.Stderr, "  warning: scan --kustomize could not render %q (scan skipped, exit 0): %v\n", a.dir, err)
		return nil
	}

	specs, err := scan.SpecsFromKustomize(rendered)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --kustomize could not parse rendered manifests for %q (scan skipped, exit 0): %v\n", a.dir, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateKustomize(specs, filepath.Base(strings.TrimRight(a.dir, "/")))
	if err != nil {
		// A kustomization that renders no gradeable workload is a fail-open skip,
		// not a crash: nothing to grade is not the same as a low score.
		fmt.Fprintf(os.Stderr, "  warning: scan --kustomize found nothing to grade in %q (scan skipped, exit 0): %v\n", a.dir, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		// Anchor at the kustomization with a logical location naming the weakest
		// workload (the rendered stream has no stable source file/line to point at).
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (render succeeded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// renderKustomize renders a kustomization directory to a multi-document manifest
// stream. It prefers the standalone `kustomize build <dir>` and falls back to
// `kubectl kustomize <dir>` (the same renderer, vendored into kubectl) when the
// kustomize binary is not on PATH. It is offline and daemon-free: no cluster
// connection, only local overlay flattening.
func renderKustomize(kustomizeBin, kubectlBin, dir string) ([]byte, error) {
	if _, err := exec.LookPath(kustomizeBin); err == nil {
		return runKustomizeRenderer(exec.Command(kustomizeBin, "build", dir), "kustomize build")
	}
	if _, err := exec.LookPath(kubectlBin); err == nil {
		return runKustomizeRenderer(exec.Command(kubectlBin, "kustomize", dir), "kubectl kustomize")
	}
	return nil, fmt.Errorf("neither kustomize (%q) nor kubectl (%q) found on PATH (install one or pass --kustomize-bin/--kubectl-bin)", kustomizeBin, kubectlBin)
}

// runKustomizeRenderer runs a prepared render command and returns its stdout,
// wrapping any failure with the renderer's stderr for a clear diagnostic.
func runKustomizeRenderer(cmd *exec.Cmd, label string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", label, msg)
	}
	return stdout.Bytes(), nil
}

// terraformArgs carries the resolved inputs for a `scan --terraform` run.
type terraformArgs struct {
	path         string
	terraformBin string
	asJSON       bool
	md           bool
	share        bool
	badge        string
	badgeJSON    string
	sarif        string
	minScore     int
}

// runTerraformScan grades the container workloads declared in a `terraform show
// -json` document (kubernetes_* pods/workloads and aws_ecs_task_definition),
// reusing the same k8s/ECS dimension mapping and weakest-link aggregate as
// --helm. It fails OPEN on a load/parse failure (terraform absent, unreadable
// path, malformed JSON): it prints a clear diagnostic to stderr and returns nil
// so an opt-in CI/Action step never crashes the build over tooling issues. Once
// workloads are graded, scoring and the --min-score CI gate apply exactly like
// --k8s/--helm (a genuinely low posture still fails the gate).
func runTerraformScan(a terraformArgs) error {
	raw, err := loadTerraformJSON(a.terraformBin, a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --terraform could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	specs, err := scan.SpecsFromTerraform(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --terraform could not parse %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateTerraform(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --terraform found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (workloads graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadTerraformJSON resolves a --terraform argument to `terraform show -json`
// bytes. A directory is rendered with `terraform -chdir=<dir> show -json` (its
// current state). A file that already IS JSON (a `terraform show -json` redirect)
// is used verbatim — the daemon-free primary path. A non-JSON file (a binary
// tfplan) is converted with `terraform show -json <file>` when terraform is on
// PATH. It is offline: `terraform show` does not contact a backend for a saved
// plan, and reads local state for a dir.
func loadTerraformJSON(terraformBin, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return runTerraformShow(terraformBin, "-chdir="+path, "show", "-json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if looksLikeJSON(raw) {
		return raw, nil
	}
	// A binary plan file: convert via `terraform show -json <file>` (needs the
	// terraform binary and to run in the config directory).
	dir := filepath.Dir(path)
	return runTerraformShow(terraformBin, "-chdir="+dir, "show", "-json", filepath.Base(path))
}

// runTerraformShow runs the terraform binary with the given args and returns its
// stdout, surfacing terraform's own stderr on failure.
func runTerraformShow(terraformBin string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(terraformBin); err != nil {
		return nil, fmt.Errorf("terraform binary %q not found on PATH (install terraform, pass --terraform-bin, or pass a `terraform show -json` JSON file directly): %w", terraformBin, err)
	}
	cmd := exec.Command(terraformBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("terraform show -json failed: %s", msg)
	}
	return stdout.Bytes(), nil
}

// looksLikeJSON reports whether raw begins (after leading whitespace) with a JSON
// object — enough to distinguish a `terraform show -json` redirect from a binary
// tfplan without a full parse.
func looksLikeJSON(raw []byte) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// nomadArgs carries the resolved inputs for a `scan --nomad` run.
type nomadArgs struct {
	path      string
	nomadBin  string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runNomadScan grades the docker-driver tasks declared in a HashiCorp Nomad job
// spec, reusing the same docker/compose dimension mapping and weakest-link
// aggregate as --helm/--terraform. It fails OPEN on a load/parse failure (nomad
// absent for an HCL file, unreadable path, malformed JSON): it prints a clear
// diagnostic to stderr and returns nil so an opt-in CI/Action step never crashes
// the build over tooling issues. Once tasks are graded, scoring and the
// --min-score CI gate apply exactly like --k8s/--helm/--terraform (a genuinely
// low posture still fails the gate).
func runNomadScan(a nomadArgs) error {
	raw, err := loadNomadJSON(a.nomadBin, a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --nomad could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	specs, err := scan.SpecsFromNomad(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --nomad could not parse %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateNomad(specs, filepath.Base(a.path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --nomad found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (tasks graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadNomadJSON resolves a --nomad argument to Nomad job JSON bytes. A file that
// already IS JSON (a `nomad job run -output` redirect, or a hand-authored API
// job) is used verbatim — the daemon-free, dependency-free primary path. A
// non-JSON file (an HCL2 `.hcl`/`.nomad` job) is converted with `nomad job run
// -output <file>` when the nomad binary is on PATH. It is offline: `-output`
// renders the job locally without submitting it to a cluster.
func loadNomadJSON(nomadBin, path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if looksLikeJSON(raw) {
		return raw, nil
	}
	if _, err := exec.LookPath(nomadBin); err != nil {
		return nil, fmt.Errorf("nomad binary %q not found on PATH (install nomad, pass --nomad-bin, or pass a `nomad job run -output` JSON file directly): %w", nomadBin, err)
	}
	cmd := exec.Command(nomadBin, "job", "run", "-output", path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("nomad job run -output failed: %s", msg)
	}
	return stdout.Bytes(), nil
}

// ecsArgs carries the resolved inputs for a `scan --ecs` run.
type ecsArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runECSScan grades an AWS ECS task definition's container contract, reusing the
// SHARED ECS scorer that the --terraform aws_ecs_task_definition path also uses. It
// accepts a `aws ecs describe-task-definition` JSON, a raw registered task-def
// JSON, or a directory of task-def JSON files (weakest-container rollup across the
// lot). It fails OPEN on a load/parse failure (unreadable path, malformed JSON): it
// prints a clear diagnostic to stderr and returns nil so an opt-in CI/Action step
// never crashes the build over tooling issues. Once containers are graded, scoring
// and the --min-score CI gate apply exactly like --k8s/--helm/--terraform/--nomad.
func runECSScan(a ecsArgs) error {
	specs, err := loadECSSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --ecs could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateECS(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --ecs found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadECSSpecs resolves a --ecs argument to graded Specs. A directory is a
// weakest-container rollup: every *.json file in it is parsed and its container
// specs are pooled, so `AggregateECS` grades the single most-exposed container
// across the whole set. A single file is parsed directly. It is daemon-free and
// offline: the JSON is read from disk (the user runs the aws CLI, if at all).
func loadECSSpecs(path string) ([]scan.Spec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		return scan.SpecsFromECS(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var specs []scan.Spec
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		fileSpecs, perr := scan.SpecsFromECS(raw)
		if perr != nil {
			// A single malformed file in a directory should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --ecs skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
	}
	return specs, nil
}

// cloudRunScanArgs carries the resolved inputs for a `scan --cloudrun` run.
type cloudRunScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runCloudRunScan grades the Google Cloud Run services declared in a Knative
// Service YAML (or a directory of them), reusing the same pod-spec dimension
// mapping and weakest-link aggregate as --helm/--terraform plus Cloud Run's
// managed-runtime guarantees. Cloud Run specs are plain YAML, so there is no
// external binary to shell out to. It fails OPEN on a load/parse failure
// (unreadable path, malformed YAML): it prints a clear diagnostic to stderr and
// returns nil so an opt-in CI/Action step never crashes the build. Once services
// are graded, scoring and the --min-score CI gate apply exactly like
// --k8s/--helm/--terraform/--nomad (a genuinely low posture still fails the gate).
func runCloudRunScan(a cloudRunScanArgs) error {
	raw, err := loadCloudRunYAML(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cloudrun could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	specs, err := scan.SpecsFromCloudRun(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cloudrun could not parse %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateCloudRun(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cloudrun found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (services graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadCloudRunYAML resolves a --cloudrun argument to a Knative Service YAML stream.
// A file is read verbatim (it may itself be a multi-document `---`-separated
// stream). A directory is walked for *.yaml/*.yml files, concatenated into one
// document stream (weakest-service rollup across the deployment). It is offline
// and daemon-free: Cloud Run specs are plain YAML, so there is nothing to shell
// out to.
func loadCloudRunYAML(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return os.ReadFile(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var docs [][]byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		docs = append(docs, raw)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no .yaml/.yml files found in directory %q", path)
	}
	return bytes.Join(docs, []byte("\n---\n")), nil
}

// openShiftScanArgs carries the resolved inputs for a `scan --openshift` run.
type openShiftScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runOpenShiftScan grades the OpenShift workloads declared in a manifest set (a
// DeploymentConfig, a plain Deployment/Pod, or a directory of them), reusing the
// same pod-spec dimension mapping and weakest-link aggregate as --k8s/--kustomize.
// OpenShift manifests are plain YAML, so there is no external binary to shell out
// to. It fails OPEN on a load/parse failure (unreadable path, malformed YAML): it
// prints a clear diagnostic to stderr and returns nil so an opt-in CI/Action step
// never crashes the build. Once workloads are graded, scoring and the --min-score
// CI gate apply exactly like --k8s/--kustomize (a genuinely low posture still
// fails the gate).
func runOpenShiftScan(a openShiftScanArgs) error {
	raw, err := loadOpenShiftYAML(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --openshift could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	specs, err := scan.SpecsFromOpenShift(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --openshift could not parse %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateOpenShift(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --openshift found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (workloads graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadOpenShiftYAML resolves a --openshift argument to an OpenShift manifest
// stream. A file is read verbatim (it may itself be a multi-document
// `---`-separated stream, e.g. `oc get all -o yaml`). A directory is walked for
// *.yaml/*.yml/*.json files, concatenated into one document stream (weakest-
// workload rollup across the set). It is offline and daemon-free: OpenShift
// manifests are plain YAML/JSON, so there is nothing to shell out to.
func loadOpenShiftYAML(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return os.ReadFile(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var docs [][]byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		docs = append(docs, raw)
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no .yaml/.yml/.json files found in directory %q", path)
	}
	return bytes.Join(docs, []byte("\n---\n")), nil
}

// cloudFormationScanArgs carries the resolved inputs for a `scan --cloudformation` run.
type cloudFormationScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runCloudFormationScan grades the AWS::ECS::TaskDefinition resources declared in a
// CloudFormation template (YAML or JSON, or a directory of them), reusing the SHARED
// ECS scorer that the --ecs and --terraform aws_ecs_task_definition paths also use.
// CloudFormation templates are plain YAML/JSON, so there is no external binary to
// shell out to. It fails OPEN on a load/parse failure (unreadable path, malformed
// template): it prints a clear diagnostic to stderr and returns nil so an opt-in
// CI/Action step never crashes the build. Once containers are graded, scoring and
// the --min-score CI gate apply exactly like --ecs/--terraform. Unresolvable CFN
// intrinsics (!Ref/!Sub/Fn::...) are graded fail-closed and noted on the report.
func runCloudFormationScan(a cloudFormationScanArgs) error {
	specs, usedIntrinsics, err := loadCFNSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cloudformation could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateCloudFormation(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cloudformation found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if usedIntrinsics {
		report.Notes = append(report.Notes, "template uses CloudFormation intrinsics (!Ref/!Sub/Fn::...): unresolved values are graded as unset (fail-closed) — verify the deployed stack matches.")
	}

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadCFNSpecs resolves a --cloudformation argument to graded Specs. A single file
// is parsed directly. A directory is a weakest-container rollup: every
// *.yaml/*.yml/*.json/*.template file in it is parsed and its container specs are
// pooled, so AggregateCloudFormation grades the single most-exposed container across
// the whole set. It is daemon-free and offline: templates are read from disk. The
// bool reports whether any file used CloudFormation intrinsics that were skipped.
func loadCFNSpecs(path string) ([]scan.Spec, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, false, rerr
		}
		return scan.SpecsFromCloudFormation(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}
	var specs []scan.Spec
	usedIntrinsics := false
	for _, e := range entries {
		if e.IsDir() || !isCFNTemplateExt(e.Name()) {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, false, rerr
		}
		fileSpecs, intr, perr := scan.SpecsFromCloudFormation(raw)
		if perr != nil {
			// A single malformed template in a directory should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --cloudformation skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
		usedIntrinsics = usedIntrinsics || intr
	}
	return specs, usedIntrinsics, nil
}

// samScanArgs carries the resolved inputs for a `scan --sam` run.
type samScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runSAMScan grades the AWS::ECS::TaskDefinition resources declared in an AWS SAM
// template (Transform: AWS::Serverless-*, YAML or JSON, or a directory of them),
// reusing the SHARED ECS scorer the --cloudformation/--ecs/--terraform paths also use.
// SAM is a CloudFormation superset — raw ECS task defs are already native CFN — so
// there is no `sam` transform step or external binary to shell out to. It fails OPEN on
// a load/parse failure: it prints a clear diagnostic to stderr and returns nil so an
// opt-in CI/Action step never crashes the build. Once containers are graded, scoring and
// the --min-score CI gate apply exactly like --cloudformation. Unresolvable CFN
// intrinsics (!Ref/!Sub/Fn::...) are graded fail-closed and noted on the report.
func runSAMScan(a samScanArgs) error {
	specs, usedIntrinsics, err := loadSAMSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --sam could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateSAM(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --sam found nothing to grade in %q (no AWS::ECS::TaskDefinition; scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if usedIntrinsics {
		report.Notes = append(report.Notes, "template uses CloudFormation intrinsics (!Ref/!Sub/Fn::...): unresolved values are graded as unset (fail-closed) — verify the deployed stack matches.")
	}

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadSAMSpecs resolves a --sam argument to graded Specs. A SAM template is a
// CloudFormation superset, so it is parsed exactly like a CloudFormation input: a single
// file is parsed directly, and a directory is a weakest-container rollup over every
// *.yaml/*.yml/*.json/*.template file in it (their container specs are pooled so
// AggregateSAM grades the single most-exposed container across the whole set). It is
// daemon-free and offline. The bool reports whether any file used CFN intrinsics that
// were skipped (graded fail-closed).
func loadSAMSpecs(path string) ([]scan.Spec, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, false, rerr
		}
		return scan.SpecsFromSAM(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}
	var specs []scan.Spec
	usedIntrinsics := false
	for _, e := range entries {
		if e.IsDir() || !isCFNTemplateExt(e.Name()) {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, false, rerr
		}
		fileSpecs, intr, perr := scan.SpecsFromSAM(raw)
		if perr != nil {
			// A single malformed template in a directory should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --sam skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
		usedIntrinsics = usedIntrinsics || intr
	}
	return specs, usedIntrinsics, nil
}

// isCFNTemplateExt reports whether a filename has a CloudFormation template
// extension (.yaml/.yml/.json/.template).
func isCFNTemplateExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json", ".template":
		return true
	default:
		return false
	}
}

// pulumiScanArgs carries the resolved inputs for a `scan --pulumi` run.
type pulumiScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runPulumiScan grades the container workloads declared in Pulumi program output
// (a `pulumi stack export` checkpoint or `pulumi preview --json`, or a directory
// of them), reusing the SHARED k8s and ECS scorers that the --k8s/--helm and
// --ecs/--terraform paths also use. Pulumi output is plain JSON, so there is no
// external binary to shell out to. It fails OPEN on a load/parse failure
// (unreadable path, malformed JSON): it prints a clear diagnostic to stderr and
// returns nil so an opt-in CI/Action step never crashes the build. Once workloads
// are graded, scoring and the --min-score CI gate apply exactly like
// --terraform/--ecs.
func runPulumiScan(a pulumiScanArgs) error {
	specs, err := loadPulumiSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --pulumi could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregatePulumi(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --pulumi found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (workloads graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadPulumiSpecs resolves a --pulumi argument to graded Specs. A single file is
// parsed directly. A directory is a weakest-workload rollup: every *.json file in
// it is parsed and its workload specs are pooled, so AggregatePulumi grades the
// single most-exposed container across the whole set. It is daemon-free and
// offline: Pulumi output is read from disk.
func loadPulumiSpecs(path string) ([]scan.Spec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		return scan.SpecsFromPulumi(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var specs []scan.Spec
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".json" {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		fileSpecs, perr := scan.SpecsFromPulumi(raw)
		if perr != nil {
			// A single malformed file in a directory should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --pulumi skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
	}
	return specs, nil
}

// azureScanArgs carries the resolved inputs for a `scan --azure` run.
type azureScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runAzureScan grades the Microsoft.ContainerInstance/containerGroups declared in an
// Azure ARM template, an `az container show`/deployment JSON, or a directory of them,
// reusing the SAME pod-spec scorer as --k8s/--cloudrun plus ACI's managed-runtime
// floors. ARM/az output is plain JSON, so there is no external binary to shell out
// to. It fails OPEN on a load/parse failure (unreadable path, malformed JSON): it
// prints a clear diagnostic to stderr and returns nil so an opt-in CI/Action step
// never crashes the build. Once containers are graded, scoring and the --min-score CI
// gate apply exactly like --cloudrun/--ecs. Unresolvable ARM expressions ("[...]")
// are graded fail-closed and noted on the report.
func runAzureScan(a azureScanArgs) error {
	specs, usedExpressions, err := loadAzureSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --azure could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateAzure(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --azure found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if usedExpressions {
		report.Notes = append(report.Notes, "template uses ARM expressions (\"[parameters(...)]\"/\"[variables(...)]\"): unresolved values are graded as unset (fail-closed) — verify the deployed group matches.")
	}

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadAzureSpecs resolves a --azure argument to graded Specs. A single file is
// parsed directly. A directory is a weakest-container rollup: every *.json file in it
// is parsed and its container specs are pooled, so AggregateAzure grades the single
// most-exposed container across the whole set. It is daemon-free and offline: ARM/az
// output is read from disk. The bool reports whether any file used ARM expressions
// that were skipped.
func loadAzureSpecs(path string) ([]scan.Spec, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, false, rerr
		}
		return scan.SpecsFromAzure(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}
	var specs []scan.Spec
	usedExpressions := false
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".json" {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, false, rerr
		}
		fileSpecs, expr, perr := scan.SpecsFromAzure(raw)
		if perr != nil {
			// A single malformed file in a directory should not sink the rollup: skip
			// it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --azure skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
		usedExpressions = usedExpressions || expr
	}
	return specs, usedExpressions, nil
}

// appRunnerScanArgs carries the resolved inputs for a `scan --app-runner` run.
type appRunnerScanArgs struct {
	path      string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// bicepScanArgs carries the resolved inputs for a `scan --bicep` run.
type bicepScanArgs struct {
	path      string
	bicepBin  string
	azBin     string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runAppRunnerScan grades an AWS App Runner service, reusing the SAME pod-spec scorer
// as --cloudrun/--azure plus App Runner's managed-runtime floors. App Runner output
// is plain JSON, so there is no external binary to shell out to. It accepts an
// `aws apprunner describe-service` JSON, a raw Service object, or a directory of them
// (weakest-service rollup). It fails OPEN on a load/parse failure (unreadable path,
// malformed JSON): it prints a clear diagnostic to stderr and returns nil so an
// opt-in CI/Action step never crashes the build. Once a service is graded, scoring
// and the --min-score CI gate apply exactly like --cloudrun/--azure/--ecs.
func runAppRunnerScan(a appRunnerScanArgs) error {
	specs, err := loadAppRunnerSpecs(a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --app-runner could not load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateAppRunner(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --app-runner found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (a service was graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadAppRunnerSpecs resolves a --app-runner argument to graded Specs. A single file
// is parsed directly. A directory is a weakest-service rollup: every *.json file in
// it is parsed and its service specs are pooled, so AggregateAppRunner grades the
// single most-exposed service across the whole set. It is daemon-free and offline:
// App Runner output is read from disk (the user runs the aws CLI, if at all).
func loadAppRunnerSpecs(path string) ([]scan.Spec, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, rerr
		}
		return scan.SpecsFromAppRunner(raw)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var specs []scan.Spec
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(path, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		fileSpecs, perr := scan.SpecsFromAppRunner(raw)
		if perr != nil {
			// A single malformed file in a directory should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --app-runner skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
	}
	return specs, nil
}

// runBicepScan compiles an Azure Bicep template (a .bicep file or a directory of
// them) to ARM JSON and grades the Microsoft.ContainerInstance/containerGroups it
// declares, reusing the SAME ACI managed-runtime scorer as --azure (Bicep transpiles
// 1:1 to ARM). It fails OPEN on a compile/parse failure (bicep/az absent, unreadable
// path, malformed template): it prints a clear diagnostic to stderr and returns nil so
// an opt-in CI/Action step never crashes the build over tooling issues. Once
// containers are graded, scoring and the --min-score CI gate apply exactly like
// --azure. Unresolvable ARM expressions ("[...]") are graded fail-closed and noted.
func runBicepScan(a bicepScanArgs) error {
	specs, usedExpressions, err := loadBicepSpecs(a.bicepBin, a.azBin, a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --bicep could not compile/load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateBicep(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --bicep found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if usedExpressions {
		report.Notes = append(report.Notes, "template uses ARM expressions (\"[parameters(...)]\"/\"[variables(...)]\") after compilation: unresolved values are graded as unset (fail-closed) — verify the deployed group matches.")
	}

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// loadBicepSpecs resolves a --bicep argument to graded Specs. A single .bicep file is
// compiled to ARM and parsed directly. A directory is a weakest-container rollup:
// every *.bicep file in it is compiled and its container specs are pooled, so
// AggregateBicep grades the single most-exposed container across the whole set. It is
// offline: the bicep compiler transpiles locally without contacting Azure. The bool
// reports whether any compiled template used ARM expressions that were skipped.
func loadBicepSpecs(bicepBin, azBin, path string) ([]scan.Spec, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		arm, cerr := compileBicep(bicepBin, azBin, path)
		if cerr != nil {
			return nil, false, cerr
		}
		return scan.SpecsFromBicepARM(arm)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}
	var specs []scan.Spec
	usedExpressions := false
	compiledAny := false
	for _, e := range entries {
		if e.IsDir() || !isBicepFileExt(e.Name()) {
			continue
		}
		arm, cerr := compileBicep(bicepBin, azBin, filepath.Join(path, e.Name()))
		if cerr != nil {
			// A single template that fails to compile should not sink the rollup: skip
			// it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --bicep skipping %s (compile error): %v\n", e.Name(), cerr)
			continue
		}
		compiledAny = true
		fileSpecs, expr, perr := scan.SpecsFromBicepARM(arm)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "  warning: scan --bicep skipping %s (parse error): %v\n", e.Name(), perr)
			continue
		}
		specs = append(specs, fileSpecs...)
		usedExpressions = usedExpressions || expr
	}
	if !compiledAny {
		return nil, false, fmt.Errorf("no compilable .bicep files found in directory")
	}
	return specs, usedExpressions, nil
}

// compileBicep transpiles a single .bicep file to ARM JSON on stdout. It prefers the
// standalone `bicep build --stdout <file>` and falls back to `az bicep build --file
// <file> --stdout` (the same compiler, shipped as an Azure CLI extension) when the
// bicep binary is not on PATH. It is offline and daemon-free: compilation is local, no
// Azure connection.
func compileBicep(bicepBin, azBin, file string) ([]byte, error) {
	if _, err := exec.LookPath(bicepBin); err == nil {
		return runKustomizeRenderer(exec.Command(bicepBin, "build", "--stdout", file), "bicep build --stdout")
	}
	if _, err := exec.LookPath(azBin); err == nil {
		return runKustomizeRenderer(exec.Command(azBin, "bicep", "build", "--file", file, "--stdout"), "az bicep build")
	}
	return nil, fmt.Errorf("neither the standalone bicep binary (%q) nor the azure-cli (%q) found on PATH (install one, or pass --bicep-bin/--az-bin)", bicepBin, azBin)
}

// isBicepFileExt reports whether a filename has the Bicep source extension (.bicep).
// A .bicepparam parameter file is not a deployable template on its own and is excluded
// from a directory rollup.
func isBicepFileExt(name string) bool {
	return strings.ToLower(filepath.Ext(name)) == ".bicep"
}

// cdkScanArgs carries the resolved inputs for a `scan --cdk` run.
type cdkScanArgs struct {
	path      string
	cdkBin    string
	asJSON    bool
	md        bool
	share     bool
	badge     string
	badgeJSON string
	sarif     string
	minScore  int
}

// runCDKScan grades an AWS CDK app by way of the CloudFormation template its
// `cdk synth` step emits, reusing the SAME shared ECS scorer as
// --cloudformation/--ecs/--terraform (the CDK synthesizes standard CFN). It accepts a
// CDK app dir (synthesized here), a pre-synthesized template file, or a synthesized
// cdk.out / template directory. It fails OPEN on a synth/parse failure (aws-cdk absent,
// a bad app, unreadable path): it prints a clear diagnostic to stderr and returns nil
// so an opt-in CI/Action step never crashes the build over tooling. Once containers are
// graded, scoring and the --min-score CI gate apply exactly like --cloudformation.
// Unresolvable CDK tokens / CFN intrinsics are graded fail-closed and noted.
func runCDKScan(a cdkScanArgs) error {
	specs, usedIntrinsics, err := loadCDKSpecs(a.cdkBin, a.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cdk could not synth/load %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}

	report, worstSpec, err := scan.AggregateCDK(specs, filepath.Base(strings.TrimRight(a.path, "/")))
	if err != nil {
		fmt.Fprintf(os.Stderr, "  warning: scan --cdk found nothing to grade in %q (scan skipped, exit 0): %v\n", a.path, err)
		return nil
	}
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if usedIntrinsics {
		report.Notes = append(report.Notes, "synthesized template uses CloudFormation intrinsics / unresolved CDK tokens (!Ref/!Sub/Fn::.../${Token[...]}): unresolved values are graded as unset (fail-closed) — verify the deployed stack matches.")
	}

	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, worstSpec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (containers graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// cdkTemplate is one synthesized CloudFormation template blob plus its source name
// (for per-file diagnostics in a directory rollup).
type cdkTemplate struct {
	name string
	raw  []byte
}

// loadCDKSpecs resolves a --cdk argument to graded Specs. A single file is treated as
// an already-synthesized CloudFormation template and parsed directly. A directory is a
// weakest-container rollup: a CDK app dir (one containing cdk.json) is synthesized with
// `cdk synth`, and any other directory is read as a set of pre-synthesized templates
// (e.g. a checked-in cdk.out cloud assembly, or a plain template dir). Every template's
// container specs are pooled, so AggregateCDK grades the single most-exposed container
// across the whole app. The bool reports whether any template used intrinsics/tokens
// that were skipped (graded fail-closed).
func loadCDKSpecs(cdkBin, path string) ([]scan.Spec, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, false, rerr
		}
		return scan.SpecsFromCDK(raw)
	}

	templates, err := cdkTemplateBytes(cdkBin, path)
	if err != nil {
		return nil, false, err
	}
	if len(templates) == 0 {
		return nil, false, fmt.Errorf("no synthesized CloudFormation templates found for CDK path %q (synthesize with `cdk synth`, or point --cdk at a template file / cdk.out dir)", path)
	}

	var specs []scan.Spec
	usedIntrinsics := false
	for _, t := range templates {
		fileSpecs, intr, perr := scan.SpecsFromCDK(t.raw)
		if perr != nil {
			// A single malformed template in the assembly should not sink the rollup:
			// skip it with a diagnostic and grade the rest (fail-open per file).
			fmt.Fprintf(os.Stderr, "  warning: scan --cdk skipping %s (parse error): %v\n", t.name, perr)
			continue
		}
		specs = append(specs, fileSpecs...)
		usedIntrinsics = usedIntrinsics || intr
	}
	return specs, usedIntrinsics, nil
}

// cdkTemplateBytes resolves a --cdk directory to the synthesized CloudFormation
// template blobs to grade. A directory containing cdk.json is a CDK app: it is
// synthesized with `cdk synth`, falling back to an already-synthesized cdk.out inside
// it when the aws-cdk binary is absent (so a CI that checks in its assembly still
// grades). Any other directory is read as a set of pre-synthesized templates (a
// cdk.out cloud assembly, or a plain CloudFormation template dir).
func cdkTemplateBytes(cdkBin, dir string) ([]cdkTemplate, error) {
	if fileExists(filepath.Join(dir, "cdk.json")) {
		synthed, serr := synthCDKApp(cdkBin, dir)
		if serr != nil {
			// Fall back to a checked-in cloud assembly so a synth-less environment
			// (no aws-cdk installed) still grades an already-synthesized app.
			if fb, ferr := readTemplateDir(filepath.Join(dir, "cdk.out"), isCDKAssemblyTemplate); ferr == nil && len(fb) > 0 {
				return fb, nil
			}
			return nil, serr
		}
		return synthed, nil
	}
	// Not a CDK app dir: grade the pre-synthesized templates already present. A
	// synthesized cdk.out contains *.template.json (plus manifest/tree/assets JSON that
	// carry no ECS resource and grade to nothing); a plain dir may hold *.yaml/*.json.
	if fileExists(filepath.Join(dir, "manifest.json")) || dirHasAssemblyTemplate(dir) {
		return readTemplateDir(dir, isCDKAssemblyTemplate)
	}
	return readTemplateDir(dir, isCFNTemplateExt)
}

// synthCDKApp runs `cdk synth` on a CDK app directory into a throwaway output dir and
// returns the synthesized CloudFormation templates. It is daemon-free and local: `cdk
// synth` transpiles the app to CloudFormation without deploying. The app dir is the
// working directory so relative cdk.json/context resolves.
func synthCDKApp(cdkBin, appDir string) ([]cdkTemplate, error) {
	if _, err := exec.LookPath(cdkBin); err != nil {
		return nil, fmt.Errorf("aws-cdk binary %q not found on PATH (install aws-cdk, pass --cdk-bin, or point --cdk at a pre-synthesized template / cdk.out dir)", cdkBin)
	}
	outDir, err := os.MkdirTemp("", "ironctl-cdk-synth-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(outDir)

	cmd := exec.Command(cdkBin, "synth", "--output", outDir)
	cmd.Dir = appDir
	// `cdk synth --output` writes the cloud assembly to outDir; stdout carries the
	// human-readable template we do not need (we read the *.template.json files).
	if _, err := runKustomizeRenderer(cmd, "cdk synth"); err != nil {
		return nil, err
	}
	return readTemplateDir(outDir, isCDKAssemblyTemplate)
}

// readTemplateDir reads every file in dir whose name matches and returns its bytes.
func readTemplateDir(dir string, match func(string) bool) ([]cdkTemplate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []cdkTemplate
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, rerr
		}
		out = append(out, cdkTemplate{name: e.Name(), raw: raw})
	}
	return out, nil
}

// dirHasAssemblyTemplate reports whether dir contains a CDK cloud-assembly template
// (*.template.json), i.e. it is (or looks like) a synthesized cdk.out.
func dirHasAssemblyTemplate(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isCDKAssemblyTemplate(e.Name()) {
			return true
		}
	}
	return false
}

// isCDKAssemblyTemplate reports whether a filename is a CDK cloud-assembly
// CloudFormation template (`<Stack>.template.json`). Restricting to this suffix skips
// the assembly's manifest.json / tree.json / *.assets.json sidecars.
func isCDKAssemblyTemplate(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".template.json")
}

// fileExists reports whether path names an existing (non-directory) file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// checkArgs carries the resolved inputs for a `scan --check` run.
type checkArgs struct {
	k8s        string   // manifest path from --k8s (takes precedence)
	positional []string // fallback manifest path(s)
	asJSON     bool
	md         bool
	sarif      string
}

// runCheck is the enforce-in-place half of the generate/enforce loop: it grades a
// Kubernetes manifest and evaluates each workload against the SAME guardrail rules
// --emit-policy would generate (scan.CheckPolicy), then exits non-zero if any
// workload breaks one. This turns the scanner into a self-contained policy-as-code
// CI gate — no cluster, no Kyverno/Gatekeeper/VAP controller needed.
//
// It reuses the SAME pod-spec parser as --k8s / --emit-policy
// (SpecsFromK8sStream); a rule is enforced per-workload, so a multi-doc manifest
// reports every workload's violations. It fails CLOSED: an unreadable/unparseable
// manifest errors (non-zero), exactly like an admission gate would DENY it.
// --md/--json select the diagnostic format; --sarif writes a code-scanning log of
// the worst-scoring workload as a side artifact (best-effort, never masks the gate
// result).
func runCheck(a checkArgs) error {
	path := a.k8s
	if path == "" {
		switch len(a.positional) {
		case 0:
			return fmt.Errorf("scan --check needs a Kubernetes manifest: pass --k8s FILE or a positional path")
		case 1:
			path = a.positional[0]
		default:
			return fmt.Errorf("scan --check grades one manifest at a time; got %d", len(a.positional))
		}
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return fmt.Errorf("read manifest: %w", rerr)
	}
	specs, serr := scan.SpecsFromK8sStream(raw)
	if serr != nil {
		return serr
	}
	res := scan.CheckPolicy(specs)

	// Diagnostics. Default is the human table; --json swaps it. --md appends a
	// PR-comment/job-summary block.
	if a.asJSON {
		if err := scan.RenderCheckJSON(os.Stdout, res); err != nil {
			return err
		}
	} else {
		scan.RenderCheckText(os.Stdout, res)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderCheckMarkdown(res))
	}

	// SARIF is a best-effort side artifact anchored on the worst-scoring workload,
	// so GitHub code-scanning surfaces the violation at the manifest. An emit error
	// never masks the gate result below (fail-open on the artifact only).
	if a.sarif != "" {
		worstReport, worstSpec := worstK8sWorkload(specs)
		opts := scan.SARIFOptions{ConfigFile: path, AnchorLine: scan.AnchorLine(raw, worstSpec.Target)}
		if err := writeSARIF(a.sarif, worstReport, worstSpec, opts); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (check result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	if !res.OK() {
		return fmt.Errorf("policy check failed: %d violation(s) across %d workload(s)", len(res.Violations), res.Workloads)
	}
	return nil
}

// worstK8sWorkload scores every pod spec and returns the lowest-scoring report and
// its spec (the most porous workload), for anchoring the SARIF side artifact. Empty
// input yields zero values; the SARIF renderer tolerates an empty report.
func worstK8sWorkload(specs []scan.Spec) (scan.Report, scan.Spec) {
	var (
		worstReport scan.Report
		worstSpec   scan.Spec
		found       bool
	)
	for _, s := range specs {
		r := scan.Score(s)
		r.Version = version.String()
		if !found || r.Score < worstReport.Score {
			worstReport, worstSpec, found = r, s, true
		}
	}
	return worstReport, worstSpec
}

// emitPolicyArgs carries the resolved inputs for a `scan --emit-policy` run.
type emitPolicyArgs struct {
	engine     string   // kyverno | gatekeeper | vap
	k8s        string   // manifest path from --k8s (takes precedence)
	positional []string // fallback manifest path(s)
}

// runEmitPolicy grades a Kubernetes manifest and emits admission-policy YAML that
// blocks the controls it failed — the delta between its grade and 100/A. It reuses
// the SAME pod-spec scorer as --k8s (SpecsFromK8sStream + Score); a control is
// enforced when ANY workload in the manifest fails it, so a multi-doc file yields
// the union of every workload's gaps. Pure emission (EmitPolicy) does the rest.
func runEmitPolicy(a emitPolicyArgs) error {
	engine, err := scan.ParsePolicyEngine(a.engine)
	if err != nil {
		return err
	}
	path := a.k8s
	if path == "" {
		switch len(a.positional) {
		case 0:
			return fmt.Errorf("scan --emit-policy needs a Kubernetes manifest: pass --k8s FILE or a positional path")
		case 1:
			path = a.positional[0]
		default:
			return fmt.Errorf("scan --emit-policy grades one manifest at a time; got %d", len(a.positional))
		}
	}
	raw, rerr := os.ReadFile(path)
	if rerr != nil {
		return fmt.Errorf("read manifest: %w", rerr)
	}
	specs, serr := scan.SpecsFromK8sStream(raw)
	if serr != nil {
		return serr
	}
	reports := make([]scan.Report, len(specs))
	for i, s := range specs {
		reports[i] = scan.Score(s)
	}
	out, eerr := scan.EmitPolicy(reports, engine)
	if eerr != nil {
		return eerr
	}
	fmt.Fprint(os.Stdout, out)
	return nil
}

// k8sAdmissionArgs carries the resolved inputs for a `scan --k8s-admission` run.
type k8sAdmissionArgs struct {
	path         string
	emitResponse bool
	asJSON       bool
	md           bool
	share        bool
	badge        string
	badgeJSON    string
	sarif        string
	minScore     int
}

// runK8sAdmissionScan grades the workload carried in a Kubernetes AdmissionReview
// request and gates admission on --min-score, reusing the SAME pod-spec scorer as
// --k8s. Two output modes:
//
//   - default (scorecard): print the table/JSON/md/badge/SARIF for the admitted
//     workload, exactly like --k8s, and trip a non-zero exit below --min-score.
//     This is the local/CI shape (inspect what a webhook WOULD decide).
//   - --admission-response: emit an admission.k8s.io/v1 AdmissionReview response
//     JSON (allow/deny, echoing the request uid) to stdout. This is the webhook
//     backend shape: a thin HTTP wrapper serves stdout as the response body.
//
// It is fail-CLOSED, unlike the fail-OPEN batch modes (--helm/--terraform): an
// admission webhook is an enforcement gate, so unreadable/unparseable input, a
// missing request, or an object with nothing to grade DENIES admission (emits a
// deny response in --admission-response mode) and exits non-zero. A denied gate
// always exits non-zero; in --admission-response mode the deny JSON is still
// written to stdout first so the webhook wrapper returns a valid response body.
func runK8sAdmissionScan(a k8sAdmissionArgs) error {
	raw, err := readFileOrStdin(a.path)
	if err != nil {
		return admissionFailClosed(a, "", fmt.Errorf("read AdmissionReview: %w", err))
	}
	spec, uid, err := scan.SpecFromAdmissionReview(raw)
	if err != nil {
		return admissionFailClosed(a, uid, err)
	}

	report := scan.Score(spec)
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	target := spec.Target
	if target == "" {
		target = "workload"
	}
	allowed := a.minScore <= 0 || report.Score >= a.minScore

	// --admission-response: emit the AdmissionReview response the API server expects
	// and return. The exit code still reflects the decision (non-zero on deny) for
	// CI callers; the JSON is written first so a webhook wrapper always has a body.
	if a.emitResponse {
		var msg string
		if allowed {
			msg = fmt.Sprintf("IronClaw containment gate: %s scored %d/100 (grade %s)", target, report.Score, report.Grade)
		} else {
			msg = fmt.Sprintf("IronClaw containment gate: %s scored %d/100 (grade %s), below the required %d", target, report.Score, report.Grade, a.minScore)
		}
		out, merr := scan.AdmissionReviewResponse(uid, allowed, msg, nil)
		if merr != nil {
			return merr
		}
		fmt.Fprintln(os.Stdout, string(out))
		if !allowed {
			return fmt.Errorf("k8s-admission denied: %s", msg)
		}
		return nil
	}

	// Scorecard mode: same representations as the other file modes.
	if a.asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
	}
	if a.md {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderMarkdown(report))
	}
	if a.share {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, scan.RenderShareReceipt(report))
	}
	if a.badge != "" {
		if err := os.WriteFile(a.badge, []byte(scan.RenderBadgeSVG(report)), 0o644); err != nil {
			return fmt.Errorf("write badge: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote badge: %s\n", a.badge)
		}
	}
	if a.badgeJSON != "" {
		if err := os.WriteFile(a.badgeJSON, []byte(scan.RenderBadgeEndpointJSON(report)), 0o644); err != nil {
			return fmt.Errorf("write badge-json: %w", err)
		}
		if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote shields endpoint badge: %s\n", a.badgeJSON)
		}
	}
	if a.sarif != "" {
		// SARIF is a best-effort side artifact: fail-open, never blocks the scan.
		if err := writeSARIF(a.sarif, report, spec, scan.SARIFOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: SARIF emit failed (scan result unaffected): %v\n", err)
		} else if !a.asJSON {
			fmt.Fprintf(os.Stdout, "  wrote SARIF: %s\n", a.sarif)
		}
	}

	// CI gate: fail-closed below the requested threshold (workload graded).
	if a.minScore > 0 && report.Score < a.minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, a.minScore)
	}
	return nil
}

// admissionFailClosed handles an input that could not be graded. As an ENFORCEMENT
// gate this DENIES: in --admission-response mode it writes a deny AdmissionReview
// (echoing the uid when one was recovered) to stdout so the webhook wrapper returns
// a valid body, then returns a non-zero error in every mode.
func admissionFailClosed(a k8sAdmissionArgs, uid string, cause error) error {
	if a.emitResponse {
		msg := "IronClaw containment gate denied (fail-closed): " + cause.Error()
		if out, merr := scan.AdmissionReviewResponse(uid, false, msg, nil); merr == nil {
			fmt.Fprintln(os.Stdout, string(out))
		}
	}
	return fmt.Errorf("k8s-admission fail-closed (denied): %w", cause)
}

// readFileOrStdin reads path, or stdin when path is "-" (the webhook-backend shape,
// where the request body is piped in).
func readFileOrStdin(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeDockerfileSARIF(path string, r scan.Report, opts scan.SARIFOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := scan.RenderSARIFDockerfile(f, r, opts); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeSARIF renders the SARIF log for report r to path. It is separated so the
// caller can fail-open on any error without leaking a half-written file into a
// later step.
func writeSARIF(path string, r scan.Report, s scan.Spec, opts scan.SARIFOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := scan.RenderSARIF(f, r, s, opts); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runtimeBins carries the resolved binary name for each supported runtime.
type runtimeBins struct{ docker, podman, nerdctl string }

// containerSpec inspects a running container with the selected (or auto-detected)
// OCI runtime and parses the result into a Spec. It fails closed: an unknown or
// unreachable runtime returns a clear error rather than a silent empty scan.
func containerSpec(runtime string, bins runtimeBins, target string) (scan.Spec, error) {
	rt := strings.ToLower(strings.TrimSpace(runtime))
	if rt == "" || rt == "auto" {
		detected, err := detectRuntime(bins)
		if err != nil {
			return scan.Spec{}, err
		}
		rt = detected
	}

	switch rt {
	case "docker":
		out, err := runInspect(bins.docker, "docker", target)
		if err != nil {
			return scan.Spec{}, err
		}
		return scan.SpecFromDockerInspect(out)
	case "podman":
		out, err := runInspect(bins.podman, "podman", target)
		if err != nil {
			return scan.Spec{}, err
		}
		return scan.SpecFromPodmanInspect(out, podmanRootless(bins.podman))
	case "nerdctl":
		out, err := runInspect(bins.nerdctl, "nerdctl", target)
		if err != nil {
			return scan.Spec{}, err
		}
		return scan.SpecFromNerdctlInspect(out)
	default:
		return scan.Spec{}, fmt.Errorf("unknown --runtime %q: expected auto|docker|podman|nerdctl", runtime)
	}
}

// detectRuntime picks the first runtime whose CLI is on PATH, preferring docker,
// then podman, then nerdctl. Fails closed with actionable guidance when none is
// found so a missing runtime never masquerades as a clean scan.
func detectRuntime(bins runtimeBins) (string, error) {
	for _, c := range []struct{ name, bin string }{
		{"docker", bins.docker},
		{"podman", bins.podman},
		{"nerdctl", bins.nerdctl},
	} {
		if _, err := exec.LookPath(c.bin); err == nil {
			return c.name, nil
		}
	}
	return "", fmt.Errorf("no container runtime found (looked for docker, podman, nerdctl on PATH); install one or pass --runtime and the matching --<runtime>-bin")
}

// runInspect runs `<bin> inspect <target>` and returns its stdout, surfacing the
// runtime's own stderr on failure.
func runInspect(bin, runtime, target string) ([]byte, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%s runtime selected but %q is not on PATH: %w", runtime, bin, err)
	}
	out, err := exec.Command(bin, "inspect", target).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(ee.Stderr))
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("%s inspect %s: %s", runtime, target, msg)
		}
		return nil, fmt.Errorf("run %s inspect: %w (is %s running and the container up?)", bin, err, runtime)
	}
	return out, nil
}

// podmanRootless probes `podman info` for the daemon-level rootless flag. On any
// error it returns Unknown; the podman adapter then falls back to the per-container
// uid-map evidence, so this is best-effort corroboration, not a hard dependency.
func podmanRootless(bin string) scan.Tristate {
	out, err := exec.Command(bin, "info", "--format", "{{.Host.Security.Rootless}}").Output()
	if err != nil {
		return scan.Unknown
	}
	switch strings.TrimSpace(string(out)) {
	case "true":
		return scan.Yes
	case "false":
		return scan.No
	default:
		return scan.Unknown
	}
}

func scanUsage(w *os.File) {
	fmt.Fprint(w, `ironctl scan — grade a container's containment posture (0-100)

USAGE:
  ironctl scan <container>                    grade a running container (docker|podman|nerdctl)
  ironctl scan --compose FILE [--service N]   grade a docker-compose service
  ironctl scan --k8s FILE                     grade a Kubernetes pod/workload manifest
  ironctl scan --k8s-admission FILE            grade the workload in a Kubernetes AdmissionReview JSON (webhook backend; '-' = stdin)
  ironctl scan --k8s FILE --emit-policy=ENGINE emit Kyverno/Gatekeeper/VAP policy YAML that blocks the controls the manifest failed
  ironctl scan --helm CHART                    render a Helm chart (dir or .tgz) and grade its workloads
  ironctl scan --terraform PATH                grade container workloads in a terraform show -json file/dir
  ironctl scan --nomad PATH                     grade docker-driver tasks in a Nomad job spec (JSON or HCL)
  ironctl scan --ecs PATH                       grade an AWS ECS task definition (describe-task-definition JSON / dir)
  ironctl scan --cloudrun PATH                  grade Google Cloud Run service specs (Knative Service YAML or dir)
  ironctl scan --cloudformation PATH            grade AWS::ECS::TaskDefinition in a CloudFormation template (YAML/JSON or dir)
  ironctl scan --sam PATH                        grade AWS::ECS::TaskDefinition in an AWS SAM template (Transform: AWS::Serverless-*, YAML/JSON or dir)
  ironctl scan --pulumi PATH                     grade container workloads in Pulumi stack-export / preview --json output (or dir)
  ironctl scan --azure PATH                     grade Azure Container Instances (ARM template / az container show JSON or dir)
  ironctl scan --app-runner PATH                grade an AWS App Runner service (apprunner describe-service JSON / dir)
  ironctl scan --cdk PATH                        synth an AWS CDK app (cdk synth) or grade a pre-synthesized template/cdk.out
  ironctl scan --kustomize DIR                  render a kustomization (kustomize build) and grade its workloads
  ironctl scan --openshift PATH                 grade OpenShift workloads (DeploymentConfig/Deployment/Pod) in a manifest file or dir
  ironctl scan --dockerfile FILE...           grade Dockerfile(s) statically (no daemon, no pull)
  ironctl scan --compare A B                   diff two containers' isolation posture

FLAGS:
  --runtime NAME      container runtime: auto|docker|podman|nerdctl (default: auto)
  --compare           compare two positional targets side-by-side (score/verdict diff)
  --json              emit the scorecard as JSON
  --fix               emit concrete remediation config for each failed dimension
  --remediate         alias for --fix
  --badge PATH        write a shareable SVG badge to PATH
  --badge-json PATH   write a shields.io endpoint JSON badge to PATH (live README badge)
  --sarif PATH        write a SARIF 2.1.0 log to PATH (GitHub code-scanning upload)
  --md                print a shareable markdown block
  --share             print a shareable scan receipt (grade badge + breakdown + hosted receipt link + install CTA; offline)
  --min-score N       exit non-zero if the score is below N (CI gate; the admission threshold for --k8s-admission)
  --admission-response  with --k8s-admission, emit an AdmissionReview response JSON (allow/deny) to stdout
  --service NAME      compose service to grade (if the file has >1)
  --helm-bin BIN      helm binary for `+"`helm template`"+` (default: helm)
  --terraform-bin BIN terraform binary for `+"`terraform show -json`"+` (default: terraform)
  --nomad-bin BIN     nomad binary for `+"`nomad job run -output`"+` (HCL->JSON; default: nomad)
  --kustomize-bin BIN kustomize binary for `+"`kustomize build`"+` (default: kustomize)
  --kubectl-bin BIN   kubectl binary for `+"`kubectl kustomize`"+` fallback (default: kubectl)
  --docker-bin BIN    docker binary for `+"`docker inspect`"+` (default: docker)
  --podman-bin BIN    podman binary for `+"`podman inspect`"+` (default: podman)
  --nerdctl-bin BIN   nerdctl binary for `+"`nerdctl inspect`"+` (default: nerdctl)

Runtime is auto-detected (docker, then podman, then nerdctl on PATH); override
with --runtime. Rootless podman is credited: a userns remap of container-uid 0
to an unprivileged host uid earns credit on the non-root dimension. A recognized
hardened runtime (gVisor/Kata/Firecracker) is surfaced informationally, but per
IRO-429 scoring stays runtime-agnostic (no points for a runtime name).

Dimensions graded: non-root user, dropped capabilities, seccomp, network
isolation, read-only rootfs, docker.sock exposure, shared host namespaces.
Unknown postures are graded fail-closed (as insecure).

--helm renders a chart locally with `+"`helm template`"+` (no cluster, daemon-free) and
grades the rendered workloads with the same k8s dimension set. The chart grade is
the WEAKEST rendered workload (a chart is only as isolated as its most-exposed
pod); every workload's score is listed in the notes. Network egress depends on a
NetworkPolicy that a pod spec does not carry, so it is graded conservatively
(the honest static ceiling). Rendering failures (helm absent / template error)
fail OPEN: a clear diagnostic and exit 0, so an opt-in CI step never crashes the
build. A successful render still trips --min-score on a low posture.

--terraform consumes `+"`terraform show -json`"+` output (a plan.json or state.json, or a
Terraform dir it runs `+"`terraform show -json`"+` against) and grades the container
workloads it declares: the kubernetes provider's pod/workload resources (mapped
through the SAME pod-spec scorer as --k8s/--helm) and aws_ecs_task_definition
container definitions. The plan grade is the WEAKEST workload; load/parse failures
fail OPEN (diagnostic + exit 0). A successful parse still trips --min-score.

--nomad grades the docker-driver tasks in a HashiCorp Nomad job spec, mapping each
task's `+"`config`"+` block (privileged, cap_add/cap_drop, network_mode, volumes/mount,
pid_mode/ipc_mode, security_opt, readonly_rootfs) through the SAME docker/compose
dimension scorer. Pass a JSON job (a `+"`nomad job run -output`"+` redirect or an API
job) for the daemon-free path, or a `+"`.hcl`/`.nomad`"+` file that is rendered to JSON
via `+"`nomad job run -output`"+` when the nomad binary is on PATH. The job grade is the
WEAKEST task; load/parse failures fail OPEN (diagnostic + exit 0). A successful
parse still trips --min-score.

--ecs grades an AWS ECS task definition's container contract, reusing the SAME ECS
dimension mapping as the --terraform aws_ecs_task_definition path (privileged,
readonlyRootFilesystem, user, linuxParameters.capabilities.{add,drop},
dockerSecurityOptions, and the task-level networkMode/pidMode/ipcMode). It accepts
the output of `+"`aws ecs describe-task-definition`"+` (a {taskDefinition:{...}}
wrapper), a raw registered task-def JSON (containerDefinitions[] at the root), or a
directory of task-def JSON files (weakest-container rollup across the lot). This is
the LIVE counterpart to --terraform: it grades the registered JSON that AWS-console
/CDK/Copilot users have but never express as terraform. networkMode host is worst;
awsvpc/bridge are egress-capable NICs (not host); ECS default seccomp is confined.
The task grade is the WEAKEST container; load/parse failures fail OPEN (diagnostic +
exit 0). A successful parse still trips --min-score.

--cloudrun grades a Google Cloud Run service spec — a Knative Service YAML
(`+"`gcloud run services describe SVC --format=export`"+`), or a directory of them (weakest
service governs). Cloud Run's revision template carries a Kubernetes-shaped pod
spec, so it reuses the same pod-spec scorer as --k8s/--helm and then folds in
Cloud Run's MANAGED-RUNTIME guarantees: the platform forbids privileged mode and
host PID/IPC/network namespaces, never mounts a host docker.sock, and sandboxes
every container (gen1 gVisor / gen2 microVM) so the syscall surface is filtered by
default — those dimensions pass by construction. Non-root user and a read-only
rootfs stay the spec's job (graded fail-closed when absent). Egress is managed
(allowed by default, restrictable via VPC egress settings) and can never be
network=none, so a fully hardened Cloud Run service tops out at 89/100 (grade B),
the honest ceiling for an egress-capable managed runtime. Load/parse failures fail
OPEN (diagnostic + exit 0). A successful parse still trips --min-score.

--cloudformation grades the AWS::ECS::TaskDefinition resources in a CloudFormation
template (YAML or JSON), reusing the SAME ECS dimension mapping as --ecs and the
--terraform aws_ecs_task_definition path (privileged, readonlyRootFilesystem, user,
linuxParameters.capabilities.{add,drop}, dockerSecurityOptions, and the task-level
networkMode/pidMode/ipcMode). CloudFormation's PascalCase properties map onto the
same task-def contract, so a template grades identically to the registered JSON of
the same task. Pass a single template file or a directory of them (weakest-container
rollup across the lot). CloudFormation intrinsics (`+"`!Ref`/`!Sub`/`Fn::*`"+`) cannot be
resolved without the deployed stack, so any graded field they cover is treated as
unset and graded fail-closed (and noted). The template grade is the WEAKEST
container; load/parse failures fail OPEN (diagnostic + exit 0). A successful parse
still trips --min-score.

--azure grades the Microsoft.ContainerInstance/containerGroups declared in an Azure
ARM template, an `+"`az container show`"+`/deployment JSON, or a directory of them,
reusing the SAME pod-spec scorer as --k8s/--cloudrun plus Azure Container Instances'
managed-runtime floors: each container group runs in a dedicated Hyper-V-isolated VM,
so host PID/IPC/network namespaces and a host docker.sock are unreachable, privileged
is not permitted on the Standard SKU, and a default seccomp profile is applied.
Egress is managed (never network=none). ACI's securityContext does NOT express a
read-only root filesystem, and a container may ADD capabilities, so a fully hardened
ACI container tops out at 79/100 (grade B) — one dimension below Cloud Run's 89/B.
ARM expressions (`+"`\"[parameters(...)]\"`"+`) cannot be resolved without the deployment, so
any graded field they cover is treated as unset and graded fail-closed (and noted).
The grade is the WEAKEST container in the group; load/parse failures fail OPEN
(diagnostic + exit 0). A successful parse still trips --min-score.

--kustomize renders a kustomization directory locally with `+"`kustomize build`"+` (or
`+"`kubectl kustomize`"+` when the standalone binary is absent — no cluster, daemon-free)
and grades the flattened workloads with the same k8s dimension set as --helm. The
build grade is the WEAKEST rendered workload (a kustomization is only as isolated
as its most-exposed pod); every workload's score is listed in the notes. As with
--helm, network egress depends on a NetworkPolicy a pod spec does not carry, so it
is graded conservatively (the honest static ceiling). Render failures (neither
kustomize nor kubectl on PATH / build error) fail OPEN (diagnostic + exit 0); a
successful render still trips --min-score.

--openshift grades the workloads in an OpenShift manifest set — an `+"`oc get -o yaml`"+`
export, a raw manifest file, or a directory of them. An OpenShift DeploymentConfig
(apps.openshift.io/v1) embeds a standard Kubernetes PodSpec at spec.template.spec,
so it reuses the SAME pod-spec scorer as --k8s/--kustomize with no new dimensions;
plain Deployment/Pod docs in the same stream grade too and OpenShift-only kinds
(Route, ImageStream, BuildConfig, …) are skipped. The set grade is the WEAKEST
workload (a manifest set is only as isolated as its most-exposed pod); every
workload's score is listed in the notes. As with --k8s, network egress depends on a
NetworkPolicy a pod spec does not carry, so it is graded conservatively. Manifests
are plain YAML/JSON (no external binary); a load/parse failure fails OPEN
(diagnostic + exit 0) and a successful parse still trips --min-score.

--pulumi grades the container workloads declared in Pulumi program output — a
`+"`pulumi stack export`"+` checkpoint or `+"`pulumi preview --json`"+` (or a directory of
them) — reusing the SAME scorers as the other input modes. Pulumi's kubernetes
inputs ARE the Kubernetes API object, so kubernetes:* pods/workloads map through
the SAME pod-spec scorer as --k8s/--helm; aws ECS task definitions (classic
`+"`aws:ecs/taskDefinition:TaskDefinition`"+` and `+"`aws-native:ecs:TaskDefinition`"+`) map
through the SAME ECS scorer as --ecs/--terraform. A program therefore grades
identically to the equivalent terraform/ECS/k8s input of the same workload. The
program grade is the WEAKEST workload; load/parse failures fail OPEN (diagnostic +
exit 0). A successful parse still trips --min-score.

--dockerfile grades a DIFFERENT, authoring-time dimension set (non-root USER,
pinned base image, no secrets in ENV/ARG, COPY over remote/opaque ADD, no
world-writable files, HEALTHCHECK, layer hygiene). Runtime hardening (caps,
seccomp, network, rootfs, docker.sock) is set at run time and is NOT expressible
in a Dockerfile, so a high static grade still needs a runtime scan.

--k8s-admission turns scan into an in-cluster ENFORCEMENT gate. It reads a
Kubernetes admission.k8s.io/v1 AdmissionReview request ('-' = stdin), grades the
admitted workload through the SAME pod-spec scorer as --k8s, and gates admission
on --min-score. By default it prints the scorecard (what a webhook WOULD decide);
with --admission-response it emits an AdmissionReview response JSON (allow/deny,
echoing the request uid) so a thin ValidatingWebhook wrapper can serve stdout as
the response body. Unlike the fail-OPEN batch modes, it is fail-CLOSED: an
unparseable review or an object with nothing to grade DENIES admission and exits
non-zero (the deny JSON is still written in --admission-response mode).
`)
}
