// This file adds a Google Gemini backend behind the same Provider/ToolConverser
// abstraction as AnthropicProvider and OpenAIProvider. It speaks the Google
// Generative Language API (POST /v1beta/models/{model}:streamGenerateContent) —
// the wire format served by Google AI Studio and, through the credential gateway,
// by the Gemini CLI's OAuth credentials. Like the other backends it dials only the
// host model-proxy unix socket, never the public internet; the sandbox holds no
// credential. The host proxy injects the host-held API key (x-goog-api-key) and
// enforces the egress allowlist.
//
// The wire history is the Anthropic-shaped provider.Message/Block; this file
// translates it to Gemini "contents" on the way out and the Gemini response back
// to a provider.Turn, so the agent loop stays provider-agnostic. Two shape
// differences from Anthropic are bridged here: Gemini names the assistant role
// "model" (not "assistant"), and it keys a function result by the function *name*
// (not a call id), so the translator threads each tool_use id → name as it walks
// the history.

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

const (
	geminiUpstreamHost = "generativelanguage.googleapis.com"
	defaultGeminiModel = "gemini-2.5-pro"
)

// GeminiProvider talks to the Google Generative Language API via the host
// model-proxy socket. The same implementation serves Google AI Studio (direct API
// key) and the Gemini CLI (host credential gateway injecting an OAuth token) — only
// the host-side credential differs; the wire format is identical.
type GeminiProvider struct {
	cfg    Config
	client *http.Client
	url    string
}

// NewGemini constructs a GeminiProvider from cfg, applying defaults for any
// zero-valued field. The model id rides in the request path (not the body), so the
// URL is fixed at construction from cfg.Model. Callers usually go through New.
func NewGemini(cfg Config) *GeminiProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = geminiUpstreamHost
	}
	if cfg.Model == "" {
		cfg.Model = defaultGeminiModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	// Gemini puts the model id in the path and streams via Server-Sent Events when
	// asked with alt=sse (the default streamGenerateContent framing is a JSON array).
	url := "http://" + cfg.UpstreamHost + "/v1beta/models/" + cfg.Model + ":streamGenerateContent?alt=sse"
	return &GeminiProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		url:    url,
	}
}

// --- wire types ---

type gemFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type gemFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type gemPart struct {
	Text             string               `json:"text,omitempty"`
	FunctionCall     *gemFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *gemFunctionResponse `json:"functionResponse,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

type gemFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type gemTool struct {
	FunctionDeclarations []gemFunctionDecl `json:"functionDeclarations"`
}

type gemGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type gemRequest struct {
	Contents          []gemContent         `json:"contents"`
	SystemInstruction *gemContent          `json:"systemInstruction,omitempty"`
	Tools             []gemTool            `json:"tools,omitempty"`
	GenerationConfig  *gemGenerationConfig `json:"generationConfig,omitempty"`
}

// gemStreamChunk is the subset of a streamed GenerateContentResponse we consume.
// Each SSE data line is one complete chunk; unlike OpenAI, a functionCall arrives
// whole (its args are not fragmented across deltas).
type gemStreamChunk struct {
	Candidates []struct {
		Content      gemContent `json:"content"`
		FinishReason string     `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// gemErr is the Generative Language API error envelope for non-200 responses.
type gemErr struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Query sends a single-turn prompt and returns the assistant's text.
func (p *GeminiProvider) Query(ctx context.Context, prompt string) (string, error) {
	req := p.baseRequest()
	req.Contents = []gemContent{{Role: "user", Parts: []gemPart{{Text: prompt}}}}
	res, err := p.do(ctx, req)
	if err != nil {
		return "", err
	}
	return res.text, nil
}

// Converse runs one model turn over the Anthropic-shaped history with the given
// tools and returns the resulting Turn (text + any tool calls + the assistant
// Message to append to history). The Message it returns is Anthropic-shaped so the
// loop and a later replay round-trip identically.
func (p *GeminiProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
	req := p.baseRequest()
	req.Contents = toGeminiContents(history)
	if len(tools) > 0 {
		decls := make([]gemFunctionDecl, 0, len(tools))
		for _, ts := range tools {
			decls = append(decls, gemFunctionDecl{Name: ts.Name, Description: ts.Description, Parameters: ts.InputSchema})
		}
		req.Tools = []gemTool{{FunctionDeclarations: decls}}
	}

	res, err := p.do(ctx, req)
	if err != nil {
		return Turn{}, err
	}

	turn := Turn{Text: res.text, StopReason: normalizeGeminiStop(res.finishReason), Assistant: Message{Role: "assistant"}}
	if res.text != "" {
		turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "text", Text: res.text})
	}
	// Gemini does not assign ids to function calls; synthesize a stable per-turn id.
	// toGeminiContents recovers the function name from the assistant block on replay,
	// so the id only has to round-trip the tool_use/tool_result pairing in the loop.
	for i, fc := range res.functionCalls {
		input := fc.args
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		id := fmt.Sprintf("call_%d", i)
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: id, Name: fc.name, Input: input})
		turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "tool_use", ID: id, Name: fc.name, Input: input})
	}
	// Gemini reports finishReason "STOP" even when it emits function calls; the loop
	// keys on tool-call presence, so reflect that in the (informational) stop reason.
	if len(turn.ToolCalls) > 0 {
		turn.StopReason = "tool_use"
	}
	return turn, nil
}

