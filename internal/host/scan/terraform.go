package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SpecsFromTerraform parses `terraform show -json` output (a plan.json or a
// state.json) and returns one graded Spec per container workload it recognizes.
// It walks the root module and every child module, and grades two workload
// classes that carry a container isolation posture:
//
//   - the Kubernetes provider's workload resources (kubernetes_pod,
//     kubernetes_deployment, kubernetes_stateful_set, kubernetes_daemon_set,
//     kubernetes_job, kubernetes_cron_job, kubernetes_replication_controller, and
//     their _v1 aliases) — the provider embeds a pod spec, which is mapped to the
//     SAME internal podSpec the --k8s / --helm paths grade (specFromPodSpec).
//   - aws_ecs_task_definition — each container_definitions entry is graded with
//     the task-level network/pid/ipc modes folded in.
//
// It is pure and unit-testable: the caller runs `terraform show -json` (I/O) and
// passes the JSON here. Non-container resources are ignored. It fails OPEN on a
// malformed top-level document (returns the parse error); the CLI wrapper turns
// that into a skip so an opt-in CI step never crashes the build.
func SpecsFromTerraform(raw []byte) ([]Spec, error) {
	var show tfShow
	if err := json.Unmarshal(raw, &show); err != nil {
		return nil, fmt.Errorf("parse terraform show -json: %w", err)
	}
	// A `terraform show -json` of a plan carries planned_values; of a state, values.
	// Prefer planned_values (what WILL be applied) and fall back to state.
	vals := show.PlannedValues
	if vals == nil {
		vals = show.Values
	}
	if vals == nil {
		return nil, nil
	}

	var resources []tfResource
	collectTFResources(&vals.RootModule, &resources)

	var specs []Spec
	for _, r := range resources {
		switch {
		case k8sWorkloadKind(r.Type) != "":
			if s, ok := specFromTFK8s(r); ok {
				specs = append(specs, s)
			}
		case r.Type == "aws_ecs_task_definition":
			specs = append(specs, specsFromTFECS(r)...)
		}
	}
	return specs, nil
}

// AggregateTerraform folds the per-workload specs from a Terraform plan/state into
// a single Report representing the configuration's isolation posture. Like
// AggregateHelm, the aggregate is the WEAKEST workload (minimum score): a plan is
// only as isolated as its most-exposed container, so grading the weakest link is
// the honest, fail-closed summary. target names the source (plan/state file base
// or "terraform"). It returns the aggregate Report and the Spec that produced it
// (for SARIF anchoring). It is pure; the caller injects Version/GeneratedAt.
// Fail-closed: an empty workload set is an error (a plan that declares no gradeable
// container workload is not a pass).
func AggregateTerraform(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable container workloads found in terraform output (looked for kubernetes_* pod/workload resources and aws_ecs_task_definition)")
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
	agg.Source = "terraform"
	agg.Notes = append(tfSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// tfSummaryNotes builds the plan-level roll-up notes for an aggregate Terraform
// report: a headline naming the weakest workload and a per-workload score list.
func tfSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d terraform workload(s); the plan grade is the WEAKEST (a plan is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
			len(all), nz(worst.spec.Target, "workload"), worst.report.Score, worst.report.Grade),
	}
	sorted := make([]scoredWorkload, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].report.Score < sorted[j].report.Score })
	rows := make([]string, len(sorted))
	for i, w := range sorted {
		rows[i] = fmt.Sprintf("%s = %d/100 (%s)", nz(w.spec.Target, "workload"), w.report.Score, w.report.Grade)
	}
	notes = append(notes, "per-workload: "+strings.Join(rows, "; "))
	return notes
}

// --------------------------------------------------------------------------- //
// terraform show -json top-level document model
// --------------------------------------------------------------------------- //

type tfShow struct {
	FormatVersion string    `json:"format_version"`
	PlannedValues *tfValues `json:"planned_values"`
	Values        *tfValues `json:"values"`
}

type tfValues struct {
	RootModule tfModule `json:"root_module"`
}

type tfModule struct {
	Resources    []tfResource `json:"resources"`
	ChildModules []tfModule   `json:"child_modules"`
}

