package scan

import (
	"strings"
	"testing"
)

// A hardened ACI container group expressed as an ARM template resource: non-root
// runAsUser, capabilities.drop ALL, a custom seccomp profile, allowPrivilegeEscalation
// off. This is the best an ACI container can express — note ACI has NO
// readOnlyRootFilesystem field, so that dimension is always fail-closed.
const armHardened = `{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "resources": [
    {
      "type": "Microsoft.ContainerInstance/containerGroups",
      "apiVersion": "2023-05-01",
      "name": "webgroup",
      "properties": {
        "osType": "Linux",
        "sku": "Standard",
        "containers": [
          {
            "name": "web",
            "properties": {
              "image": "example/web:1.2.3",
              "securityContext": {
                "privileged": false,
                "allowPrivilegeEscalation": false,
                "runAsUser": 1000,
                "capabilities": { "drop": ["ALL"] },
                "seccompProfile": "eyJkZWZhdWx0QWN0aW9uIjoiU0NNUF9BQ1RfRVJSTk8ifQ=="
              }
            }
          }
        ]
      }
    }
  ]
}`

// An `az container show` single-object document with a root, privileged container.
const aciShowPorous = `{
  "id": "/subscriptions/0000/resourceGroups/rg/providers/Microsoft.ContainerInstance/containerGroups/legacy",
  "name": "legacy",
  "type": "Microsoft.ContainerInstance/containerGroups",
  "location": "eastus",
  "properties": {
    "osType": "Linux",
    "containers": [
      {
        "name": "agent",
        "properties": {
          "image": "legacy:latest",
          "securityContext": { "privileged": true, "runAsUser": 0 }
        }
      }
    ]
  }
}`

func TestSpecsFromAzure_ARMHardened(t *testing.T) {
	specs, expr, err := SpecsFromAzure([]byte(armHardened))
	if err != nil {
		t.Fatal(err)
	}
	if expr {
		t.Errorf("no ARM expressions in this template; expr should be false")
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d: %v", len(specs), targets(specs))
	}
	s := specs[0]
	if s.Source != "azure" {
		t.Errorf("source: want azure, got %q", s.Source)
	}
	if s.Target != "azure/webgroup/web" {
		t.Errorf("target: want azure/webgroup/web, got %q", s.Target)
	}
	if s.RunAsNonRoot != Yes {
		t.Errorf("user: runAsUser 1000 should be non-root: %v", s.RunAsNonRoot)
	}
	if s.CapDropAll != Yes || len(s.CapAdd) != 0 {
		t.Errorf("caps: want drop-all none-added, CapDropAll=%v CapAdd=%v", s.CapDropAll, s.CapAdd)
	}
	if s.Seccomp != "confined" {
		t.Errorf("seccomp: custom profile should grade confined, got %q", s.Seccomp)
	}
	// Managed floors: no host namespaces, no docker.sock, privileged forced off.
	if s.Privileged != No || s.HostPID != No || s.HostIPC != No || s.HostNetwork != No || s.DockerSock != No {
		t.Errorf("managed floors not applied: %+v", s)
	}
	// ACI cannot express a read-only root filesystem: always fail-closed.
	if s.ReadonlyRoot != Unknown {
		t.Errorf("readonly rootfs is unexpressible on ACI; want Unknown, got %v", s.ReadonlyRoot)
	}
	// The honest ACI ceiling is 79/100 (grade B): one dimension (read-only rootfs,
	// 10 pts) below Cloud Run's egress-capable 89/B ceiling.
	r := Score(s)
	if r.Score != 79 || r.Grade != "B" {
		t.Errorf("hardened ACI ceiling: want 79/B, got %d/%s", r.Score, r.Grade)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "read-only root filesystem") {
		t.Errorf("expected a note explaining the read-only-rootfs ceiling gap: %v", r.Notes)
	}
}

