package provider

import (
	"context"
	"encoding/json"
	"strings"
)

// MockProvider is a deterministic, offline model backend used for local demos
// and end-to-end tests. It makes NO network call — not even to the host
// model-proxy socket — so the full pipeline (inbound queue → sandbox loop →
// provider → outbound queue → UI) is exercisable with zero credentials and
// without depending on any external, revocable token.
//
// It implements both Provider (Query) and ToolConverser (Converse). By default
// it echoes the prompt, which lets a test assert round-trip delivery of a unique
// marker. It also understands a tiny directive so tool use itself is testable
// without a real model: a message containing
//
//	tool:<name> {<json args>}
//
// makes it emit exactly that tool call; the loop runs the tool and feeds the
// result back, and the mock then returns the tool's output. This is how an agent
// "using an added tool" (e.g. web_search, read_file) is demonstrated end-to-end
// with no credential.
type MockProvider struct{}

// NewMock constructs a MockProvider. cfg is accepted for signature parity with
// the other constructors; every field is ignored (the mock has no network,
// model, or socket to configure).
func NewMock(_ Config) *MockProvider { return &MockProvider{} }

// mockReplyPrefix labels every mock reply so it is unmistakable in transcripts.
const mockReplyPrefix = "mock-agent received: "

// mockToolResultPrefix labels a reply that surfaces an executed tool's output.
const mockToolResultPrefix = "mock-agent tool result: "

// Query returns a deterministic reply derived from the prompt. It honors context
// cancellation so a shutting-down loop is not blocked. (The loop uses this path
// only when no tools are registered; otherwise it drives Converse.)
func (p *MockProvider) Query(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return mockReplyPrefix + strings.TrimSpace(prompt), nil
}

// Converse runs one deterministic turn. If the latest message carries a tool
// result, it surfaces that result as the final answer. Otherwise, if the latest
// user text contains a `tool:<name> {json}` directive naming an OFFERED tool, it
// emits that single tool call. Failing both, it echoes the user text.
func (p *MockProvider) Converse(ctx context.Context, history []Message, tools []ToolSpec) (Turn, error) {
	if err := ctx.Err(); err != nil {
		return Turn{}, err
	}

	// A tool result just came back — answer with it and stop.
	if n := len(history); n > 0 {
		for _, b := range history[n-1].Content {
			if b.Type == "tool_result" {
				return textTurn(mockToolResultPrefix + b.Content), nil
			}
		}
	}

	user := lastUserText(history)
	if name, args, ok := parseToolDirective(user); ok && toolOffered(tools, name) {
		const id = "mocktool_1"
		return Turn{
			ToolCalls:  []ToolCall{{ID: id, Name: name, Input: args}},
			StopReason: "tool_use",
			Assistant: Message{Role: "assistant", Content: []Block{
				{Type: "tool_use", ID: id, Name: name, Input: args},
			}},
		}, nil
	}
	return textTurn(mockReplyPrefix + strings.TrimSpace(user)), nil
}

// textTurn builds a terminal (no tool calls) Turn carrying text.
func textTurn(text string) Turn {
	return Turn{
		Text:       text,
		StopReason: "end_turn",
		Assistant:  Message{Role: "assistant", Content: []Block{{Type: "text", Text: text}}},
	}
}

// lastUserText returns the text of the most recent user message's first text
// block, or "" if there is none.
func lastUserText(history []Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		for _, b := range history[i].Content {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

// parseToolDirective extracts a `tool:<name>` directive and its optional JSON
// args object from s. name is the run of [a-zA-Z_] after "tool:"; args is the
// first {...} object that follows (defaulting to {} when absent).
func parseToolDirective(s string) (name string, args json.RawMessage, ok bool) {
	i := strings.Index(s, "tool:")
	if i < 0 {
		return "", nil, false
	}
	rest := s[i+len("tool:"):]
	j := 0
	for j < len(rest) && (rest[j] == '_' ||
		(rest[j] >= 'a' && rest[j] <= 'z') || (rest[j] >= 'A' && rest[j] <= 'Z')) {
		j++
	}
	name = rest[:j]
	if name == "" {
		return "", nil, false
	}
	args = json.RawMessage("{}")
	if b := strings.Index(rest, "{"); b >= 0 {
		if e := strings.LastIndex(rest, "}"); e > b {
			args = json.RawMessage(rest[b : e+1])
		}
	}
	return name, args, true
}

// toolOffered reports whether name is among the offered tool specs.
func toolOffered(tools []ToolSpec, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}
