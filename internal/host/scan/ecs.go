package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SpecsFromECS parses an AWS ECS task definition and returns one graded Spec per
// container definition. It accepts the two JSON shapes a user actually has:
//
//   - `aws ecs describe-task-definition --task-definition NAME` output, which
//     wraps the definition under a top-level {"taskDefinition": {...}} key.
//   - a raw registered/authored task definition whose root object carries
//     containerDefinitions[] directly (CDK / Copilot / hand-written API JSON).
//
// This is the LIVE counterpart to the terraform adapter's aws_ecs_task_definition
// path (SpecsFromTerraform): terraform grades the definition expressed in HCL/plan
// (where container_definitions is a JSON-encoded STRING), while --ecs grades the
// registered JSON that most AWS-console/CDK/Copilot users have but never express as
// terraform. Both decode into the SAME ecsContainerDef and grade through the SAME
// ecsSpec mapper, so the two entrypoints can never diverge.
//
// It is pure and unit-testable: the caller reads the file / runs the aws CLI (I/O)
// and passes the JSON here. It fails OPEN on a malformed top-level document
// (returns the parse error); the CLI wrapper turns that into a skip so an opt-in CI
// step never crashes the build.
func SpecsFromECS(raw []byte) ([]Spec, error) {
	var doc ecsDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse ecs task definition JSON: %w", err)
	}
	// describe-task-definition wraps the definition; a raw registered task def puts
	// containerDefinitions at the root. Prefer the wrapper when present.
	td := doc.ecsTaskDef
	if doc.TaskDefinition != nil && len(doc.TaskDefinition.ContainerDefinitions) > 0 {
		td = *doc.TaskDefinition
	}
	if len(td.ContainerDefinitions) == 0 {
		return nil, nil
	}

	family := nz(td.Family, "task")

	// docker.sock exposure is a task-level property: an ECS host volume whose source
	// path is the control socket is mountable into any container in the task.
	dockerSock := No
	for _, vol := range td.Volumes {
		if vol.Host != nil && isControlSocket(vol.Host.SourcePath) {
			dockerSock = Yes
		}
	}

	modes := ecsTaskModes{NetworkMode: td.NetworkMode, PidMode: td.PidMode, IpcMode: td.IpcMode}
	specs := make([]Spec, 0, len(td.ContainerDefinitions))
	for _, d := range td.ContainerDefinitions {
		specs = append(specs, ecsSpec("ecs", family, d, modes, dockerSock))
	}
	return specs, nil
}

