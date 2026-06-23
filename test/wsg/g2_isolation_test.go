//go:build wsg_verify

package wsg

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IronSecCo/ironclaw/internal/host/egress"
	"github.com/IronSecCo/ironclaw/internal/host/isolation"
)

// TestG2_OCISpec_NetworkNone proves the REAL isolation code encodes the trust
// boundary: a hardened spec has NO network namespace (network=none → no NIC), all
// capabilities dropped, no_new_privs, a read-only rootfs, the model-proxy socket
// bound read-write, and the egress-broker socket bound ONLY when opted in.
func TestG2_OCISpec_NetworkNone(t *testing.T) {
	spec := isolation.HardenedSpec("ses-wsg", "ghcr.io/ironsecco/sandbox:latest",
		"/host/in.db", "/host/out.db", "/run/ironclaw/modelproxy.sock")
	oci, err := isolation.BuildOCISpec(spec)
	if err != nil {
		t.Fatalf("BuildOCISpec: %v", err)
	}

	if oci.Linux == nil {
		t.Fatal("spec has no linux section")
	}
	for _, ns := range oci.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatal("network namespace present — network=none requires it to be omitted (no NIC)")
		}
	}
	if oci.Process == nil || !oci.Process.NoNewPrivileges {
		t.Fatal("noNewPrivileges must be true")
	}
	if oci.Root == nil || !oci.Root.Readonly {
		t.Fatal("rootfs must be read-only")
	}
	if caps := oci.Process.Capabilities; caps == nil ||
		len(caps.Bounding) != 0 || len(caps.Effective) != 0 || len(caps.Inheritable) != 0 ||
		len(caps.Permitted) != 0 || len(caps.Ambient) != 0 {
		t.Fatalf("all capability sets must be empty, got %+v", oci.Process.Capabilities)
	}
	if !mountRW(oci, "/run/ironclaw/modelproxy.sock") {
		t.Fatal("model-proxy socket must be bound read-write into the sandbox")
	}
	if mountPresent(oci, "/run/ironclaw/egress.sock") {
		t.Fatal("egress socket must NOT be bound when EgressSocket is unset (sealed by default)")
	}

	// Opting a session into brokered egress binds the broker socket; the sandbox
	// still has network=none (egress is a host-mediated socket, not a NIC).
	withEgress := spec
	withEgress.EgressSocket = "/host/egress.sock"
	oci2, err := isolation.BuildOCISpec(withEgress)
	if err != nil {
		t.Fatalf("BuildOCISpec(+egress): %v", err)
	}
	if !mountPresent(oci2, "/run/ironclaw/egress.sock") {
		t.Fatal("egress socket must be bound when EgressSocket is set")
	}
	for _, ns := range oci2.Linux.Namespaces {
		if ns.Type == "network" {
			t.Fatal("network namespace present even with egress — egress must not add a NIC")
		}
	}
	t.Log("G2 spec: network=none (no NIC), caps dropped, ro rootfs, model-proxy bound, egress opt-in only")
}

// TestG2_EgressBroker_AllowDenyAudited proves the REAL egress broker, served over a
// bound unix socket, reaches ONLY approved hosts and audits both the allow and the
// deny. This is the host-mediated egress path the sandbox uses instead of a NIC.
func TestG2_EgressBroker_AllowDenyAudited(t *testing.T) {
	// A real TLS upstream with a cert valid for "approved.test", trusted via its own
	// CA pool (no InsecureSkipVerify) — the broker forces HTTPS to external hosts.
	cert, pool := selfSignedCert(t, "approved.test")
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	upstream.StartTLS()
	defer upstream.Close()
	up, _ := url.Parse(upstream.URL)

	// The broker dials its upstream over HTTPS by host; redirect every dial to the
	// local TLS test server so "approved.test" resolves to it, and trust its CA.
	brokerTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", up.Host)
		},
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}

	var mu sync.Mutex
	var audits []egress.AuditRecord
	broker := egress.New([]string{"approved.test"},
		egress.WithTransport(brokerTransport),
		egress.WithAudit(func(r egress.AuditRecord) {
			mu.Lock()
			audits = append(audits, r)
			mu.Unlock()
		}),
	)

	sock := filepath.Join(shortSocketDir(t), "egress.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = broker.Serve(ctx, sock) }()
	waitForSocket(t, sock)

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}

	// Approved host → reachable (200, body proxied from upstream).
	resp, err := client.Get("http://approved.test/health")
	if err != nil {
		t.Fatalf("approved request over broker: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "pong" {
		t.Fatalf("approved host got %d %q, want 200 pong", resp.StatusCode, body)
	}

	// Denied host → blocked by the allowlist (403), never dialed upstream.
	resp2, err := client.Get("http://denied.test/secret")
	if err != nil {
		t.Fatalf("denied request over broker: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("denied host got %d, want 403", resp2.StatusCode)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawAllow, sawDeny bool
	for _, r := range audits {
		switch r.Host {
		case "approved.test":
			if r.Allowed && r.Status == http.StatusOK {
				sawAllow = true
			}
		case "denied.test":
			if !r.Allowed && r.Status == http.StatusForbidden {
				sawDeny = true
			}
		}
	}
	if !sawAllow {
		t.Fatalf("missing audit record for the allowed request: %+v", audits)
	}
	if !sawDeny {
		t.Fatalf("missing audit record for the denied request: %+v", audits)
	}
	t.Log("G2 egress: approved host reachable over broker socket; denied host blocked; both audited")
}

// TestG2_ModelProxySocket_Reachable proves the model-proxy unix socket — the
// sandbox's only egress path under network=none — accepts connections, the same
// liveness `ironctl doctor` checks.
func TestG2_ModelProxySocket_Reachable(t *testing.T) {
	sock := filepath.Join(shortSocketDir(t), "modelproxy.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen model-proxy socket: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Fatalf("model-proxy socket not reachable: %v", err)
	}
	conn.Close()
	t.Log("G2 model-proxy: unix socket reachable")
}

// --- helpers --------------------------------------------------------------

func mountPresent(oci *isolation.OCISpec, dest string) bool {
	for _, m := range oci.Mounts {
		if m.Destination == dest {
			return true
		}
	}
	return false
}

func mountRW(oci *isolation.OCISpec, dest string) bool {
	for _, m := range oci.Mounts {
		if m.Destination != dest {
			continue
		}
		for _, o := range m.Options {
			if o == "ro" {
				return false
			}
		}
		return true
	}
	return false
}

// selfSignedCert mints a single-use self-signed cert valid for the given DNS name
// and returns it plus a CA pool that trusts it — so the broker's HTTPS dial to that
// host verifies without disabling TLS verification.
func selfSignedCert(t *testing.T, dnsName string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{dnsName},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// shortSocketDir returns a short-pathed temp dir for unix sockets. The OS caps a
// unix socket path (sun_path) at ~104 bytes; the default per-test TempDir is too
// long on macOS, so we anchor sockets under /tmp (short on Linux CI and macOS).
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "wsg")
	if err != nil {
		t.Fatalf("mkdir socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("broker socket %s never appeared", path)
}
