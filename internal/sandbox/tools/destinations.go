package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// MessageContext gives the messaging tools read access to the session's allowed
// send destinations and its own reply coordinates. contract.InboundReader (the
// sandbox's read-only inbound view) satisfies it, so the loop wires the live
// inbound reader in; tests inject a fake. The messaging tools never write the
// inbound queue — they only resolve where an outbound message should go.
type MessageContext interface {
	Destinations() ([]contract.Destination, error)
	SessionRouting() (contract.SessionRouting, error)
}

// target is a resolved set of outbound platform coordinates.
type target struct {
	channelType string
	platformID  string
	threadID    *string
	label       string // human-friendly name for the tool result / model
}

// currentThreadAliases are the destination names that mean "reply in the current
// conversation" rather than a named destination.
func isCurrentThread(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "current", "here", "thread", "current_thread", "this":
		return true
	}
	return false
}

// resolveTarget maps a destination name to outbound coordinates. An empty/"current"
// name resolves to the session's own routing (a reply in the originating chat); any
// other name must match a registered Destination that carries channel + platform
// coordinates. The host delivery independently re-checks that the agent group is
// permitted to send there.
func resolveTarget(ctxt MessageContext, name string) (target, error) {
	if isCurrentThread(name) {
		r, err := ctxt.SessionRouting()
		if err != nil {
			return target{}, fmt.Errorf("resolve current thread: %w", err)
		}
		if r.ChannelType == "" || r.PlatformID == "" {
			return target{}, fmt.Errorf("no routing for the current thread yet — name a destination instead")
		}
		return target{channelType: r.ChannelType, platformID: r.PlatformID, threadID: r.ThreadID, label: "current thread"}, nil
	}

	dests, err := ctxt.Destinations()
	if err != nil {
		return target{}, fmt.Errorf("list destinations: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(name))
	for _, d := range dests {
		if strings.ToLower(d.Name) != want {
			continue
		}
		if d.ChannelType == nil || d.PlatformID == nil || *d.ChannelType == "" || *d.PlatformID == "" {
			return target{}, fmt.Errorf("destination %q has no channel/platform coordinates to send to", d.Name)
		}
		return target{channelType: *d.ChannelType, platformID: *d.PlatformID, label: d.Name}, nil
	}
	return target{}, fmt.Errorf("unknown destination %q (known: %s)", name, knownNames(dests))
}

// knownNames renders the available destination names for an error message.
func knownNames(dests []contract.Destination) string {
	if len(dests) == 0 {
		return "none configured — reply in the current thread instead"
	}
	names := make([]string, 0, len(dests))
	for _, d := range dests {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ListDestinationsTool lets the agent discover the named destinations it is allowed
// to send to (beyond replying in the current thread). It is read-only and emits no
// outbound message.
type ListDestinationsTool struct {
	ctxt MessageContext
}

// NewListDestinationsTool constructs the tool over a MessageContext.
func NewListDestinationsTool(ctxt MessageContext) *ListDestinationsTool {
	return &ListDestinationsTool{ctxt: ctxt}
}

func (t *ListDestinationsTool) Name() string { return "list_destinations" }

func (t *ListDestinationsTool) Description() string {
	return "List the named destinations you are allowed to send messages or files to with send_message/send_file. " +
		"You can always reply in the current thread without naming a destination."
}

func (t *ListDestinationsTool) JSONSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

type destinationView struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Channel     string `json:"channel,omitempty"`
}

func (t *ListDestinationsTool) Invoke(_ context.Context, _ json.RawMessage) (string, error) {
	dests, err := t.ctxt.Destinations()
	if err != nil {
		return "", fmt.Errorf("list_destinations: %w", err)
	}
	views := make([]destinationView, 0, len(dests))
	for _, d := range dests {
		v := destinationView{Name: d.Name}
		if d.DisplayName != nil {
			v.DisplayName = *d.DisplayName
		}
		if d.ChannelType != nil {
			v.Channel = *d.ChannelType
		}
		views = append(views, v)
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Name < views[j].Name })
	b, err := json.Marshal(views)
	if err != nil {
		return "", fmt.Errorf("list_destinations: %w", err)
	}
	return string(b), nil
}
