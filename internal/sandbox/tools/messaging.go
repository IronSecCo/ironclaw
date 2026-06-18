package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// OutboundEmitter is implemented by a tool whose successful result must be written
// to the OUTBOUND queue as one or more chat messages — send_message and send_file.
// The loop calls ToOutbound on the tool's Invoke output, assigns each returned
// message a fresh ID/seq/timestamp, and writes it via the OutboundWriter; the host
// delivery then independently enforces that the destination is permitted.
//
// This is distinct from HostForwarder, which emits a KindSystem action for host
// re-authorization (capability changes, scheduling). An emitter sends ordinary
// chat traffic the agent is already allowed to send; it never carries a privileged
// action and is never executed host-side.
type OutboundEmitter interface {
	ToOutbound(toolOutput string) ([]contract.MessageOut, error)
}

// emitted is the wire envelope a messaging tool's Invoke returns and ToOutbound
// parses back. It is an internal sandbox-tools shape (NOT a contract type): it
// carries exactly the resolved coordinates + content the loop needs to build a
// contract.MessageOut.
type emitted struct {
	To          string `json:"to"`
	ChannelType string `json:"channel_type"`
	PlatformID  string `json:"platform_id"`
	ThreadID    string `json:"thread_id,omitempty"`
	Content     string `json:"content"`
}

// toOutbound is the shared OutboundEmitter implementation: it parses the Invoke
// envelope and produces a single KindChat MessageOut. The loop fills ID, Seq, and
// Timestamp.
func toOutbound(toolOutput string) ([]contract.MessageOut, error) {
	var e emitted
	if err := json.Unmarshal([]byte(toolOutput), &e); err != nil {
		return nil, fmt.Errorf("sandbox/tools: parse emitted message: %w", err)
	}
	if e.ChannelType == "" || e.PlatformID == "" {
		return nil, fmt.Errorf("sandbox/tools: emitted message missing channel/platform coordinates")
	}
	msg := contract.MessageOut{
		Kind:        contract.KindChat,
		Content:     e.Content,
		ChannelType: strPtrOrNil(e.ChannelType),
		PlatformID:  strPtrOrNil(e.PlatformID),
		ThreadID:    strPtrOrNil(e.ThreadID),
	}
	return []contract.MessageOut{msg}, nil
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// --- send_message ---

// SendMessageTool sends a chat message to a named destination or, when no
// destination is named, into the current thread. It resolves coordinates through a
// MessageContext (the live inbound reader) and emits a KindChat outbound message;
// the host delivery re-checks destination permission.
type SendMessageTool struct {
	ctxt MessageContext
}

// NewSendMessageTool constructs the tool over a MessageContext.
func NewSendMessageTool(ctxt MessageContext) *SendMessageTool {
	return &SendMessageTool{ctxt: ctxt}
}

// Compile-time check: SendMessageTool emits outbound chat.
var _ OutboundEmitter = (*SendMessageTool)(nil)

func (t *SendMessageTool) Name() string { return "send_message" }

func (t *SendMessageTool) Description() string {
	return "Send a chat message. Omit \"to\" (or use \"current\") to reply in the current conversation; " +
		"set \"to\" to a named destination (see list_destinations) to message somewhere you are allowed to. " +
		"This sends ordinary chat — it cannot change any settings."
}

func (t *SendMessageTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"text":{"type":"string","description":"The message body to send."},` +
		`"to":{"type":"string","description":"Destination name from list_destinations, or omit/\"current\" for the current thread."}` +
		`},"required":["text"],"additionalProperties":false}`)
}

type sendMessageInput struct {
	Text string `json:"text"`
	To   string `json:"to"`
}

func (t *SendMessageTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in sendMessageInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("send_message: invalid input: %w", err)
	}
	if strings.TrimSpace(in.Text) == "" {
		return "", fmt.Errorf("send_message: text is required")
	}
	tgt, err := resolveTarget(t.ctxt, in.To)
	if err != nil {
		return "", fmt.Errorf("send_message: %w", err)
	}
	return marshalEmitted(tgt, in.Text)
}

// ToOutbound implements OutboundEmitter.
func (t *SendMessageTool) ToOutbound(toolOutput string) ([]contract.MessageOut, error) {
	return toOutbound(toolOutput)
}

// --- send_file ---

// SendFileTool sends the contents of a workspace file as a chat message to a named
// destination or the current thread. The frozen wire has no attachment field, so
// the file is delivered inline as UTF-8 text under a header; binary or oversized
// files are refused. Reading is jailed to the workspace via the shared Workspace.
type SendFileTool struct {
	ws   *Workspace
	ctxt MessageContext
}

// NewSendFileTool constructs the tool over the workspace (for jailed reads) and a
// MessageContext (for destination resolution).
func NewSendFileTool(ws *Workspace, ctxt MessageContext) *SendFileTool {
	return &SendFileTool{ws: ws, ctxt: ctxt}
}

// Compile-time check: SendFileTool emits outbound chat.
var _ OutboundEmitter = (*SendFileTool)(nil)

func (t *SendFileTool) Name() string { return "send_file" }

func (t *SendFileTool) Description() string {
	return "Send a UTF-8 text file from your workspace as a message. Omit \"to\" (or use \"current\") for the " +
		"current conversation, or name a destination from list_destinations. Binary or very large files are refused."
}

func (t *SendFileTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"path":{"type":"string","description":"Path of the file to send, relative to the workspace root."},` +
		`"to":{"type":"string","description":"Destination name from list_destinations, or omit/\"current\" for the current thread."},` +
		`"caption":{"type":"string","description":"Optional note to include above the file contents."}` +
		`},"required":["path"],"additionalProperties":false}`)
}

