// CodexProvider speaks the ChatGPT "Codex" backend Responses API
// (https://chatgpt.com/backend-api/codex/responses) so an agent can be powered
// by a ChatGPT/Codex OAuth credential instead of a raw provider API key. Like the
// other providers it never holds a credential and never reaches the network
// directly: its HTTP client dials the host model-proxy unix socket, addresses the
// request to chatgpt.com, and the host proxy routes it — here, through an
// operator-vetted credential gateway (e.g. OneCLI) that injects the real Codex
// token. The request carries only the non-secret Codex client headers
// (OpenAI-Beta / originator / session_id); the bearer is added host-side.
//
// The Responses API is a different wire shape from the Messages / chat-completions
// APIs: a flat `input` array of typed items and a server-sent-event stream of
// `response.*` events whose `response.output_text.delta` events carry the text.
package provider

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// KindCodex routes to the ChatGPT Codex Responses API (chatgpt.com), powered by a
// ChatGPT/Codex OAuth credential injected host-side (e.g. via OneCLI). NewCodex
// applies this backend's default host and model, so the registered factory just
// delegates.
const KindCodex = "codex"

func init() {
	Register(KindCodex, func(cfg Config) (Provider, error) { return NewCodex(cfg), nil })
}

// Codex backend constants. The model defaults to the current ChatGPT-account
// Codex model; the upstream host is what the model-proxy allowlists and the
// credential gateway matches to inject the Codex token.
const (
	codexUpstreamHost  = "chatgpt.com"
	codexResponsesPath = "/backend-api/codex/responses"
	defaultCodexModel  = "gpt-5.5"

	// Non-secret Codex client headers. These identify the request as Codex CLI
	// traffic so the backend accepts the Responses call; the bearer credential is
	// NOT here — the host credential gateway injects it.
	codexOriginator = "codex_cli_rs"
	codexOpenAIBeta = "responses=experimental"
)

// CodexProvider talks to the ChatGPT Codex Responses API via the host model-proxy
// socket. It implements both Provider (single-turn Query) and ToolConverser
// (Converse), so an agent on the Codex backend can use tools — including the
// mandatory request_capability_change / ask_user_question — exactly like the
// Anthropic and OpenAI backends. The Responses API expresses tools as flat
// function items and streams function_call items, which Converse translates to and
// from the Anthropic-shaped provider.Message/Block the loop uses.
type CodexProvider struct {
	cfg    Config
	client *http.Client
	url    string
}

// NewCodex constructs a CodexProvider from cfg, applying defaults for any
// zero-valued field. The HTTP client dials only the model-proxy unix socket.
func NewCodex(cfg Config) *CodexProvider {
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.UpstreamHost == "" {
		cfg.UpstreamHost = codexUpstreamHost
	}
	if cfg.Model == "" {
		cfg.Model = defaultCodexModel
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}
	return &CodexProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		// Plain http over the unix socket; the host proxy upgrades to https and
		// routes through the credential gateway.
		url: "http://" + cfg.UpstreamHost + codexResponsesPath,
	}
}

// codexInputText is one typed content part of a Responses input message.
type codexInputText struct {
	Type string `json:"type"` // "input_text"
	Text string `json:"text"`
}

// codexInputMessage is one "message" item of the Responses `input` array. The
// content part type is "input_text" for user/system text and "output_text" for a
// replayed assistant message.
type codexInputMessage struct {
	Type    string           `json:"type"` // "message"
	Role    string           `json:"role"`
	Content []codexInputText `json:"content"`
}

// codexFunctionCall is an `input` item replaying a prior assistant tool call.
type codexFunctionCall struct {
	Type      string `json:"type"` // "function_call"
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded args string
}

