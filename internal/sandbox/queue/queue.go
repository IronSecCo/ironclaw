// OWNER: AGENT2

// Package queue provides the sandbox-side queue implementations: an
// contract.InboundReader over contract.OpenInboundRO (read-only) and an
// contract.OutboundWriter over contract.OpenOutboundRW.
//
// The inbound handle is reopened every poll (mmap_size=0, query_only) to defeat
// the guest page cache; a corruption streak exits the process so the host
// respawns with a fresh mount. No method here writes inbound — that is enforced
// at the type level by InboundReader.
package queue

import (
	"errors"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// sandboxInbound is the sandbox's read-only implementation of the inbound queue.
type sandboxInbound struct{}

// OpenInbound opens the inbound queue read-only (sandbox side).
func OpenInbound(path string, k contract.SessionKey) (contract.InboundReader, error) {
	return nil, errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxInbound) PendingMessages(firstPoll bool) ([]contract.MessageIn, error) {
	return nil, errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxInbound) Destinations() ([]contract.Destination, error) {
	return nil, errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxInbound) SessionRouting() (contract.SessionRouting, error) {
	return contract.SessionRouting{}, errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxInbound) Close() error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}

// sandboxOutbound is the sandbox's write implementation of the outbound queue.
type sandboxOutbound struct{}

// OpenOutbound opens the outbound queue read/write (sandbox side, sole writer).
func OpenOutbound(path string, k contract.SessionKey) (contract.OutboundWriter, error) {
	return nil, errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxOutbound) WriteMessageOut(contract.MessageOut) error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxOutbound) MarkProcessing(ids []contract.MessageID) error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxOutbound) MarkCompleted(ids []contract.MessageID) error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxOutbound) PutSessionState(key, value string) error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}

func (s *sandboxOutbound) Close() error {
	return errors.New("sandbox/queue: not implemented (AGENT2)")
}
