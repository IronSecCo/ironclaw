// This file adds an OpenAI-compatible backend (OpenAI and OpenRouter) behind the
// same Provider/ToolConverser abstraction as AnthropicProvider. It speaks the Chat
// Completions API (POST /v1/chat/completions) and, like the Anthropic backend,
// dials only the host model-proxy unix socket — never the public internet. The
// sandbox holds no credentials: the host proxy authenticates per provider (Bearer
// token) and enforces the egress allowlist.
//
// The wire history is the Anthropic-shaped provider.Message/Block; this file
// translates it to OpenAI chat messages on the way out and the OpenAI response
// back to a provider.Turn, so the agent loop stays provider-agnostic.

package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// KindOpenAI selects the OpenAI Chat Completions backend. Its kind constant and
// default upstream host/model live here (not in provider.go) so this backend is
// self-contained; it self-registers below.
const (
	KindOpenAI         = "openai"
	openAIUpstreamHost = "api.openai.com"
	defaultOpenAIModel = "gpt-4o"
)

func init() {
	Register(KindOpenAI, func(cfg Config) (Provider, error) { return NewOpenAI(cfg), nil })
}

// OpenAIProvider talks to an OpenAI-compatible Chat Completions API via the host
// model-proxy socket. It serves both OpenAI and OpenRouter (an OpenAI-compatible
// gateway); the only difference is the upstream host, model id, and request path.
type OpenAIProvider struct {
	cfg    Config
	client *http.Client
	url    string
}

// NewOpenAI constructs an OpenAIProvider from cfg, applying defaults for any
// zero-valued field. cfg.UpstreamHost selects OpenAI vs OpenRouter and the request
// path (OpenRouter serves chat completions under /api/v1). Callers usually go
// through New, which fills the per-kind upstream host and model.
func NewOpenAI(cfg Config) *OpenAIProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = openAIUpstreamHost
	}
	if cfg.Model == "" {
		cfg.Model = defaultOpenAIModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	// OpenRouter serves the OpenAI-compatible API under /api/v1; OpenAI under /v1.
	path := "/v1/chat/completions"
	if strings.Contains(strings.ToLower(cfg.UpstreamHost), "openrouter.ai") {
		path = "/api/v1/chat/completions"
	}
	return &OpenAIProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		url:    "http://" + cfg.UpstreamHost + path,
	}
}

// --- wire types ---

type oaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON-encoded string, not an object
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiChatMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaiChatRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens,omitempty"`
	Stream    bool             `json:"stream"`
	Messages  []oaiChatMessage `json:"messages"`
	Tools     []oaiTool        `json:"tools,omitempty"`
}

// oaiStreamChunk is the subset of a streamed chat.completion.chunk we consume.
type oaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// oaiErr is the Chat Completions error envelope for non-200 responses.
type oaiErr struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Query sends a single-turn prompt and returns the assistant's text.
func (p *OpenAIProvider) Query(ctx context.Context, prompt string) (string, error) {
	msgs := p.systemPrefix()
	msgs = append(msgs, oaiChatMessage{Role: "user", Content: prompt})
	resp, err := p.do(ctx, oaiChatRequest{Model: p.cfg.Model, MaxTokens: p.cfg.MaxTokens, Stream: true, Messages: msgs})
	if err != nil {
		return "", err
	}
	return resp.text, nil
}

// Converse runs one model turn over the Anthropic-shaped history with the given
// tools and returns the resulting Turn (text + any tool calls + the assistant
// Message to append to history). The Message it returns is Anthropic-shaped so the
// loop and a later replay round-trip identically.
func (p *OpenAIProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
	req := oaiChatRequest{Model: p.cfg.Model, MaxTokens: p.cfg.MaxTokens, Stream: true}
	req.Messages = append(p.systemPrefix(), toOpenAIMessages(history)...)
	for _, ts := range tools {
		var t oaiTool
		t.Type = "function"
		t.Function.Name = ts.Name
		t.Function.Description = ts.Description
		t.Function.Parameters = ts.InputSchema
		req.Tools = append(req.Tools, t)
	}

	resp, err := p.do(ctx, req)
	if err != nil {
		return Turn{}, err
	}

	turn := Turn{Text: resp.text, StopReason: normalizeStop(resp.finishReason), Assistant: Message{Role: "assistant"}}
	if resp.text != "" {
		turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "text", Text: resp.text})
	}
	for _, tc := range resp.toolCalls {
		input := json.RawMessage(tc.args)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: tc.id, Name: tc.name, Input: input})
		turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "tool_use", ID: tc.id, Name: tc.name, Input: input})
	}
	return turn, nil
}

