package scan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// SpecsFromAzure parses an Azure Container Instances definition — an ARM template
// JSON, an `az container show` / deployment JSON document, or a bare containerGroup
// object — and returns one graded Spec per container of every
// Microsoft.ContainerInstance/containerGroups resource it declares. It is the Azure
// counterpart to the --cloudrun (GCP) and --ecs (AWS) managed-runtime paths, so the
// wedge covers the big-3 clouds.
//
// The mapping reuses the SAME specFromPodSpec scorer as --k8s/--cloudrun: an ACI
// container's securityContext (privileged, runAsUser, capabilities.add/drop,
// seccompProfile) is adapted onto the shared container securityContext, graded, and
// then ACI's managed-runtime floors are folded in — each container GROUP runs in a
// dedicated Hyper-V-isolated VM, so host PID/IPC/network namespaces and a host
// docker.sock are unreachable, privileged is not permitted on the Standard SKU, and
// a default seccomp profile is applied. Egress is managed (allowed by default, never
// network=none), so the network dimension caps at the WARN tier.
//
// Two ACI-specific honesty notes drive the ceiling below Cloud Run's 89/B:
//   - ACI's securityContext does NOT express a read-only root filesystem, so that
//     dimension is always graded fail-closed (unlike Cloud Run's pod spec).
//   - ACI lets a container ADD capabilities, so cap-drop is NOT credited by the
//     platform (the user must express capabilities.drop=[ALL]).
//
// A fully hardened ACI container therefore tops out at 79/100 (grade B) — one
// dimension (read-only rootfs, 10 pts) below Cloud Run's egress-capable 89/B.
//
// ARM template expressions ("[parameters('x')]" / "[variables('y')]") are
// unresolvable without the deployment context, so each is nullified before decoding
// (same fail-open posture as the CloudFormation intrinsic handling). A graded field
// an expression covered then reads as unset and is graded fail-closed; the second
// return value reports whether any expression was skipped so the caller can note it.
//
// It is pure and unit-testable: the caller reads the file / runs the az CLI (I/O)
// and passes the JSON here. It fails OPEN on a malformed top-level document (returns
// the parse error); the CLI wrapper turns that into a skip so an opt-in CI step never
// crashes the build. A document with no containerGroups resource returns no specs.
func SpecsFromAzure(raw []byte) ([]Spec, bool, error) {
	var root interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, false, fmt.Errorf("parse azure arm/aci json: %w", err)
	}
	skipped := sanitizeARMExpressions(&root)

	groups := collectContainerGroups(root)
	var specs []Spec
	for _, g := range groups {
		js, err := json.Marshal(g)
		if err != nil {
			continue
		}
		var cg aciContainerGroup
		// Case-insensitive key matching maps ARM's property names onto the json tags.
		if err := json.Unmarshal(js, &cg); err != nil {
			// Fail-open per resource: one undecodable containerGroup must not sink the
			// whole document.
			continue
		}
		specs = append(specs, specsFromACIGroup(cg)...)
	}
	return specs, skipped > 0, nil
}

