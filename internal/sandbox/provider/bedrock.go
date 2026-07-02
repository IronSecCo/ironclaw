// This file adds AWS Bedrock as a first-class provider so orgs that can only reach
// models through Bedrock (no direct Anthropic/OpenAI keys) can run IronClaw. The
// primary target is Claude-on-Bedrock, which speaks the Anthropic Messages wire
// format — the SAME "messages"/tools out, content/stop_reason back shape the
// Anthropic backend uses — so this file reuses the shared Block/Message/toolDef/
// respBlock types and only re-envelopes the request for Bedrock's InvokeModel API:
//
//   - URL: the model id rides in the path — /model/{modelId}/invoke — served from
//     the regional bedrock-runtime.{region}.amazonaws.com host. The body therefore
//     carries NO "model" field (Bedrock takes it from the path) and instead carries
//     anthropic_version:"bedrock-2023-05-31", the Bedrock-required schema marker.
//   - Transport: non-streaming InvokeModel returns a single JSON body (the Anthropic
//     Messages response), so unlike the Anthropic SSE path there is no event stream
//     to accumulate — we decode the response directly. (Streaming Bedrock uses the
//     binary vnd.amazon.eventstream framing, deliberately not taken on here.)
//   - Auth: AWS SigV4, signed HOST-SIDE by modelproxy.BedrockInjector using a
//     host-held access key. As with every backend the sandbox holds no credential
//     and dials only the host model-proxy unix socket; the proxy signs the request
//     and enforces the bedrock-runtime.{region}.amazonaws.com egress allowlist.

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	// defaultBedrockModel is applied when cfg.Model is empty. It targets Claude on
	// Bedrock; operators pass their region's exact model id or cross-region
	// inference-profile id (e.g. us.anthropic.claude-3-5-sonnet-20241022-v2:0) via
	// --model when it differs.
	defaultBedrockModel = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	// bedrockAnthropicVersion is the schema marker Bedrock requires in the request
	// body for Anthropic (Claude) models, in place of the anthropic-version header.
	bedrockAnthropicVersion = "bedrock-2023-05-31"
)

// BedrockProvider talks to the AWS Bedrock Runtime InvokeModel API via the host
// model-proxy socket. It reuses the Anthropic Messages wire format; only the
// request envelope (URL + body markers) and the non-streaming response differ.
type BedrockProvider struct {
	cfg    Config
	client *http.Client
	url    string
}

// NewBedrock constructs a Bedrock backend. Unlike the cloud providers with a single
// global host, Bedrock is regional and the SigV4 signature is bound to that exact
// host, so there is no safe default upstream host: an unset host would sign one
// region while the host-held credential belongs to another, yielding a confusing
// upstream 403. NewBedrock therefore requires cfg.UpstreamHost (the control-plane
// backfills it from the deployment region; see selectModelFromRegistry). The AWS
// credential is added host-side by modelproxy.BedrockInjector — this provider never
// holds it. Callers usually go through New.
func NewBedrock(cfg Config) (*BedrockProvider, error) {
	if cfg.UpstreamHost == "" {
		return nil, fmt.Errorf("sandbox/provider: bedrock provider requires an upstream host (set --model-host, e.g. bedrock-runtime.us-east-1.amazonaws.com)")
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = DefaultSocketPath
	}
	if cfg.Model == "" {
		cfg.Model = defaultBedrockModel
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = defaultHTTPTimeout
	}

	return &BedrockProvider{
		cfg:    cfg,
		client: newSocketClient(cfg.SocketPath, cfg.HTTPTimeout),
		url:    bedrockURL(cfg.UpstreamHost, cfg.Model),
	}, nil
}

// bedrockURL builds the InvokeModel endpoint. The model id rides in the path and
// may contain characters like ':' (e.g. ...-v2:0) — they are left literal here; the
// host SigV4 signer canonicalizes the path the same way AWS does, so the signature
// matches. The scheme is plain http to the unix socket — the host proxy upgrades to
// https upstream.
func bedrockURL(host, model string) string {
	return "http://" + host + "/model/" + model + "/invoke"
}

// bedrockRequest is the InvokeModel body for an Anthropic (Claude) model. It is the
// Messages API body WITHOUT the model field (Bedrock takes the model from the URL)
// and WITH anthropic_version set to the Bedrock schema marker.
type bedrockRequest struct {
	AnthropicVersion string          `json:"anthropic_version"`
	MaxTokens        int             `json:"max_tokens"`
	System           string          `json:"system,omitempty"`
	Thinking         *thinkingConfig `json:"thinking,omitempty"`
	Tools            []toolDef       `json:"tools,omitempty"`
	Messages         []Message       `json:"messages"`
}

// bedrockResponse is the non-streaming InvokeModel response body — the standard
// Anthropic Messages response (content blocks + stop reason).
type bedrockResponse struct {
	Content    []respBlock `json:"content"`
	StopReason string      `json:"stop_reason"`
}

// baseRequest builds a request with the configured token cap and system prompt and
// the Bedrock schema marker; the caller fills in Messages, Thinking, and Tools.
func (p *BedrockProvider) baseRequest() bedrockRequest {
	return bedrockRequest{AnthropicVersion: bedrockAnthropicVersion, MaxTokens: p.cfg.MaxTokens, System: p.cfg.System}
}

// Query sends a single-turn prompt and returns the concatenated text blocks of the
// model's response. Thinking blocks, if any, are ignored.
func (p *BedrockProvider) Query(ctx context.Context, prompt string) (string, error) {
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
// append). Like the Anthropic backend it does not enable thinking on the tool path.
func (p *BedrockProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
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

// do sends one non-streaming InvokeModel request and decodes the JSON response. It
// sets only content-type — the SigV4 auth headers are added host-side by the
// model-proxy injector, never in the sandbox.
func (p *BedrockProvider) do(ctx context.Context, reqBody bedrockRequest) (messagesResponse, error) {
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(buf))
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: model-proxy request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return messagesResponse{}, parseBedrockError(resp.StatusCode, body)
	}

	var br bedrockResponse
	if err := json.Unmarshal(body, &br); err != nil {
		return messagesResponse{}, fmt.Errorf("sandbox/provider: decode bedrock response: %w", err)
	}
	return messagesResponse{Content: br.Content, StopReason: br.StopReason}, nil
}

// bedrockErrorBody is the Bedrock error envelope. Bedrock returns a top-level
// "message" (or "Message") for InvokeModel errors rather than the Anthropic
// {"error":{...}} envelope.
type bedrockErrorBody struct {
	Message  string `json:"message"`
	MessageU string `json:"Message"`
}

// parseBedrockError turns a non-200 InvokeModel response into an error, preferring
// the Bedrock message field and falling back to the raw body.
func parseBedrockError(status int, body []byte) error {
	var be bedrockErrorBody
	if err := json.Unmarshal(body, &be); err == nil {
		if msg := be.Message; msg != "" {
			return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, msg)
		}
		if msg := be.MessageU; msg != "" {
			return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, msg)
		}
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return fmt.Errorf("sandbox/provider: model-proxy returned %d: %s", status, s)
	}
	return fmt.Errorf("sandbox/provider: model-proxy returned %d", status)
}
