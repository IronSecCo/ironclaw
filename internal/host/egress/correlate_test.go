package egress

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestNewCorrelationIDFormatAndUniqueness(t *testing.T) {
	a, err := newCorrelationID()
	if err != nil {
		t.Fatalf("newCorrelationID: %v", err)
	}
	if len(a) != correlationIDBytes*2 {
		t.Fatalf("id length = %d, want %d hex chars", len(a), correlationIDBytes*2)
	}
	for _, r := range a {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("id %q is not lowercase hex", a)
		}
	}
	b, _ := newCorrelationID()
	if a == b {
		t.Fatal("two correlation ids must differ")
	}
}

func TestStampSetsHeaderAndReturns(t *testing.T) {
	req := &http.Request{Header: http.Header{}}
	id, err := NewCorrelator().Stamp(req)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if id == "" {
		t.Fatal("Stamp returned an empty id")
	}
	if got := req.Header.Get(CorrelationHeader); got != id {
		t.Fatalf("header %q != returned id %q", got, id)
	}
	if got := CorrelationID(req); got != id {
		t.Fatalf("CorrelationID readback = %q, want %q", got, id)
	}
}

// TestStampOverwritesInbound is the security property: a sandbox-supplied
// correlation id is overwritten by a fresh host-generated one, so the audit join
// cannot be forged or collided from inside the sandbox.
func TestStampOverwritesInbound(t *testing.T) {
	req := &http.Request{Header: http.Header{}}
	req.Header.Set(CorrelationHeader, "forged-by-sandbox")
	id, err := NewCorrelator().Stamp(req)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if id == "forged-by-sandbox" {
		t.Fatal("Stamp must overwrite a sandbox-supplied correlation id")
	}
	if got := req.Header.Get(CorrelationHeader); got != id {
		t.Fatalf("header not overwritten: %q", got)
	}
}

// TestStampDeterministicGenerator exercises the stamping mechanism without
// crypto/rand by injecting a counter generator.
func TestStampDeterministicGenerator(t *testing.T) {
	n := 0
	c := &Correlator{gen: func() (string, error) {
		n++
		return "id-" + string(rune('0'+n)), nil
	}}
	req := &http.Request{Header: http.Header{}}
	id1, _ := c.Stamp(req)
	if id1 != "id-1" {
		t.Fatalf("first id = %q, want id-1", id1)
	}
	id2, _ := c.Stamp(req)
	if id2 != "id-2" {
		t.Fatalf("second id = %q, want id-2", id2)
	}
}

// TestSharedJoinKey demonstrates end-to-end correlation: the broker stamps the id on
// the vault request; the vault side reads the SAME id back — the documented join key
// linking the broker audit to the vault audit.
func TestSharedJoinKey(t *testing.T) {
	// Broker side: a vault-addressed request gets a correlation id.
	vaultReq := &http.Request{Host: "vault", URL: &url.URL{Path: "/github/x"}, Header: http.Header{}}
	brokerAuditID, err := NewCorrelator().Stamp(vaultReq)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	// Vault side: reads the id off the forwarded request for its own audit.
	vaultAuditID := CorrelationID(vaultReq)
	if vaultAuditID != brokerAuditID || vaultAuditID == "" {
		t.Fatalf("join key mismatch: broker=%q vault=%q", brokerAuditID, vaultAuditID)
	}
}

func TestCorrelationIDEmpty(t *testing.T) {
	if got := CorrelationID(&http.Request{Header: http.Header{}}); got != "" {
		t.Fatalf("absent correlation id should read empty, got %q", got)
	}
	if got := CorrelationID(&http.Request{}); got != "" {
		t.Fatalf("nil-header request should read empty, got %q", got)
	}
	if got := CorrelationID(nil); got != "" {
		t.Fatalf("nil request should read empty, got %q", got)
	}
}

func TestStampGeneratorError(t *testing.T) {
	c := &Correlator{gen: func() (string, error) { return "", errors.New("entropy failure") }}
	req := &http.Request{Header: http.Header{}}
	if _, err := c.Stamp(req); err == nil {
		t.Fatal("Stamp must propagate a generator error")
	}
	if got := req.Header.Get(CorrelationHeader); got != "" {
		t.Fatalf("failed Stamp must not set a header, got %q", got)
	}
}
