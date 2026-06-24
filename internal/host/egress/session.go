package egress

// Per-session egress sockets. The egress broker serves a SHARED socket for ordinary
// allowlist traffic, but vault enforcement needs a session identity the broker can
// TRUST. Mirroring internal/host/mcp.Broker.SocketForSession, the broker serves a
// distinct unix socket per session whose handler fixes the session id to the one the
// host established at launch — not the spoofable X-Ironclaw-Session header. A request
// arriving on that socket is attributed to that session no matter what header it sets,
// so a compromised sandbox cannot borrow another group's vault grants.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// sessionSock is a per-session serving socket and its listener.
type sessionSock struct {
	path string
	ln   net.Listener
	srv  *http.Server
}

// SocketForSession creates (idempotently) and serves a per-session unix socket under
// dir and returns its path, for the launch layer to bind into that one sandbox. The
// socket serves the allowlist + vault handler with the session id FIXED to session, so
// the broker trusts that identity for vault policy regardless of any request header.
func (b *Broker) SocketForSession(session, dir string) (string, error) {
	if session == "" {
		return "", errors.New("host/egress: SocketForSession needs a session id")
	}
	b.socksMu.Lock()
	defer b.socksMu.Unlock()
	if s, ok := b.socks[session]; ok {
		return s.path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("host/egress: socket dir: %w", err)
	}
	path := sessionSocketPath(dir, session)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", fmt.Errorf("host/egress: listen %s: %w", path, err)
	}
	srv := &http.Server{Handler: b.sessionHandler(session)}
	go func() { _ = srv.Serve(ln) }()
	b.socks[session] = &sessionSock{path: path, ln: ln, srv: srv}
	return path, nil
}

// CloseSession stops serving and removes a session's socket (idempotent).
func (b *Broker) CloseSession(session string) {
	b.socksMu.Lock()
	s := b.socks[session]
	delete(b.socks, session)
	b.socksMu.Unlock()
	if s != nil {
		shutdownSessionSock(s)
	}
}

// CloseSessions stops every per-session socket (best-effort). The shared Serve socket
// is governed by its own ctx; this only tears down the per-session ones.
func (b *Broker) CloseSessions() {
	b.socksMu.Lock()
	socks := b.socks
	b.socks = map[string]*sessionSock{}
	b.socksMu.Unlock()
	for _, s := range socks {
		shutdownSessionSock(s)
	}
}

// sessionHandler serves the allowlist + vault handler for one fixed, host-trusted
// session id.
func (b *Broker) sessionHandler(session string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.serve(w, r, session, true)
	})
}

func shutdownSessionSock(s *sessionSock) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = os.Remove(s.path)
}

// sessionSocketPath builds a filesystem-safe, short per-session socket path (the unix
// socket path length is bounded ~104 bytes), mirroring the MCP broker.
func sessionSocketPath(dir, session string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, session)
	return dir + "/egress-" + safe + ".sock"
}
