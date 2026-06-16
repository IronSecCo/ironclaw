// OWNER: AGENT2

// Package provider abstracts the model backend. The first implementation,
// AnthropicProvider, speaks the Messages API (tool use + streaming). Its HTTP
// client dials the host model-proxy unix socket, NOT the public internet — the
// sandbox has network=none.
//
// The sandbox holds no model credentials: the host model-proxy authenticates and
// enforces the egress allowlist. The request carries only the anthropic-version
// header; the proxy adds x-api-key on the way out.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
// most-capable Claude model; max tokens stays under the non-streaming SDK
// timeout ceiling.
const (
	defaultModel       = "claude-opus-4-8"
	defaultMaxTokens   = 16000
	defaultHTTPTimeout = 120 * time.Second
	anthropicVersion   = "2023-06-01"
	messagesPath       = "http://ironclaw-modelproxy/v1/messages"
)

// Config configures an AnthropicProvider.
type Config struct {
	// SocketPath is the host model-proxy unix socket. Defaults to DefaultSocketPath.
	SocketPath string
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
}

// NewAnthropic constructs an AnthropicProvider from cfg, applying defaults for
// any zero-valued field. The HTTP client dials only the unix socket — there is
// no route to the public internet.
func NewAnthropic(cfg Config) *AnthropicProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
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

	socket := cfg.SocketPath
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	return &AnthropicProvider{
		cfg:    cfg,
		client: &http.Client{Transport: transport, Timeout: cfg.HTTPTimeout},
	}
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
	System    string          `json:"system,omitempty"`
	Thinking  *thinkingConfig `json:"thinking,omitempty"`
	Tools     []toolDef       `json:"tools,omitempty"`
	Messages  []Message       `json:"messages"`
}

// respBlock is one block of the Messages API response content array.
type respBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// messagesResponse is the relevant subset of the POST /v1/messages response.
type messagesResponse struct {
	Content    []respBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
}

// apiError is the Messages API error envelope.
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
// happy, which streaming/interleaved-thinking support will handle later.
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

// baseRequest builds a request with the configured model, token cap, and system
// prompt; the caller fills in Messages, Thinking, and Tools.
func (p *AnthropicProvider) baseRequest() messagesRequest {
	return messagesRequest{Model: p.cfg.Model, MaxTokens: p.cfg.MaxTokens, System: p.cfg.System}
}

// do marshals and sends one Messages API request and decodes the response.
func (p *AnthropicProvider) do(ctx context.Context, reqBody messagesRequest) (messagesResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesPath, bytes.NewReader(buf))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return messagesResponse{}, parseAPIError(resp.StatusCode, body)
	}

	var mr messagesResponse
	if err := json.Unmarshal(body, &mr); err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: decode response: %w", err)
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
