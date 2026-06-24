// Command ironclaw-vault-injector is IronClaw's minimal reference credential
// injector: the SEPARATE host-side principal the egress broker forwards a vault://
// request TO. It holds the credentials (read from the host environment) and attaches
// them host-side, so the egress broker injects nothing and the sandbox never sees a
// key (threat model §11).
//
// Run it as its OWN OS user (NOT the control-plane user), listening on a loopback
// port or unix socket the broker reaches via --vault-endpoint. The control-plane and
// the injector share ONE config file (cred -> {upstream, secretEnv}): the broker reads
// it for the cred->upstream-host policy map, the injector reads it to attach secrets.
//
//	ironclaw-vault-injector --config vault-injector.json --addr 127.0.0.1:8200
//
// Config example (secrets live in the environment, NEVER in the file):
//
//	{ "creds": { "github": { "upstream": "https://api.github.com",
//	                          "secretEnv": "VAULT_GITHUB_TOKEN" } } }
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/vaultinjector"
)

func main() {
	cfgPath := flag.String("config", "", "path to the JSON injector config (cred -> {upstream, secretEnv})")
	addr := flag.String("addr", "127.0.0.1:8200", "TCP address to listen on (use a loopback port the broker allowlists)")
	socket := flag.String("socket", "", "unix socket to listen on instead of --addr (bound into the broker's reach)")
	flag.Parse()

	if *cfgPath == "" {
		log.Fatal("ironclaw-vault-injector: --config is required")
	}
	cfg, err := vaultinjector.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("ironclaw-vault-injector: %v", err)
	}
	inj, err := vaultinjector.New(cfg, os.LookupEnv, vaultinjector.WithAudit(func(rec vaultinjector.AuditRecord) {
		// Audit carries the credential NAME + correlation id — never the secret value.
		log.Printf("inject cred=%q upstream=%q path=%q status=%d allowed=%v corr=%q dur_ms=%d",
			rec.Credential, rec.Upstream, rec.Path, rec.Status, rec.Allowed, rec.CorrelationID, rec.Duration.Milliseconds())
	}))
	if err != nil {
		log.Fatalf("ironclaw-vault-injector: %v", err)
	}
	log.Printf("ironclaw-vault-injector: ready with %d credential(s): %s", len(inj.Creds()), strings.Join(inj.Creds(), ", "))

	ln, err := listen(*socket, *addr)
	if err != nil {
		log.Fatalf("ironclaw-vault-injector: listen: %v", err)
	}
	srv := &http.Server{Handler: inj.Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	where := *addr
	if *socket != "" {
		where = "unix:" + *socket
	}
	log.Printf("ironclaw-vault-injector: listening on %s", where)

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		if *socket != "" {
			_ = os.Remove(*socket)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ironclaw-vault-injector: serve: %v", err)
		}
	}
}

// listen opens a unix socket when socket is set (removing any stale file first),
// otherwise a TCP listener on addr.
func listen(socket, addr string) (net.Listener, error) {
	if socket != "" {
		_ = os.Remove(socket)
		return net.Listen("unix", socket)
	}
	return net.Listen("tcp", addr)
}
