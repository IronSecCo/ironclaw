package egress

// Credential-vault addressing for the egress broker. An agent reaches a vaulted
// API by LOGICAL NAME — vault://<cred>/<path> — and never by holding a key. The
// broker treats "vault" as a single host-local destination and forwards the
// request, its own bytes unchanged, to a SEPARATE host-side principal: the
// injector, which holds the credential and attaches it host-side.
//
// The load-bearing property, asserted by the tests in vault_test.go: the broker
// itself injects NO secret. Forward only rewrites the DESTINATION to the configured
// injector and tags the request with the logical credential NAME; it adds no key
// and strips any client-supplied Authorization, so the broker can never become a
// secret sink (B4-E, threat-model §5/§7). The vault is deny-by-default: with no
// endpoint configured, vault addressing is refused.
//
// This file is the addressing + destination unit. Wiring Forward into the
// broker's request Handler (and allowlisting the injector endpoint) is the
// integration step that follows; this unit is standalone and fully tested.

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	// VaultHost is the reserved request host meaning "route to the vault". It is not
	// a real DNS name, so it never collides with an allowlisted external host.
	VaultHost = "vault"
	// VaultScheme is the equivalent URL-scheme form: vault://<cred>/<path>.
	VaultScheme = "vault"
	// VaultCredHeader carries the LOGICAL credential name (never a key) to the
	// injector so it knows which credential to attach. The broker sets it from the
	// parsed address; it is not a secret.
	VaultCredHeader = "X-Ironclaw-Vault-Cred"
)

// Vault is the broker's configured, host-local injector destination — the one place
// the vault endpoint is named. An empty endpoint means "no vault configured", in
// which case vault addressing is denied (deny-by-default, like any unapproved host).
type Vault struct {
	// endpoint is the host-local injector address (e.g. "http://127.0.0.1:8200" or
	// "https://vault.internal"). nil when the vault is disabled.
	endpoint *url.URL
}

// NewVault parses a host-local injector endpoint into a Vault. An empty endpoint
// yields a DISABLED vault (Configured()==false) rather than an error, so a broker
// with no vault simply denies vault addressing. A non-empty endpoint that is
// unparseable, not http/https, or hostless is a configuration error.
func NewVault(endpoint string) (*Vault, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return &Vault{}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("host/egress: invalid vault endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("host/egress: vault endpoint scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("host/egress: vault endpoint %q has no host", endpoint)
	}
	return &Vault{endpoint: u}, nil
}

// Configured reports whether a vault injector endpoint is set. When false, the
// broker must deny vault-addressed requests.
func (v *Vault) Configured() bool {
	return v != nil && v.endpoint != nil
}

// Endpoint returns the injector host (host or host:port) the broker must have on its
// allowlist for vault routing to be permitted — the vault destination is
// deny-by-default like any other host. Empty when not configured.
func (v *Vault) Endpoint() string {
	if !v.Configured() {
		return ""
	}
	return v.endpoint.Host
}

// IsVaultAddressed reports whether a request names the vault, by the vault:// scheme
// or the reserved "vault" host (port-insensitive, case-insensitive). Pure
// inspection — no secrets, no side effects.
func IsVaultAddressed(host, scheme string) bool {
	if strings.EqualFold(strings.TrimSpace(scheme), VaultScheme) {
		return true
	}
	return hostLabel(host) == VaultHost
}

// Forward rewrites a vault-addressed request to target the configured injector and
// returns the logical credential name (for audit). It performs NO credential
// injection: it sets the destination to the injector, tags the request with the
// credential NAME, and STRIPS any client-supplied Authorization — the broker neither
// adds a key nor passes a sandbox-supplied one through; the injector attaches the
// real credential host-side. It returns an error (deny) when the vault is not
// configured or the address is malformed, and on error leaves req unmodified.
func (v *Vault) Forward(req *http.Request) (cred string, err error) {
	if !v.Configured() {
		return "", errors.New("host/egress: vault addressing used but no vault endpoint is configured")
	}
	scheme := ""
	if req.URL != nil {
		scheme = req.URL.Scheme
	}
	if !IsVaultAddressed(req.Host, scheme) {
		// Be strict: Forward is only for vault-addressed requests. A caller that
		// misroutes a normal request here gets a clear error rather than a silent
		// redirect to the injector.
		return "", fmt.Errorf("host/egress: Forward called on a non-vault request (host %q)", req.Host)
	}

	path := "/"
	if req.URL != nil {
		path = req.URL.Path
	}
	cred, upstream, err := ParseCredential(req.Host, scheme, path)
	if err != nil {
		return "", err
	}

	// Rewrite the destination to the host-local injector; the injector maps <cred>
	// to the real upstream and attaches the credential. The broker supplies none.
	if req.URL == nil {
		req.URL = &url.URL{}
	}
	req.URL.Scheme = v.endpoint.Scheme
	req.URL.Host = v.endpoint.Host
	req.Host = v.endpoint.Host
	req.URL.Path = upstream
	req.Header.Set(VaultCredHeader, cred)
	// B4-E: the broker injects no secret and forwards no inbound credential — strip
	// any Authorization the sandbox supplied so a forged credential cannot ride
	// through to the injector; the injector is the sole source of the real one.
	req.Header.Del("Authorization")
	return cred, nil
}

// ParseCredential extracts the logical credential name and the upstream path from a
// vault-addressed request. Two equivalent forms are accepted:
//
//	scheme form: vault://<cred>/<path>  -> url.Host == <cred>, url.Path == /<path>
//	host form:   Host: vault, path /<cred>/<path>
//
// cred must be a non-empty logical label with no path traversal — it names a
// configured credential, never a key.
func ParseCredential(host, scheme, path string) (cred, upstream string, err error) {
	if hostLabel(host) == VaultHost {
		// host form: /<cred>/<rest>
		seg := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)
		cred = seg[0]
		if len(seg) == 2 && seg[1] != "" {
			upstream = "/" + seg[1]
		} else {
			upstream = "/"
		}
	} else {
		// scheme form: the host IS the credential name.
		cred = hostLabel(host)
		upstream = path
	}
	if !validCredName(cred) {
		return "", "", fmt.Errorf("host/egress: invalid vault credential name %q", cred)
	}
	if upstream == "" {
		upstream = "/"
	}
	if !strings.HasPrefix(upstream, "/") {
		upstream = "/" + upstream
	}
	return cred, upstream, nil
}

// hostLabel lowercases host and strips any :port, mirroring hostOnly in egress.go.
func hostLabel(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(host)
}

// validCredName accepts a non-empty logical credential label (a-z, 0-9, -, _, .)
// with no path traversal, so it can only ever name a configured credential and can
// never escape into a path segment.
func validCredName(s string) bool {
	if s == "" || len(s) > 128 || s == "." || s == ".." || strings.Contains(s, "..") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
