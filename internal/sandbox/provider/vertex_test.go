package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestVertexQuerySuccess checks the Vertex transport envelope: the project and
// location ride in the path, the host is the regional aiplatform endpoint, and the
// body/response reuse the Gemini wire format unchanged.
func TestVertexQuerySuccess(t *testing.T) {
	var gotBody []byte
	var gotPath, gotHost string
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		gotBody, _ = io.ReadAll(r.Body)
		writeSSE(w, geminiHelloStream) // identical wire format to Gemini
	}))

	p := NewVertex(Config{SocketPath: sock, Project: "my-proj", Location: "us-central1", Model: "gemini-2.5-pro"})
	out, err := p.Query(context.Background(), "hi there")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("Query output = %q, want %q", out, "hello world")
	}
	// project + location + model all ride in the path.
	for _, want := range []string{"/v1/projects/my-proj/locations/us-central1/publishers/google/models/gemini-2.5-pro", ":streamGenerateContent"} {
		if !strings.Contains(gotPath, want) {
			t.Fatalf("path = %q, want it to contain %q", gotPath, want)
		}
	}
	if gotHost != "us-central1-aiplatform.googleapis.com" {
		t.Fatalf("Host = %q, want the regional aiplatform host", gotHost)
	}
	// Body is the Gemini shape (reused translation): one user text part.
	var req gemRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(req.Contents) != 1 || req.Contents[0].Parts[0].Text != "hi there" {
		t.Fatalf("contents = %+v, want one user text part", req.Contents)
	}
}

// TestVertexReusesGeminiToolTranslation confirms NewVertex reuses GeminiProvider's
// Converse/tool path verbatim — only the URL differs from the AI Studio backend.
func TestVertexReusesGeminiToolTranslation(t *testing.T) {
	sock := serveUnix(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, sse(
			`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"a.txt"}}}]},"finishReason":"STOP"}]}`,
		))
	}))
	p := NewVertex(Config{SocketPath: sock, Project: "p", Location: "europe-west4"})
	turn, err := p.Converse(context.Background(),
		[]Message{UserTextMessage("read a.txt")},
		[]ToolSpec{{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}})
	if err != nil {
		t.Fatalf("Converse: %v", err)
	}
	if turn.StopReason != "tool_use" || len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "read_file" {
		t.Fatalf("turn = %+v, want a single read_file tool call", turn)
	}
}

// TestVertexDefaultsAndGlobalHost checks the region defaults and the global endpoint.
func TestVertexDefaultsAndGlobalHost(t *testing.T) {
	// Empty location → the default region's host and model.
	def := NewVertex(Config{SocketPath: "/x.sock", Project: "p"})
	if !strings.Contains(def.url, defaultVertexLocation+"-aiplatform.googleapis.com") {
		t.Fatalf("default url = %q, want the default-region host", def.url)
	}
	if !strings.Contains(def.url, defaultVertexModel) {
		t.Fatalf("default url = %q, want the default model", def.url)
	}
	// "global" → the region-less endpoint, with locations/global in the path.
	g := NewVertex(Config{SocketPath: "/x.sock", Project: "p", Location: "global"})
	if !strings.Contains(g.url, "//aiplatform.googleapis.com/") {
		t.Fatalf("global url = %q, want the global host", g.url)
	}
	if !strings.Contains(g.url, "/locations/global/") {
		t.Fatalf("global url = %q, want locations/global in the path", g.url)
	}
}

// TestVertexFactory checks New(Kind:"vertex") builds a Vertex-shaped GeminiProvider.
func TestVertexFactory(t *testing.T) {
	pv, err := New(Config{Kind: "Vertex", Project: "proj", Location: "asia-northeast1", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatalf("vertex: %v", err)
	}
	gp, ok := pv.(*GeminiProvider)
	if !ok {
		t.Fatalf("vertex kind = %T, want *GeminiProvider (reused)", pv)
	}
	want := "asia-northeast1-aiplatform.googleapis.com/v1/projects/proj/locations/asia-northeast1/publishers/google/models/gemini-2.5-flash:streamGenerateContent?alt=sse"
	if !strings.Contains(gp.url, want) {
		t.Fatalf("vertex url = %q, want it to contain %q", gp.url, want)
	}
}

func TestVertexHost(t *testing.T) {
	cases := map[string]string{
		"":             "aiplatform.googleapis.com",
		"global":       "aiplatform.googleapis.com",
		"us-central1":  "us-central1-aiplatform.googleapis.com",
		"europe-west4": "europe-west4-aiplatform.googleapis.com",
	}
	for in, want := range cases {
		if got := vertexHost(in); got != want {
			t.Errorf("vertexHost(%q) = %q, want %q", in, got, want)
		}
	}
}
