package scan

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden regenerates the committed golden SARIF fixture: `go test
// ./internal/host/scan/ -run Golden -update`.
var updateGolden = flag.Bool("update", false, "update the golden SARIF fixture")

// goldenSpec is a fixed weak compose service used for the golden fixture: root,
// default caps, seccomp=unconfined, bridge egress, writable rootfs, docker.sock
// mounted, host PID shared. Every dimension is non-PASS, so it exercises the
// full result set with a stable, deterministic shape.
func goldenSpec() Spec {
	return Spec{
		Source: "compose", Target: "web", Image: "nginx:latest",
		RunAsNonRoot: No, User: "0",
		CapDropAll: No, Seccomp: "unconfined",
		NetworkMode: "bridge", ReadonlyRoot: No,
		DockerSock: Yes, HostPathMounts: []string{"/var/run/docker.sock"},
		HostPID: Yes,
	}
}

// TestRenderSARIF_Golden pins the exact SARIF byte output for a fixed spec so
// any accidental shape/field change is caught in review. Regenerate with
// -update after an intentional change.
func TestRenderSARIF_Golden(t *testing.T) {
	s := goldenSpec()
	r := Score(s)
	r.Version = "v0.0.0-golden"
	var b bytes.Buffer
	if err := RenderSARIF(&b, r, s, SARIFOptions{ConfigFile: "docker-compose.yml", AnchorLine: 3}); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "weak_compose.sarif.json")
	if *updateGolden {
		if err := os.WriteFile(golden, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Errorf("SARIF output drifted from golden. Run: go test ./internal/host/scan/ -run Golden -update\n--- got ---\n%s", b.String())
	}
}

// decodeSARIF renders r/s/opts to SARIF and unmarshals it, failing on malformed
// JSON so every test gets a typed tree to assert against.
func decodeSARIF(t *testing.T, r Report, s Spec, opts SARIFOptions) sarifLog {
	t.Helper()
	var b bytes.Buffer
	if err := RenderSARIF(&b, r, s, opts); err != nil {
		t.Fatalf("RenderSARIF: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(b.Bytes(), &log); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v\n%s", err, b.String())
	}
	return log
}

// TestRenderSARIF_Shape asserts the top-level 2.1.0 envelope and driver metadata
// required by the schema and by GitHub's code-scanning ingest.
func TestRenderSARIF_Shape(t *testing.T) {
	s := weakDockerSpec()
	log := decodeSARIF(t, Score(s), s, SARIFOptions{})

	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if !strings.Contains(log.Schema, "sarif-2.1.0") {
		t.Errorf("$schema = %q, missing sarif-2.1.0", log.Schema)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(log.Runs))
	}
	d := log.Runs[0].Tool.Driver
	if d.Name != "ironctl-scan" {
		t.Errorf("driver.name = %q, want ironctl-scan", d.Name)
	}
	if d.InformationURI == "" {
		t.Error("driver.informationUri is empty")
	}
	// One rule per graded dimension, indices aligned with results.
	if len(d.Rules) != len(scorers) {
		t.Fatalf("rules = %d, want %d (one per dimension)", len(d.Rules), len(scorers))
	}
	for i, rule := range d.Rules {
		if rule.ID != scorers[i].key {
			t.Errorf("rule[%d].id = %q, want %q", i, rule.ID, scorers[i].key)
		}
		if rule.ShortDescription.Text == "" || rule.FullDescription.Text == "" {
			t.Errorf("rule %q missing descriptions", rule.ID)
		}
		if rule.Help.Text == "" || rule.Help.Markdown == "" {
			t.Errorf("rule %q missing help", rule.ID)
		}
		if rule.DefaultConfiguration.Level != "error" && rule.DefaultConfiguration.Level != "warning" {
			t.Errorf("rule %q level = %q, want error|warning", rule.ID, rule.DefaultConfiguration.Level)
		}
	}
}

// TestRenderSARIF_CleanTargetZeroResults is the core acceptance: a hardened
// 100/A workload emits rules but ZERO results (nothing shows in the Security tab).
func TestRenderSARIF_CleanTargetZeroResults(t *testing.T) {
	s := Spec{
		Source: "docker", Target: "ic-sbx-demo",
		RunAsNonRoot: Yes, User: "65532", CapDropAll: Yes, Seccomp: "confined",
		NetworkMode: "none", ReadonlyRoot: Yes, DockerSock: No,
		HostPID: No, HostIPC: No, HostNetwork: No,
	}
	r := Score(s)
	if r.Score != 100 {
		t.Fatalf("precondition: score = %d, want 100", r.Score)
	}
	log := decodeSARIF(t, r, s, SARIFOptions{})
	if got := len(log.Runs[0].Results); got != 0 {
		t.Errorf("clean target produced %d results, want 0", got)
	}
	// results must still marshal as [] (not null) for schema validity.
	var b bytes.Buffer
	_ = RenderSARIF(&b, r, s, SARIFOptions{})
	if !strings.Contains(b.String(), `"results": []`) {
		t.Error("empty results must marshal as [], not null")
	}
}

