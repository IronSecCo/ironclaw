package onboard

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// execLookPath is the real PATH lookup used by New.
func execLookPath(name string) (string, error) { return exec.LookPath(name) }

// httpPing checks that the control-plane API answers on its unauthenticated
// liveness endpoint. /healthz is intentionally open (AGENTS-aligned), so this
// works before a token is configured.
func httpPing(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	url := strings.TrimRight(addr, "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
