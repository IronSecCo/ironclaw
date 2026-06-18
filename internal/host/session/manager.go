// Package session composes the per-session live lifecycle. A Manager binds the
// pieces the control-plane built independently — the encrypted queue factory
// (host/queue), the key custodian (host/keys), the isolator
// (host/isolation), and the registry — into a single object that can open a
// session's queues, launch and track its sandbox, probe its liveness, and stop it.
//
// The Manager is the production wiring for the hooks the router and sweep take as
// interfaces:
//
//   - router.Waker / sweep.Waker  → Wake   (launch-or-no-op for a session)
//   - sweep.Prober                → Probe  (heartbeat + claim age for a session)
//   - sweep.Killer                → Kill   (stop a tracked sandbox)
//
// and it supplies the per-session writer/reader factories the router and delivery
// expect (InboundWriter / OutboundReader), each of which lazily provisions the
// session's encrypted files and key first.
//
// Launch is best-effort by design: a session's inbound message is durably queued
// before the sandbox is asked to start, so a launch failure (e.g. an
// un-provisioned rootfs in dev, or a missing runtime binary) is logged and the
// message stays queued for a later Wake — it never breaks routing or the sweep.
package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
	"github.com/IronSecCo/ironclaw/internal/host/keys"
	"github.com/IronSecCo/ironclaw/internal/host/queue"
	"github.com/IronSecCo/ironclaw/internal/host/registry"
	"github.com/IronSecCo/ironclaw/internal/host/sweep"
)

// Config wires a Manager's dependencies. Factory, Keys, Isolator, and Registry are
// required; the rest take sane defaults.
type Config struct {
	// Factory opens the per-session encrypted queues (host/queue).
	Factory *queue.Factory
	// Keys generates and custodies the per-session SessionKeys.
	Keys *keys.Custodian
	// Isolator launches sandboxes (gVisor/runsc in production; a fake in tests).
	Isolator isolation.Isolator
	// Registry resolves a session's messaging-group coordinates (for reply routing).
	Registry registry.Registry

	// ModelProxySocket is the host unix socket bound into each sandbox as its only
	// model egress path.
	ModelProxySocket string
	// EgressSocket is the OPTIONAL host unix socket of the egress broker,
	// bound into each sandbox so an agent can reach operator-approved external APIs
	// (and vault://-addressed credentials). Empty (the default) binds no egress
	// socket, leaving the sandbox sealed to the model proxy alone; the sandbox stays
	// network=none either way.
	EgressSocket string
	// SearchBackend selects the web_search tool's provider ("duckduckgo" or
	// "brave[:cred]") for every session. It only takes effect alongside EgressSocket
	// (web_search rides the egress broker); empty registers no search tool.
	SearchBackend string
	// SkillsDir is the curated skills source root. When set, a group's installed
	// skills are mounted read-only at /skills/<name> from <SkillsDir>/<name>/<version>
	//. Empty mounts no skills.
	SkillsDir string
	// MCPBroker provisions a per-session MCP broker socket for a group that has
	// gateway-approved MCP grants, bound into the sandbox so its approved MCP
	// tools work. Nil (the default) binds no MCP socket — the sandbox gets no MCP
	// surface and stays unchanged.
	MCPBroker MCPProvisioner
	// MCPSocketDir is where per-session MCP sockets are created (a tmpfs/loopback path
	// alongside the model-proxy socket in production). Defaults to
	// <os.TempDir>/ironclaw/mcp.
	MCPSocketDir string
	// Image is the sandbox container image reference recorded in the OCI spec.
	Image string
	// KeyDir is where per-session key files are written for hand-off to the sandbox
	// (a tmpfs path in production). Defaults to <os.TempDir>/ironclaw/keys.
	KeyDir string
	// WorkspaceRoot is the parent of each session's writable workspace; the
	// heartbeat file the sweep probes lives at <WorkspaceRoot>/<id>/.heartbeat.
	// Defaults to <os.TempDir>/ironclaw/workspaces.
	WorkspaceRoot string

	// SelectModel optionally resolves a per-session model backend at launch.
	// Nil (the default) launches every sandbox with the default Anthropic backend.
	// A typical wiring maps the session's agent group to its registry-configured
	// Provider/Model. The host model-proxy still authenticates and allowlists, so a
	// selection is only reachable if the host enabled that provider's credential.
	SelectModel func(contract.SessionID) ModelSelection

	// Clock returns the current time; injectable for tests. Defaults to time.Now.
	Clock func() time.Time
	// Logger receives lifecycle diagnostics. Defaults to log.Default().
	Logger *log.Logger
}

