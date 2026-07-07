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
//	  [--json] [--badge scan.svg] [--md] [--min-score N]
func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit the scorecard as JSON")
	badge := fs.String("badge", "", "write a shareable SVG badge to this path")
	md := fs.Bool("md", false, "print a shareable markdown block (README/blog section)")
	compose := fs.String("compose", "", "grade a service in this docker-compose file")
	service := fs.String("service", "", "compose service name (required if the file has >1 service)")
	k8s := fs.String("k8s", "", "grade the first container in this Kubernetes pod/workload manifest")
	dockerBin := fs.String("docker-bin", envOrDefault("DOCKER", "docker"), "docker binary used for `docker inspect`")
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

	// Resolve the source and build a normalized Spec.
	var (
		spec scan.Spec
		err  error
	)
	switch {
	case *compose != "":
		raw, rerr := os.ReadFile(*compose)
		if rerr != nil {
			return fmt.Errorf("read compose file: %w", rerr)
		}
		spec, err = scan.SpecFromCompose(raw, *service)
	case *k8s != "":
		raw, rerr := os.ReadFile(*k8s)
		if rerr != nil {
			return fmt.Errorf("read manifest: %w", rerr)
		}
		spec, err = scan.SpecFromK8s(raw)
	default:
		if len(positional) < 1 {
			scanUsage(os.Stderr)
			return fmt.Errorf("scan needs a target: a container name/id, or --compose/--k8s FILE")
		}
		if len(positional) > 1 {
			return fmt.Errorf("scan grades one target at a time; got %d: %s", len(positional), strings.Join(positional, " "))
		}
		spec, err = dockerSpec(*dockerBin, positional[0])
	}
	if err != nil {
		return err
	}

	report := scan.Score(spec)
	report.Version = version.String()
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	// Emit the requested representations. Table is the default; --json swaps it.
	if *asJSON {
		if err := scan.RenderJSON(os.Stdout, report); err != nil {
			return err
		}
	} else {
		scan.RenderTable(os.Stdout, report)
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

	// CI gate: fail-closed below the requested threshold.
	if *minScore > 0 && report.Score < *minScore {
		return fmt.Errorf("containment score %d/100 is below the required %d", report.Score, *minScore)
	}
	return nil
}

// dockerSpec runs `docker inspect <target>` and parses the result into a Spec.
func dockerSpec(dockerBin, target string) (scan.Spec, error) {
	cmd := exec.Command(dockerBin, "inspect", target)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(ee.Stderr))
			if msg == "" {
				msg = err.Error()
			}
			return scan.Spec{}, fmt.Errorf("docker inspect %s: %s", target, msg)
		}
		return scan.Spec{}, fmt.Errorf("run %s inspect: %w (is Docker installed and the container running?)", dockerBin, err)
	}
	return scan.SpecFromDockerInspect(out)
}

func scanUsage(w *os.File) {
	fmt.Fprint(w, `ironctl scan — grade a container's containment posture (0-100)

USAGE:
  ironctl scan <container>                    grade a running docker container
  ironctl scan --compose FILE [--service N]   grade a docker-compose service
  ironctl scan --k8s FILE                     grade a Kubernetes pod/workload manifest

FLAGS:
  --json              emit the scorecard as JSON
  --badge PATH        write a shareable SVG badge to PATH
  --md                print a shareable markdown block
  --min-score N       exit non-zero if the score is below N (CI gate)
  --service NAME      compose service to grade (if the file has >1)
  --docker-bin BIN    docker binary for `+"`docker inspect`"+` (default: docker)

Dimensions graded: non-root user, dropped capabilities, seccomp, network
isolation, read-only rootfs, docker.sock exposure, shared host namespaces.
Unknown postures are graded fail-closed (as insecure).
`)
}
