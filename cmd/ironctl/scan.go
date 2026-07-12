package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
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
`)
}
