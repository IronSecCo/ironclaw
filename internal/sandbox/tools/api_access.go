package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// RequestApiAccessToolName is the ergonomic, single-call path to ask for network access
// to a new external host. It is a typed shortcut for request_capability_change with a
// wiring/egress payload: same gateway, same mandatory human approval, same audited
// egress broker — just one obvious tool the model reaches for instead of hand-building
// the nested capability-change envelope.
const RequestApiAccessToolName = "request_api_access"

// RequestApiAccessTool lets the agent ASK for the egress broker to allow one or more
// external hosts. It performs no privileged action: it validates the hosts and returns
// a CapabilityChange envelope (kind=wiring, payload {"egress": [...]}) for the loop to
// forward to the host gateway, which requires human approval. After approval the host
// adds the hosts to the deny-by-default allowlist and the agent can reach them with
// http_fetch.
type RequestApiAccessTool struct{}

// NewRequestApiAccessTool constructs the API-access request tool.
func NewRequestApiAccessTool() *RequestApiAccessTool { return &RequestApiAccessTool{} }

func (t *RequestApiAccessTool) Name() string { return RequestApiAccessToolName }

func (t *RequestApiAccessTool) Description() string {
	return "Request permission to reach an external API or website by hostname (e.g. \"api.github.com\"). " +
		"This does NOT grant access: it submits a request to the host gateway, which a human must approve. " +
		"Once approved, the host adds the hostname to the audited egress allowlist and you can call it with " +
		"http_fetch. Use this whenever you need to reach a service you cannot currently reach — it is the " +
		"sanctioned path to new network access (do not claim it is impossible without offering to request it)."
}

func (t *RequestApiAccessTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"hosts":{"type":"array","items":{"type":"string"},"description":"Hostnames to request, e.g. [\"api.github.com\"]. Bare hosts only — no scheme, path, or wildcard."},` +
		`"reason":{"type":"string","description":"Why you need access (shown to the human approver)."}` +
		`},"required":["hosts"],"additionalProperties":false}`)
}

type requestApiAccessInput struct {
	Hosts  []string `json:"hosts"`
	Reason string   `json:"reason"`
}

// Invoke validates the requested hosts and returns the CapabilityChange envelope. It
// deliberately mutates nothing.
func (t *RequestApiAccessTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in requestApiAccessInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("request_api_access: invalid input: %w", err)
	}
	hosts, err := normalizeEgressHosts(in.Hosts)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string][]string{"egress": hosts})
	if err != nil {
		return "", fmt.Errorf("request_api_access: marshal payload: %w", err)
	}
	envelope := CapabilityChange{Kind: contract.ChangeWiring, Payload: payload, Reason: in.Reason}
	out, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("request_api_access: marshal envelope: %w", err)
	}
	return string(out), nil
}

// ToHostAction implements HostForwarder: the egress request is re-rendered into the
// host system-action wire format so host delivery routes it through the mandatory
// gateway, exactly like request_capability_change.
func (t *RequestApiAccessTool) ToHostAction(toolOutput string) (string, error) {
	cc, err := ParseCapabilityChange(toolOutput)
	if err != nil {
		return "", err
	}
	return cc.SystemActionJSON()
}

// normalizeEgressHosts validates and cleans the requested hosts: it tolerates a pasted
// URL (strips scheme + path), lowercases, drops blanks/dupes, and rejects anything that
// is not a bare host[:port]. It returns an error when nothing usable remains.
func normalizeEgressHosts(in []string) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		h := strings.ToLower(strings.TrimSpace(raw))
		if i := strings.Index(h, "://"); i >= 0 { // tolerate a full URL
			h = h[i+3:]
		}
		if i := strings.IndexAny(h, "/?#"); i >= 0 { // drop any path/query/fragment
			h = h[:i]
		}
		if h == "" {
			continue
		}
		if strings.ContainsAny(h, " \t*") || strings.Contains(h, "..") {
			return nil, fmt.Errorf("request_api_access: invalid host %q (use a bare hostname, e.g. api.github.com)", raw)
		}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("request_api_access: at least one host is required")
	}
	return out, nil
}