// AggregateAzure folds the per-container specs from one or more ACI containerGroups
// into a single Report. Like AggregateECS / AggregateCloudRun, the aggregate is the
// WEAKEST container (minimum score): a group is only as isolated as its most-exposed
// container, so grading the weakest link is the honest, fail-closed summary. target
// names the source (file base, or a directory rollup label). It returns the
// aggregate Report and the Spec that produced it (for SARIF anchoring). It is pure;
// the caller injects Version/GeneratedAt. Fail-closed: an empty container set is an
// error (a document that declares no gradeable ACI container is not a pass).
func AggregateAzure(specs []Spec, target string) (Report, Spec, error) {
	if len(specs) == 0 {
		return Report{}, Spec{}, fmt.Errorf("no gradeable containers found (looked for Microsoft.ContainerInstance/containerGroups resources with properties.containers)")
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
	agg.Source = "azure"
	agg.Notes = append(azureSummaryNotes(all[worst], all), agg.Notes...)

	return agg, worstSpec, nil
}

// azureSummaryNotes builds the group-level roll-up notes for an aggregate Azure
// report: a headline naming the weakest container and a per-container score list.
func azureSummaryNotes(worst scoredWorkload, all []scoredWorkload) []string {
	notes := []string{
		fmt.Sprintf("graded %d ACI container(s); the grade is the WEAKEST (a container group is only as isolated as its most-exposed container). Weakest: %s at %d/100 (grade %s).",
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
// Azure Container Instances containerGroup document model.
//
// An ARM resource of type Microsoft.ContainerInstance/containerGroups carries its
// containers under properties.containers[], each with a nested properties block
// holding the image and (optionally) a securityContext. The az-CLI `az container
// show` output and a bare containerGroup object share this exact shape.
// --------------------------------------------------------------------------- //

type aciContainerGroup struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	Properties aciGroupProps `json:"properties"`
}

type aciGroupProps struct {
	OsType     string         `json:"osType"`
	Sku        string         `json:"sku"`
	Containers []aciContainer `json:"containers"`
}

type aciContainer struct {
	Name       string            `json:"name"`
	Properties aciContainerProps `json:"properties"`
}

type aciContainerProps struct {
	Image           string     `json:"image"`
	SecurityContext *aciSecCtx `json:"securityContext"`
}

// aciSecCtx mirrors an ACI container securityContext. Unlike Kubernetes/Cloud Run,
// ACI has NO readOnlyRootFilesystem field (that dimension is unexpressible and thus
// always fail-closed), and its seccompProfile is a (base64) custom-profile STRING
// rather than an object. capabilities is the shared add/drop shape.
type aciSecCtx struct {
	Privileged               *bool         `json:"privileged"`
	AllowPrivilegeEscalation *bool         `json:"allowPrivilegeEscalation"`
	RunAsGroup               *int64        `json:"runAsGroup"`
	RunAsUser                *int64        `json:"runAsUser"`
	Capabilities             *capabilities `json:"capabilities"`
	SeccompProfile           *string       `json:"seccompProfile"`
}

// collectContainerGroups walks a decoded JSON tree and returns every object whose
// "type" is Microsoft.ContainerInstance/containerGroups. This handles all three
// input shapes at once: an ARM template (resources[] array), an `az container show`
// / bare containerGroup object at the root, and a nested deployment whose resources
// carry the group deeper in the tree.
func collectContainerGroups(n interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	switch v := n.(type) {
	case map[string]interface{}:
		if t, _ := v["type"].(string); strings.EqualFold(strings.TrimSpace(t), "Microsoft.ContainerInstance/containerGroups") {
			out = append(out, v)
		}
		for _, val := range v {
			out = append(out, collectContainerGroups(val)...)
		}
	case []interface{}:
		for _, e := range v {
			out = append(out, collectContainerGroups(e)...)
		}
	}
	return out
}

// specsFromACIGroup grades every container in a containerGroup, keyed with source
// "azure". It returns one Spec per container so AggregateAzure can grade the weakest.
func specsFromACIGroup(cg aciContainerGroup) []Spec {
	if len(cg.Properties.Containers) == 0 {
		return nil
	}
	group := nz(cg.Name, "containergroup")
	specs := make([]Spec, 0, len(cg.Properties.Containers))
	for _, c := range cg.Properties.Containers {
		specs = append(specs, specFromACIContainer(group, c))
	}
	return specs
}

// specFromACIContainer maps one ACI container to a graded Spec. It adapts the ACI
// securityContext onto the shared container securityContext, grades the expressible
// posture through specFromPodSpec (exactly like --k8s/--cloudrun), then folds in
// ACI's managed-runtime floors.
func specFromACIContainer(group string, c aciContainer) Spec {
	name := strings.TrimSpace(c.Name)
	target := "azure/" + nz(group, "containergroup")
	if name != "" {
		target = "azure/" + nz(group, "containergroup") + "/" + name
	}

	// Adapt ACI's securityContext to the shared container securityContext. ACI has no
	// hostPath volumes (managed runtime, no host mounts), so the pod spec carries a
	// single container and no volumes. Note ACI has NO readOnlyRootFilesystem field,
	// so ReadOnlyRootFilesystem stays nil → graded fail-closed.
	csc := &containerSecCtx{}
	sc := c.Properties.SecurityContext
	if sc != nil {
		csc.Privileged = sc.Privileged
		csc.AllowPrivilegeEscalation = sc.AllowPrivilegeEscalation
		csc.RunAsUser = sc.RunAsUser
		csc.Capabilities = sc.Capabilities
		if sc.SeccompProfile != nil && strings.TrimSpace(*sc.SeccompProfile) != "" {
			// ACI carries a custom seccomp profile as a (base64) string; its presence
			// means a profile IS applied to the container.
			csc.SeccompProfile = &seccompProfile{Type: "Localhost"}
		}
	}
	ps := podSpec{Containers: []k8sContainer{{Name: name, SecurityContext: csc}}}
	s := specFromPodSpec("azure", target, ps)

	// The k8s pod-spec grader appends conservative notes that are WRONG for ACI's
	// managed runtime (it assumes an invisible NetworkPolicy governs egress and that
	// seccomp is unconfined by default). Drop them; the managed floors below give
	// concrete, honest answers.
	s.Notes = dropNotesContaining(s.Notes, "NetworkPolicy", "unconfined by default")

	// --- managed-runtime floors (platform-enforced, not spec-visible) ----------
	// Each ACI container GROUP runs in a dedicated Hyper-V-isolated VM. There is no
	// tenant host to share, so host PID/IPC/network namespaces and a host docker
	// socket are unreachable — credit them explicitly (the raw pod-spec grader scores
	// them fail-closed / Unknown).
	s.HostPID = No
	s.HostIPC = No
	s.HostNetwork = No
	s.DockerSock = No

	// ACI Standard does not permit privileged containers; an unset privileged flag is
	// the managed default (not privileged). Respect an explicit privileged:true (the
	// worse posture) if the spec set it.
	if s.Privileged == Unknown {
		s.Privileged = No
	}

	// The managed runtime applies a default seccomp profile (like Docker/ECS): an
	// unset seccompProfile is confined here, not unconfined.
	if strings.TrimSpace(s.Seccomp) == "" {
		s.Seccomp = "confined"
	}

	// --- network (managed egress → honest B ceiling) ---------------------------
	// ACI containers are egress-capable by default (public or private IP) and can
	// never be network=none, so grade egress-capable (WARN) — the same honest ceiling
	// as any managed runtime.
	s.NetworkMode = "aci (managed egress)"

	// --- managed-runtime + expressible-posture notes ---------------------------
	s.Notes = append(s.Notes,
		"Azure Container Instances runs each container group in a dedicated Hyper-V-isolated VM; host PID/IPC/network namespaces and a host docker.sock are not reachable — those dimensions pass by construction",
		"managed runtime: ACI Standard does not permit privileged containers, and applies a default seccomp profile unless a custom profile is supplied",
		"ACI's container securityContext does not express a read-only root filesystem, so that dimension is always graded fail-closed; a fully hardened ACI container therefore tops out at 79/100 (grade B) — one dimension (read-only rootfs) below Cloud Run's egress-capable 89/B ceiling",
		"network egress is managed (allowed by default) and can never be network=none")
	if sc == nil {
		s.Notes = append(s.Notes, "no container securityContext declared; user, capabilities, and privilege posture are graded fail-closed (harden via properties.securityContext: runAsUser + capabilities.drop=[ALL])")
	} else if sc.AllowPrivilegeEscalation != nil && !*sc.AllowPrivilegeEscalation {
		s.Notes = append(s.Notes, "allowPrivilegeEscalation: false declared (informational — not a separate containment dimension in the 7-dim scorer)")
	}

	return s
}

// --------------------------------------------------------------------------- //
// ARM template expression tolerance
//
// ARM templates carry unresolvable references as string expressions wrapped in
// square brackets — "[parameters('image')]", "[variables('cpu')]", "[concat(...)]".
// Without the deployment we cannot resolve them, so each is nullified before the
// typed decode. A graded field an expression covered then reads as unset and is
// graded fail-closed. This keeps decoding robust (an expression where a bool/int was
// expected never errors) and honest (an unknown posture scores as insecure). An
// escaped literal "[[..." (ARM's way to emit a literal leading bracket) is not an
// expression and is left intact.
// --------------------------------------------------------------------------- //

// sanitizeARMExpressions walks a decoded JSON tree and replaces every ARM template
// expression string ("[...]") with nil, in place. It returns the number of
// expressions nullified so the caller can note that some values were skipped.
func sanitizeARMExpressions(n *interface{}) int {
	switch v := (*n).(type) {
	case map[string]interface{}:
		count := 0
		for k, val := range v {
			vv := val
			count += sanitizeARMExpressions(&vv)
			v[k] = vv
		}
		return count
	case []interface{}:
		count := 0
		for i := range v {
			count += sanitizeARMExpressions(&v[i])
		}
		return count
	case string:
		if isARMExpression(v) {
			*n = nil
			return 1
		}
	}
	return 0
}

// isARMExpression reports whether a string is an ARM template expression: wrapped in
// a single pair of square brackets ("[...]"). ARM escapes a literal leading bracket
// as "[[...", which is NOT an expression.
func isARMExpression(s string) bool {
	s = strings.TrimSpace(s)
	return len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' && !strings.HasPrefix(s, "[[")
}
