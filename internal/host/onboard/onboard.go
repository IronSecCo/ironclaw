// Package onboard implements the guided first-run wizard behind `ironctl onboard`.
//
// It takes an operator from a clean checkout to a verifiably-running control-plane
// in a few ordered, idempotent steps: detect a container runtime, ensure an API
// token, confirm the model credential, point at the sandbox image build, pair a
// first channel, and verify the API is reachable.
//
// Design notes:
//   - All side effects go through injected function fields (Deps), so the whole
//     wizard is unit-testable with fakes and never reaches the network/filesystem
//     in tests it doesn't intend to.
//   - It is non-interactive by construction (reads env + flags), so --yes is the
//     default posture; --dry-run plans without writing; --force allows overwriting
//     an existing token/config. Re-running is safe (idempotent): satisfied steps
//     report "skipped".
//   - The long or irreversible steps (image build, channel pairing) are *guided*:
//     the wizard detects state and emits the exact command/next action rather than
//     silently executing a multi-minute build or a live mutation. Honest and safe.
package onboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Status is the outcome of a single wizard step.
type Status string

const (
	StatusOK      Status = "ok"      // satisfied by this run
	StatusSkipped Status = "skipped" // already satisfied (idempotent re-run)
	StatusPlanned Status = "planned" // would act, but --dry-run
	StatusAction  Status = "action"  // needs an operator action before continuing
	StatusFailed  Status = "failed"  // a hard error
)

// Step is one line of the wizard's report.
type Step struct {
	Name   string
	Status Status
	Detail string
}

// Result is the full outcome of a wizard run.
type Result struct {
	Steps        []Step
	Token        string // the API token in effect (minted or pre-existing)
	APIURL       string
	FirstMessage string // the suggested first command to run next
}

// Ok reports whether the run finished without a failed step.
func (r Result) Ok() bool {
	for _, s := range r.Steps {
		if s.Status == StatusFailed {
			return false
		}
	}
	return true
}

// Options control a single run.
type Options struct {
	Addr   string // control-plane API base URL
	Yes    bool   // non-interactive (reserved; the wizard is non-interactive anyway)
	DryRun bool   // plan only; no writes
	Force  bool   // overwrite an existing token/config
}

// Deps are the injected side-effecting collaborators. The zero value is unusable;
// use New for real os/exec-backed deps, or populate fields directly in tests.
type Deps struct {
	LookPath  func(string) (string, error)
	Getenv    func(string) string
	GenToken  func() (string, error)
	ReadFile  func(string) ([]byte, error)
	WriteFile func(string, []byte, fs.FileMode) error
	MkdirAll  func(string, fs.FileMode) error
	Stat      func(string) (fs.FileInfo, error)
	Ping      func(ctx context.Context, addr string) error
	// ImageInspect reports whether a container engine already knows about an image
	// ref (so a built/pulled sandbox image reads as satisfied instead of nagging).
	ImageInspect func(ctx context.Context, engine, ref string) (bool, error)
	Stdout       io.Writer
	ConfigPath   string // env file the token is persisted to (0600)
}

// New returns Deps wired to the real os/exec/filesystem and the given config path.
func New(configPath string) Deps {
	return Deps{
		LookPath:     execLookPath,
		Getenv:       os.Getenv,
		GenToken:     genToken,
		ReadFile:     os.ReadFile,
		WriteFile:    os.WriteFile,
		MkdirAll:     os.MkdirAll,
		Stat:         os.Stat,
		Ping:         httpPing,
		ImageInspect: execImageInspect,
		Stdout:       os.Stdout,
		ConfigPath:   configPath,
	}
}

// runtimeCandidates are checked in order; the first found is the sandbox runtime.
var runtimeCandidates = []string{"runsc", "containerd", "docker", "podman", "nerdctl"}