func TestSpecsFromAzure_AzShowPorous(t *testing.T) {
	specs, _, err := SpecsFromAzure([]byte(aciShowPorous))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	if s.Source != "azure" || s.Target != "azure/legacy/agent" {
		t.Errorf("header: source=%q target=%q", s.Source, s.Target)
	}
	// An explicit privileged:true is the WORSE posture and must be respected over the
	// managed floor.
	if s.Privileged != Yes {
		t.Errorf("explicit privileged:true must be respected over the floor: %v", s.Privileged)
	}
	if s.RunAsNonRoot != No {
		t.Errorf("runAsUser 0 should be root: %v", s.RunAsNonRoot)
	}
	if r := Score(s); r.Score > 25 {
		t.Errorf("privileged root container should grade low, got %d/100 (%s)", r.Score, r.Grade)
	}
}

// TestSpecsFromAzure_BareIsFailClosed proves a container group with NO securityContext
// grades every user/cap/privilege dimension fail-closed while still crediting the
// managed floors (seccomp default, no host ns, no docker.sock).
func TestSpecsFromAzure_BareIsFailClosed(t *testing.T) {
	const bare = `{
      "type": "Microsoft.ContainerInstance/containerGroups",
      "name": "bare",
      "properties": { "containers": [ { "name": "c", "properties": { "image": "x:1" } } ] }
    }`
	specs, _, err := SpecsFromAzure([]byte(bare))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	if s.RunAsNonRoot != Unknown || s.CapDropAll != Unknown {
		t.Errorf("bare container should grade user/caps fail-closed: user=%v caps=%v", s.RunAsNonRoot, s.CapDropAll)
	}
	// seccomp default confined (15) + network WARN (4) + docker.sock (15) + host ns (10) = 44.
	r := Score(s)
	if r.Score != 44 {
		t.Errorf("bare ACI container: want 44 (floors only), got %d/%s", r.Score, r.Grade)
	}
	if !strings.Contains(strings.Join(r.Notes, " "), "no container securityContext declared") {
		t.Errorf("expected a note prompting securityContext hardening: %v", r.Notes)
	}
}

// TestSpecsFromAzure_ExpressionsTolerated proves an ARM template full of "[...]"
// expressions still parses fail-open: a graded field behind an expression reads as
// unset (fail-closed) and the expression flag is raised, rather than erroring out.
func TestSpecsFromAzure_ExpressionsTolerated(t *testing.T) {
	const tmpl = `{
      "resources": [
        {
          "type": "Microsoft.ContainerInstance/containerGroups",
          "name": "[parameters('groupName')]",
          "properties": {
            "containers": [
              {
                "name": "app",
                "properties": {
                  "image": "[parameters('img')]",
                  "securityContext": {
                    "runAsUser": "[parameters('uid')]",
                    "privileged": "[parameters('priv')]"
                  }
                }
              }
            ]
          }
        }
      ]
    }`
	specs, expr, err := SpecsFromAzure([]byte(tmpl))
	if err != nil {
		t.Fatalf("ARM expressions must not error (fail-open): %v", err)
	}
	if !expr {
		t.Errorf("expr flag should be true (template uses [parameters(...)])")
	}
	if len(specs) != 1 {
		t.Fatalf("want 1 container, got %d", len(specs))
	}
	s := specs[0]
	// Group name unresolved -> the fallback label; the container name is still literal.
	if s.Target != "azure/containergroup/app" {
		t.Errorf("target with unresolved group name: want azure/containergroup/app, got %q", s.Target)
	}
	// runAsUser behind an expression is unknown -> fail-closed (not credited non-root).
	if s.RunAsNonRoot == Yes {
		t.Errorf("unresolved runAsUser must not be credited non-root: %v", s.RunAsNonRoot)
	}
	// privileged behind an expression is unknown -> the managed floor makes it No
	// (not Yes): an unresolved flag must never be treated as privileged.
	if s.Privileged == Yes {
		t.Errorf("unresolved privileged expression must not grade Yes: %v", s.Privileged)
	}
}

