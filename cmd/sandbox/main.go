// OWNER: AGENT2

// Command sandbox is the in-sandbox agent entrypoint. It receives the session key
// and queue paths, constructs the queue, and runs the reasoning poll loop.
//
// The session key is read from a file (delivered via tmpfs at launch), never from
// an environment variable — the sandbox image never contains a key. The encrypted
// SQLite binding is live (RFC-0001): the queues open directly and the reasoning
// poll loop starts.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/sandbox/loop"
	"github.com/nivardsec/ironclaw/internal/sandbox/provider"
	"github.com/nivardsec/ironclaw/internal/sandbox/queue"
	"github.com/nivardsec/ironclaw/internal/sandbox/tools"
	"github.com/nivardsec/ironclaw/internal/version"
)

// defaultDirs returns the default queue/key/socket directory and the workspace
// directory. On Linux the sandbox runs inside the gVisor container where the host
// binds the queues under /run/ironclaw and the workspace at /workspace. Off-Linux
// (e.g. macOS development, where the sandbox runs as an ordinary process rather
// than under gVisor) it defaults to a user-writable base so cmd/sandbox runs
// without root; a real run still passes explicit paths matching the host.
func defaultDirs() (queueDir, workspace string) {
	if runtime.GOOS == "linux" {
		return "/run/ironclaw", "/workspace"
	}
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	root := filepath.Join(base, "ironclaw", "sandbox")
	return root, filepath.Join(root, "workspace")
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("ironclaw sandbox: %v", err)
	}
}

func run() error {
	qd, ws := defaultDirs()
	var (
		inboundPath  = flag.String("inbound", filepath.Join(qd, "inbound.db"), "path to the inbound queue database")
		outboundPath = flag.String("outbound", filepath.Join(qd, "outbound.db"), "path to the outbound queue database")
		keyPath      = flag.String("key", filepath.Join(qd, "session.key"), "path to the per-session key (tmpfs; raw 32 bytes or 64 hex chars)")
		workspace    = flag.String("workspace", ws, "writable workspace directory exposed to file tools")
		heartbeat    = flag.String("heartbeat", filepath.Join(ws, ".heartbeat"), "heartbeat file touched every poll")
		modelSocket  = flag.String("model-socket", filepath.Join(qd, "modelproxy.sock"), "host model-proxy unix socket")
		modelHost    = flag.String("model-host", "", "upstream model host the proxy allowlists (defaults to api.anthropic.com)")
		model        = flag.String("model", "", "model id override (defaults to the provider's default)")
		showVersion  = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("ironclaw-sandbox " + version.String())
		return nil
	}

	key, err := loadSessionKey(*keyPath)
	if err != nil {
		return err
	}

	// Open the encrypted queues (the RFC-0001 binding is live). The sandbox holds a
	// write view of outbound and a read-only view of inbound.
	outbound, err := queue.OpenOutbound(*outboundPath, key)
	if err != nil {
		return fmt.Errorf("open outbound queue: %w", err)
	}
	defer outbound.Close()

	inbound, err := queue.OpenInbound(*inboundPath, key)
	if err != nil {
		return fmt.Errorf("open inbound queue: %w", err)
	}
	defer inbound.Close()

	registry, err := buildTools(*workspace, inbound)
	if err != nil {
		return err
	}

	prov := provider.NewAnthropic(provider.Config{
		SocketPath:   *modelSocket,
		UpstreamHost: *modelHost,
		Model:        *model,
		System:       loop.DefaultSystemPrompt,
	})

	l, err := loop.New(loop.Config{
		Inbound:       inbound,
		Outbound:      outbound,
		Provider:      prov,
		Tools:         registry,
		HeartbeatPath: *heartbeat,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("ironclaw sandbox: starting poll loop (%d tools enabled)", len(registry.Names()))
	if err := l.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// buildTools assembles the in-sandbox tool registry: workspace file operations,
// the gateway-bound capability-change request tool, scheduling plus its
// task-management tools (list/cancel/pause/resume/update), and the messaging
// tools (send_message / send_file / list_destinations) that emit outbound chat the
// host delivery enforces. msgCtx (the read-only inbound view) lets the messaging
// tools resolve allowed destinations and the current-thread routing. There are
// deliberately no package-install, MCP, or self-edit tools.
func buildTools(workspaceDir string, msgCtx tools.MessageContext) (*tools.Registry, error) {
	registry := tools.NewRegistry()

	ws, err := tools.NewWorkspace(workspaceDir)
	if err != nil {
		return nil, err
	}
	for _, t := range ws.Tools() {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	if err := registry.Register(tools.NewRequestCapabilityChangeTool()); err != nil {
		return nil, fmt.Errorf("register request_capability_change: %w", err)
	}
	if err := registry.Register(tools.NewScheduleTaskTool()); err != nil {
		return nil, fmt.Errorf("register schedule_task: %w", err)
	}
	if err := registry.Register(tools.NewAskUserQuestionTool()); err != nil {
		return nil, fmt.Errorf("register ask_user_question: %w", err)
	}
	for _, t := range []tools.Tool{
		tools.NewSendMessageTool(msgCtx),
		tools.NewSendFileTool(ws, msgCtx),
		tools.NewListDestinationsTool(msgCtx),
	} {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	// Task-management tools (list/cancel/pause/resume/update) for the prompts the
	// agent has scheduled. Like schedule_task they forward a non-privileged system
	// action to the host's scheduling store and execute nothing directly.
	for _, t := range tools.TaskManagementTools() {
		if err := registry.Register(t); err != nil {
			return nil, fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	return registry, nil
}

// loadSessionKey reads the per-session key from path. It accepts either 64 hex
// characters (with optional surrounding whitespace) or a raw 32-byte file.
func loadSessionKey(path string) (contract.SessionKey, error) {
	var key contract.SessionKey
	raw, err := os.ReadFile(path)
	if err != nil {
		return key, fmt.Errorf("read session key %q: %w", path, err)
	}

	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == hex.EncodedLen(len(key)): // 64 hex chars
		if _, err := hex.Decode(key[:], trimmed); err != nil {
			return key, fmt.Errorf("decode hex session key: %w", err)
		}
	case len(raw) == len(key): // raw 32 bytes
		copy(key[:], raw)
	default:
		return key, fmt.Errorf("session key %q: expected 32 raw bytes or 64 hex chars, got %d bytes", path, len(raw))
	}
	return key, nil
}
