package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// The sandbox NEVER speaks MCP and NEVER reaches an MCP server. Its only MCP-related
// endpoint is the per-session host broker socket bound at /run/ironclaw/mcp.sock. At
// launch the sandbox asks the broker for the tools this agent is gateway-approved to
// use (GET /tools) and registers each as a normal tool; invoking one POSTs to /call,
// and the broker enforces the grant deny-by-default and audits every call. So an MCP
// tool here is just a thin client of the host choke point — no protocol, no network.

// mcpFetchTimeout bounds the one-time tool discovery at startup.
const mcpFetchTimeout = 20 * time.Second

// mcpCallTimeout bounds a single MCP tool call from the sandbox side (the broker has
// its own, slightly shorter, per-call timeout).
const mcpCallTimeout = 90 * time.Second

// mcpMaxResponseBytes caps how much of an MCP result is read back into the agent's
// context so a large result cannot blow up the turn.
const mcpMaxResponseBytes = 256 * 1024

// mcpDescriptor mirrors one element of the broker's GET /tools response.
type mcpDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// mcpCallResponse mirrors the broker's POST /call response.
type mcpCallResponse struct {
	Content string `json:"content"`
	IsError bool   `json:"isError"`
}

// mcpTool presents one broker-exposed MCP tool to the model as an ordinary tool. Its
// Invoke forwards the call to the host broker over the bound unix socket; it performs
// no network and reaches no MCP server itself.
type mcpTool struct {
	client      *http.Client
	name        string
	description string
	schema      json.RawMessage
}

func (t *mcpTool) Name() string        { return t.name }
func (t *mcpTool) Description() string { return t.description }

func (t *mcpTool) JSONSchema() json.RawMessage {
	if len(t.schema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return t.schema
}

// Invoke forwards the call to the broker's /call endpoint and returns its result. A
// broker-reported tool error (policy denial or an upstream tool error) is surfaced as
// a Go error so the loop marks the tool result is_error for the model.
func (t *mcpTool) Invoke(ctx context.Context, input json.RawMessage) (string, error) {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	body, err := json.Marshal(struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}{Name: t.name, Input: input})
	if err != nil {
		return "", fmt.Errorf("%s: marshal call: %w", t.name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://mcp/call", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("%s: build request: %w", t.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: MCP broker unreachable: %w", t.name, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, mcpMaxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s: read response: %w", t.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: broker returned %s: %s", t.name, resp.Status, strings.TrimSpace(string(data)))
	}
	var out mcpCallResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("%s: decode response: %w", t.name, err)
	}
	if out.IsError {
		return "", fmt.Errorf("%s", out.Content)
	}
	return out.Content, nil
}

// mcpSocketClient builds an HTTP client that dials only the MCP broker unix socket,
// so every MCP request necessarily traverses the host broker.
func mcpSocketClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

// MCPTools connects to the host MCP broker over socketPath, fetches the agent's
// gateway-approved tool surface, and returns a Tool for each. An empty surface (the
// agent has no MCP grants) returns no tools and no error. A broker that is unreachable
// at startup returns an error the caller logs and continues past — the sandbox simply
// launches without MCP tools that turn, and a later relaunch retries.
func MCPTools(socketPath string) ([]Tool, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, nil
	}
	listClient := mcpSocketClient(socketPath, mcpFetchTimeout)
	ctx, cancel := context.WithTimeout(context.Background(), mcpFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://mcp/tools", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: build tools request: %w", err)
	}
	resp, err := listClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: broker unreachable for tool discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcp: broker /tools returned %s", resp.Status)
	}
	var body struct {
		Tools []mcpDescriptor `json:"tools"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, mcpMaxResponseBytes+1)).Decode(&body); err != nil {
		return nil, fmt.Errorf("mcp: decode /tools: %w", err)
	}

	// One call client shared by every MCP tool (separate, longer timeout than discovery).
	callClient := mcpSocketClient(socketPath, mcpCallTimeout)
	out := make([]Tool, 0, len(body.Tools))
	for _, d := range body.Tools {
		if strings.TrimSpace(d.Name) == "" {
			continue
		}
		out = append(out, &mcpTool{
			client:      callClient,
			name:        d.Name,
			description: d.Description,
			schema:      d.InputSchema,
		})
	}
	return out, nil
}