// codexFunctionCallOutput is an `input` item carrying a tool result back.
type codexFunctionCallOutput struct {
	Type   string `json:"type"` // "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// codexTool is a function tool offered to the model (flat Responses shape).
type codexTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// codexRequest is the POST /backend-api/codex/responses body (the subset we send).
// Input is heterogeneous (message / function_call / function_call_output items),
// so it is typed as []any and each element marshals to its own item shape.
type codexRequest struct {
	Model        string      `json:"model"`
	Instructions string      `json:"instructions,omitempty"`
	Input        []any       `json:"input"`
	Tools        []codexTool `json:"tools,omitempty"`
	ToolChoice   string      `json:"tool_choice,omitempty"`
	Stream       bool        `json:"stream"`
	Store        bool        `json:"store"`
}

// Query sends a single-turn prompt and returns the model's concatenated output
// text. The system prompt is sent as the Responses `instructions` field.
func (p *CodexProvider) Query(ctx context.Context, prompt string) (string, error) {
	reqBody := codexRequest{
		Model:        p.cfg.Model,
		Instructions: p.cfg.System,
		Input: []any{codexInputMessage{
			Type:    "message",
			Role:    "user",
			Content: []codexInputText{{Type: "input_text", Text: prompt}},
		}},
		Stream: true,
		Store:  false,
	}
	resp, err := p.post(ctx, reqBody)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return accumulateCodexSSE(resp.Body)
}

// Converse runs one model turn over the Anthropic-shaped history with the given
// tools and returns the resulting Turn (text + any tool calls + the assistant
// Message to append). History and the returned Message are Anthropic-shaped so the
// loop and a later replay round-trip identically; this method translates to/from
// the Responses `input`/function-call wire shape.
func (p *CodexProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
	reqBody := codexRequest{
		Model:        p.cfg.Model,
		Instructions: p.cfg.System,
		Input:        toCodexInput(history),
		Stream:       true,
		Store:        false,
	}
	for _, ts := range tools {
		params := ts.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		reqBody.Tools = append(reqBody.Tools, codexTool{
			Type: "function", Name: ts.Name, Description: ts.Description, Parameters: params,
		})
	}
	if len(reqBody.Tools) > 0 {
		reqBody.ToolChoice = "auto"
	}

	resp, err := p.post(ctx, reqBody)
	if err != nil {
		return Turn{}, err
	}
	defer resp.Body.Close()
	res, err := accumulateCodexConverse(resp.Body)
	if err != nil {
		return Turn{}, err
	}

	turn := Turn{Text: res.text, StopReason: "end_turn", Assistant: Message{Role: "assistant"}}
	if res.text != "" {
		turn.Assistant.Content = append(turn.Assistant.Content, Block{Type: "text", Text: res.text})
	}
	for _, c := range res.toolCalls {
		input := json.RawMessage(c.args)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{ID: c.callID, Name: c.name, Input: input})
		turn.Assistant.Content = append(turn.Assistant.Content,
			Block{Type: "tool_use", ID: c.callID, Name: c.name, Input: input})
	}
	if len(turn.ToolCalls) > 0 {
		turn.StopReason = "tool_use"
	}
	return turn, nil
}

// post marshals reqBody, sends the streaming Responses request with the Codex
// client headers, and returns the live response (caller closes Body). A non-200 is
// turned into a parsed error.
func (p *CodexProvider) post(ctx context.Context, reqBody codexRequest) (*http.Response, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("sandbox/provider: marshal codex request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("sandbox/provider: build codex request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("OpenAI-Beta", codexOpenAIBeta)
	req.Header.Set("originator", codexOriginator)
	req.Header.Set("session_id", newSessionID())

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sandbox/provider: model-proxy request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseCodexError(resp.StatusCode, body)
	}
	return resp, nil
}

// toCodexInput translates Anthropic-shaped history into Responses `input` items:
// an assistant message becomes a "message" item (output_text) plus one
// "function_call" item per tool_use; a user message becomes a "message" item
// (input_text) plus one "function_call_output" item per tool_result.
func toCodexInput(history []Message) []any {
	var out []any
	for _, m := range history {
		if m.Role == "assistant" {
			var texts []codexInputText
			var calls []any
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					if b.Text != "" {
						texts = append(texts, codexInputText{Type: "output_text", Text: b.Text})
					}
				case "tool_use":
					args := string(b.Input)
					if args == "" {
						args = "{}"
					}
					calls = append(calls, codexFunctionCall{
						Type: "function_call", CallID: b.ID, Name: b.Name, Arguments: args,
					})
				}
			}
			if len(texts) > 0 {
				out = append(out, codexInputMessage{Type: "message", Role: "assistant", Content: texts})
			}
			out = append(out, calls...)
			continue
		}
		// user (and any other) — text and/or tool_result blocks
		var texts []codexInputText
		for _, b := range m.Content {
			switch b.Type {
			case "text":
				if b.Text != "" {
					texts = append(texts, codexInputText{Type: "input_text", Text: b.Text})
				}
			case "tool_result":
				out = append(out, codexFunctionCallOutput{
					Type: "function_call_output", CallID: b.ToolUseID, Output: b.Content,
				})
			}
		}
		if len(texts) > 0 {
			out = append(out, codexInputMessage{Type: "message", Role: "user", Content: texts})
		}
	}
	return out
}

// codexEvent is the subset of a Responses stream event we consume.
type codexEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Response *struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