// imageProbers are container engines (in order) that can answer "is this image
// present?" via `<engine> image inspect <ref>`. gVisor (runsc) and containerd
// aren't CLI image stores, so a host with only those is *guided* (we can't
// introspect) rather than told the image is missing when it isn't.
var imageProbers = []string{"docker", "podman", "nerdctl"}

// modelCredentialEnv maps each model-credential the control-plane honors
// (cmd/controlplane/main.go) to a human label. IronClaw is multi-provider, so ANY
// one being set satisfies the step — demanding ANTHROPIC_API_KEY specifically would
// wrongly nag a host that runs on OpenAI/OpenRouter or a credential gateway.
var modelCredentialEnv = [][2]string{
	{"ANTHROPIC_API_KEY", "Anthropic"},
	{"OPENAI_API_KEY", "OpenAI"},
	{"OPENROUTER_API_KEY", "OpenRouter"},
	{"IRONCLAW_MODEL_GATEWAY_URL", "credential gateway"},
}

// channelProbe is one auto-registered channel adapter and the env var (plus, when
// the var must equal an exact value, want) that arms it on control-plane boot.
type channelProbe struct{ name, env, want string }

// ready reports whether this channel is armed in the given environment.
func (p channelProbe) ready(getenv func(string) string) bool {
	v := getenv(p.env)
	if p.want != "" {
		return v == p.want
	}
	return v != ""
}

// channelEnv mirrors the adapters registerChannelAdapters auto-registers from the
// environment (cmd/controlplane/main.go) — keep the two in sync. The names are the
// REAL vars the daemon reads (e.g. SLACK_BOT_TOKEN, not an IRONCLAW_-prefixed one),
// so a configured channel actually lights up here. Explicitly-wired adapters
// (WhatsApp/Email/Matrix/Google Chat/Webhook) have no single arming var and so
// aren't env-detectable; the step's guidance points at the registry instead.
var channelEnv = []channelProbe{
	{"slack", "SLACK_BOT_TOKEN", ""},
	{"discord", "DISCORD_BOT_TOKEN", ""},
	{"telegram", "TELEGRAM_BOT_TOKEN", ""},
	{"teams", "IRONCLAW_TEAMS_WEBHOOK_URL", ""},
	{"signal", "IRONCLAW_SIGNAL_CLI_URL", ""},
	{"imessage", "IRONCLAW_IMESSAGE_ENABLE", "1"},
}

// Run executes the wizard and returns its Result. It never returns a non-nil error
// for an expected "operator must do X" condition — those are StatusAction steps; a
// non-nil error is reserved for unexpected internal failures (e.g. token mint).
func (d Deps) Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Addr == "" {
		opts.Addr = "http://127.0.0.1:8787"
	}
	res := Result{APIURL: opts.Addr}

	// 1. Container runtime.
	if name, path := d.detectRuntime(); name != "" {
		res.Steps = append(res.Steps, Step{"runtime", StatusOK, fmt.Sprintf("found %s (%s)", name, path)})
	} else {
		res.Steps = append(res.Steps, Step{"runtime", StatusAction,
			"no container runtime found (runsc/containerd/docker/podman). Required for production sandboxes; --dev needs none."})
	}

	// 2. API token (the one real persisted mutation).
	tok, step, err := d.ensureToken(opts)
	if err != nil {
		return res, err
	}
	res.Token = tok
	res.Steps = append(res.Steps, step)

	// 3. Model credential (never copied/persisted — only its presence is checked).
	res.Steps = append(res.Steps, modelCredentialStep(d.Getenv))

	// 4. Sandbox image (probed when an engine is present; guided otherwise).
	res.Steps = append(res.Steps, d.sandboxImageStep(ctx))

	// 5. Pair a first channel (guided — live mutation; report what's ready).
	res.Steps = append(res.Steps, d.channelStep())

	// 6. Verify the API is reachable.
	if err := d.Ping(ctx, opts.Addr); err != nil {
		res.Steps = append(res.Steps, Step{"verify", StatusAction,
			fmt.Sprintf("control-plane not reachable at %s — start it, then re-run `ironctl onboard` to verify", opts.Addr)})
	} else {
		res.Steps = append(res.Steps, Step{"verify", StatusOK, "control-plane API is reachable"})
	}

	res.FirstMessage = fmt.Sprintf("ironctl --addr %s change submit --kind persona --group default --by you", opts.Addr)
	return res, nil
}