type tfResource struct {
	Address string          `json:"address"`
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Values  json.RawMessage `json:"values"`
}

// collectTFResources flattens a module tree (root + child modules, recursively)
// into a single resource slice.
func collectTFResources(m *tfModule, out *[]tfResource) {
	*out = append(*out, m.Resources...)
	for i := range m.ChildModules {
		collectTFResources(&m.ChildModules[i], out)
	}
}

// --------------------------------------------------------------------------- //
// Kubernetes provider mapping (terraform-provider-kubernetes)
//
// The provider models Kubernetes blocks as single-element JSON arrays with
// snake_case field names (e.g. metadata:[{...}], spec:[{...}],
// security_context:[{...}]). We decode that shape and convert it to the SAME
// internal podSpec the --k8s / --helm paths grade, so the dimension scoring is
// byte-for-byte identical across artifact classes.
// --------------------------------------------------------------------------- //

// k8sWorkloadKind maps a terraform kubernetes resource type to the Kubernetes
// Kind it represents, or "" if the type is not a gradeable workload. It tolerates
// the "_v1"/"_v1beta1" provider aliases.
func k8sWorkloadKind(tfType string) string {
	base := strings.TrimPrefix(tfType, "kubernetes_")
	if base == tfType {
		return "" // not a kubernetes_* resource
	}
	base = strings.TrimSuffix(base, "_v1beta1")
	base = strings.TrimSuffix(base, "_v1")
	switch base {
	case "pod":
		return "Pod"
	case "deployment":
		return "Deployment"
	case "stateful_set":
		return "StatefulSet"
	case "daemon_set":
		return "DaemonSet"
	case "replication_controller":
		return "ReplicationController"
	case "job":
		return "Job"
	case "cron_job":
		return "CronJob"
	default:
		return ""
	}
}

type tfK8sValues struct {
	Metadata []tfMetadata `json:"metadata"`
	Spec     []tfK8sSpec  `json:"spec"`
}

type tfMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// tfK8sSpec is a resource's top-level spec block. For kubernetes_pod it embeds the
// pod spec directly; for workload kinds the pod spec is nested under template; for
// kubernetes_cron_job it nests one level deeper under job_template.
type tfK8sSpec struct {
	tfK8sPodSpec
	Template    []tfTemplate    `json:"template"`
	JobTemplate []tfJobTemplate `json:"job_template"`
}

type tfTemplate struct {
	Spec []tfK8sPodSpec `json:"spec"`
}

type tfJobTemplate struct {
	Spec []tfJobSpec `json:"spec"`
}

type tfJobSpec struct {
	Template []tfTemplate `json:"template"`
}

type tfK8sPodSpec struct {
	HostNetwork      *bool         `json:"host_network"`
	HostPID          *bool         `json:"host_pid"`
	HostIPC          *bool         `json:"host_ipc"`
	RuntimeClassName string        `json:"runtime_class_name"`
	SecurityContext  []tfPodSecCtx `json:"security_context"`
	Container        []tfContainer `json:"container"`
	Volume           []tfVolume    `json:"volume"`
}

type tfPodSecCtx struct {
	RunAsNonRoot   *bool       `json:"run_as_non_root"`
	RunAsUser      *tfInt      `json:"run_as_user"`
	SeccompProfile []tfSeccomp `json:"seccomp_profile"`
}

type tfSeccomp struct {
	Type string `json:"type"`
}

type tfContainer struct {
	Name            string              `json:"name"`
	Image           string              `json:"image"`
	SecurityContext []tfContainerSecCtx `json:"security_context"`
	VolumeMount     []tfVolumeMount     `json:"volume_mount"`
}

type tfContainerSecCtx struct {
	RunAsNonRoot             *bool          `json:"run_as_non_root"`
	RunAsUser                *tfInt         `json:"run_as_user"`
	Privileged               *bool          `json:"privileged"`
	ReadOnlyRootFilesystem   *bool          `json:"read_only_root_filesystem"`
	AllowPrivilegeEscalation *bool          `json:"allow_privilege_escalation"`
	Capabilities             []tfCapability `json:"capabilities"`
	SeccompProfile           []tfSeccomp    `json:"seccomp_profile"`
}

