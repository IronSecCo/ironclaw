package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SpecsFromNomad parses a HashiCorp Nomad job specification in JSON form (the
// output of `nomad job run -output job.hcl`, or a raw api.Job document) and
// returns one graded Spec per docker-driver task across every task group.
//
// Nomad's docker task driver is Docker-flavored: its `config` block carries the
// same isolation-relevant knobs as a `docker run` / compose service (privileged,
// cap_add/cap_drop, network_mode, volumes/mount, pid_mode/ipc_mode, security_opt,
// userns_mode, readonly_rootfs). We map each docker task's effective config onto
// the SAME normalized Spec the docker/compose adapters produce, so a single
// grading model serves every artifact class. Non-docker drivers (exec, raw_exec,
// java, qemu, …) carry no comparable container-isolation posture and are skipped.
//
// It is pure and unit-testable: the caller loads the JSON (I/O) and passes the
// bytes here. It fails OPEN on a malformed top-level document (returns the parse
// error); the CLI wrapper turns that into a skip so an opt-in CI step never
// crashes the build.
func SpecsFromNomad(raw []byte) ([]Spec, error) {
	// `nomad job run -output` wraps the job as {"Job": {...}} (the HTTP register
	// request shape). A raw api.Job document (no wrapper) is also accepted.
	var wrap nomadWrapper
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("parse nomad job json: %w", err)
	}
	job := wrap.Job
	if job == nil {
		var j nomadJob
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("parse nomad job json: %w", err)
		}
		job = &j
	}

	var specs []Spec
	for _, g := range job.TaskGroups {
		for _, t := range g.Tasks {
			if s, ok := specFromNomadTask(g, t); ok {
				specs = append(specs, s)
			}
		}
	}
	return specs, nil
}

