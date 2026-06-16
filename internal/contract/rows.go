// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

import "time"

// MessageIn is a row in the inbound queue. The host is the sole writer; the
// sandbox reads only.
type MessageIn struct {
	ID              MessageID
	Seq             int64
	Kind            MessageKind
	Timestamp       time.Time
	Status          string
	ProcessAfter    *time.Time
	Recurrence      *string
	SeriesID        *string
	Tries           int
	Trigger         int
	PlatformID      *string
	ChannelType     *string
	ThreadID        *string
	Content         string
	SourceSessionID *string
	OnWake          bool
}

// MessageOut is a row in the outbound queue. The sandbox is the sole writer; the
// host reads only.
type MessageOut struct {
	ID           MessageID
	Seq          int64
	InReplyTo    *MessageID
	Timestamp    time.Time
	DeliverAfter *time.Time
	Recurrence   *string
	Kind         MessageKind
	PlatformID   *string
	ChannelType  *string
	ThreadID     *string
	Content      string
}

// ProcessingAck reports the sandbox's progress on a message back to the host.
type ProcessingAck struct {
	MessageID     MessageID
	Status        string
	StatusChanged time.Time
}

// Destination describes a place the agent group is allowed to send messages.
type Destination struct {
	Name         string
	DisplayName  *string
	Type         string
	ChannelType  *string
	PlatformID   *string
	AgentGroupID *AgentGroupID
}

// SessionRouting captures the platform coordinates of a session.
type SessionRouting struct {
	ChannelType string
	PlatformID  string
	ThreadID    *string
}

// SessionState is a single key/value entry of per-session durable state.
type SessionState struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}