// TestSpecsFromAzure_MultiContainerAggregate grades a group with a hardened and a
// porous container and confirms the aggregate is the WEAKEST container.
func TestSpecsFromAzure_MultiContainerAggregate(t *testing.T) {
	const grp = `{
      "type": "Microsoft.ContainerInstance/containerGroups",
      "name": "mixed",
      "properties": {
        "containers": [
          {
            "name": "safe",
            "properties": { "securityContext": {
              "runAsUser": 1000, "capabilities": { "drop": ["ALL"] }, "seccompProfile": "abc=="
            } }
          },
          {
            "name": "risky",
            "properties": { "securityContext": { "privileged": true, "runAsUser": 0 } }
          }
        ]
      }
    }`
	specs, _, err := SpecsFromAzure([]byte(grp))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 containers, got %d: %v", len(specs), targets(specs))
	}
	report, worst, err := AggregateAzure(specs, "group.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Source != "azure" || report.Target != "group.json" {
		t.Errorf("aggregate header: source=%q target=%q", report.Source, report.Target)
	}
	if worst.Target != "azure/mixed/risky" {
		t.Errorf("weakest container: want azure/mixed/risky, got %q", worst.Target)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "WEAKEST") {
		t.Errorf("aggregate should note the weakest-link rollup: %v", report.Notes)
	}
}

func TestSpecsFromAzure_NoContainerGroupIsNoSpecs(t *testing.T) {
	const tmpl = `{ "resources": [ { "type": "Microsoft.Storage/storageAccounts", "name": "sa" } ] }`
	specs, _, err := SpecsFromAzure([]byte(tmpl))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("want 0 specs for a document with no containerGroups, got %d", len(specs))
	}
	if _, _, err := AggregateAzure(specs, "empty.json"); err == nil {
		t.Errorf("aggregate of no containers should be a fail-closed error")
	}
}

func TestSpecsFromAzure_MalformedIsError(t *testing.T) {
	if _, _, err := SpecsFromAzure([]byte("{ not json")); err == nil {
		t.Errorf("malformed JSON should return a parse error (fail-open surfaces it)")
	}
}

// TestAzureManagedFloorsMatchCloudRun locks the SHARED managed-runtime floors: for
// the dimensions ACI and Cloud Run both floor (host namespaces, docker.sock, default
// seccomp, egress-capable network), an equivalently-configured ACI container and
// Cloud Run service must agree. They diverge ONLY on the two ACI-specific limits
// (read-only rootfs unexpressible, capabilities addable), which is why the ACI
// ceiling is 79/B vs Cloud Run's 89/B.
func TestAzureManagedFloorsMatchCloudRun(t *testing.T) {
	const aci = `{
      "type": "Microsoft.ContainerInstance/containerGroups",
      "name": "g",
      "properties": { "containers": [ { "name": "c", "properties": {
        "securityContext": { "runAsUser": 1000, "capabilities": { "drop": ["ALL"] } }
      } } ] }
    }`
	aciSpecs, _, err := SpecsFromAzure([]byte(aci))
	if err != nil {
		t.Fatal(err)
	}
	const cr = `apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: g
spec:
  template:
    spec:
      containers:
        - image: x:1
          securityContext:
            runAsUser: 1000
            readOnlyRootFilesystem: true
            capabilities:
              drop: [ALL]
`
	crSpecs, err := SpecsFromCloudRun([]byte(cr))
	if err != nil {
		t.Fatal(err)
	}
	if len(aciSpecs) != 1 || len(crSpecs) != 1 {
		t.Fatalf("want 1 spec each, got aci=%d cr=%d", len(aciSpecs), len(crSpecs))
	}
	a, c := aciSpecs[0], crSpecs[0]
	// Shared managed floors agree exactly.
	if a.HostPID != c.HostPID || a.HostIPC != c.HostIPC || a.HostNetwork != c.HostNetwork ||
		a.DockerSock != c.DockerSock || a.Privileged != c.Privileged || a.Seccomp != c.Seccomp {
		t.Errorf("managed floors diverge:\n aci=%+v\n cr=%+v", a, c)
	}
	// Both are egress-capable (never network=none) → the network dimension caps both.
	if got := gradeVerdict(gradeNetwork, a); got != VerdictWarn {
		t.Errorf("ACI network should be WARN (egress-capable), got %v", got)
	}
	// The 10-point read-only-rootfs gap is the whole ceiling difference.
	if Score(c).Score-Score(a).Score != 10 {
		t.Errorf("ceiling gap should be exactly the 10-pt read-only rootfs dimension: aci=%d cr=%d",
			Score(a).Score, Score(c).Score)
	}
}

// gradeVerdict runs a single dimension scorer and returns its verdict (test helper).
func gradeVerdict(fn func(Spec) (int, Verdict, string), s Spec) Verdict {
	_, v, _ := fn(s)
	return v
}
