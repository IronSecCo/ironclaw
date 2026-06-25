package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
	"github.com/IronSecCo/ironclaw/internal/host/vaultinjector"
)

// newVaultRotateSignaller builds the gateway.RotateCredentialFunc seam used to
// materialize an approved credential-secret rotation (IRO-144). It POSTs to the
// injector's rotation CONTROL surface (its --control-addr/--control-socket), which is
// SEPARATE from the broker's --vault-endpoint so only the control plane can trigger a
// rotation. The request carries only the credential NAME (vaultinjector.CredHeader) —
// never a secret, in either direction — and any non-2xx is treated as a failed
// rotation so an approved-but-unapplied rotation surfaces loudly rather than silently
// no-opping. endpoint forms: "http://127.0.0.1:8201" or
// "unix:/run/ironclaw/vault-control.sock".
func newVaultRotateSignaller(endpoint string) (func(contract.AgentGroupID, string) error, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("empty vault control endpoint")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	var base string
	if strings.HasPrefix(endpoint, "unix:") {
		sock := strings.TrimPrefix(endpoint, "unix:")
		if sock == "" {
			return nil, fmt.Errorf("vault control endpoint %q names no socket path", endpoint)
		}
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		}
		base = "http://unix"
	} else {
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			return nil, fmt.Errorf("vault control endpoint must be http(s):// or unix: (got %q)", endpoint)
		}
		base = strings.TrimRight(endpoint, "/")
	}
	return func(_ contract.AgentGroupID, credential string) error {
		req, err := http.NewRequest(http.MethodPost, base+"/rotate", nil)
		if err != nil {
			return err
		}
		// The credential NAME only — the injector re-resolves the secret host-side.
		req.Header.Set(vaultinjector.CredHeader, credential)
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("signal injector rotate: %w", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("injector rotate %q: HTTP %d: %s", credential, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return nil
	}, nil
}
