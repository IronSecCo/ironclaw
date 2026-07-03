// Package provider abstracts the model backend. The first implementation,
// AnthropicProvider, speaks the Messages API (tool use + streaming). Its HTTP
// client dials the host model-proxy unix socket, NOT the public internet — the
// sandbox has network=none.
//
// Requests are addressed to the real upstream host (api.anthropic.com) so the
// host model-proxy — an allowlisting reverse proxy keyed on the request Host —
// validates and routes them to https://<host>. The connection itself always goes
// to the unix socket (the custom DialContext ignores the address). The sandbox
// holds no model credentials: the host proxy authenticates and enforces the
// egress allowlist; the request carries only the anthropic-version header.
//
// Responses are streamed (stream:true → text/event-stream) and accumulated, which
// avoids HTTP timeouts on long generations; the reverse proxy flushes SSE through.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Provider is the minimal model backend abstraction: a single-turn text query.
type Provider interface {
	Query(ctx context.Context, prompt string) (string, error)
}

// ToolConverser is the richer surface used to drive a tool-use loop: one model
// turn given the conversation history and the available tool specs. A Provider
// that also implements ToolConverser can run the agentic loop; one that does not
// falls back to plain Query.
type ToolConverser interface {
	Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error)
}

// DefaultSocketPath is where the host binds the model-proxy unix socket inside
// the sandbox. It can be overridden via Config.SocketPath.
const DefaultSocketPath = "/run/ironclaw/modelproxy.sock"

// Default request parameters. The model default follows the current
// most-capable Claude model; the upstream host is what the model-proxy allowlists.
const (
	defaultModel        = "claude-opus-4-8"
	defaultMaxTokens    = 16000
	defaultUpstreamHost = "api.anthropic.com"
	defaultHTTPTimeout  = 5 * time.Minute // streamed generations can run long
	anthropicVersion    = "2023-06-01"
)

// KindAnthropic is the default backend: an empty cfg.Kind resolves to it, so the
// sealed single-provider posture is unchanged unless a group opts into another
// backend. Its provider (AnthropicProvider) lives in this file and self-registers
// in init below. Every OTHER kind is declared and registered in its own backend
// file (openai.go, azure.go, ollama.go, ...) so adding a provider means adding ONE
// file that touches no shared line here. Each kind maps to a model-proxy-allowlisted
// upstream host that the host authenticates with its own credential (see
// internal/host/modelproxy).
const KindAnthropic = "anthropic"

// Factory builds a Provider from cfg. A backend's Factory is responsible for
// applying that backend's own default upstream host and model when cfg leaves them
// zero (colocated with the backend, not here), so New stays a pure lookup. Backends
// register their Factory for their kind via Register, called from an init() in the
// backend's own file.
type Factory func(cfg Config) (Provider, error)

// registry maps a normalized (lowercased, trimmed) provider kind to its Factory.
// It is written ONLY by Register, which runs exclusively from package init()
// functions — before main starts and before any New call — so it is never mutated
// concurrently with the reads New performs. New does a single keyed lookup and
// never ranges the map, so provider selection has no map-iteration-order dependence
// and is race-free.
var registry = map[string]Factory{}

// Register wires a Factory for kind. Call it from a backend file's init(). It
// panics on an empty kind, a nil factory, or a duplicate registration: all three
// are init-time programming errors, and a silent overwrite could mask a backend
// whose defaults changed. The kind is normalized (lowercased, trimmed) to match the
// lookup New performs.
func Register(kind string, f Factory) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		panic("sandbox/provider: Register called with empty kind")
	}
	if f == nil {
		panic("sandbox/provider: Register called with nil factory for kind " + kind)
	}
	if _, dup := registry[kind]; dup {
		panic("sandbox/provider: duplicate provider registration for kind " + kind)
	}
	registry[kind] = f
}

func init() {
	// Anthropic is the default backend; its provider lives in this file, so it is
	// registered here rather than in a separate backend file.
	Register(KindAnthropic, func(cfg Config) (Provider, error) { return NewAnthropic(cfg), nil })
}

// New builds the Provider for cfg.Kind by looking up its registered Factory and
// delegating (the Factory applies that kind's default host/model). An empty kind
// selects Anthropic, the sealed default. The lookup is case-insensitive and trims
// surrounding whitespace. The returned Provider always reaches the network only
// through the host model-proxy unix socket (cfg.SocketPath) — the sandbox has
// network=none — and holds no credentials; the proxy authenticates per provider and
// enforces the egress allowlist. An unknown kind is an error.
func New(cfg Config) (Provider, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind == "" {
		kind = KindAnthropic
	}
	f, ok := registry[kind]
	if !ok {
		return nil, fmt.Errorf("sandbox/provider: unknown provider kind %q", cfg.Kind)
	}
	return f(cfg)
}