// MCPProvisioner provisions a per-session MCP broker socket for a group with
// gateway-approved MCP grants and tears it down when the session stops. Satisfied
// host-side by *mcp.Broker; a tiny interface so the session package does not import
// the mcp package.
type MCPProvisioner interface {
	// SocketForSession creates (idempotently) a per-session MCP socket under dir and
	// returns its host path, to be bound into that one sandbox.
	SocketForSession(session, dir string) (string, error)
	// CloseSession stops serving and removes a session's MCP socket.
	CloseSession(session string)
}

// ModelSelection is an optional per-session model backend override. The zero value
// (empty Provider) keeps the default Anthropic backend and its default host/model.
type ModelSelection struct {
	Provider string // "anthropic" (default), "openai", or "openrouter"
	Model    string // model id override; empty = the provider's default
	Host     string // upstream host override; empty = the provider's default
}

// tracked is a launched sandbox the Manager is responsible for.
type tracked struct {
	handle     isolation.Handle
	launchedAt time.Time
}

// Manager owns the live per-session lifecycle. It is safe for concurrent use.
type Manager struct {
	cfg Config

	mu      sync.Mutex
	running map[contract.SessionID]*tracked
	ensured map[contract.SessionID]struct{} // sessions whose files+key are initialized
}

// Compile-time checks: the Manager satisfies the hook interfaces the router and
// sweep depend on.
var (
	_ sweep.Prober = (*Manager)(nil)
	_ sweep.Killer = (*Manager)(nil)
	_ sweep.Waker  = (*Manager)(nil)
)