type tfCapability struct {
	Add  []string `json:"add"`
	Drop []string `json:"drop"`
}

type tfVolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
}

type tfVolume struct {
	Name     string          `json:"name"`
	HostPath []tfVolHostPath `json:"host_path"`
}

type tfVolHostPath struct {
	Path string `json:"path"`
}

// specFromTFK8s decodes one kubernetes_* resource, resolves its pod spec (bare
// pod / workload template / cron_job job_template), converts it to the internal
// podSpec, and grades it with the shared specFromPodSpec mapper. ok is false when
// the resource has no container to grade.
func specFromTFK8s(r tfResource) (Spec, bool) {
	var v tfK8sValues
	if err := json.Unmarshal(r.Values, &v); err != nil {
		return Spec{}, false
	}
	tps, ok := v.resolvePodSpec()
	if !ok {
		return Spec{}, false
	}
	ps := tps.toPodSpec()
	if len(ps.Containers) == 0 {
		return Spec{}, false
	}
	name := r.Name
	if len(v.Metadata) > 0 && strings.TrimSpace(v.Metadata[0].Name) != "" {
		name = v.Metadata[0].Name
	}
	target := k8sWorkloadKind(r.Type) + "/" + name
	return specFromPodSpec("terraform", target, ps), true
}

// resolvePodSpec resolves the three nesting shapes to the innermost pod spec,
// mirroring k8sObject.podSpecOf.
func (v tfK8sValues) resolvePodSpec() (tfK8sPodSpec, bool) {
	if len(v.Spec) == 0 {
		return tfK8sPodSpec{}, false
	}
	s := v.Spec[0]
	// Bare pod: the resource spec IS the pod spec.
	if len(s.tfK8sPodSpec.Container) > 0 {
		return s.tfK8sPodSpec, true
	}
	// Workload kinds: spec.template.spec.
	if len(s.Template) > 0 && len(s.Template[0].Spec) > 0 && len(s.Template[0].Spec[0].Container) > 0 {
		return s.Template[0].Spec[0], true
	}
	// CronJob: spec.job_template.spec.template.spec.
	if len(s.JobTemplate) > 0 && len(s.JobTemplate[0].Spec) > 0 {
		js := s.JobTemplate[0].Spec[0]
		if len(js.Template) > 0 && len(js.Template[0].Spec) > 0 && len(js.Template[0].Spec[0].Container) > 0 {
			return js.Template[0].Spec[0], true
		}
	}
	return tfK8sPodSpec{}, false
}

// toPodSpec converts a Terraform-decoded pod spec to the internal podSpec that the
// shared scorer understands.
func (t tfK8sPodSpec) toPodSpec() podSpec {
	ps := podSpec{
		HostPID:      t.HostPID,
		HostIPC:      t.HostIPC,
		HostNetwork:  t.HostNetwork,
		RuntimeClass: t.RuntimeClassName,
	}
	if len(t.SecurityContext) > 0 {
		sc := t.SecurityContext[0]
		ps.SecurityContext = &podSecCtx{
			RunAsNonRoot: sc.RunAsNonRoot,
			RunAsUser:    sc.RunAsUser.ptr(),
		}
		if len(sc.SeccompProfile) > 0 {
			ps.SecurityContext.SeccompProfile = &seccompProfile{Type: sc.SeccompProfile[0].Type}
		}
	}
	for _, c := range t.Container {
		kc := k8sContainer{Name: c.Name}
		if len(c.SecurityContext) > 0 {
			csc := c.SecurityContext[0]
			kc.SecurityContext = &containerSecCtx{
				RunAsNonRoot:             csc.RunAsNonRoot,
				RunAsUser:                csc.RunAsUser.ptr(),
				Privileged:               csc.Privileged,
				ReadOnlyRootFilesystem:   csc.ReadOnlyRootFilesystem,
				AllowPrivilegeEscalation: csc.AllowPrivilegeEscalation,
			}
			if len(csc.Capabilities) > 0 {
				kc.SecurityContext.Capabilities = &capabilities{
					Add:  csc.Capabilities[0].Add,
					Drop: csc.Capabilities[0].Drop,
				}
			}
			if len(csc.SeccompProfile) > 0 {
				kc.SecurityContext.SeccompProfile = &seccompProfile{Type: csc.SeccompProfile[0].Type}
			}
		}
		for _, vm := range c.VolumeMount {
			kc.VolumeMounts = append(kc.VolumeMounts, volumeMount{Name: vm.Name, MountPath: vm.MountPath})
		}
		ps.Containers = append(ps.Containers, kc)
	}
	for _, v := range t.Volume {
		kv := k8sVolume{Name: v.Name}
		if len(v.HostPath) > 0 {
			kv.HostPath = &struct {
				Path string `yaml:"path"`
			}{Path: v.HostPath[0].Path}
		}
		ps.Volumes = append(ps.Volumes, kv)
	}
	return ps
}