// Config configures a Provider. The same struct serves every backend; fields a
// given backend ignores (e.g. DisableThinking for OpenAI) are simply unused.
type Config struct {
	// Kind selects the backend: "" / "anthropic" (default), "openai",
	// "openrouter", "codex", "gemini", "vertex", "local", "azure", or "ollama". See
	// New. The kind is chosen per agent group host-side.
	Kind string
	// Project and Location are the Google Cloud project id and region used by the
	// Vertex AI backend (KindVertex); they ride in the request URL path. They are
	// ignored by every other backend. Empty Location defaults to the Vertex default
	// region; an empty Project yields a malformed Vertex URL (a misconfiguration the
	// upstream rejects), so the control-plane only selects vertex when a project is set.
	Project  string
	Location string
	// APIVersion is the Azure OpenAI api-version query parameter (KindAzure only); it
	// rides in the request URL query. Ignored by every other backend. Empty uses the
	// provider default (defaultAzureAPIVersion).
	APIVersion string
	// SocketPath is the host model-proxy unix socket. Defaults to DefaultSocketPath.
	SocketPath string
	// UpstreamHost is the model API host the proxy allowlists and routes to.
	// Defaults to api.anthropic.com.
	UpstreamHost string
	// Model is the Claude model id. Defaults to defaultModel.
	Model string
	// MaxTokens caps a single response. Defaults to defaultMaxTokens.
	MaxTokens int
	// System is an optional system prompt prepended to every request.
	System string
	// DisableThinking turns off adaptive thinking on the plain Query path (on by
	// default). The tool-use Converse path never enables thinking — see Converse.
	DisableThinking bool
	// HTTPTimeout bounds a single request. Defaults to defaultHTTPTimeout.
	HTTPTimeout time.Duration
}

// AnthropicProvider talks to the Messages API via the host model-proxy socket.
type AnthropicProvider struct {
	cfg    Config
	client *http.Client
	url    string
}

// NewAnthropic constructs an AnthropicProvider from cfg, applying defaults for
// any zero-valued field. The HTTP client dials only the unix socket — there is
// no route to the public internet.
func NewAnthropic(cfg Config) *AnthropicProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = defaultUpstreamHost
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	return &AnthropicProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		// Plain http over the unix socket; the proxy upgrades to https upstream.
		url: "http://" + cfg.UpstreamHost + "/v1/messages",
	}
}

// newSocketClient returns an HTTP client whose every dial goes to the host
// model-proxy unix socket regardless of the request address — the sandbox has no
// NIC. The request still addresses the real upstream host so the proxy's
// allowlist matches and routes it.
func newSocketClient(socketPath string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// ToolSpec describes a tool offered to the model.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolCall is a model request to invoke a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the outcome of executing a ToolCall, fed back to the model.
type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// Turn is one model response: text, any tool calls, the stop reason, and the
// assistant Message to append to history before sending tool results back.
type Turn struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason string
	Assistant  Message
}

// thinkingConfig selects adaptive thinking (the recommended mode for current models).
type thinkingConfig struct {
	Type string `json:"type"`
}

// Block is one content block of a Messages API message (text, tool_use, or
// tool_result). Unused fields are omitted so each block marshals to the exact
// shape the API expects.
type Block struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Message is one turn in the Messages API conversation.
type Message struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

// UserTextMessage builds a user message carrying a single text block.
func UserTextMessage(text string) Message {
	return Message{Role: "user", Content: []Block{{Type: "text", Text: text}}}
}

// ToolResultsMessage builds the user message that carries tool results back to
// the model.
func ToolResultsMessage(results []ToolResult) Message {
	blocks := make([]Block, len(results))
	for i, r := range results {
		blocks[i] = Block{Type: "tool_result", ToolUseID: r.ToolUseID, Content: r.Content, IsError: r.IsError}
	}
	return Message{Role: "user", Content: blocks}
}

// toolDef is the wire shape of a tool definition.
type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// messagesRequest is the POST /v1/messages body.
type messagesRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream,omitempty"`
	System    string          `json:"system,omitempty"`
	Thinking  *thinkingConfig `json:"thinking,omitempty"`
	Tools     []toolDef       `json:"tools,omitempty"`
	Messages  []Message       `json:"messages"`
}

// respBlock is one accumulated block of the model response.
type respBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// messagesResponse is the accumulated result of a (streamed) response.
type messagesResponse struct {
	Content    []respBlock
	StopReason string
}

