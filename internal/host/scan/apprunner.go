package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SpecsFromAppRunner parses an AWS App Runner service definition — the JSON emitted
// by `aws apprunner describe-service`, a raw Service object, or a directory of them
// (via the CLI wrapper) — and returns one graded Spec per service. It is the App
// Runner counterpart to the --cloudrun (GCP) and --azure (ACI) managed-runtime
// paths.
//
// The mapping reuses the SAME specFromPodSpec scorer as --k8s/--cloudrun/--azure and
// then folds in App Runner's managed-runtime floors. App Runner runs your container
// on AWS Fargate (a Firecracker microVM), so host PID/IPC/network namespaces and a
// host docker.sock are unreachable, privileged mode is not permitted, and a default
// seccomp profile is applied — those dimensions pass by construction. Egress is
// managed (allowed by default, never network=none), so the network dimension caps at
// the WARN tier.
//
// App Runner is HONEST-graded lower than Cloud Run (89/B) and ACI (79/B) because its
// service configuration exposes NO container securityContext at all — there is no
// field to set a non-root user, drop capabilities, or enable a read-only root
// filesystem (unlike Cloud Run's pod spec or ACI's securityContext). Those
// user-hardenable dimensions are therefore always graded fail-closed:
//   - non-root user: unexpressible (App Runner respects the image USER, which the
//     service config cannot override) → fail-closed (−15).
//   - read-only rootfs: no field (like ACI) → fail-closed (−10).
//   - capabilities: App Runner runs on Fargate, which retains Docker's default
//     capability set and gives no knob to drop it, so caps are NOT credited as
//     dropped (unlike Cloud Run, whose runtime strips capabilities).
//
// A fully hardened App Runner service therefore tops out at 48/100 (grade D) on the
// managed-runtime floors alone (seccomp 15 + network 4 + docker.sock 15 + host-ns 10
// + default-caps-retained 4). The strong Firecracker microVM boundary is surfaced as
// an informational HardenedRuntime note but awards no points (scoring stays
// runtime-agnostic per IRO-429). The honest takeaway the scan documents: App Runner
// buys you microVM isolation but almost no config-expressible hardening surface.
//
// It is pure and unit-testable: the caller reads the file / runs the aws CLI (I/O)
// and passes the JSON here. It fails OPEN on a malformed top-level document (returns
// the parse error); the CLI wrapper turns that into a skip so an opt-in CI step never
// crashes the build. A document with no App Runner service returns no specs.
func SpecsFromAppRunner(raw []byte) ([]Spec, error) {
	var doc appRunnerDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse app runner service JSON: %w", err)
	}
	// describe-service wraps the service under {"Service": {...}}; a raw Service
	// object carries the fields at the document root. Prefer the wrapper.
	svc := doc.appRunnerService
	if doc.Service != nil {
		svc = *doc.Service
	}
	if !svc.isAppRunnerService() {
		return nil, nil
	}
	return []Spec{specFromAppRunnerService(svc)}, nil
}