// AggregateNomad folds the per-task specs of a Nomad job into a single Report
// representing the job's isolation posture. Like AggregateHelm/AggregateTerraform,
// the aggregate is the WEAKEST task (minimum score): a job is only as isolated as
// its most-exposed container, so grading the weakest link is the honest,
// fail-closed summary. target names the source job. It returns the aggregate
// Report and the Spec that produced it (for SARIF anchoring). It is pure; the
// caller injects Version/GeneratedAt. Fail-closed: an empty task set is an error
// (a job that declares no gradeable docker task is not a pass).
func AggregateNomad(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable container workloads found in nomad job (looked for tasks using the docker driver)")
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
	agg.Source = "nomad"
	agg.Notes = append(nomadSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// nomadSummaryNotes builds the job-level roll-up notes for an aggregate Nomad
// report: a headline naming the weakest task and a per-task score list.
func nomadSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d nomad docker task(s); the job grade is the WEAKEST (a job is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "task"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "task"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-task: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// Nomad job JSON document model (api.Job, capitalized field names).
// --------------------------------------------------------------------------- //

type nomadWrapper struct {
	Job *nomadJob `json:"Job"`
}

type nomadJob struct {
	ID         string           `json:"ID"`
	Name       string           `json:"Name"`
	TaskGroups []nomadTaskGroup `json:"TaskGroups"`
}

type nomadTaskGroup struct {
	Name     string         `json:"Name"`
	Networks []nomadNetwork `json:"Networks"`
	Tasks    []nomadTask    `json:"Tasks"`
}

type nomadNetwork struct {
	Mode string `json:"Mode"`
}

type nomadTask struct {
	Name   string          `json:"Name"`
	Driver string          `json:"Driver"`
	User   string          `json:"User"`
	Config json.RawMessage `json:"Config"`
}

// nomadDockerConfig is the security-relevant subset of a docker-driver task's
// `config` block. Field names are the driver's HCL keys (lowercase). Scalars that
// may arrive as a bool OR a quoted string (HCL2 vs. hand-authored JSON) decode
// through nomadBool.
type nomadDockerConfig struct {
	Image          string             `json:"image"`
	Privileged     nomadBool          `json:"privileged"`
	CapAdd         []string           `json:"cap_add"`
	CapDrop        []string           `json:"cap_drop"`
	NetworkMode    string             `json:"network_mode"`
	Volumes        []string           `json:"volumes"`
	Mounts         []nomadDockerMount `json:"mount"`
	PidMode        string             `json:"pid_mode"`
	IpcMode        string             `json:"ipc_mode"`
	SecurityOpt    []string           `json:"security_opt"`
	UsernsMode     string             `json:"userns_mode"`
	ReadonlyRootfs nomadBool          `json:"readonly_rootfs"`
}

// nomadDockerMount mirrors a docker-driver `mount` block. Only a bind mount whose
// source is a host path can expose the control socket or a sensitive host path.
type nomadDockerMount struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Source string `json:"source"`
}

// nomadBool decodes a value that may be a JSON bool OR a quoted "true"/"false"
// string. An absent/null/unparseable value decodes to a nil pointer (unknown),
// so the scorers grade it fail-closed.
type nomadBool struct{ v *bool }

func (n *nomadBool) UnmarshalJSON(b []byte) error {
	switch strings.Trim(strings.TrimSpace(string(b)), `"`) {
	case "true":
		t := true
		n.v = &t
	case "false":
		f := false
		n.v = &f
	}
	return nil
}

func (n nomadBool) ptr() *bool { return n.v }

// specFromNomadTask maps one Nomad task (plus its group networking) to a graded
// Spec. ok is false for a non-docker task (no container isolation posture to
// grade). The mapping mirrors SpecFromCompose: the docker driver applies Docker's
// defaults (default seccomp profile, default capability set, egress-capable
// bridge network) unless the config overrides them.
func specFromNomadTask(g nomadTaskGroup, t nomadTask) (Spec, bool) {
	if !strings.EqualFold(strings.TrimSpace(t.Driver), "docker") {
		return Spec{}, false
	}
	var cfg nomadDockerConfig
	if len(t.Config) > 0 {
		if err := json.Unmarshal(t.Config, &cfg); err != nil {
			return Spec{}, false
		}
	}

	target := nz(g.Name, "group") + "/" + nz(t.Name, "task")
	s := Spec{Source: "nomad", Target: target, Image: cfg.Image}

	// --- user (task-level User; docker driver runs the container as it) --------
	switch u := strings.TrimSpace(t.User); {
	case u == "":
		s.RunAsNonRoot = Unknown
		s.Notes = append(s.Notes, "no task user set; the image default may be root")
	case u == "0" || strings.HasPrefix(u, "0:") || u == "root" || strings.HasPrefix(u, "root:"):
		s.RunAsNonRoot = No
		s.User = u
	default:
		s.RunAsNonRoot = Yes
		s.User = u
	}

	// --- capabilities / privilege ------------------------------------------
	s.Privileged = triFromPtr(cfg.Privileged.ptr())
	s.CapDropAll = No
	for _, c := range cfg.CapDrop {
		if strings.EqualFold(strings.TrimSpace(c), "ALL") {
			s.CapDropAll = Yes
		}
	}
	for _, c := range cfg.CapAdd {
		if c = strings.TrimSpace(c); c != "" {
			s.CapAdd = append(s.CapAdd, strings.ToUpper(c))
		}
	}

	// --- seccomp / no-new-privileges ---------------------------------------
	// The docker driver applies Docker's DEFAULT seccomp profile unless a
	// security_opt disables it (mirrors the compose/docker adapters).
	s.Seccomp = "confined"
	for _, opt := range cfg.SecurityOpt {
		low := strings.ToLower(strings.TrimSpace(opt))
		switch {
		case strings.HasPrefix(low, "no-new-privileges"):
			s.NoNewPrivs = Yes
		case strings.HasPrefix(low, "seccomp"):
			if strings.Contains(low, "unconfined") {
				s.Seccomp = "unconfined"
			} else {
				s.Seccomp = "confined"
			}
		}
	}
	if s.Privileged == Yes {
		// --privileged disables seccomp and grants the full capability set.
		s.Seccomp = "unconfined"
	}

	// --- read-only rootfs ---------------------------------------------------
	s.ReadonlyRoot = triFromPtr(cfg.ReadonlyRootfs.ptr())

	// --- network ------------------------------------------------------------
	// The docker driver's config.network_mode governs the container network. When
	// it is unset and the group declares a network block, the task shares the
	// group's namespace, so fall back to the group mode. Both unset = the default
	// bridge network (egress-capable).
	netMode := strings.TrimSpace(cfg.NetworkMode)
	if netMode == "" && len(g.Networks) > 0 {
		netMode = strings.TrimSpace(g.Networks[0].Mode)
	}
	switch strings.ToLower(netMode) {
	case "none":
		s.NetworkMode = "none"
	case "host":
		s.NetworkMode = "host"
		s.HostNetwork = Yes
	case "":
		s.NetworkMode = "bridge"
	default:
		s.NetworkMode = netMode
	}
	if s.HostNetwork != Yes {
		s.HostNetwork = No
	}

	// --- namespaces ---------------------------------------------------------
	s.HostPID = boolTri(strings.EqualFold(strings.TrimSpace(cfg.PidMode), "host"))
	s.HostIPC = boolTri(strings.EqualFold(strings.TrimSpace(cfg.IpcMode), "host"))
	if strings.EqualFold(strings.TrimSpace(cfg.UsernsMode), "host") {
		s.Notes = append(s.Notes, "userns_mode=host: the container shares the host user namespace (no uid remap)")
	}

	// --- docker.sock / host mounts ------------------------------------------
	s.DockerSock = No
	seen := map[string]bool{}
	// volumes: "host:container[:mode]" (compose-style).
	for _, v := range cfg.Volumes {
		parts := strings.SplitN(v, ":", 3)
		src := strings.TrimSpace(parts[0])
		dst := ""
		if len(parts) > 1 {
			dst = strings.TrimSpace(parts[1])
		}
		if isControlSocket(src) || isControlSocket(dst) {
			s.DockerSock = Yes
		}
		if isSensitiveHostPath(src) && !seen[src] {
			seen[src] = true
			s.HostPathMounts = append(s.HostPathMounts, src)
		}
	}
	// mount blocks: only a bind mount exposes a host source path.
	for _, m := range cfg.Mounts {
		if !strings.EqualFold(strings.TrimSpace(m.Type), "bind") {
			continue
		}
		src := strings.TrimSpace(m.Source)
		if isControlSocket(src) || isControlSocket(strings.TrimSpace(m.Target)) {
			s.DockerSock = Yes
		}
		if isSensitiveHostPath(src) && !seen[src] {
			seen[src] = true
			s.HostPathMounts = append(s.HostPathMounts, src)
		}
	}

	return s, true
}
