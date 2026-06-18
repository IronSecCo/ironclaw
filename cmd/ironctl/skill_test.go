package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordedReq struct {
	method string
	path   string
	query  string
	body   map[string]string
}

// skillMock returns an httptest server that records the last request and replies
// with the given status + body.
func skillMock(t *testing.T, status int, respBody string) (*httptest.Server, *recordedReq) {
	t.Helper()
	rec := &recordedReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &rec.body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestCmdSkillAdd(t *testing.T) {
	srv, rec := skillMock(t, http.StatusAccepted, `{"id":"chg_1"}`)
	if err := cmdSkillAdd(srv.URL, []string{"incident-triage@1.4.0", "--group", "grp-1", "--by", "cli:admin"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/v1/skills/install" {
		t.Fatalf("unexpected request: %s %s", rec.method, rec.path)
	}
	if rec.body["skill"] != "incident-triage" || rec.body["version"] != "1.4.0" ||
		rec.body["agentGroupId"] != "grp-1" || rec.body["requestedBy"] != "cli:admin" {
		t.Fatalf("unexpected install body: %v", rec.body)
	}
}

func TestCmdSkillAddBadArgs(t *testing.T) {
	srv, _ := skillMock(t, http.StatusAccepted, "{}")
	// Missing --group.
	if err := cmdSkillAdd(srv.URL, []string{"x@1.0.0"}); err == nil {
		t.Error("add without --group should error")
	}
	// Missing @version.
	if err := cmdSkillAdd(srv.URL, []string{"x", "--group", "g"}); err == nil {
		t.Error("add without @version should error")
	}
	// Server-side rejection (400) propagates as an error.
	bad, _ := skillMock(t, http.StatusBadRequest, "unsigned bundle")
	if err := cmdSkillAdd(bad.URL, []string{"x@1.0.0", "--group", "g"}); err == nil {
		t.Error("a 400 from the daemon should surface as an error")
	}
}

func TestCmdSkillList(t *testing.T) {
	srv, rec := skillMock(t, http.StatusOK, `[{"name":"a","version":"1.0.0"},{"name":"b","version":"2.0.0"}]`)
	if err := cmdSkillList(srv.URL); err != nil {
		t.Fatalf("list: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != "/v1/skills" {
		t.Fatalf("unexpected request: %s %s", rec.method, rec.path)
	}
}

func TestCmdSkillRemove(t *testing.T) {
	srv, rec := skillMock(t, http.StatusNoContent, "")
	if err := cmdSkillRemove(srv.URL, []string{"triage@1.0.0"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != "/v1/skills/triage" || rec.query != "version=1.0.0" {
		t.Fatalf("unexpected request: %s %s ?%s", rec.method, rec.path, rec.query)
	}

	// Without a version, removes all versions (no query).
	srv2, rec2 := skillMock(t, http.StatusNoContent, "")
	if err := cmdSkillRemove(srv2.URL, []string{"triage"}); err != nil {
		t.Fatalf("remove all: %v", err)
	}
	if rec2.query != "" {
		t.Fatalf("expected no version query, got %q", rec2.query)
	}
}

func TestSkillDispatchUnknown(t *testing.T) {
	if err := cmdSkill("http://x", []string{"frobnicate"}); err == nil {
		t.Error("unknown skill subcommand should error")
	}
	if err := cmdSkill("http://x", nil); err == nil {
		t.Error("no subcommand should error")
	}
}

func TestSplitNameVersion(t *testing.T) {
	if _, _, err := splitNameVersion("x"); err == nil {
		t.Error("name without version should error")
	}
	if _, _, err := splitNameVersion("@1.0"); err == nil {
		t.Error("empty name should error")
	}
	n, v, err := splitNameVersion("triage@1.4.0")
	if err != nil || n != "triage" || v != "1.4.0" {
		t.Fatalf("got %q %q %v", n, v, err)
	}

	n, v, err = splitNameVersionOptional("triage")
	if err != nil || n != "triage" || v != "" {
		t.Fatalf("optional: got %q %q %v", n, v, err)
	}
	if _, _, err := splitNameVersionOptional(""); err == nil {
		t.Error("empty optional name should error")
	}
}