// apiError is the Messages API error envelope (returned for non-200 responses).
type apiError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Query sends a single-turn prompt and returns the concatenated text blocks of
// the model's response. Thinking blocks, if any, are ignored.
func (p *AnthropicProvider) Query(ctx context.Context, prompt string) (string, error) {
	req := p.baseRequest()
	if !p.cfg.DisableThinking {
		req.Thinking = &thinkingConfig{Type: "adaptive"}
	}
	req.Messages = []Message{UserTextMessage(prompt)}

	resp, err := p.do(ctx, req)
	if err != nil {
		return "", err
	}
	return extractText(resp), nil
}

// Converse runs one model turn over the given history with the given tools and
// returns the resulting Turn (text + any tool calls + the assistant message to
// append). It does not enable thinking: a multi-turn tool loop would have to
// preserve and replay thinking blocks (with their signatures) to keep the API
// happy, which is left for a later iteration.
func (p *AnthropicProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
	req := p.baseRequest()
	req.Messages = history
	for _, ts := range tools {
		req.Tools = append(req.Tools, toolDef{Name: ts.Name, Description: ts.Description, InputSchema: ts.InputSchema})
	}

	resp, err := p.do(ctx, req)
	if err != nil {
		return Turn{}, err
	}

	turn := Turn{StopReason: resp.StopReason, Assistant: Message{Role: "assistant"}}
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			turn.Text += b.Text
			turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "text", Text: b.Text})
		case "tool_use":
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Input: b.Input})
			turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "tool_use", ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return turn, nil
}

// baseRequest builds a streaming request with the configured model, token cap,
// and system prompt; the caller fills in Messages, Thinking, and Tools.
func (p *AnthropicProvider) baseRequest() messagesRequest {
	return messagesRequest{Model: p.cfg.Model, MaxTokens: p.cfg.MaxTokens, Stream: true, System: p.cfg.System}
}

// do sends one streaming Messages API request and accumulates the SSE response.
func (p *AnthropicProvider) do(ctx context.Context, reqBody messagesRequest) (messagesResponse, error) {
	reqBody.Stream = true
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(buf))
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(req)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: model-proxy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return messagesResponse{}, parseAPIError(resp.StatusCode, body)
	}
	return accumulateSSE(resp.Body)
}

// sseEvent is the subset of a Messages API stream event we consume.
type sseEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// accumulateSSE reads a text/event-stream body and reduces it to a
// messagesResponse: text blocks have their deltas concatenated, tool_use blocks
// have their input_json_delta fragments reassembled into Input, and the stop
// reason is taken from message_delta. A stream error event aborts with an error.
func accumulateSSE(body io.Reader) (messagesResponse, error) {
	var mr messagesResponse
	blocks := map[int]*respBlock{}
	partial := map[int]*bytes.Buffer{}
	var order []int

	ensure := func(idx int) *respBlock {
		b, ok := blocks[idx]
		if !ok {
			b = &respBlock{}
			blocks[idx] = b
			partial[idx] = &bytes.Buffer{}
			order = append(order, idx)
		}
		return b
	}

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // skip event:, comments, blank separators
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return messagesResponse{}, fmt.Errorf("sandbox/provider: decode stream event: %w", err)
		}
		switch ev.Type {
		case "content_block_start":
			b := ensure(ev.Index)
			b.Type = ev.ContentBlock.Type
			b.ID = ev.ContentBlock.ID
			b.Name = ev.ContentBlock.Name
			b.Text = ev.ContentBlock.Text
		case "content_block_delta":
			b := ensure(ev.Index)
			switch ev.Delta.Type {
			case "text_delta":
				b.Text += ev.Delta.Text
			case "input_json_delta":
				partial[ev.Index].WriteString(ev.Delta.PartialJSON)
			}
		case "message_delta":
			if ev.Delta.StopReason != "" {
				mr.StopReason = ev.Delta.StopReason
			}
		case "error":
			return messagesResponse{}, fmt.Errorf("sandbox/provider: stream error (%s): %s", ev.Error.Type, ev.Error.Message)
		}
	}
	if err := sc.Err(); err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: read stream: %w", err)
	}

	for _, idx := range order {
		b := blocks[idx]
		if pj := partial[idx]; pj != nil && pj.Len() > 0 {
			b.Input = json.RawMessage(pj.Bytes())
		}
		mr.Content = append(mr.Content, *b)
	}
	return mr, nil
}

// extractText concatenates the text of all text-type content blocks.
func extractText(mr messagesResponse) string {
	var b bytes.Buffer
	for _, blk := range mr.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

// parseAPIError turns a non-200 Messages API response into an error, preferring
// the structured error envelope and falling back to the raw body.
func parseAPIError(status int, body []byte) error {
	var ae apiError
	if err := json.Unmarshal(body, &ae); err == nil && ae.Error.Message != "" {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d (%s): %s",
			status, ae.Error.Type, ae.Error.Message)
	}
	if len(body) == 0 {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d", status)
	}
	return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, string(body))
}
