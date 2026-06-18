// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

// The queue access surface is interface-segregated so that read-only-inbound is
// enforced at the TYPE level: the sandbox is handed only InboundReader and
// OutboundWriter, and no method on either of those writes to the inbound queue.
// The host gets the mirror images (InboundWriter + OutboundReader). Combined with
// PRAGMA query_only and an OS read-only bind mount, this gives three independent
// layers of inbound-write protection.

// InboundReader is the sandbox's read-only view of the inbound queue.
type InboundReader interface {
	PendingMessages(firstPoll bool) ([]MessageIn, error)
	Destinations() ([]Destination, error)
	SessionRouting() (SessionRouting, error)
	Close() error
}

// OutboundWriter is the sandbox's write view of the outbound queue.
type OutboundWriter interface {
	WriteMessageOut(MessageOut) error
	MarkProcessing(ids []MessageID) error
	MarkCompleted(ids []MessageID) error
	PutSessionState(key, value string) error
	Close() error
}

// InboundWriter is the host's write view of the inbound queue.
type InboundWriter interface {
	WriteMessageIn(MessageIn) error
	UpsertDestinations([]Destination) error
	MarkDelivered(id MessageID, platformMsgID *string) error
	Close() error
}

// OutboundReader is the host's read-only view of the outbound queue.
type OutboundReader interface {
	DueMessages() ([]MessageOut, error)
	ProcessingAcks() ([]ProcessingAck, error)
	Close() error
}