// systemPrefix returns the leading system message, or nil when no system prompt is
// configured.
func (p *OpenAIProvider) systemPrefix() []oaiChatMessage {
	if p.cfg.System == "" {
		return nil
	}
	return []oaiChatMessage{{Role: "system", Content: p.cfg.System}}
}

// toOpenAIMessages translates Anthropic-shaped history into OpenAI chat messages.
// An assistant message becomes one chat message carrying its text and tool_calls;
// a user message carrying tool_result blocks becomes one "tool" message per result
// (OpenAI keys tool output by tool_call_id), and its text blocks become a "user"
// message.
func toOpenAIMessages(history []Message) []oaiChatMessage {
	var out []oaiChatMessage
	for _, m := range history {
		switch m.Role {
		case "assistant":
			cm := oaiChatMessage{Role: "assistant"}
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					cm.Content += b.Text
				case "tool_use":
					args := string(b.Input)
					if args == "" {
						args = "{}"
					}
					cm.ToolCalls = append(cm.ToolCalls, oaiToolCall{
						ID: b.ID, Type: "function",
						Function: oaiFunctionCall{Name: b.Name, Arguments: args},
					})
				}
			}
			out = append(out, cm)
		default: // "user" (and any other) — text and/or tool_result blocks
			var text string
			for _, b := range m.Content {
				switch b.Type {
				case "tool_result":
					out = append(out, oaiChatMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: b.Content})
				case "text":
					text += b.Text
				}
			}
			if text != "" {
				out = append(out, oaiChatMessage{Role: "user", Content: text})
			}
		}
	}
	return out
}

// normalizeStop maps an OpenAI finish_reason to the Anthropic-style stop reason the
// rest of the codebase expects. The loop keys only on tool-call presence, so this
// is informational, but keeping it consistent avoids surprises for audit/logging.
func normalizeStop(reason string) string {
	switch reason {
	case "tool_calls":
		return "tool_use"
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

// oaiResult is the accumulated outcome of a streamed chat completion.
type oaiResult struct {
	text         string
	toolCalls    []oaiAccumCall
	finishReason string
}

type oaiAccumCall struct {
	id   string
	name string
	args string
}

// do sends one streaming Chat Completions request and accumulates the SSE stream.
func (p *OpenAIProvider) do(ctx context.Context, reqBody oaiChatRequest) (oaiResult, error) {
	reqBody.Stream = true
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return oaiResult{}, fmt.Errorf("sandbox/provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(buf))
	if err != nil {
		return oaiResult{}, fmt.Errorf("sandbox/provider: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return oaiResult{}, fmt.Errorf("sandbox/provider: model-proxy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return oaiResult{}, parseOpenAIError(resp.StatusCode, body)
	}
	return accumulateChatSSE(resp.Body)
}

// accumulateChatSSE reduces a Chat Completions text/event-stream to an oaiResult:
// content deltas are concatenated, tool_call deltas are reassembled by index (the
// id/name arrive in the first fragment, the arguments stream in across the rest),
// and the finish_reason is captured. A stream error event aborts with an error.
// The stream terminates with a `data: [DONE]` sentinel.
func accumulateChatSSE(body io.Reader) (oaiResult, error) {
	var res oaiResult
	calls := map[int]*oaiAccumCall{}
	var order []int

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return oaiResult{}, fmt.Errorf("sandbox/provider: decode stream event: %w", err)
		}
		if chunk.Error != nil {
			return oaiResult{}, fmt.Errorf("sandbox/provider: stream error (%s): %s", chunk.Error.Type, chunk.Error.Message)
		}
		for _, ch := range chunk.Choices {
			res.text += ch.Delta.Content
			for _, tc := range ch.Delta.ToolCalls {
				acc, ok := calls[tc.Index]
				if !ok {
					acc = &oaiAccumCall{}
					calls[tc.Index] = acc
					order = append(order, tc.Index)
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args += tc.Function.Arguments
			}
			if ch.FinishReason != "" {
				res.finishReason = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return oaiResult{}, fmt.Errorf("sandbox/provider: read stream: %w", err)
	}
	for _, idx := range order {
		res.toolCalls = append(res.toolCalls, *calls[idx])
	}
	return res, nil
}

// parseOpenAIError turns a non-200 Chat Completions response into an error,
// preferring the structured error envelope and falling back to the raw body.
func parseOpenAIError(status int, body []byte) error {
	var e oaiErr
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d (%s): %s", status, e.Error.Type, e.Error.Message)
	}
	if len(body) == 0 {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d", status)
	}
	return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, string(body))
}
