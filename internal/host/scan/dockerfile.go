package scan

import (
	"fmt"
	"regexp"
	"strings"
)

// --------------------------------------------------------------------------- //
// Static Dockerfile posture grading (IRO-493).
//
// The live modes (docker/compose/k8s) grade a RUNNING or fully-specified
// workload: they can see caps, seccomp, network mode, read-only rootfs, the
// docker.sock mount, and shared host namespaces. A Dockerfile cannot. Those are
// all `docker run` / orchestration-time knobs — statically UNDECIDABLE from the
// build recipe alone. Grading a Dockerfile against them would either fail every
// Dockerfile (useless) or fake a pass (dishonest).
//
// So this mode grades a DIFFERENT, authoring-time dimension set — the postures a
// Dockerfile author actually controls: the shipped USER, base-image pinning,
// secrets baked into ENV/ARG, remote/opaque ADD, world-writable permissions,
// HEALTHCHECK, and layer/cache hygiene. It reuses the same Report/Dimension
// types, 0-100/A-F bands, and every output path (table/json/md/badge/sarif).
//
// Honest ceiling: even a perfect 100/A Dockerfile says NOTHING about runtime
// isolation. ScoreDockerfile always appends a note pointing the author at
// `ironctl scan <container>` for the runtime posture the file cannot express.
// --------------------------------------------------------------------------- //

// DockerfileSpec is the normalized authoring-time posture extracted from a parsed
// Dockerfile. Like Spec, it is source-agnostic evidence the pure scorers read;
// the parser (SpecFromDockerfile) is the only thing that touches raw bytes.
type DockerfileSpec struct {
	Source string // always "dockerfile"
	Target string // display name (file path or image), set by the caller
	Stages int    // number of build stages (FROM count)

	// --- shipped user (final stage) -----------------------------------------
	// FinalUser is the last USER instruction in the FINAL build stage ("" = none
	// declared, so the image defaults to whatever the base sets — usually root).
	FinalUser string
	// BaseLooksNonRoot is true when the final stage's base image reference itself
	// advertises a non-root default (e.g. distroless ":nonroot"). Only consulted
	// when the Dockerfile sets no explicit USER.
	BaseLooksNonRoot bool

	// --- base image pinning (final stage's external base) -------------------
	BaseImage string // resolved external base ref of the final stage
	// BasePin classifies the base tag: "digest" (@sha256), "tag" (explicit,
	// mutable), "latest" (":latest" or implicit), "scratch", or "" (none/FROM
	// unparsed).
	BasePin string

	// --- authoring signals (scanned across all stages) ----------------------
	Healthcheck   bool     // HEALTHCHECK declared in the final stage
	RemoteADD     []string // ADD <http(s)://...>: network fetch into a layer
	LocalADD      []string // ADD <localpath>: tar auto-extract / COPY-in-disguise
	Secrets       []string // ENV/ARG keys whose literal value looks like a secret
	WorldWritable []string // chmod 777 / o+w occurrences (evidence)
	DirtyPkg      []string // package installs that leave the cache in the layer

	Notes []string
}

// dfLine is one logical Dockerfile instruction: the uppercased keyword and the
// remainder (with line-continuations already joined).
type dfLine struct {
	instr string
	args  string
	stage int // 0-based build-stage index this instruction belongs to
}

