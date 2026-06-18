// Package questions holds the host-side store of pending ask_user_question
// requests a sandbox raised (RFC-0003). It is host-internal — NOT the frozen
// contract — and deliberately small: a thread-safe in-memory set that an operator
// surface (API/CLI) can list and resolve. Recording a question changes nothing and
// runs nothing; it is the non-privileged counterpart to a gateway change.
//
// Feeding the human's answer back to the session as an ordinary inbound message is
// a follow-on; this package owns the pending set and its lifecycle.
package questions

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// Pending is a question the sandbox asked a human, awaiting an answer.
type Pending struct {
	ID            string
	SessionID     contract.SessionID
	AgentGroupID  contract.AgentGroupID
	Question      string
	Options       []string
	AllowFreeform bool
	AskedAt       time.Time
}

// Store is a thread-safe in-memory set of pending questions keyed by ID.
type Store struct {
	mu    sync.Mutex
	items map[string]Pending
	seq   int64
	now   func() time.Time
}

// NewStore constructs an empty question store.
func NewStore() *Store {
	return &Store{items: make(map[string]Pending), now: time.Now}
}

// Record adds a pending question for a session and returns it (with a generated
// ID and AskedAt timestamp).
func (s *Store) Record(sessionID contract.SessionID, agentGroupID contract.AgentGroupID, req contract.AskUserRequest) Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	p := Pending{
		ID:            fmt.Sprintf("q_%s_%d", sessionID, s.seq),
		SessionID:     sessionID,
		AgentGroupID:  agentGroupID,
		Question:      req.Question,
		Options:       append([]string(nil), req.Options...),
		AllowFreeform: req.AllowFreeform,
		AskedAt:       s.now().UTC(),
	}
	s.items[p.ID] = p
	return p
}

// List returns all pending questions, oldest first (ties broken by ID).
func (s *Store) List() []Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Pending, 0, len(s.items))
	for _, p := range s.items {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AskedAt.Equal(out[j].AskedAt) {
			return out[i].AskedAt.Before(out[j].AskedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Get returns a pending question by ID.
func (s *Store) Get(id string) (Pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[id]
	return p, ok
}

// Resolve removes a pending question (an operator has answered it) and returns it.
// The bool is false if no such question is pending.
func (s *Store) Resolve(id string) (Pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[id]
	if ok {
		delete(s.items, id)
	}
	return p, ok
}

// Len reports how many questions are currently pending.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