// AggregateECS folds the per-container specs from one or more ECS task definitions
// into a single Report. Like AggregateTerraform/AggregateNomad, the aggregate is
// the WEAKEST container (minimum score): a task is only as isolated as its most
// -exposed container, so grading the weakest link is the honest, fail-closed
// summary. target names the source (task-def file base, or a directory rollup
// label). It returns the aggregate Report and the Spec that produced it (for SARIF
// anchoring). It is pure; the caller injects Version/GeneratedAt. Fail-closed: an
// empty container set is an error (a task def that declares no container is not a
// pass).
func AggregateECS(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable container definitions found in ECS task definition(s) (looked for containerDefinitions[])")
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
	agg.Source = "ecs"
	agg.Notes = append(ecsSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// ecsSummaryNotes builds the task-level roll-up notes for an aggregate ECS report:
// a headline naming the weakest container and a per-container score list.
func ecsSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d ECS container(s); the task grade is the WEAKEST (a task is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "container"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "container"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-container: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// Live ECS task definition document model (`aws ecs describe-task-definition`)
//
// Unlike terraform's aws_ecs_task_definition (where the task-level fields are
// snake_case and container_definitions is a JSON-encoded STRING), the registered
// task definition is camelCase throughout and containerDefinitions is a real JSON
// array — so it decodes straight into ecsContainerDef with no inner-string step.
// --------------------------------------------------------------------------- //

// ecsDoc accepts both input shapes at once: the describe-task-definition wrapper
// (TaskDefinition) and a raw registered task def (the embedded ecsTaskDef fields at
// the document root).
type ecsDoc struct {
	TaskDefinition *ecsTaskDef `json:"taskDefinition"`
	ecsTaskDef
}

type ecsTaskDef struct {
	Family               string            `json:"family"`
	NetworkMode          string            `json:"networkMode"`
	PidMode              string            `json:"pidMode"`
	IpcMode              string            `json:"ipcMode"`
	ContainerDefinitions []ecsContainerDef `json:"containerDefinitions"`
	Volumes              []ecsVolume       `json:"volumes"`
}

type ecsVolume struct {
	Name string `json:"name"`
	Host *struct {
		SourcePath string `json:"sourcePath"`
	} `json:"host"`
}

// --------------------------------------------------------------------------- //
// Shared ECS container scoring (used by BOTH --ecs and --terraform)
//
// container_definitions entries are camelCase and Docker-flavored in every ECS
// surface (registered JSON and terraform's inner string alike). Grading them plus
// the task-level network/pid/ipc modes lives here so the live and terraform
// entrypoints stay byte-for-byte in sync.
// --------------------------------------------------------------------------- //

// ecsTaskModes carries the task-level isolation modes that apply to every
// container in a task definition. Both the live JSON (camelCase) and terraform's
// aws_ecs_task_definition (snake_case) adapters populate this struct so the shared
// ecsSpec mapper sees one shape.
type ecsTaskModes struct {
	NetworkMode string
	PidMode     string
	IpcMode     string
}

// ecsContainerDef is one entry of an ECS task's containerDefinitions array. The
// field shape is identical between a live `aws ecs describe-task-definition`
// document and terraform's container_definitions (a JSON-encoded string), so both
// adapters decode into this type and grade through ecsSpec.
type ecsContainerDef struct {
	Name                   string `json:"name"`
	Privileged             *bool  `json:"privileged"`
	ReadonlyRootFilesystem *bool  `json:"readonlyRootFilesystem"`
	User                   string `json:"user"`
	LinuxParameters        *struct {
		Capabilities *struct {
			Add  []string `json:"add"`
			Drop []string `json:"drop"`
		} `json:"capabilities"`
		InitProcessEnabled *bool `json:"initProcessEnabled"`
	} `json:"linuxParameters"`
	DockerSecurityOptions []string `json:"dockerSecurityOptions"`
}

// ecsSpec maps one ECS container definition (plus the task-level modes) to a Spec.
// source is the origin adapter ("ecs" for a live/registered task def, "terraform"
// for an aws_ecs_task_definition resource) and only affects the report header;
// every dimension is graded identically regardless of source.
func ecsSpec(source, family string, d ecsContainerDef, modes ecsTaskModes, dockerSock Tristate) Spec {
	target := "ecs/" + family
	if strings.TrimSpace(d.Name) != "" {
		target = "ecs/" + family + "/" + d.Name
	}
	s := Spec{Source: source, Target: target, DockerSock: dockerSock}

	// --- user ---------------------------------------------------------------
	// ECS `user` is a string: "", "0", "root", "1000", "1000:1000", "appuser".
	switch u := strings.TrimSpace(d.User); {
	case u == "":
		s.RunAsNonRoot = Unknown
		s.Notes = append(s.Notes, "no container user set; the image default may be root")
	case ecsUserIsRoot(u):
		s.RunAsNonRoot = No
		s.User = u
	default:
		s.RunAsNonRoot = Yes
		s.User = u
	}

	// --- capabilities / privilege ------------------------------------------
	s.Privileged = triFromPtr(d.Privileged)
	if d.LinuxParameters != nil && d.LinuxParameters.Capabilities != nil {
		caps := d.LinuxParameters.Capabilities
		for _, dr := range caps.Drop {
			if strings.EqualFold(strings.TrimSpace(dr), "ALL") {
				s.CapDropAll = Yes
			}
		}
		if s.CapDropAll == Unknown {
			s.CapDropAll = No
		}
		for _, a := range caps.Add {
			if a = strings.TrimSpace(a); a != "" {
				s.CapAdd = append(s.CapAdd, strings.ToUpper(a))
			}
		}
	} else {
		// No linuxParameters.capabilities: ECS keeps Docker's default capability
		// set (fail-closed, same as the compose/docker default).
		s.CapDropAll = No
	}

	// --- read-only rootfs ---------------------------------------------------
	s.ReadonlyRoot = triFromPtr(d.ReadonlyRootFilesystem)

	// --- seccomp ------------------------------------------------------------
	// ECS applies Docker's DEFAULT seccomp profile unless a dockerSecurityOption
	// disables it. Mirror the docker/compose adapters: default profile = confined.
	s.Seccomp = "confined"
	for _, opt := range d.DockerSecurityOptions {
		o := strings.ToLower(strings.TrimSpace(opt))
		if o == "seccomp=unconfined" || o == "seccomp:unconfined" {
			s.Seccomp = "unconfined"
		}
		if o == "no-new-privileges" || o == "no-new-privileges:true" {
			s.NoNewPrivs = Yes
		}
	}

	// --- network ------------------------------------------------------------
	switch nm := strings.ToLower(strings.TrimSpace(modes.NetworkMode)); nm {
	case "none":
		s.NetworkMode = "none"
	case "host":
		s.NetworkMode = "host"
		s.HostNetwork = Yes
	case "":
		// ECS default network mode is bridge on EC2; egress-capable.
		s.NetworkMode = "bridge"
	default:
		// awsvpc / bridge / default: an egress-capable NIC (not host).
		s.NetworkMode = nm
	}
	if s.HostNetwork != Yes {
		s.HostNetwork = No
	}

	// --- namespaces ---------------------------------------------------------
	s.HostPID = boolTri(strings.EqualFold(strings.TrimSpace(modes.PidMode), "host"))
	s.HostIPC = boolTri(strings.EqualFold(strings.TrimSpace(modes.IpcMode), "host"))

	return s
}

// ecsUserIsRoot reports whether an ECS `user` string resolves to uid 0.
func ecsUserIsRoot(u string) bool {
	u = strings.TrimSpace(u)
	// "uid[:gid]" or "name[:group]": the leading field is the user.
	if i := strings.IndexByte(u, ':'); i >= 0 {
		u = u[:i]
	}
	return u == "0" || strings.EqualFold(u, "root")
}
