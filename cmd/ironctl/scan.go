package main

import (
	"bytes"
	"flag"
	"fmt"
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
	sarif := fs.String("sarif", "", "write a SARIF 2.1.0 log to this path (GitHub code-scanning upload)")
	md := fs.Bool("md", false, "print a shareable markdown block (README/blog section)")
	fix := fs.Bool("fix", false, "emit concrete remediation config for each failed dimension")
	remediate := fs.Bool("remediate", false, "alias for --fix")
	compareFlag := fs.Bool("compare", false, "compare two containers side-by-side (takes two positional targets)")
	compose := fs.String("compose", "", "grade a service in this docker-compose file")
	service := fs.String("service", "", "compose service name (required if the file has >1 service)")
	k8s := fs.String("k8s", "", "grade the first container in this Kubernetes pod/workload manifest")
	helm := fs.String("helm", "", "render a Helm chart (dir or .tgz) with `helm template` and grade the isolation posture of its workloads")
	helmBin := fs.String("helm-bin", envOrDefault("HELM", "helm"), "helm binary used to render the chart")
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
			badge:     *badge,
			badgeJSON: *badgeJSON,
			sarif:     *sarif,
			minScore:  *minScore,
		})
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

// writeDockerfileSARIF renders the Dockerfile SARIF log to path, fail-open on error.
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
  ironctl scan --helm CHART                    render a Helm chart (dir or .tgz) and grade its workloads
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
  --min-score N       exit non-zero if the score is below N (CI gate)
  --service NAME      compose service to grade (if the file has >1)
  --helm-bin BIN      helm binary for `+"`helm template`"+` (default: helm)
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

--dockerfile grades a DIFFERENT, authoring-time dimension set (non-root USER,
pinned base image, no secrets in ENV/ARG, COPY over remote/opaque ADD, no
world-writable files, HEALTHCHECK, layer hygiene). Runtime hardening (caps,
seccomp, network, rootfs, docker.sock) is set at run time and is NOT expressible
in a Dockerfile, so a high static grade still needs a runtime scan.
`)
}
