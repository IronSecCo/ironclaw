package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nivardsec/ironclaw/internal/host/gateway"
)

// writeAuditLog appends entries to a fresh JSONL audit file and returns its path.
func writeAuditLog(t *testing.T, entries ...gateway.AuditEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log, err := gateway.NewAuditLog(path)
	if err != nil {
		t.Fatalf("new audit log: %v", err)
	}
	for _, e := range entries {
		if err := log.Append(e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	return path
}

func newAuditTestGateway() *gateway.Gateway {
	return gateway.New(
		gateway.VerifierChain{gateway.AlwaysRequireHuman{}},
		gateway.NewManualApprover(),
		gateway.NewLogApplier(),
		gateway.NewMemoryStore(),
	)
}

func TestUIAuditReturnsProjectedEntries(t *testing.T) {
	path := writeAuditLog(t,
		gateway.AuditEntry{Stage: gateway.AuditSubmit, ChangeID: "chg_1", Kind: "persona", Detail: "submitted"},
		gateway.AuditEntry{Stage: gateway.AuditDecision, ChangeID: "chg_1", Detail: "approved"},
	)
	h := New(newAuditTestGateway()).WithAuditPath(path).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/audit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ui/audit: got %d, want 200", rec.Code)
	}
	var views []auditEntryView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if len(views) != 2 {
		t.Fatalf("got %d entries, want 2", len(views))
	}
	// Every entry must carry a formatted timestamp and its change id.
	for _, v := range views {
		if v.Time == "" || v.ChangeID != "chg_1" {
			t.Errorf("unexpected view: %+v", v)
		}
	}
	if views[0].Stage == "" {
		t.Errorf("stage should be projected: %+v", views[0])
	}
}

func TestUIAuditEmptyWhenNoPath(t *testing.T) {
	h := New(newAuditTestGateway()).Handler() // no WithAuditPath

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/audit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var views []auditEntryView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("want empty list with no audit path, got %d", len(views))
	}
}

func TestUIAuditRespectsLimit(t *testing.T) {
	path := writeAuditLog(t,
		gateway.AuditEntry{Stage: gateway.AuditSubmit, ChangeID: "a"},
		gateway.AuditEntry{Stage: gateway.AuditSubmit, ChangeID: "b"},
		gateway.AuditEntry{Stage: gateway.AuditSubmit, ChangeID: "c"},
	)
	h := New(newAuditTestGateway()).WithAuditPath(path).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/audit?limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var views []auditEntryView
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if len(views) != 2 {
		t.Errorf("limit=2 should bound the result, got %d", len(views))
	}
}

func TestUIAuditRequiresToken(t *testing.T) {
	path := writeAuditLog(t, gateway.AuditEntry{Stage: gateway.AuditSubmit, ChangeID: "x"})
	h := New(newAuditTestGateway()).WithAuditPath(path).WithToken("s3cret").Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ui/audit", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}
}