// baseRequest builds a request carrying the system instruction and token cap; the
// caller fills in Contents and Tools.
func (p *GeminiProvider) baseRequest() gemRequest {
	var req gemRequest
	if p.cfg.System != "" {
		req.SystemInstruction = &gemContent{Parts: []gemPart{{Text: p.cfg.System}}}
	}
	if p.cfg.MaxTokens > 0 {
		req.GenerationConfig = &gemGenerationConfig{MaxOutputTokens: p.cfg.MaxTokens}
	}
	return req
}

// toGeminiContents translates Anthropic-shaped history into Gemini contents. An
// assistant message becomes a role:"model" content whose tool_use blocks become
// functionCall parts; a user message's tool_result blocks become functionResponse
// parts keyed by the function *name*. Because Gemini omits call ids, the name is
// recovered from the tool_use id seen earlier in the same history — a single
// in-order pass keeps this correct even if synthesized ids repeat across turns,
// since a tool_result always follows its tool_use.
func toGeminiContents(history []Message) []gemContent {
	var out []gemContent
	names := map[string]string{} // tool_use id -> function name
	for _, m := range history {
		switch m.Role {
		case "assistant":
			c := gemContent{Role: "model"}
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					if b.Text != "" {
						c.Parts = append(c.Parts, gemPart{Text: b.Text})
					}
				case "tool_use":
					names[b.ID] = b.Name
					args := b.Input
					if len(args) == 0 {
						args = json.RawMessage("{}")
					}
					c.Parts = append(c.Parts, gemPart{FunctionCall: &gemFunctionCall{Name: b.Name, Args: args}})
				}
			}
			if len(c.Parts) > 0 {
				out = append(out, c)
			}
		default: // "user" (and any other) — text and/or tool_result blocks
			c := gemContent{Role: "user"}
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					if b.Text != "" {
						c.Parts = append(c.Parts, gemPart{Text: b.Text})
					}
				case "tool_result":
					c.Parts = append(c.Parts, gemPart{FunctionResponse: &gemFunctionResponse{
						Name:     names[b.ToolUseID],
						Response: geminiResultResponse(b.Content, b.IsError),
					}})
				}
			}
			if len(c.Parts) > 0 {
				out = append(out, c)
			}
		}
	}
	return out
}

// geminiResultResponse wraps a tool result string in the JSON object Gemini's
// functionResponse.response field requires (a google.protobuf.Struct), keyed
// "output" for success and "error" for a failed tool call.
func geminiResultResponse(content string, isError bool) json.RawMessage {
	key := "output"
	if isError {
		key = "error"
	}
	b, err := json.Marshal(map[string]string{key: content})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

// normalizeGeminiStop maps a Gemini finishReason to the Anthropic-style stop reason
// the rest of the codebase expects. The loop keys only on tool-call presence, so
// this is informational, but keeping it consistent avoids surprises for audit/logging.
func normalizeGeminiStop(reason string) string {
	switch reason {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "":
		return ""
	default:
		return strings.ToLower(reason)
	}
}

// gemResult is the accumulated outcome of a streamed generateContent response.
type gemResult struct {
	text          string
	functionCalls []gemAccumCall
	finishReason  string
}

type gemAccumCall struct {
	name string
	args json.RawMessage
}

// do sends one streaming generateContent request and accumulates the SSE stream.
func (p *GeminiProvider) do(ctx context.Context, reqBody gemRequest) (gemResult, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return gemResult{}, fmt.Errorf("sandbox/provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(buf))
	if err != nil {
		return gemResult{}, fmt.Errorf("sandbox/provider: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return gemResult{}, fmt.Errorf("sandbox/provider: model-proxy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return gemResult{}, parseGeminiError(resp.StatusCode, body)
	}
	return accumulateGeminiSSE(resp.Body)
}

// accumulateGeminiSSE reduces a generateContent text/event-stream to a gemResult:
// text parts are concatenated, functionCall parts are collected whole (Gemini does
// not fragment a call's args), and the finishReason is captured. A stream error
// object aborts with an error.
func accumulateGeminiSSE(body io.Reader) (gemResult, error) {
	var res gemResult
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" {
			continue
		}
		var chunk gemStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return gemResult{}, fmt.Errorf("sandbox/provider: decode stream event: %w", err)
		}
		if chunk.Error != nil {
			return gemResult{}, fmt.Errorf("sandbox/provider: stream error (%s): %s", chunk.Error.Status, chunk.Error.Message)
		}
		for _, cand := range chunk.Candidates {
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					res.text += part.Text
				}
				if part.FunctionCall != nil {
					res.functionCalls = append(res.functionCalls, gemAccumCall{name: part.FunctionCall.Name, args: part.FunctionCall.Args})
				}
			}
			if cand.FinishReason != "" {
				res.finishReason = cand.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return gemResult{}, fmt.Errorf("sandbox/provider: read stream: %w", err)
	}
	return res, nil
}

// parseGeminiError turns a non-200 generateContent response into an error,
// preferring the structured error envelope and falling back to the raw body.
func parseGeminiError(status int, body []byte) error {
	var e gemErr
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d (%s): %s", status, e.Error.Status, e.Error.Message)
	}
	if len(body) == 0 {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d", status)
	}
	return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, string(body))
}