// accumulateCodexSSE reads a text/event-stream body and concatenates the
// `response.output_text.delta` event deltas into the final text. A stream error
// event (or a failed/incomplete terminal response) aborts with an error.
func accumulateCodexSSE(body io.Reader) (string, error) {
	var sb strings.Builder
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // skip event:, comments, blank separators
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev codexEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue // tolerate unknown event shapes
		}
		switch ev.Type {
		case "response.output_text.delta":
			sb.WriteString(ev.Delta)
		case "error", "response.failed", "response.incomplete":
			msg := "codex stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			} else if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "" {
				msg = ev.Response.Error.Message
			}
			return "", fmt.Errorf("sandbox/provider: codex stream: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("sandbox/provider: read codex stream: %w", err)
	}
	return sb.String(), nil
}

// codexResult is the accumulated outcome of a streamed Converse response.
type codexResult struct {
	text      string
	toolCalls []codexAccumCall
}

type codexAccumCall struct {
	callID string
	name   string
	args   string
}

// codexConverseEvent is the subset of a Responses stream event Converse consumes:
// text deltas, function-call item lifecycle, argument deltas, and terminal errors.
type codexConverseEvent struct {
	Type   string `json:"type"`
	Delta  string `json:"delta"`
	ItemID string `json:"item_id"`
	Item   *struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Response *struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

// accumulateCodexConverse reduces a Responses text/event-stream to a codexResult:
// `response.output_text.delta` events concatenate into the text; function calls are
// assembled from `response.output_item.added` (name + call_id),
// `response.function_call_arguments.delta` (streamed args), and finalized by
// `response.output_item.done` (which carries the complete item). A stream error
// (or a failed/incomplete terminal response) aborts with an error.
func accumulateCodexConverse(body io.Reader) (codexResult, error) {
	var res codexResult
	calls := map[string]*codexAccumCall{} // by streaming item id
	args := map[string]*strings.Builder{}
	var order []string
	ensure := func(id string) *codexAccumCall {
		c, ok := calls[id]
		if !ok {
			c = &codexAccumCall{}
			calls[id] = c
			args[id] = &strings.Builder{}
			order = append(order, id)
		}
		return c
	}

	var sb strings.Builder
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
		var ev codexConverseEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue // tolerate unknown event shapes
		}
		switch ev.Type {
		case "response.output_text.delta":
			sb.WriteString(ev.Delta)
		case "response.output_item.added":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				c := ensure(ev.Item.ID)
				c.name = ev.Item.Name
				c.callID = ev.Item.CallID
			}
		case "response.function_call_arguments.delta":
			if ev.ItemID != "" {
				if _, ok := calls[ev.ItemID]; ok {
					args[ev.ItemID].WriteString(ev.Delta)
				}
			}
		case "response.output_item.done":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				c := ensure(ev.Item.ID)
				if ev.Item.Name != "" {
					c.name = ev.Item.Name
				}
				if ev.Item.CallID != "" {
					c.callID = ev.Item.CallID
				}
				if ev.Item.Arguments != "" { // the done item carries the full args
					args[ev.Item.ID].Reset()
					args[ev.Item.ID].WriteString(ev.Item.Arguments)
				}
			}
		case "error", "response.failed", "response.incomplete":
			msg := "codex stream error"
			if ev.Error != nil && ev.Error.Message != "" {
				msg = ev.Error.Message
			} else if ev.Response != nil && ev.Response.Error != nil && ev.Response.Error.Message != "" {
				msg = ev.Response.Error.Message
			}
			return codexResult{}, fmt.Errorf("sandbox/provider: codex stream: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		return codexResult{}, fmt.Errorf("sandbox/provider: read codex stream: %w", err)
	}

	res.text = sb.String()
	for _, id := range order {
		c := calls[id]
		c.args = args[id].String()
		if c.name != "" { // skip any stray item that never received a name
			res.toolCalls = append(res.toolCalls, *c)
		}
	}
	return res, nil
}

// parseCodexError renders a non-200 Responses error body. The Codex backend
// returns {"detail":"..."}; the standard OpenAI shape {"error":{"message":...}}
// is also handled. Falls back to the raw body.
func parseCodexError(status int, body []byte) error {
	var detail struct {
		Detail string `json:"detail"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &detail) == nil {
		if detail.Detail != "" {
			return fmt.Errorf("sandbox/provider: codex API status %d: %s", status, detail.Detail)
		}
		if detail.Error.Message != "" {
			return fmt.Errorf("sandbox/provider: codex API status %d: %s", status, detail.Error.Message)
		}
	}
	return fmt.Errorf("sandbox/provider: codex API status %d: %s", status, strings.TrimSpace(string(body)))
}

// newSessionID returns a random v4-style UUID for the Codex session_id header.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Non-fatal: the backend only needs a unique-ish opaque id.
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