// tfInt decodes a terraform value that may be a JSON number OR a quoted number
// string (the kubernetes provider models run_as_user as a string). An empty or
// unparseable value decodes to a nil pointer (unknown).
type tfInt struct{ v *int64 }

func (t *tfInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` || s == "" {
		return nil
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil // tolerate non-numeric (e.g. a username): leave unknown
	}
	t.v = &n
	return nil
}

func (t *tfInt) ptr() *int64 {
	if t == nil {
		return nil
	}
	return t.v
}

// --------------------------------------------------------------------------- //
// AWS ECS task definition mapping
//
// container_definitions is a JSON-ENCODED STRING holding an array of container
// definitions (camelCase, Docker-flavored). We decode the string, grade each
// container, and fold in the task-level network_mode / pid_mode / ipc_mode.
// --------------------------------------------------------------------------- //

type tfEcsValues struct {
	Family               string     `json:"family"`
	NetworkMode          string     `json:"network_mode"`
	PidMode              string     `json:"pid_mode"`
	IpcMode              string     `json:"ipc_mode"`
	ContainerDefinitions string     `json:"container_definitions"`
	Volume               []tfEcsVol `json:"volume"`
}

type tfEcsVol struct {
	Name     string `json:"name"`
	HostPath string `json:"host_path"`
}

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
	} `json:"linuxParameters"`
	DockerSecurityOptions []string `json:"dockerSecurityOptions"`
}

// specsFromTFECS decodes one aws_ecs_task_definition and returns a graded Spec per
// container definition. The task-level network/pid/ipc modes and any docker.sock
// host volume apply to every container in the task.
func specsFromTFECS(r tfResource) []Spec {
	var v tfEcsValues
	if err := json.Unmarshal(r.Values, &v); err != nil {
		return nil
	}
	var defs []ecsContainerDef
	if strings.TrimSpace(v.ContainerDefinitions) != "" {
		if err := json.Unmarshal([]byte(v.ContainerDefinitions), &defs); err != nil {
			return nil
		}
	}
	if len(defs) == 0 {
		return nil
	}

	family := nz(v.Family, r.Name)

	// docker.sock exposure is a task-level property: an ECS host volume whose
	// source path is the control socket is mountable into any container.
	dockerSock := No
	for _, vol := range v.Volume {
		if isControlSocket(vol.HostPath) {
			dockerSock = Yes
		}
	}

	specs := make([]Spec, 0, len(defs))
	for _, d := range defs {
		specs = append(specs, ecsSpec(family, d, v, dockerSock))
	}
	return specs
}

// ecsSpec maps one ECS container definition (plus task-level modes) to a Spec.
func ecsSpec(family string, d ecsContainerDef, task tfEcsValues, dockerSock Tristate) Spec {
	target := "ecs/" + family
	if strings.TrimSpace(d.Name) != "" {
		target = "ecs/" + family + "/" + d.Name
	}
	s := Spec{Source: "terraform", Target: target, DockerSock: dockerSock}

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
	switch nm := strings.ToLower(strings.TrimSpace(task.NetworkMode)); nm {
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
	s.HostPID = boolTri(strings.EqualFold(strings.TrimSpace(task.PidMode), "host"))
	s.HostIPC = boolTri(strings.EqualFold(strings.TrimSpace(task.IpcMode), "host"))

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