// AggregateAppRunner folds one or more App Runner service specs into a single Report.
// Like AggregateCloudRun/AggregateAzure, the aggregate is the WEAKEST service
// (minimum score): a deployment is only as isolated as its most-exposed service, so
// grading the weakest link is the honest, fail-closed summary. target names the
// source (file/dir). It returns the aggregate Report and the Spec that produced it
// (for SARIF anchoring). It is pure; the caller injects Version/GeneratedAt.
// Fail-closed: an empty service set is an error (a document that declares no App
// Runner service is not a pass).
func AggregateAppRunner(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable App Runner services found (looked for a describe-service document or a raw Service object with SourceConfiguration)")
	}

	all := make([]scoredWorkload, len(specs))
	worst := 0
	for i, s := range specs {
		all[i] = scoredWorkload{spec: s, report: Score(s)}
		if all[i].report.Score < all[worst].report.Score {
			worst = i
		}
	}

	agg := all[worst].report
	worstSpec := all[worst].spec
	if strings.TrimSpace(target) != "" {
		agg.Target = target
	}
	agg.Source = "app-runner"
	agg.Notes = append(appRunnerSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// appRunnerSummaryNotes builds the deployment-level roll-up notes for an aggregate
// App Runner report: a headline naming the weakest service and a per-service score
// list.
func appRunnerSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d App Runner service(s); the grade is the WEAKEST (a deployment is only as isolated as its most-exposed service). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "service"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "service"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-service: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// AWS App Runner service document model.
//
// `aws apprunner describe-service` wraps the service under a top-level "Service"
// key; a raw Service object (from the API/SDK) carries the fields at the root. Both
// share the same PascalCase shape. App Runner exposes NO container securityContext,
// so the security-relevant fields are limited to the network egress mode and the
// (informational) source image / instance sizing.
// --------------------------------------------------------------------------- //

// appRunnerDoc accepts both input shapes at once: the describe-service wrapper
// (Service) and a raw Service object (the embedded appRunnerService fields at the
// document root).
type appRunnerDoc struct {
	Service *appRunnerService `json:"Service"`
	appRunnerService
}

type appRunnerService struct {
	ServiceName         string `json:"ServiceName"`
	ServiceArn          string `json:"ServiceArn"`
	SourceConfiguration struct {
		ImageRepository *struct {
			ImageIdentifier     string `json:"ImageIdentifier"`
			ImageRepositoryType string `json:"ImageRepositoryType"`
			ImageConfiguration  *struct {
				Port string `json:"Port"`
			} `json:"ImageConfiguration"`
		} `json:"ImageRepository"`
		CodeRepository *struct {
			RepositoryUrl string `json:"RepositoryUrl"`
		} `json:"CodeRepository"`
	} `json:"SourceConfiguration"`
	InstanceConfiguration struct {
		Cpu             string `json:"Cpu"`
		Memory          string `json:"Memory"`
		InstanceRoleArn string `json:"InstanceRoleArn"`
	} `json:"InstanceConfiguration"`
	NetworkConfiguration struct {
		EgressConfiguration *struct {
			EgressType      string `json:"EgressType"`
			VpcConnectorArn string `json:"VpcConnectorArn"`
		} `json:"EgressConfiguration"`
		IngressConfiguration *struct {
			IsPubliclyAccessible *bool `json:"IsPubliclyAccessible"`
		} `json:"IngressConfiguration"`
	} `json:"NetworkConfiguration"`
}

// isAppRunnerService reports whether a decoded document is an App Runner service
// worth grading. It matches on an App Runner service ARN or the presence of a
// SourceConfiguration image/code repository, so an unrelated JSON object is not
// mistaken for a service.
func (svc appRunnerService) isAppRunnerService() bool {
	if strings.Contains(strings.ToLower(svc.ServiceArn), ":apprunner:") {
		return true
	}
	return svc.SourceConfiguration.ImageRepository != nil || svc.SourceConfiguration.CodeRepository != nil
}

// specFromAppRunnerService maps one App Runner service to a graded Spec. It grades an
// EMPTY container securityContext through specFromPodSpec (App Runner exposes no
// securityContext, so non-root / read-only rootfs / capabilities all grade
// fail-closed exactly as an unset k8s container would), then folds in App Runner's
// managed-runtime floors.
func specFromAppRunnerService(svc appRunnerService) Spec {
	name := strings.TrimSpace(svc.ServiceName)
	target := "app-runner/" + nz(name, "service")

	// App Runner has NO container securityContext: grade an empty pod spec so the
	// user-hardenable dimensions (non-root, read-only rootfs, capabilities) fall to
	// their fail-closed defaults, exactly like an unset k8s container.
	ps := podSpec{Containers: []k8sContainer{{Name: name}}}
	s := specFromPodSpec("app-runner", target, ps)

	// The k8s pod-spec grader appends conservative notes that are WRONG for App
	// Runner's managed runtime (it assumes an invisible NetworkPolicy governs egress,
	// that seccomp is unconfined by default, and phrases non-root in k8s terms). Drop
	// them; the managed floors below give concrete, honest answers.
	s.Notes = dropNotesContaining(s.Notes,
		"NetworkPolicy", "egress depends on", "no seccompProfile set", "no runAsNonRoot/runAsUser set")

	// --- managed-runtime floors (platform-enforced, not spec-visible) ----------
	// App Runner runs every service on AWS Fargate (a Firecracker microVM): there is
	// no tenant host to share, so host PID/IPC/network namespaces and a host docker
	// socket are unreachable, and privileged mode is not permitted. Credit them
	// explicitly (the raw pod-spec grader scores them fail-closed / Unknown).
	s.Privileged = No
	s.HostPID = No
	s.HostIPC = No
	s.HostNetwork = No
	s.DockerSock = No

	// Capabilities: unlike Cloud Run (whose runtime strips all Linux capabilities),
	// App Runner runs on Fargate, which RETAINS Docker's default capability set and
	// exposes no knob to drop it. Grade it honestly as the default set retained (a
	// FAIL earning partial credit), NOT as drop-all.
	s.CapDropAll = No

	// Fargate applies Docker's default seccomp profile: an unset profile is confined
	// here, not unconfined (mirrors the docker/ecs default-profile mapping).
	if strings.TrimSpace(s.Seccomp) == "" {
		s.Seccomp = "confined"
	}

	// --- network (managed egress → honest WARN tier) --------------------------
	// App Runner egress is managed (public by default, or routed through a VPC
	// connector) and can never be network=none, so grade egress-capable (WARN).
	egress := "app-runner (managed egress)"
	if ec := svc.NetworkConfiguration.EgressConfiguration; ec != nil && strings.EqualFold(strings.TrimSpace(ec.EgressType), "VPC") {
		egress = "app-runner (VPC egress)"
	}
	s.NetworkMode = egress

	// Firecracker microVM isolation: surface it informationally (scoring stays
	// runtime-agnostic per IRO-429, so no points are awarded for the runtime name).
	s.Runtime = "firecracker"

	// --- managed-runtime + honest-ceiling notes -------------------------------
	s.Notes = append(s.Notes,
		"AWS App Runner runs every service on AWS Fargate (a Firecracker microVM); host PID/IPC/network namespaces and a host docker.sock are not reachable, and privileged mode is not permitted — those dimensions pass by construction",
		"App Runner's service configuration exposes NO container securityContext: you cannot set a non-root user, drop capabilities, or enable a read-only root filesystem, so those dimensions are graded fail-closed",
		"managed runtime: Fargate applies Docker's default seccomp profile but RETAINS Docker's default capability set (App Runner gives no knob to drop it) — unlike Cloud Run, capabilities are not credited as dropped",
		"a fully hardened App Runner service tops out at 48/100 (grade D): the managed-runtime floors (seccomp, network, no docker.sock, no host namespaces) are the only expressible wins — App Runner buys microVM isolation but almost no config-expressible hardening surface")

	// Source image / instance sizing are informational evidence — not containment
	// dimensions in the 7-dim scorer.
	if img := svc.SourceConfiguration.ImageRepository; img != nil && strings.TrimSpace(img.ImageIdentifier) != "" {
		s.Notes = append(s.Notes, "source image: "+strings.TrimSpace(img.ImageIdentifier)+" (informational — the image USER determines the runtime uid, which App Runner cannot override)")
	} else if code := svc.SourceConfiguration.CodeRepository; code != nil {
		s.Notes = append(s.Notes, "source: code repository build (App Runner builds and runs the image; the runtime uid is set by the managed builder)")
	}
	if cpu, mem := strings.TrimSpace(svc.InstanceConfiguration.Cpu), strings.TrimSpace(svc.InstanceConfiguration.Memory); cpu != "" || mem != "" {
		s.Notes = append(s.Notes, fmt.Sprintf("instance sizing: cpu=%s memory=%s (informational — not a containment dimension)", nz(cpu, "default"), nz(mem, "default")))
	}

	return s
}
