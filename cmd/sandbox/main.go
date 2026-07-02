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
	"strings"
	"syscall"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/sandbox/loop"
	"github.com/IronSecCo/ironclaw/internal/sandbox/provider"
	"github.com/IronSecCo/ironclaw/internal/sandbox/queue"
	"github.com/IronSecCo/ironclaw/internal/sandbox/tools"
	"github.com/IronSecCo/ironclaw/internal/version"
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
		inboundPath   = flag.String("inbound", filepath.Join(qd, "inbound.db"), "path to the inbound queue database")
		outboundPath  = flag.String("outbound", filepath.Join(qd, "outbound.db"), "path to the outbound queue database")
		keyPath       = flag.String("key", filepath.Join(qd, "session.key"), "path to the per-session key (tmpfs; raw 32 bytes or 64 hex chars)")
		workspace     = flag.String("workspace", ws, "writable workspace directory exposed to file tools")
		heartbeat     = flag.String("heartbeat", filepath.Join(ws, ".heartbeat"), "heartbeat file touched every poll")
		modelSocket   = flag.String("model-socket", filepath.Join(qd, "modelproxy.sock"), "host model-proxy unix socket")
		egressSocket  = flag.String("egress-socket", "", "host egress-broker unix socket; when set, enables the http_fetch tool for operator-approved external APIs (empty = no egress, sandbox reaches only the model proxy)")
		mcpSocket     = flag.String("mcp-socket", "", "host MCP-broker unix socket; when set, registers the agent's gateway-approved MCP-server tools (empty = no MCP surface)")
		modelHost     = flag.String("model-host", "", "upstream model host the proxy allowlists (defaults to the provider's host)")
		model         = flag.String("model", "", "model id override (defaults to the provider's default)")
		modelKind     = flag.String("provider", "", "model provider: anthropic (default), openai, openrouter, codex, gemini, vertex, local (self-hosted OpenAI-compatible: Ollama/LM Studio/vLLM), or azure (Azure OpenAI); selected per agent group host-side")
		modelProject  = flag.String("model-project", "", "Google Cloud project id for the vertex provider (rides in the request URL path)")
		modelLocation = flag.String("model-location", "", "Google Cloud region for the vertex provider (empty = the provider's default region)")
		modelAPIVer   = flag.String("model-api-version", "", "Azure OpenAI api-version query parameter (azure provider only; empty = the provider's default)")
		persona       = flag.String("persona", "", "group system-persona text appended to the system prompt (set host-side from the registry; never by the sandbox)")
		enabledTools  = flag.String("enabled-tools", "", "comma-separated subset of compiled tools to enable (empty = all; set host-side per agent group)")
		searchBackend = flag.String("search-backend", "", "search provider for the web_search tool: duckduckgo (keyless) or brave[:cred] (keyed via the vault). Requires --egress-socket; empty disables web_search")
		showVersion   = flag.Bool("version", false, "print version and exit")
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

	registry, err := buildTools(*workspace, inbound, *egressSocket, *persona, *searchBackend)
	if err != nil {
		return err
	}
	// Restrict to the group's gateway-approved enabled tools when set (empty = all).
	registry, err = tools.FilterRegistry(registry, splitCSV(*enabledTools))
	if err != nil {
		return err
	}
	// MCP tools are registered AFTER the enabled-tools filter: MCP access is its own
	// independent gateway grant (ChangeMCPAccess), not one of the compiled tools the
	// enabled-tools set restricts, so the filter must not strip it. Opt-in: only when
	// the host bound a per-session MCP-broker socket. Discovery is best-effort — a
	// momentarily unreachable broker is logged and skipped so the sandbox still starts.
	if *mcpSocket != "" {
		mcpTools, err := tools.MCPTools(*mcpSocket)
		if err != nil {
			log.Printf("ironclaw sandbox: MCP tools unavailable this launch: %v", err)
		}
		for _, t := range mcpTools {
			if err := registry.Register(t); err != nil {
				log.Printf("ironclaw sandbox: skip MCP tool %q: %v", t.Name(), err)
			}
		}
	}

	prov, err := provider.New(provider.Config{
		Kind:         *modelKind,
		SocketPath:   *modelSocket,
		UpstreamHost: *modelHost,
		Model:        *model,
		Project:      *modelProject,
		Location:     *modelLocation,
		APIVersion:   *modelAPIVer,
		System:       loop.SystemPromptWith(*persona),
	})
	if err != nil {
		return err
	}

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

// splitCSV splits a comma-separated flag value into trimmed, non-empty items.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// buildTools assembles the in-sandbox tool registry: workspace file operations,
// the gateway-bound capability-change request tool, scheduling plus its
// task-management tools (list/cancel/pause/resume/update), and the messaging
// tools (send_message / send_file / list_destinations) that emit outbound chat the
// host delivery enforces. msgCtx (the read-only inbound view) lets the messaging
// tools resolve allowed destinations and the current-thread routing. There are
// deliberately no package-install, MCP, or self-edit tools.
//
// egressSocket, when non-empty, enables the http_fetch tool over the host egress
// broker so the agent can reach operator-approved external APIs. Empty
// (the default) registers no egress tool, leaving the sandbox reachable only to
// the model proxy.
//
// searchBackend, when set alongside egressSocket, additionally registers the
// web_search tool over the same broker (the backend — duckduckgo or brave[:cred] —
// is chosen host-side, never by the agent). Empty registers no search tool.
//
// MCP-server tools are NOT built here: they are an independent gateway grant
// (ChangeMCPAccess) registered by the caller AFTER the enabled-tools filter, so the
// filter never strips them.
func buildTools(workspaceDir string, msgCtx tools.MessageContext, egressSocket, persona, searchBackend string) (*tools.Registry, error) {
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
	// Ergonomic single-call path to request network access to a new host (a typed
	// shortcut for request_capability_change's wiring/egress payload). Always present —
	// it only submits a request; the host gateway still requires human approval.
	if err := registry.Register(tools.NewRequestApiAccessTool()); err != nil {
		return nil, fmt.Errorf("register request_api_access: %w", err)
	}
	if err := registry.Register(tools.NewScheduleTaskTool()); err != nil {
		return nil, fmt.Errorf("register schedule_task: %w", err)
	}
	if err := registry.Register(tools.NewAskUserQuestionTool()); err != nil {
		return nil, fmt.Errorf("register ask_user_question: %w", err)
	}
	if err := registry.Register(tools.NewReadPersonaTool(persona)); err != nil {
		return nil, fmt.Errorf("register read_persona: %w", err)
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
	// Egress is opt-in: only when the host bound an egress-broker socket do we give
	// the agent the http_fetch tool to reach operator-approved external APIs. With
	// no socket the sandbox stays reachable only by the model proxy.
	if egressSocket != "" {
		if err := registry.Register(tools.NewHTTPFetchTool(egressSocket)); err != nil {
			return nil, fmt.Errorf("register %s: %w", tools.HTTPFetchToolName, err)
		}
		// web_search rides the same broker socket. It is opt-in twice over: only when
		// egress is bound AND the host selected a search backend. The backend is fixed
		// host-side so the agent cannot pick an arbitrary provider.
		if searchBackend != "" {
			st, err := tools.NewWebSearchTool(egressSocket, searchBackend)
			if err != nil {
				return nil, fmt.Errorf("build %s: %w", tools.WebSearchToolName, err)
			}
			if err := registry.Register(st); err != nil {
				return nil, fmt.Errorf("register %s: %w", tools.WebSearchToolName, err)
			}
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
