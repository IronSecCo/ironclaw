package queue

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Factory opens the per-session encrypted queue databases under a single root
// directory. Each session owns a subdirectory holding two encrypted SQLite files
// — inbound.db and outbound.db — both keyed by that session's SessionKey via the
// frozen contract openers (RFC-0001). The factory is the one place that knows the
// on-disk layout, so the SessionManager and the sandbox launcher compose
// queues without duplicating path or provisioning logic.
//
// Role/opener matrix (all four contract openers):
//
//	         inbound.db            outbound.db
//	host     OpenInboundRW (write) OpenOutboundRO (read)
//	sandbox  OpenInboundRO  (read) OpenOutboundRW (write)
//
// The inbound file is created+schema'd by the host (sole inbound writer); the
// outbound file is created+schema'd by the sandbox (sole outbound writer). Because
// a read-only open (mode=ro) cannot create a missing file, Provision pre-creates
// both files with their schemas so the opposite side's read-only open succeeds at
// session start regardless of which process opens first.
type Factory struct {
	root string
}

// NewFactory returns a Factory rooted at dir. The directory is created lazily on
// the first Provision/open.
func NewFactory(dir string) *Factory { return &Factory{root: dir} }

// SessionPaths are the resolved on-disk locations for one session's queues.
type SessionPaths struct {
	Dir      string // per-session directory
	Inbound  string // inbound.db (host RW / sandbox RO)
	Outbound string // outbound.db (sandbox RW / host RO)
}

// Paths resolves the per-session layout for sessionID. It returns an error if
// sessionID is empty or would escape the root (path separators, "..", etc.), so a
// hostile or malformed id can never read or write outside the factory root.
func (f *Factory) Paths(sessionID string) (SessionPaths, error) {
	if err := validateSessionID(sessionID); err != nil {
		return SessionPaths{}, err
	}
	dir := filepath.Join(f.root, sessionID)
	return SessionPaths{
		Dir:      dir,
		Inbound:  filepath.Join(dir, "inbound.db"),
		Outbound: filepath.Join(dir, "outbound.db"),
	}, nil
}

// validateSessionID rejects ids that are empty or contain path separators / ".."
// so filepath.Join can never escape the root.
func validateSessionID(id string) error {
	if id == "" {
		return fmt.Errorf("host/queue: empty session id")
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("host/queue: unsafe session id %q", id)
	}
	return nil
}

// Provision creates the per-session directory and both encrypted DB files keyed by
// k, applying the inbound and outbound schemas. It is idempotent: re-provisioning
// an existing session re-opens the files (schemas use IF NOT EXISTS) and leaves
// their data intact. Provisioning with the wrong key fails (SQLITE_NOTADB) rather
// than silently corrupting a session.
func (f *Factory) Provision(sessionID string, k contract.SessionKey) error {
	p, err := f.Paths(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.Dir, 0o700); err != nil {
		return fmt.Errorf("host/queue: create session dir %q: %w", p.Dir, err)
	}
	// OpenInboundRW / OpenOutboundRW each create the file (if absent) and ensure
	// their schema; close immediately — runtime handles are opened per role below.
	indb, err := contract.OpenInboundRW(p.Inbound, k)
	if err != nil {
		return fmt.Errorf("host/queue: provision inbound: %w", err)
	}
	if err := indb.Close(); err != nil {
		return fmt.Errorf("host/queue: close provisioned inbound: %w", err)
	}
	outdb, err := contract.OpenOutboundRW(p.Outbound, k)
	if err != nil {
		return fmt.Errorf("host/queue: provision outbound: %w", err)
	}
	if err := outdb.Close(); err != nil {
		return fmt.Errorf("host/queue: close provisioned outbound: %w", err)
	}
	return nil
}

// OpenHostInbound opens the host's read/write inbound view for sessionID (the host
// is the sole inbound writer). The inbound file is created+schema'd if absent.
func (f *Factory) OpenHostInbound(sessionID string, k contract.SessionKey) (contract.InboundWriter, error) {
	p, err := f.Paths(sessionID)
	if err != nil {
		return nil, err
	}
	return OpenInbound(p.Inbound, k)
}

// OpenHostOutbound opens the host's read-only outbound view for sessionID (the
// host is the sole outbound reader). The outbound file must already exist — call
// Provision (or let the sandbox create it) first; a read-only open cannot create
// it.
func (f *Factory) OpenHostOutbound(sessionID string, k contract.SessionKey) (contract.OutboundReader, error) {
	p, err := f.Paths(sessionID)
	if err != nil {
		return nil, err
	}
	return OpenOutbound(p.Outbound, k)
}

// OpenSandboxInbound opens the sandbox's read-only inbound view for sessionID as a
// raw encrypted handle (reopen-per-poll discipline). The sandbox normally opens
// this by path in its own process; the factory exposes it for same-process
// composition and tests. The inbound file must already exist (Provision /
// OpenHostInbound creates it).
func (f *Factory) OpenSandboxInbound(sessionID string, k contract.SessionKey) (*sql.DB, error) {
	p, err := f.Paths(sessionID)
	if err != nil {
		return nil, err
	}
	return contract.OpenInboundRO(p.Inbound, k)
}

// OpenSandboxOutbound opens the sandbox's read/write outbound view for sessionID as
// a raw encrypted handle (the sandbox is the sole outbound writer). The file is
// created+schema'd if absent. Exposed for same-process composition and tests.
func (f *Factory) OpenSandboxOutbound(sessionID string, k contract.SessionKey) (*sql.DB, error) {
	p, err := f.Paths(sessionID)
	if err != nil {
		return nil, err
	}
	return contract.OpenOutboundRW(p.Outbound, k)
}
