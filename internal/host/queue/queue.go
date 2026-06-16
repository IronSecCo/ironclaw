// OWNER: AGENT1

// Package queue provides the host-side queue implementations: an
// contract.InboundWriter (the host is the sole writer of inbound) and an
// contract.OutboundReader (the host reads outbound read-only, with the
// reopen-per-poll discipline from design-plan §1).
package queue

import (
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// hostInbound is the host's write implementation of the inbound queue.
type hostInbound struct{}

// OpenInbound opens the inbound queue for writing (host side).
func OpenInbound(path string, k contract.SessionKey) (contract.InboundWriter, error) {
	return nil, errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostInbound) WriteMessageIn(contract.MessageIn) error {
	return errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostInbound) UpsertDestinations([]contract.Destination) error {
	return errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostInbound) MarkDelivered(id contract.MessageID, platformMsgID *string) error {
	return errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostInbound) Close() error {
	return errors.New("host/queue: not implemented (AGENT1)")
}

// hostOutbound is the host's read implementation of the outbound queue.
type hostOutbound struct{}

// OpenOutbound opens the outbound queue for reading (host side).
func OpenOutbound(path string, k contract.SessionKey) (contract.OutboundReader, error) {
	return nil, errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostOutbound) DueMessages() ([]contract.MessageOut, error) {
	return nil, errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostOutbound) ProcessingAcks() ([]contract.ProcessingAck, error) {
	return nil, errors.New("host/queue: not implemented (AGENT1)")
}

func (h *hostOutbound) Close() error {
	return errors.New("host/queue: not implemented (AGENT1)")
}