func (d Deps) detectRuntime() (name, path string) {
	for _, c := range runtimeCandidates {
		if p, err := d.LookPath(c); err == nil && p != "" {
			return c, p
		}
	}
	return "", ""
}

// ensureToken returns the token in effect, persisting a freshly-minted one when needed.
// Precedence: an existing env token always wins (skipped). Otherwise an existing config
// token is reused unless --force. Otherwise mint + persist (unless --dry-run).
func (d Deps) ensureToken(opts Options) (string, Step, error) {
	if env := d.Getenv("IRONCLAW_API_TOKEN"); env != "" {
		return env, Step{"api-token", StatusSkipped, "using IRONCLAW_API_TOKEN from the environment"}, nil
	}
	existing := d.readConfigToken()
	if existing != "" && !opts.Force {
		return existing, Step{"api-token", StatusSkipped,
			fmt.Sprintf("reusing the token in %s (use --force to mint a new one)", d.ConfigPath)}, nil
	}
	if opts.DryRun {
		detail := "would mint a new API token and write it to " + d.ConfigPath
		if existing != "" {
			detail = "would overwrite the token in " + d.ConfigPath + " (--force)"
		}
		return "", Step{"api-token", StatusPlanned, detail}, nil
	}
	tok, err := d.GenToken()
	if err != nil {
		return "", Step{"api-token", StatusFailed, "could not generate a token"}, fmt.Errorf("mint token: %w", err)
	}
	if err := d.writeConfigToken(tok); err != nil {
		return "", Step{"api-token", StatusFailed, err.Error()}, fmt.Errorf("persist token: %w", err)
	}
	verb := "minted"
	if existing != "" {
		verb = "replaced"
	}
	return tok, Step{"api-token", StatusOK, fmt.Sprintf("%s an API token and wrote it to %s (0600)", verb, d.ConfigPath)}, nil
}