// SpecFromDockerfile parses a Dockerfile and extracts its authoring-time posture.
// name is the display target (the file path). Pure and unit-testable: it touches
// no daemon, pulls no image, and reads no clock. A malformed file still returns a
// best-effort spec (fail-closed on the signals it could not read) rather than an
// error, so `scan --dockerfile` degrades gracefully.
func SpecFromDockerfile(raw []byte, name string) (DockerfileSpec, error) {
	lines := lexDockerfile(string(raw))
	s := DockerfileSpec{Source: "dockerfile", Target: nz(name, "Dockerfile")}

	// Map stage alias -> external base ref, and record the final stage index.
	stageBase := map[string]string{} // lower(alias) -> base ref
	var finalStage int
	var finalStageBaseRef string
	for _, l := range lines {
		if l.instr == "FROM" {
			ref, alias := parseFrom(l.args)
			s.Stages++
			finalStage = l.stage
			finalStageBaseRef = ref
			if alias != "" {
				stageBase[strings.ToLower(alias)] = ref
			}
		}
	}
	if s.Stages == 0 {
		return s, fmt.Errorf("no FROM instruction: not a Dockerfile")
	}

	// Resolve the final stage's ULTIMATE external base: follow FROM <prevstage>
	// aliases until we hit a real image reference. Guard against cycles.
	base := finalStageBaseRef
	for i := 0; i < s.Stages; i++ {
		next, ok := stageBase[strings.ToLower(base)]
		if !ok || next == base {
			break
		}
		base = next
	}
	s.BaseImage = base
	s.BasePin = classifyPin(base)
	s.BaseLooksNonRoot = baseLooksNonRoot(base)

	// Walk instructions. USER/HEALTHCHECK are final-stage only (they shape the
	// shipped image); ADD/RUN/ENV/ARG security signals are scanned across every
	// stage (a build-time fetch or a baked secret is a risk wherever it appears).
	for _, l := range lines {
		switch l.instr {
		case "USER":
			if l.stage == finalStage {
				if u := strings.TrimSpace(l.args); u != "" {
					s.FinalUser = u
				}
			}
		case "HEALTHCHECK":
			if l.stage == finalStage && !strings.EqualFold(strings.TrimSpace(l.args), "NONE") {
				s.Healthcheck = true
			}
		case "ADD":
			src := firstArg(l.args)
			if isRemoteRef(src) {
				s.RemoteADD = append(s.RemoteADD, src)
			} else if src != "" {
				s.LocalADD = append(s.LocalADD, src)
			}
		case "ENV", "ARG":
			for _, kv := range parseKeyVals(l.instr, l.args) {
				if looksSecretKey(kv.key) && isLiteralSecret(kv.val) {
					s.Secrets = append(s.Secrets, kv.key)
				}
			}
		case "RUN":
			if m := worldWritableRe.FindString(l.args); m != "" {
				s.WorldWritable = append(s.WorldWritable, strings.TrimSpace(m))
			}
			if pkg := dirtyPackageInstall(l.args); pkg != "" {
				s.DirtyPkg = append(s.DirtyPkg, pkg)
			}
		}
	}
	return s, nil
}

// --------------------------------------------------------------------------- //
// Lexer
// --------------------------------------------------------------------------- //

// lexDockerfile turns raw Dockerfile text into logical instruction lines: it
// joins backslash line-continuations, drops comment and blank lines, and tags
// each instruction with its build-stage index (incremented at every FROM).
func lexDockerfile(raw string) []dfLine {
	var out []dfLine
	var buf strings.Builder
	stage := -1
	flush := func() {
		joined := strings.TrimSpace(buf.String())
		buf.Reset()
		if joined == "" {
			return
		}
		fields := strings.SplitN(joined, " ", 2)
		instr := strings.ToUpper(fields[0])
		var args string
		if len(fields) > 1 {
			args = strings.TrimSpace(fields[1])
		}
		if instr == "FROM" {
			stage++
		}
		st := stage
		if st < 0 {
			st = 0
		}
		out = append(out, dfLine{instr: instr, args: args, stage: st})
	}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip comments / parser directives ONLY when not mid-continuation.
		if buf.Len() == 0 && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
			continue
		}
		// A line continues if it ends with a backslash (ignoring trailing space).
		if strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			cont := strings.TrimRight(line, " \t")
			cont = strings.TrimSuffix(cont, "\\")
			buf.WriteString(cont)
			buf.WriteString(" ")
			continue
		}
		buf.WriteString(line)
		flush()
	}
	flush()
	return out
}

// parseFrom extracts the image reference and optional stage alias from a FROM
// argument, stripping flags like --platform=linux/amd64.
func parseFrom(args string) (ref, alias string) {
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "--") {
			continue // --platform=… and friends
		}
		if ref == "" {
			ref = f
			continue
		}
		if strings.EqualFold(f, "AS") && i+1 < len(fields) {
			alias = fields[i+1]
			break
		}
	}
	return ref, alias
}