type sendFileInput struct {
	Path    string `json:"path"`
	To      string `json:"to"`
	Caption string `json:"caption"`
}

func (t *SendFileTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in sendFileInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("send_file: invalid input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return "", fmt.Errorf("send_file: path is required")
	}
	// Jailed read via the shared workspace (safeJoin rejects absolute paths and any
	// path escaping the workspace root). maxFileBytes caps the chat-wire payload.
	full, err := t.ws.safeJoin(in.Path)
	if err != nil {
		return "", fmt.Errorf("send_file: %w", err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("send_file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("send_file: %q is a directory", in.Path)
	}
	if info.Size() > maxFileBytes {
		return "", fmt.Errorf("send_file: %q is %d bytes, exceeds %d-byte limit", in.Path, info.Size(), maxFileBytes)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("send_file: %w", err)
	}
	data := string(raw)
	if !utf8.ValidString(data) {
		return "", fmt.Errorf("send_file: %q is not UTF-8 text; only text files can be sent over the chat wire", in.Path)
	}
	tgt, err := resolveTarget(t.ctxt, in.To)
	if err != nil {
		return "", fmt.Errorf("send_file: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[file: %s]\n", in.Path)
	if c := strings.TrimSpace(in.Caption); c != "" {
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	b.WriteString(data)
	return marshalEmitted(tgt, b.String())
}

// ToOutbound implements OutboundEmitter.
func (t *SendFileTool) ToOutbound(toolOutput string) ([]contract.MessageOut, error) {
	return toOutbound(toolOutput)
}

// marshalEmitted renders the resolved target + content as the emitted-message wire
// envelope the loop turns into an outbound message.
func marshalEmitted(tgt target, content string) (string, error) {
	e := emitted{
		To:          tgt.label,
		ChannelType: tgt.channelType,
		PlatformID:  tgt.platformID,
		Content:     content,
	}
	if tgt.threadID != nil {
		e.ThreadID = *tgt.threadID
	}
	b, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("sandbox/tools: marshal emitted message: %w", err)
	}
	return string(b), nil
}
