// FROZEN CONTRACT — do not edit without a joint RFC (see docs/contract.md).

package contract

// Typed identifiers prevent accidental mixups between the many string-shaped IDs
// that flow across the host/sandbox seam.
type (
	SessionID        string
	MessageID        string
	AgentGroupID     string
	MessagingGroupID string
	UserID           string
	ChangeID         string
)
