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
	LookPath   func(string) (string, error)
	Getenv     func(string) string
	GenToken   func() (string, error)
	ReadFile   func(string) ([]byte, error)
	WriteFile  func(string, []byte, fs.FileMode) error
	MkdirAll   func(string, fs.FileMode) error
	Stat       func(string) (fs.FileInfo, error)
	Ping       func(ctx context.Context, addr string) error
	Stdout     io.Writer
	ConfigPath string // env file the token is persisted to (0600)
}

// New returns Deps wired to the real os/exec/filesystem and the given config path.
func New(configPath string) Deps {
	return Deps{
		LookPath:   execLookPath,
		Getenv:     os.Getenv,
		GenToken:   genToken,
		ReadFile:   os.ReadFile,
		WriteFile:  os.WriteFile,
		MkdirAll:   os.MkdirAll,
		Stat:       os.Stat,
		Ping:       httpPing,
		Stdout:     os.Stdout,
		ConfigPath: configPath,
	}
}

// runtimeCandidates are checked in order; the first found is the sandbox runtime.
var runtimeCandidates = []string{"runsc", "containerd", "docker", "podman", "nerdctl"}

// channelEnv maps a channel name to the env var that, when set, means it's ready to pair.
var channelEnv = [][2]string{
	{"slack", "IRONCLAW_SLACK_BOT_TOKEN"},
	{"discord", "IRONCLAW_DISCORD_BOT_TOKEN"},
	{"telegram", "IRONCLAW_TELEGRAM_BOT_TOKEN"},
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
	if d.Getenv("ANTHROPIC_API_KEY") != "" {
		res.Steps = append(res.Steps, Step{"model-credential", StatusOK, "ANTHROPIC_API_KEY is set (held host-side; never enters a sandbox)"})
	} else {
		res.Steps = append(res.Steps, Step{"model-credential", StatusAction, "set ANTHROPIC_API_KEY in the control-plane's environment"})
	}

	// 4. Sandbox image (guided — building is long/irreversible).
	res.Steps = append(res.Steps, d.sandboxImageStep())

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

func (d Deps) sandboxImageStep() Step {
	if _, err := d.Stat("container/build.sh"); err == nil {
		return Step{"sandbox-image", StatusAction, "build the sandbox image: `bash container/build.sh` (needs a container runtime)"}
	}
	return Step{"sandbox-image", StatusAction, "build/pull the sandbox image per docs (container/) before running non-dev sessions"}
}

func (d Deps) channelStep() Step {
	var ready []string
	for _, ce := range channelEnv {
		if d.Getenv(ce[1]) != "" {
			ready = append(ready, ce[0])
		}
	}
	if len(ready) == 0 {
		return Step{"channel", StatusAction,
			"no channel token in the environment — set e.g. IRONCLAW_TELEGRAM_BOT_TOKEN, then wire it with `ironctl registry wiring ...`"}
	}
	return Step{"channel", StatusOK, fmt.Sprintf("token(s) present for: %s — wire with `ironctl registry wiring ...`", strings.Join(ready, ", "))}
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