// readConfigToken extracts IRONCLAW_API_TOKEN from the env-file at ConfigPath, if any.
func (d Deps) readConfigToken() string {
	b, err := d.ReadFile(d.ConfigPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		if v, ok := strings.CutPrefix(line, "IRONCLAW_API_TOKEN="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func (d Deps) writeConfigToken(tok string) error {
	if dir := filepath.Dir(d.ConfigPath); dir != "" && dir != "." && d.MkdirAll != nil {
		if err := d.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	content := fmt.Sprintf("# IronClaw onboarding config (chmod 0600 — keep this token secret)\nIRONCLAW_API_TOKEN=%s\n", tok)
	if err := d.WriteFile(d.ConfigPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", d.ConfigPath, err)
	}
	return nil
}

// ModelCredentials returns the human labels of every supported model credential
// (or the credential gateway) present in the environment. It is provider-agnostic
// and never reads the secret value — only presence. An empty result means none is
// configured, in which case the zero-credential `mock` provider still works.
// Exported so `ironctl doctor` reports exactly what onboarding checks, with no
// risk of the two lists drifting apart.
func ModelCredentials(getenv func(string) string) []string {
	var have []string
	for _, mc := range modelCredentialEnv {
		if getenv(mc[0]) != "" {
			have = append(have, mc[1])
		}
	}
	return have
}

// ArmedChannels returns the names of the channel adapters armed by the current
// environment (e.g. slack when SLACK_BOT_TOKEN is set). Mirrors what the
// control-plane auto-registers on boot; shared with `ironctl doctor`.
func ArmedChannels(getenv func(string) string) []string {
	var ready []string
	for _, ce := range channelEnv {
		if ce.ready(getenv) {
			ready = append(ready, ce.name)
		}
	}
	return ready
}

// modelCredentialStep reports OK when any supported provider credential (or the
// credential gateway) is present, naming which — never persisted, only detected.
func modelCredentialStep(getenv func(string) string) Step {
	have := ModelCredentials(getenv)
	if len(have) == 0 {
		return Step{"model-credential", StatusAction,
			"set a model credential in the control-plane's environment — one of ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, or IRONCLAW_MODEL_GATEWAY_URL"}
	}
	return Step{"model-credential", StatusOK,
		fmt.Sprintf("%s configured (held host-side; never enters a sandbox)", strings.Join(have, ", "))}
}

// sandboxImageStep reports whether the sandbox image is present. When an
// image-capable engine (docker/podman/nerdctl) is on PATH we actually probe it, so
// a built/pulled image reads "skipped" instead of nagging forever; otherwise we
// fall back to guidance (gVisor/containerd alone can't be introspected this way).
func (d Deps) sandboxImageStep(ctx context.Context) Step {
	ref := d.sandboxImageRef()
	if engine := d.imageEngine(); engine != "" && d.ImageInspect != nil {
		if found, _ := d.ImageInspect(ctx, engine, ref); found {
			return Step{"sandbox-image", StatusSkipped, fmt.Sprintf("image %s is present (%s)", ref, engine)}
		}
		return Step{"sandbox-image", StatusAction,
			fmt.Sprintf("image %s not found — build it: `bash container/build.sh` (%s)", ref, engine)}
	}
	if _, err := d.Stat("container/build.sh"); err == nil {
		return Step{"sandbox-image", StatusAction,
			fmt.Sprintf("build the sandbox image %s: `bash container/build.sh` (needs a container runtime)", ref)}
	}
	return Step{"sandbox-image", StatusAction,
		fmt.Sprintf("build/pull the sandbox image %s per docs (container/) before running non-dev sessions", ref)}
}

// sandboxImageRef is the image the wizard reports, honoring IRONCLAW_SANDBOX_IMAGE
// (the same var deploy/build.sh use) and defaulting to the control-plane default.
func (d Deps) sandboxImageRef() string {
	if r := d.Getenv("IRONCLAW_SANDBOX_IMAGE"); r != "" {
		return r
	}
	return "ironclaw-sandbox:latest"
}

// imageEngine returns the first image-capable container engine on PATH, or "".
func (d Deps) imageEngine() string {
	for _, e := range imageProbers {
		if p, err := d.LookPath(e); err == nil && p != "" {
			return e
		}
	}
	return ""
}

func (d Deps) channelStep() Step {
	ready := ArmedChannels(d.Getenv)
	if len(ready) == 0 {
		return Step{"channel", StatusAction,
			"no channel armed from the environment — set e.g. SLACK_BOT_TOKEN or TELEGRAM_BOT_TOKEN (see docs/channels.md), or wire one with `ironctl registry wiring ...`"}
	}
	return Step{"channel", StatusOK, fmt.Sprintf("armed from env: %s — manage wiring with `ironctl registry wiring ...`", strings.Join(ready, ", "))}
}

// Report writes a human-readable summary of the run to Stdout.
func (d Deps) Report(res Result) {
	w := d.Stdout
	fmt.Fprintln(w, "IronClaw onboarding")
	fmt.Fprintln(w, "===================")
	for _, s := range res.Steps {
		fmt.Fprintf(w, "  [%-7s] %-16s %s\n", s.Status, s.Name, s.Detail)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "API URL: %s\n", res.APIURL)
	if res.FirstMessage != "" {
		fmt.Fprintf(w, "Next:    %s\n", res.FirstMessage)
	}
}

// --- real os/exec-backed dep implementations (kept thin; not exercised by unit tests) ---

func genToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