// classifyPin grades how tightly a base image reference is pinned.
func classifyPin(ref string) string {
	r := strings.TrimSpace(ref)
	if r == "" {
		return ""
	}
	if strings.EqualFold(r, "scratch") {
		return "scratch"
	}
	if strings.Contains(r, "@sha256:") || strings.Contains(r, "@sha512:") {
		return "digest"
	}
	// Separate the tag from the ref, ignoring a registry-host ":port".
	name := r
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	lastColon := strings.LastIndex(name, ":")
	lastSlash := strings.LastIndex(name, "/")
	if lastColon > lastSlash { // a tag, not a registry port
		tag := name[lastColon+1:]
		if strings.EqualFold(tag, "latest") {
			return "latest"
		}
		return "tag"
	}
	return "latest" // no tag => implicit :latest
}

// baseLooksNonRoot reports whether a base image reference itself advertises a
// non-root default (distroless :nonroot, chainguard nonroot variants, …). Only a
// hint; consulted only when the Dockerfile declares no explicit USER.
func baseLooksNonRoot(ref string) bool {
	return strings.Contains(strings.ToLower(ref), "nonroot")
}

// --------------------------------------------------------------------------- //
// Instruction helpers
// --------------------------------------------------------------------------- //

func firstArg(args string) string {
	// Skip leading --flags (e.g. ADD --chown=user).
	for _, f := range strings.Fields(args) {
		if strings.HasPrefix(f, "--") {
			continue
		}
		return f
	}
	return ""
}

func isRemoteRef(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") ||
		strings.HasPrefix(l, "git@") || strings.Contains(l, "github.com/")
}

type kv struct{ key, val string }

// parseKeyVals extracts KEY=VALUE pairs from an ENV/ARG instruction. It handles
// the `KEY=val KEY2=val2` form and the legacy `ENV KEY the rest is the value`
// form. ARG with no value is a pure declaration (no pair emitted).
func parseKeyVals(instr, args string) []kv {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	// Legacy ENV form: `ENV KEY value with spaces` (no '=' before whitespace).
	if instr == "ENV" && !strings.Contains(firstToken(args), "=") {
		f := strings.SplitN(args, " ", 2)
		if len(f) == 2 {
			return []kv{{key: f[0], val: strings.TrimSpace(f[1])}}
		}
		return nil
	}
	var out []kv
	for _, tok := range splitEnvTokens(args) {
		eq := strings.Index(tok, "=")
		if eq < 0 {
			continue // ARG KEY (declaration only) or bare token
		}
		out = append(out, kv{key: tok[:eq], val: strings.Trim(tok[eq+1:], `"'`)})
	}
	return out
}

func firstToken(s string) string {
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

// splitEnvTokens splits on whitespace but keeps quoted values together, so
// `A="b c" D=e` yields ["A=\"b c\"", "D=e"].
func splitEnvTokens(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case r == ' ' || r == '\t':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

var secretKeyRe = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|_key$|^key$|auth[_-]?token)`)

func looksSecretKey(key string) bool {
	return secretKeyRe.MatchString(strings.TrimSpace(key))
}

// isLiteralSecret reports whether a value looks like an actual baked-in secret:
// non-empty, not a build-arg reference ($FOO / ${FOO}), not an obvious empty
// placeholder. A literal value in a secret-named key ships in the image layer /
// build history — the heuristic the issue calls for.
func isLiteralSecret(val string) bool {
	v := strings.TrimSpace(val)
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "$") { // ${SECRET} / $SECRET: injected, not baked
		return false
	}
	return true
}

var worldWritableRe = regexp.MustCompile(`chmod\s+(-[A-Za-z]+\s+)*(0?777|a\+w|o\+w|ugo\+w)`)

// dirtyPackageInstall returns a short label when a RUN installs packages but
// leaves the package cache in the layer (image bloat). Low-signal / low-weight.
func dirtyPackageInstall(run string) string {
	l := strings.ToLower(run)
	switch {
	case strings.Contains(l, "apt-get install") || strings.Contains(l, "apt install"):
		if !strings.Contains(l, "rm -rf /var/lib/apt/lists") {
			return "apt cache not pruned (rm -rf /var/lib/apt/lists/*)"
		}
	case strings.Contains(l, "apk add"):
		if !strings.Contains(l, "--no-cache") && !strings.Contains(l, "rm -rf /var/cache/apk") {
			return "apk cache not pruned (apk add --no-cache)"
		}
	case strings.Contains(l, "yum install") || strings.Contains(l, "dnf install"):
		if !strings.Contains(l, "clean all") {
			return "yum/dnf cache not pruned (&& yum clean all)"
		}
	}
	return ""
}
