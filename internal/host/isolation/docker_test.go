package isolation

import (
	"context"
	"encoding/binary"
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

// dockerLogFrame encodes one byte slice in Docker's multiplexed log framing
// (8-byte header: [stream, 0,0,0, size:uint32be] then the payload).
func dockerLogFrame(stream byte, payload string) []byte {
	hdr := make([]byte, 8)
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	return append(hdr, []byte(payload)...)
}

// TestDockerHandleExitInfo drives dockerHandle.ExitInfo against a fake Engine API:
// an exited container reports its code + first log line; a running container and a
// vanished (404) one both report exited=false (nothing to surface). This is the
// IRO-171 early-exit diagnostic the Manager logs after launch.
func TestDockerHandleExitInfo(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	const keyErr = `ironclaw sandbox: read session key "/var/lib/ironclaw/state/keys/ses_x/session.key": open ...: no such file or directory`
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/exited/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"State":{"Running":false,"ExitCode":1}}`))
	})
	mux.HandleFunc("/containers/exited/logs", func(w http.ResponseWriter, r *http.Request) {
		// Two frames: a blank stderr line then the real error — the first NON-EMPTY
		// line should be returned.
		_, _ = w.Write(dockerLogFrame(2, "\n"))
		_, _ = w.Write(dockerLogFrame(2, keyErr+"\n"))
	})
	mux.HandleFunc("/containers/running/json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"State":{"Running":true,"ExitCode":0}}`))
	})
	mux.HandleFunc("/containers/gone/json", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"No such container: gone"}`, http.StatusNotFound)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	d := NewDocker(sock, "none", nil, "0:0")

	t.Run("exited non-zero surfaces code and first log line", func(t *testing.T) {
		h := &dockerHandle{iso: d, id: "exited"}
		exited, code, line, err := h.ExitInfo(context.Background())
		if err != nil {
			t.Fatalf("ExitInfo: %v", err)
		}
		if !exited || code != 1 {
			t.Fatalf("got exited=%v code=%d, want true/1", exited, code)
		}
		if line != keyErr {
			t.Errorf("logLine = %q, want %q", line, keyErr)
		}
	})

	t.Run("running reports not exited", func(t *testing.T) {
		h := &dockerHandle{iso: d, id: "running"}
		exited, _, _, err := h.ExitInfo(context.Background())
		if err != nil || exited {
			t.Fatalf("got exited=%v err=%v, want false/nil", exited, err)
		}
	})

	t.Run("gone container reports not exited", func(t *testing.T) {
		h := &dockerHandle{iso: d, id: "gone"}
		exited, _, _, err := h.ExitInfo(context.Background())
		if err != nil || exited {
			t.Fatalf("got exited=%v err=%v, want false/nil", exited, err)
		}
	})
}

// TestDemuxDockerStream checks the log-framing decoder handles both multiplexed
// frames and a raw (un-framed) buffer.
func TestDemuxDockerStream(t *testing.T) {
	framed := append(dockerLogFrame(1, "hello\n"), dockerLogFrame(2, "world\n")...)
	if got := demuxDockerStream(framed); got != "hello\nworld\n" {
		t.Errorf("framed demux = %q, want %q", got, "hello\nworld\n")
	}
	// A buffer that does not look like framing is returned verbatim.
	raw := "plain text line\n"
	if got := demuxDockerStream([]byte(raw)); got != raw {
		t.Errorf("raw demux = %q, want %q", got, raw)
	}
	if got := firstNonEmptyLine("\n\n  second\nthird\n"); got != "second" {
		t.Errorf("firstNonEmptyLine = %q, want %q", got, "second")
	}
}
