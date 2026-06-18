package isolation

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerSafeName(t *testing.T) {
	if got := dockerSafeName("ses_ab.12-CD"); got != "ses_ab.12-CD" {
		t.Errorf("valid chars changed: %q", got)
	}
	if got := dockerSafeName("a/b c:d"); got != "a-b-c-d" {
		t.Errorf("got %q, want a-b-c-d", got)
	}
}

func TestSandboxArgs(t *testing.T) {
	spec := SandboxSpec{
		ReadOnlyInboundPath:   "/s/in.db",
		ReadWriteOutboundPath: "/s/out.db",
		KeyPath:               "/s/k.key",
		ModelProxySocket:      "/run/m.sock",
		WorkspacePath:         "/s/ws",
		ModelProvider:         "codex",
		ModelID:               "gpt-5.5",
		ModelHost:             "chatgpt.com",
		Persona:               "be nice",
		EnabledTools:          []string{"a", "b"},
	}
	got := strings.Join(sandboxArgs(spec), " ")
	for _, want := range []string{
		"--inbound /s/in.db", "--outbound /s/out.db", "--key /s/k.key",
		"--model-socket /run/m.sock", "--workspace /s/ws", "--provider codex",
		"--model gpt-5.5", "--model-host chatgpt.com", "--persona be nice",
		"--enabled-tools a,b",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q\n  got: %q", want, got)
		}
	}
	// Optional fields are omitted when empty.
	min := strings.Join(sandboxArgs(SandboxSpec{
		ReadOnlyInboundPath: "/i", ReadWriteOutboundPath: "/o", ModelProxySocket: "/m",
	}), " ")
	for _, absent := range []string{"--provider", "--model ", "--key", "--persona", "--workspace", "--enabled-tools"} {
		if strings.Contains(min, absent) {
			t.Errorf("minimal args should omit %q, got: %q", absent, min)
		}
	}
}

// TestDockerLaunchAndStop drives Launch/Stop against a fake Docker Engine API on a
// unix socket, asserting the create request shape and that start/remove are called.
func TestDockerLaunchAndStop(t *testing.T) {
	var createBody []byte
	var started, removed bool
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/create", func(w http.ResponseWriter, r *http.Request) {
		createBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"Id":"abc123"}`))
	})
	mux.HandleFunc("/containers/abc123/start", func(w http.ResponseWriter, r *http.Request) {
		started = true
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/abc123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			removed = true
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// Catch-all for the best-effort pre-clean DELETE by name.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	d := NewDocker(sock, "none", []string{"vol:/p"}, "0:0")
	h, err := d.Launch(context.Background(), SandboxSpec{
		SessionID: "ses_x", Image: "img",
		ReadOnlyInboundPath: "/i", ReadWriteOutboundPath: "/o", ModelProxySocket: "/m",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if !started {
		t.Error("container was not started")
	}
	var cb dockerCreateReq
	if err := json.Unmarshal(createBody, &cb); err != nil {
		t.Fatalf("create body: %v", err)
	}
	if cb.Image != "img" {
		t.Errorf("Image = %q, want img", cb.Image)
	}
	if cb.User != "0:0" {
		t.Errorf("User = %q, want 0:0", cb.User)
	}
	if cb.HostConfig.NetworkMode != "none" {
		t.Errorf("NetworkMode = %q, want none", cb.HostConfig.NetworkMode)
	}
	if len(cb.HostConfig.Binds) != 1 || cb.HostConfig.Binds[0] != "vol:/p" {
		t.Errorf("Binds = %v, want [vol:/p]", cb.HostConfig.Binds)
	}
	if err := h.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !removed {
		t.Error("container was not removed on Stop")
	}
}

// TestDockerHandleAlive drives dockerHandle.Alive against a fake Engine API,
// asserting a running container reports alive, an exited one reports dead, and a
// vanished one (HTTP 404 — the `docker rm` / crash case) reports dead.
func TestDockerHandleAlive(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/alive/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"State":{"Running":true,"Status":"running"}}`))
	})
	mux.HandleFunc("/containers/exited/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"State":{"Running":false,"Status":"exited"}}`))
	})
	mux.HandleFunc("/containers/gone/json", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"No such container: gone"}`, http.StatusNotFound)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	d := NewDocker(sock, "none", nil, "0:0")
	cases := []struct {
		id   string
		want bool
	}{
		{"alive", true},
		{"exited", false},
		{"gone", false},
	}
	for _, c := range cases {
		h := &dockerHandle{iso: d, id: c.id}
		if got := h.Alive(context.Background()); got != c.want {
			t.Errorf("Alive(%s) = %v, want %v", c.id, got, c.want)
		}
	}
}