// New constructs a Manager, validating required dependencies and applying defaults.
func New(cfg Config) (*Manager, error) {
	if cfg.Factory == nil {
		return nil, fmt.Errorf("host/session: Factory is required")
	}
	if cfg.Keys == nil {
		return nil, fmt.Errorf("host/session: Keys custodian is required")
	}
	if cfg.Isolator == nil {
		return nil, fmt.Errorf("host/session: Isolator is required")
	}
	if cfg.Registry == nil {
		return nil, fmt.Errorf("host/session: Registry is required")
	}
	if cfg.KeyDir == "" {
		cfg.KeyDir = filepath.Join(os.TempDir(), "ironclaw", "keys")
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = filepath.Join(os.TempDir(), "ironclaw", "workspaces")
	}
	if cfg.MCPSocketDir == "" {
		cfg.MCPSocketDir = filepath.Join(os.TempDir(), "ironclaw", "mcp")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &Manager{
		cfg:     cfg,
		running: make(map[contract.SessionID]*tracked),
		ensured: make(map[contract.SessionID]struct{}),
	}, nil
}

// InboundWriter returns the host's inbound writer for a session, provisioning the
// session's key and encrypted files first. Its signature matches
// router.InboundWriterFactory and delivery.InboundWriterFactory. The caller closes
// the returned writer.
func (m *Manager) InboundWriter(id contract.SessionID) (contract.InboundWriter, error) {
	key, err := m.ensureSession(id)
	if err != nil {
		return nil, err
	}
	return m.cfg.Factory.OpenHostInbound(string(id), key)
}

// OutboundReader returns the host's read-only outbound view for a session,
// provisioning the session's key and encrypted files first. Its signature matches
// delivery.OutboundReaderFactory. The caller closes the returned reader.
func (m *Manager) OutboundReader(id contract.SessionID) (contract.OutboundReader, error) {
	key, err := m.ensureSession(id)
	if err != nil {
		return nil, err
	}
	return m.cfg.Factory.OpenHostOutbound(string(id), key)
}

// ensureKey returns the session's key, generating and custodying a fresh one the
// first time. Concurrency-safe via the custodian's own locking.
func (m *Manager) ensureKey(id contract.SessionID) (contract.SessionKey, error) {
	if k, ok := m.cfg.Keys.Get(id); ok {
		return k, nil
	}
	return m.cfg.Keys.Generate(id)
}

// ensureSession lazily generates the session key and provisions the encrypted
// queue files exactly once, returning the resolved key. Subsequent calls only
// resolve the key (cheap) and skip provisioning.
func (m *Manager) ensureSession(id contract.SessionID) (contract.SessionKey, error) {
	key, err := m.ensureKey(id)
	if err != nil {
		return key, fmt.Errorf("host/session: ensure key for %s: %w", id, err)
	}
	m.mu.Lock()
	_, done := m.ensured[id]
	m.mu.Unlock()
	if done {
		return key, nil
	}
	if err := m.cfg.Factory.Provision(string(id), key); err != nil {
		return key, fmt.Errorf("host/session: provision %s: %w", id, err)
	}
	// Seed reply routing from the registry's messaging-group coordinates so the
	// sandbox addresses its outbound replies to the originating chat. Best-effort:
	// a session with no registry record yet (or no messaging group) still works, it
	// just replies without explicit platform coordinates.
	m.seedRouting(id, key)

	m.mu.Lock()
	m.ensured[id] = struct{}{}
	m.mu.Unlock()
	return key, nil
}

// seedRouting writes the session's platform coordinates into the single
// session_routing row of its inbound queue. The host is the sole inbound writer,
// so this opens the inbound DB read/write directly (the factory's host-inbound
// view exposes only the message-level InboundWriter interface). Best-effort:
// failures are logged, never fatal.
func (m *Manager) seedRouting(id contract.SessionID, key contract.SessionKey) {
	sess, ok := m.cfg.Registry.GetSession(id)
	if !ok {
		return
	}
	mg, ok := m.cfg.Registry.GetMessagingGroup(sess.MessagingGroupID)
	if !ok {
		return
	}
	paths, err := m.cfg.Factory.Paths(string(id))
	if err != nil {
		m.cfg.Logger.Printf("host/session: routing paths for %s: %v", id, err)
		return
	}
	db, err := contract.OpenInboundRW(paths.Inbound, key)
	if err != nil {
		m.cfg.Logger.Printf("host/session: open inbound for routing %s: %v", id, err)
		return
	}
	defer db.Close()
	var threadID any
	if sess.ThreadID != nil {
		threadID = *sess.ThreadID
	}
	_, err = db.Exec(`
        INSERT INTO session_routing (id, channel_type, platform_id, thread_id)
        VALUES (1, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            channel_type=excluded.channel_type,
            platform_id=excluded.platform_id,
            thread_id=excluded.thread_id`,
		mg.ChannelType, mg.PlatformID, threadID)
	if err != nil {
		m.cfg.Logger.Printf("host/session: write session routing for %s: %v", id, err)
	}
}

// Wake launches the sandbox for a session if it is not already running. It is
// idempotent (a running session is a no-op) and best-effort on launch failure
// (logged, not returned) so a missing rootfs/runtime in dev never breaks the
// caller — the triggering message is already durably queued.
//
// Its signature matches both router.Waker and sweep.Waker.
func (m *Manager) Wake(id contract.SessionID) error {
	m.mu.Lock()
	t, already := m.running[id]
	m.mu.Unlock()
	if already {
		// Presence is not liveness. A sandbox can die out-of-band (crash, OOM, a
		// manual `docker rm`) and leave a stale handle tracked here; treating that as
		// "running" would strand the session — inbound messages queue unprocessed —
		// until the sweep's 30-minute heartbeat ceiling reaps it. Probe the tracked
		// handle and relaunch immediately when it is no longer alive.
		actx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		alive := t.handle.Alive(actx)
		cancel()
		if alive {
			return nil
		}
		m.cfg.Logger.Printf("host/session: tracked sandbox for %s is not alive; relaunching", id)
		// Untrack the dead handle — but only if it is still the one we probed, so a
		// relaunch that raced us is not clobbered — then stop it best-effort before
		// the launch below. Stopping t.handle directly (not via m.Stop) ensures we
		// only ever tear down the dead handle, never a fresh one a racer installed.
		m.mu.Lock()
		if cur, ok := m.running[id]; ok && cur == t {
			delete(m.running, id)
		}
		m.mu.Unlock()
		sctx, scancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = t.handle.Stop(sctx)
		scancel()
	}

	key, err := m.ensureSession(id)
	if err != nil {
		return err
	}
	if err := m.writeKeyFile(id, key); err != nil {
		return err
	}
	if err := os.MkdirAll(m.workspaceDir(id), 0o700); err != nil {
		return fmt.Errorf("host/session: create workspace for %s: %w", id, err)
	}
	paths, err := m.cfg.Factory.Paths(string(id))
	if err != nil {
		return fmt.Errorf("host/session: paths for %s: %w", id, err)
	}

	spec := isolation.HardenedSpec(id, m.cfg.Image, paths.Inbound, paths.Outbound, m.cfg.ModelProxySocket)
	// Hand the per-session key path to the isolator. The gVisor/OCI path delivers
	// the key via a tmpfs bundle file and ignores this; process/Docker isolators
	// pass it to cmd/sandbox as --key.
	spec.KeyPath = m.keyFilePath(id)
	if m.cfg.SelectModel != nil {
		sel := m.cfg.SelectModel(id)
		spec.ModelProvider = sel.Provider
		spec.ModelID = sel.Model
		spec.ModelHost = sel.Host
	}
	// Bind the egress-broker socket when the daemon configured one (opt-in); the
	// sandbox then gets the http_fetch tool and can reach approved hosts + vault://
	// credentials. Empty keeps the sealed default.
	if m.cfg.EgressSocket != "" {
		spec.EgressSocket = m.cfg.EgressSocket
		// web_search rides the egress broker, so the search backend only takes effect
		// here, alongside a bound egress socket.
		spec.SearchBackend = m.cfg.SearchBackend
	}
	// Load the group's gateway-approved persona and enabled-tools restriction into the
	// spec. Best-effort: session -> agent group; an unresolved session,
	// empty persona, or empty enabled-tools just leaves the defaults (base prompt, all
	// compiled tools).
	if sess, ok := m.cfg.Registry.GetSession(id); ok {
		if g, ok := m.cfg.Registry.GetAgentGroup(sess.AgentGroupID); ok {
			// ComposePersona renders the structured persona docs (identity/soul/
			// instructions) into one string, or falls back to the legacy Persona blob.
			spec.Persona = registry.ComposePersona(g)
			spec.EnabledTools = g.EnabledTools
			// Mount each installed skill's bundle read-only at /skills/<name> from the
			// curated source (only when a SkillsDir is configured).
			if m.cfg.SkillsDir != "" {
				for _, s := range g.InstalledSkills {
					spec.SkillMounts = append(spec.SkillMounts, isolation.SkillMount{
						Name:     s.Name,
						HostPath: filepath.Join(m.cfg.SkillsDir, s.Name, s.Version),
					})
				}
			}
			// MCP: when the group has gateway-approved MCP grants and a broker is wired,
			// provision a per-session MCP socket and bind it so the agent's approved MCP
			// tools register. Best-effort: a failure logs and leaves the sandbox without
			// MCP rather than blocking launch (the trigger message is already queued).
			if m.cfg.MCPBroker != nil && len(g.GrantedMCP) > 0 {
				if sock, err := m.cfg.MCPBroker.SocketForSession(string(id), m.cfg.MCPSocketDir); err != nil {
					m.cfg.Logger.Printf("host/session: mcp socket for %s deferred: %v", id, err)
				} else {
					spec.MCPSocket = sock
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	handle, err := m.cfg.Isolator.Launch(ctx, spec)
	if err != nil {
		// Best-effort: the message is already queued. Log and leave the session
		// un-launched so a later Wake retries once the environment can run it.
		m.cfg.Logger.Printf("host/session: launch %s deferred: %v", id, err)
		return nil
	}

	m.mu.Lock()
	// Re-check under lock in case a concurrent Wake won the race.
	if _, raced := m.running[id]; raced {
		m.mu.Unlock()
		_ = handle.Stop(ctx)
		return nil
	}
	m.running[id] = &tracked{handle: handle, launchedAt: m.cfg.Clock().UTC()}
	m.mu.Unlock()
	m.cfg.Logger.Printf("host/session: launched sandbox for %s", id)
	return nil
}

// Probe reports the liveness signals the sweep uses to decide whether a sandbox is
// stuck: the age (ms) of its heartbeat file and the age (ms) of its oldest
// outstanding message claim. A session the Manager is not tracking as running
// reports (-1, -1) — "unknown" — so the sweep leaves it alone. Its signature
// matches sweep.Prober.
func (m *Manager) Probe(id contract.SessionID) (heartbeatAgeMs, oldestClaimAgeMs int64, err error) {
	m.mu.Lock()
	_, running := m.running[id]
	m.mu.Unlock()
	if !running {
		return -1, -1, nil
	}
	now := m.cfg.Clock().UTC()
	return m.heartbeatAgeMs(id, now), m.oldestClaimAgeMs(id, now), nil
}

// heartbeatAgeMs returns the age of the session's heartbeat file in ms, or -1 if
// it is absent (unknown — e.g. the sandbox has not written one yet).
func (m *Manager) heartbeatAgeMs(id contract.SessionID, now time.Time) int64 {
	fi, err := os.Stat(m.heartbeatPath(id))
	if err != nil {
		return -1
	}
	age := now.Sub(fi.ModTime().UTC()).Milliseconds()
	if age < 0 {
		age = 0
	}
	return age
}

// oldestClaimAgeMs returns the age (ms) of the oldest outstanding "processing"
// claim from the session's outbound queue, or -1 if there is none / it can't be
// read.
func (m *Manager) oldestClaimAgeMs(id contract.SessionID, now time.Time) int64 {
	key, ok := m.cfg.Keys.Get(id)
	if !ok {
		return -1
	}
	reader, err := m.cfg.Factory.OpenHostOutbound(string(id), key)
	if err != nil {
		return -1
	}
	defer reader.Close()
	acks, err := reader.ProcessingAcks()
	if err != nil {
		return -1
	}
	oldest := int64(-1)
	for _, a := range acks {
		if a.Status != contract.StatusProcessing {
			continue
		}
		age := now.Sub(a.StatusChanged.UTC()).Milliseconds()
		if age > oldest {
			oldest = age
		}
	}
	return oldest
}

// Kill stops the tracked sandbox for a session. It satisfies sweep.Killer. The
// action is recorded for diagnostics; the response is the same — stop the sandbox
// so the host can reset orphaned claims and respawn it on the next trigger.
func (m *Manager) Kill(id contract.SessionID, action sweep.StuckAction) error {
	m.cfg.Logger.Printf("host/session: killing sandbox for %s (action=%v)", id, action)
	return m.Stop(context.Background(), id)
}

// Stop stops and untracks the sandbox for a session. It is a no-op (nil) if the
// session is not currently running.
func (m *Manager) Stop(ctx context.Context, id contract.SessionID) error {
	m.mu.Lock()
	t, ok := m.running[id]
	if ok {
		delete(m.running, id)
	}
	m.mu.Unlock()
	// Tear down the session's MCP socket (if any) regardless of whether a sandbox
	// handle was tracked, so resources are freed and a relaunch provisions a fresh one.
	if m.cfg.MCPBroker != nil {
		m.cfg.MCPBroker.CloseSession(string(id))
	}
	if !ok {
		return nil
	}
	return t.handle.Stop(ctx)
}

// RespawnGroup stops every live sandbox belonging to agent group gid so the next
// inbound message relaunches it (via Wake) with the group's CURRENT configuration —
// the mechanism that makes a just-approved capability change (enabled tools, persona,
// skill mounts) take effect on a running agent instead of only on the next cold start.
// Sessions not currently running are unaffected; they already launch fresh. It does
// not itself relaunch: a stopped session stays down until its next trigger, and the
// triggering/last message is durably queued, so a stop that races an in-flight turn at
// worst reprocesses that turn rather than losing it. Returns the number stopped.
//
// Its signature satisfies gateway.GroupRespawner, wired post-construction so an
// approved change can call back into the live lifecycle.
func (m *Manager) RespawnGroup(gid contract.AgentGroupID) int {
	if gid == "" {
		return 0
	}
	m.mu.Lock()
	ids := make([]contract.SessionID, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	n := 0
	for _, id := range ids {
		sess, ok := m.cfg.Registry.GetSession(id)
		if !ok || sess.AgentGroupID != gid {
			continue
		}
		if err := m.Stop(context.Background(), id); err != nil {
			m.cfg.Logger.Printf("host/session: respawn-stop %s (group %s): %v", id, gid, err)
			continue
		}
		m.cfg.Logger.Printf("host/session: stopped %s to apply an approved change for group %s; it relaunches on the next message", id, gid)
		n++
	}
	return n
}

// StopAll stops every tracked sandbox (used on daemon shutdown). It returns the
// first error encountered but always attempts to stop them all.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]contract.SessionID, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var firstErr error
	for _, id := range ids {
		if err := m.Stop(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Running reports whether the Manager is tracking a live sandbox for the session.
func (m *Manager) Running(id contract.SessionID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.running[id]
	return ok
}

// --- paths + key hand-off ---

func (m *Manager) workspaceDir(id contract.SessionID) string {
	return filepath.Join(m.cfg.WorkspaceRoot, string(id))
}

func (m *Manager) heartbeatPath(id contract.SessionID) string {
	return filepath.Join(m.workspaceDir(id), ".heartbeat")
}

func (m *Manager) keyFilePath(id contract.SessionID) string {
	return filepath.Join(m.cfg.KeyDir, string(id), "session.key")
}

// writeKeyFile writes the session key (hex) to a 0600 file for hand-off to the
// sandbox. In production this path is a tmpfs bound into the container; the key is
// never an env var and never baked into the image.
func (m *Manager) writeKeyFile(id contract.SessionID, key contract.SessionKey) error {
	path := m.keyFilePath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("host/session: create key dir for %s: %w", id, err)
	}
	if err := os.WriteFile(path, []byte(key.Hex()), 0o600); err != nil {
		return fmt.Errorf("host/session: write key file for %s: %w", id, err)
	}
	return nil
}
