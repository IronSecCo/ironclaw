package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// dfScorer grades one authoring-time Dockerfile dimension. Mirrors the runtime
// `scorer` shape but reads a DockerfileSpec. Every scorer is total for its
// dimension (full points for a hardened posture, partial for weakened, zero for
// insecure) and carries a concrete fix for the SARIF/remediation surface.
type dfScorer struct {
	key   string
	title string
	max   int
	fix   string
	grade func(DockerfileSpec) (int, Verdict, string)
}

// dfScorers is the ordered Dockerfile dimension set. Weights sum to 100. The
// heavy weights sit on the postures that ship a compromise INTO the image: the
// shipped USER (a root default = every runtime escape starts as uid 0), an
// unpinned base (silent supply-chain drift), and a baked-in secret (credential
// leaked in a layer). HEALTHCHECK/bloat are liveness/quality, weighted low.
var dfScorers = []dfScorer{
	{"dockerfile.user", "Non-root USER", 25,
		"USER 65532:65532  (declare a non-root uid in the final stage)", gradeDfUser},
	{"dockerfile.base.pinned", "Pinned base image", 20,
		"FROM image@sha256:<digest>  (pin to an immutable digest)", gradeDfBase},
	{"dockerfile.secrets", "No secrets in ENV/ARG", 20,
		"remove secret-valued ENV/ARG; inject at runtime (env/--mount=type=secret)", gradeDfSecrets},
	{"dockerfile.add", "COPY over remote/opaque ADD", 12,
		"replace ADD with COPY; fetch remote files with a checksum-verified RUN", gradeDfAdd},
	{"dockerfile.permissions", "No world-writable files", 10,
		"drop chmod 777; use COPY --chown and least-privilege modes", gradeDfPerms},
	{"dockerfile.healthcheck", "HEALTHCHECK defined", 8,
		"HEALTHCHECK CMD <liveness probe>", gradeDfHealth},
	{"dockerfile.bloat", "Layer / cache hygiene", 5,
		"chain package-cache cleanup into the install RUN", gradeDfBloat},
}

// staticCeilingNote is appended to every Dockerfile report. It is the honest
// ceiling the issue calls for: a static scan cannot see runtime hardening, so a
// high grade here is necessary but not sufficient.
const staticCeilingNote = "static Dockerfile scan grades AUTHORING-TIME posture only. Runtime hardening (dropped capabilities, seccomp, network=none, read-only rootfs, no docker.sock, no shared host namespaces) is set at `docker run` / orchestration time and is NOT expressible in a Dockerfile, so it is not graded here. A high static grade still requires a runtime scan: `ironctl scan <container>`."

// ScoreDockerfile grades a DockerfileSpec across every authoring-time dimension
// and returns the full Report. Pure: no I/O, no clock, deterministic for a given
// spec. Reuses the shared 0-100/A-F bands and Report shape so every output path
// (table/json/md/badge/sarif) works unchanged.
func ScoreDockerfile(s DockerfileSpec) Report {
	r := Report{
		Source: s.Source,
		Target: s.Target,
		Max:    TotalWeight,
		Notes:  append([]string(nil), s.Notes...),
	}
	sum := 0
	for _, sc := range dfScorers {
		pts, v, detail := sc.grade(s)
		if pts < 0 {
			pts = 0
		}
		if pts > sc.max {
			pts = sc.max
		}
		sum += pts
		r.Dimensions = append(r.Dimensions, Dimension{
			Key: sc.key, Title: sc.title, Verdict: v, Score: pts, Max: sc.max, Detail: detail,
		})
	}
	r.Score = sum
	r.Grade = grade(sum)
	r.Notes = append(r.Notes, staticCeilingNote)
	return r
}

// dockerfileDimFix returns the concrete fix for a Dockerfile dimension key, for
// the SARIF help text. Returns ("", "") for an unknown key.
func dockerfileDimFix(key string) string {
	for _, sc := range dfScorers {
		if sc.key == key {
			return sc.fix
		}
	}
	return ""
}

// --------------------------------------------------------------------------- //
// Dimension scorers.
// --------------------------------------------------------------------------- //

func gradeDfUser(s DockerfileSpec) (int, Verdict, string) {
	u := strings.TrimSpace(s.FinalUser)
	if u != "" {
		// A USER instruction is present in the final stage.
		name := u
		if i := strings.Index(name, ":"); i >= 0 { // strip :gid
			name = name[:i]
		}
		if name == "0" || strings.EqualFold(name, "root") {
			return 0, VerdictFail, fmt.Sprintf("final stage sets USER %s: the image runs as root", u)
		}
		return 25, VerdictPass, fmt.Sprintf("final stage runs as USER %s (non-root)", u)
	}
	// No explicit USER: the image inherits the base default.
	if s.BaseLooksNonRoot {
		return 20, VerdictWarn, fmt.Sprintf("no USER set; base %q advertises a non-root default. Add an explicit USER to be sure", s.BaseImage)
	}
	return 0, VerdictFail, "no USER instruction: the image defaults to root (uid 0)"
}

func gradeDfBase(s DockerfileSpec) (int, Verdict, string) {
	switch s.BasePin {
	case "digest":
		return 20, VerdictPass, fmt.Sprintf("base pinned to an immutable digest (%s)", shortRef(s.BaseImage))
	case "scratch":
		return 20, VerdictPass, "base is scratch: no base packages, minimal attack surface"
	case "tag":
		return 14, VerdictWarn, fmt.Sprintf("base pinned by mutable tag (%s); prefer an @sha256 digest for reproducibility", shortRef(s.BaseImage))
	case "latest":
		return 0, VerdictFail, fmt.Sprintf("base %q is unpinned (:latest / implicit): non-reproducible, silent drift", s.BaseImage)
	default:
		return 0, VerdictUnknown, "base image not determinable; assuming unpinned (fail-closed)"
	}
}

