package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// AskUserQuestionToolName matches the host's non-privileged ask-user action
// (contract.ActionAskUser): the host records the question for a human to answer.
const AskUserQuestionToolName = contract.ActionAskUser

// AskUserQuestionTool lets the agent put a question to a human — optionally with
// preset choices (a "choice card") — and have the host record it as a pending
// question. It performs NO privileged action and carries no script/command field:
// it emits a contract.AskUserRequest that the loop forwards to the host as a system
// message (see HostForwarder); the host re-validates it as non-privileged and
// tracks it for an operator to answer. Use it when a human decision is needed
// before proceeding.
type AskUserQuestionTool struct{}

// NewAskUserQuestionTool constructs the ask-user tool.
func NewAskUserQuestionTool() *AskUserQuestionTool { return &AskUserQuestionTool{} }

// Compile-time check: the tool forwards to the host as a system action.
var _ HostForwarder = (*AskUserQuestionTool)(nil)

func (t *AskUserQuestionTool) Name() string { return AskUserQuestionToolName }

func (t *AskUserQuestionTool) Description() string {
	return "Ask a human a question and pause for their decision, optionally offering preset choices (a choice card). " +
		"This does NOT run code or change any settings — it records a question for an operator to answer. " +
		"Use it when you need a human decision before proceeding."
}

func (t *AskUserQuestionTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"question":{"type":"string","description":"The question to put to the human."},` +
		`"options":{"type":"array","items":{"type":"string"},"description":"Optional preset choices to offer the human."},` +
		`"allow_freeform":{"type":"boolean","description":"Whether the human may answer with free text in addition to any preset options."}` +
		`},"required":["question"],"additionalProperties":false}`)
}

type askUserInput struct {
	Question      string   `json:"question"`
	Options       []string `json:"options"`
	AllowFreeform bool     `json:"allow_freeform"`
}

// Invoke validates the request and returns the contract.AskUserRequest wire body.
// It mutates nothing: the loop forwards the body to the host (see ToHostAction),
// which records the pending question.
func (t *AskUserQuestionTool) Invoke(_ context.Context, input json.RawMessage) (string, error) {
	var in askUserInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("%s: invalid input: %w", AskUserQuestionToolName, err)
	}
	if strings.TrimSpace(in.Question) == "" {
		return "", fmt.Errorf("%s: question is required", AskUserQuestionToolName)
	}
	var opts []string
	for _, o := range in.Options {
		if strings.TrimSpace(o) != "" {
			opts = append(opts, o)
		}
	}
	return contract.MarshalAskUserRequest(contract.AskUserRequest{
		Question:      in.Question,
		Options:       opts,
		AllowFreeform: in.AllowFreeform,
	})
}

// ToHostAction implements HostForwarder: the Invoke output is already the
// ask_user_question wire body, so it forwards verbatim after a parse check.
func (t *AskUserQuestionTool) ToHostAction(toolOutput string) (string, error) {
	if _, err := contract.ParseAskUserRequest(toolOutput); err != nil {
		return "", err
	}
	return toolOutput, nil
}