// TestRenderSARIF_FailingDims asserts every non-PASS dimension yields exactly one
// result, with a valid level, a rule-aligned index, a location, and a fingerprint.
func TestRenderSARIF_FailingDims(t *testing.T) {
	s := weakDockerSpec()
	r := Score(s)
	log := decodeSARIF(t, r, s, SARIFOptions{ConfigFile: "docker-compose.yml", AnchorLine: 4})
	results := log.Runs[0].Results

	// Count non-PASS dimensions; every one must produce a result.
	wantN := 0
	for _, d := range r.Dimensions {
		if d.Verdict != VerdictPass {
			wantN++
		}
	}
	if len(results) != wantN {
		t.Fatalf("results = %d, want %d (one per non-PASS dim)", len(results), wantN)
	}

	seen := map[string]bool{}
	for _, res := range results {
		seen[res.RuleID] = true
		if res.Level != "error" && res.Level != "warning" {
			t.Errorf("result %q level = %q, want error|warning", res.RuleID, res.Level)
		}
		if res.RuleIndex < 0 || res.RuleIndex >= len(scorers) || scorers[res.RuleIndex].key != res.RuleID {
			t.Errorf("result %q ruleIndex %d does not point at its rule", res.RuleID, res.RuleIndex)
		}
		if len(res.Locations) == 0 {
			t.Errorf("result %q has no location", res.RuleID)
		}
		if res.Message.Text == "" {
			t.Errorf("result %q has empty message", res.RuleID)
		}
		if res.PartialFingerprints["ironclawScan/v1"] == "" {
			t.Errorf("result %q missing partialFingerprint", res.RuleID)
		}
		// File-anchored results point at the config with the derived region.
		pl := res.Locations[0].PhysicalLocation
		if pl == nil || pl.ArtifactLocation.URI != "docker-compose.yml" {
			t.Errorf("result %q not anchored at the config file: %+v", res.RuleID, res.Locations[0])
		}
		if pl.Region == nil || pl.Region.StartLine != 4 {
			t.Errorf("result %q region = %+v, want startLine 4", res.RuleID, pl.Region)
		}
	}
	// docker.sock exposure is a headline failure; it must be present as an error.
	if !seen["docker.sock"] {
		t.Error("expected a docker.sock result on the weak spec")
	}
}

// TestRenderSARIF_LogicalLocationForContainer: a live-container scan (no config
// file) anchors results at a logical location naming the workload, not a bogus
// physical path.
func TestRenderSARIF_LogicalLocationForContainer(t *testing.T) {
	s := weakDockerSpec()
	log := decodeSARIF(t, Score(s), s, SARIFOptions{})
	loc := log.Runs[0].Results[0].Locations[0]
	if loc.PhysicalLocation != nil {
		t.Error("container scan should not fabricate a physicalLocation")
	}
	if len(loc.LogicalLocations) == 0 || loc.LogicalLocations[0].Name != "weak" {
		t.Errorf("logicalLocation = %+v, want name 'weak'", loc.LogicalLocations)
	}
}

// TestSARIF_FingerprintStable: the partial fingerprint is deterministic per
// (rule, file) so GitHub dedupes across runs and differs when the file differs.
func TestSARIF_FingerprintStable(t *testing.T) {
	a := sarifFingerprint("caps.dropped", "docker-compose.yml")
	if a != sarifFingerprint("caps.dropped", "docker-compose.yml") {
		t.Error("fingerprint is not deterministic")
	}
	if a == sarifFingerprint("caps.dropped", "k8s.yaml") {
		t.Error("fingerprint should vary with the file")
	}
	if a == sarifFingerprint("seccomp", "docker-compose.yml") {
		t.Error("fingerprint should vary with the rule")
	}
}

func TestSARIFLevelMapping(t *testing.T) {
	if sarifSeverity(20) != "error" || sarifSeverity(15) != "error" {
		t.Error("weight >= 15 should be error")
	}
	if sarifSeverity(10) != "warning" {
		t.Error("weight < 15 should be warning")
	}
	// WARN never escalates past warning even on a heavy dimension.
	if sarifResultLevel(VerdictWarn, 20) != "warning" {
		t.Error("WARN must map to warning")
	}
	if sarifResultLevel(VerdictFail, 20) != "error" {
		t.Error("FAIL on a heavy dim must map to error")
	}
	if sarifResultLevel(VerdictUnknown, 20) != "error" {
		t.Error("UNKNOWN (fail-closed) must map to error")
	}
}

func TestAnchorLine(t *testing.T) {
	raw := []byte("version: '3'\nservices:\n  web:\n    image: nginx\n  db:\n    image: postgres\n")
	if got := AnchorLine(raw, "db"); got != 5 {
		t.Errorf("AnchorLine(db) = %d, want 5", got)
	}
	if got := AnchorLine(raw, "missing"); got != 0 {
		t.Errorf("AnchorLine(missing) = %d, want 0", got)
	}
	if got := AnchorLine(raw, ""); got != 0 {
		t.Errorf("AnchorLine(empty) = %d, want 0", got)
	}
}

func TestSarifRuleName(t *testing.T) {
	cases := map[string]string{
		"user.nonroot":     "UserNonroot",
		"caps.dropped":     "CapsDropped",
		"namespaces.host":  "NamespacesHost",
		"docker.sock":      "DockerSock",
		"rootfs.readonly":  "RootfsReadonly",
		"network.isolated": "NetworkIsolated",
	}
	for in, want := range cases {
		if got := sarifRuleName(in); got != want {
			t.Errorf("sarifRuleName(%q) = %q, want %q", in, got, want)
		}
	}
}