func gradeDfSecrets(s DockerfileSpec) (int, Verdict, string) {
	if len(s.Secrets) == 0 {
		return 20, VerdictPass, "no secret-like literal values in ENV/ARG"
	}
	return 0, VerdictFail, fmt.Sprintf("secret-like literal(s) baked into ENV/ARG: %s (persists in image layers / build history)", strings.Join(s.Secrets, ", "))
}

func gradeDfAdd(s DockerfileSpec) (int, Verdict, string) {
	if len(s.RemoteADD) > 0 {
		return 0, VerdictFail, fmt.Sprintf("remote ADD fetches over the network into a layer (no checksum, MITM): %s", strings.Join(s.RemoteADD, ", "))
	}
	if len(s.LocalADD) > 0 {
		return 8, VerdictWarn, fmt.Sprintf("ADD used for local path(s) %s: auto-extracts archives and hides intent; prefer COPY", strings.Join(s.LocalADD, ", "))
	}
	return 12, VerdictPass, "no remote or archive-extracting ADD (COPY used for local files)"
}

func gradeDfPerms(s DockerfileSpec) (int, Verdict, string) {
	if len(s.WorldWritable) > 0 {
		return 0, VerdictFail, fmt.Sprintf("world-writable permissions granted: %s (any in-container user can tamper)", strings.Join(s.WorldWritable, ", "))
	}
	return 10, VerdictPass, "no world-writable (chmod 777 / o+w) permissions"
}

func gradeDfHealth(s DockerfileSpec) (int, Verdict, string) {
	if s.Healthcheck {
		return 8, VerdictPass, "HEALTHCHECK declared: orchestrators can detect a wedged container"
	}
	return 0, VerdictWarn, "no HEALTHCHECK: a hung process is invisible to the orchestrator"
}

func gradeDfBloat(s DockerfileSpec) (int, Verdict, string) {
	if len(s.DirtyPkg) == 0 {
		return 5, VerdictPass, "no unpruned package caches detected"
	}
	return 2, VerdictWarn, fmt.Sprintf("package cache left in layer: %s", strings.Join(s.DirtyPkg, "; "))
}

// RenderSARIFDockerfile writes a SARIF 2.1.0 log for a Dockerfile report to w.
// It mirrors RenderSARIF but is driven by the Dockerfile dimension set (dfScorers)
// and anchors every result at the Dockerfile itself. One rule per dimension; one
// result per non-PASS dimension. A clean 100/A Dockerfile yields zero results.
// Deterministic: no clock, stable per-(rule,file) fingerprints.
func RenderSARIFDockerfile(w io.Writer, r Report, opts SARIFOptions) error {
	driver := sarifDriver{
		Name:           "ironctl-scan",
		Version:        r.Version,
		InformationURI: scanDocsURL,
	}
	ruleIndex := make(map[string]int, len(dfScorers))
	for i, sc := range dfScorers {
		ruleIndex[sc.key] = i
		driver.Rules = append(driver.Rules, sarifRule{
			ID:               sc.key,
			Name:             sarifRuleName(sc.key),
			ShortDescription: sarifText{Text: sc.title},
			FullDescription:  sarifText{Text: sc.title},
			Help: sarifMsg{
				Text:     fmt.Sprintf("Fix: %s\nMore: %s", sc.fix, scanDocsURL),
				Markdown: fmt.Sprintf("**Fix:** `%s`\n\n[Hardening guide](%s)", sc.fix, scanDocsURL),
			},
			DefaultConfiguration: sarifRuleConfig{Level: sarifSeverity(sc.max)},
			Properties:           sarifRuleProps{Tags: []string{"security", "containment", "dockerfile", "ironclaw"}},
		})
	}

	results := []sarifResult{}
	for _, d := range r.Dimensions {
		if d.Verdict == VerdictPass {
			continue
		}
		idx := ruleIndex[d.Key]
		results = append(results, sarifResult{
			RuleID:    d.Key,
			RuleIndex: idx,
			Level:     sarifResultLevel(d.Verdict, dfScorers[idx].max),
			Message: sarifText{Text: fmt.Sprintf("%s (%s): %s. Fix: %s",
				d.Title, d.Verdict, d.Detail, dfScorers[idx].fix)},
			Locations:           []sarifLocation{dockerfileSARIFLoc(opts, r.Target)},
			PartialFingerprints: map[string]string{"ironclawScan/v1": sarifFingerprint(d.Key, opts.ConfigFile)},
		})
	}

	out, err := json.MarshalIndent(sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs:    []sarifRun{{Tool: sarifTool{Driver: driver}, Results: results}},
	}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(out, '\n'))
	return err
}

func dockerfileSARIFLoc(opts SARIFOptions, target string) sarifLocation {
	if opts.ConfigFile != "" {
		pl := &sarifPhysical{ArtifactLocation: sarifArtifact{URI: opts.ConfigFile}}
		if opts.AnchorLine > 0 {
			pl.Region = &sarifRegion{StartLine: opts.AnchorLine}
		}
		return sarifLocation{PhysicalLocation: pl}
	}
	name := nz(target, "Dockerfile")
	return sarifLocation{LogicalLocations: []sarifLogical{{Name: name, FullyQualifiedName: "dockerfile/" + name}}}
}

// shortRef trims a long image@digest to something readable in a table cell.
func shortRef(ref string) string {
	if i := strings.Index(ref, "@sha256:"); i >= 0 {
		digest := ref[i+len("@sha256:"):]
		if len(digest) > 12 {
			digest = digest[:12] + "…"
		}
		return ref[:i] + "@sha256:" + digest
	}
	return ref
}
