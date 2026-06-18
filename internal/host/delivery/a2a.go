package delivery

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
	"github.com/nivardsec/ironclaw/internal/host/registry"
)

// Agent-to-agent (a2a) routing (RFC-0004). a2a lets one agent group hand work to
// another. It needs ZERO frozen-contract change: the host represents an a2a target
// as a Destination row whose ChannelType is the "agent" sentinel and whose
// PlatformID is the target agent-group id, so the sandbox's existing send_message
// tool faithfully copies it into the outbound message with no sandbox change. The
// host recognizes the sentinel here and routes the message INBOUND to the target
// group's active sessions, stamping provenance.
//
// Safety (RFC-0004 open questions, resolved at the maintainer's "Moderate"):
//   - Authorization is deny-by-default: the sender's agent group must hold an
//     explicit agent-destination grant for the target (reusing the existing
//     registry destination allowlist with the "agent" channel). Operators express
//     a trust group by granting these.
//   - Hop depth is bounded (default 5) so a2a cannot ping-pong indefinitely.
//   - Per-agent-group send rate is bounded (default 120/min) so fan-out is capped.

const (
	// agentChannel is the host-internal sentinel ChannelType marking an outbound
	// message as agent-to-agent. The target agent-group id is carried in PlatformID.
	agentChannel = "agent"
	// defaultA2AHopLimit caps the a2a chain depth.
	defaultA2AHopLimit = 5
	// defaultA2ASendsPerMinute caps per-agent-group a2a fan-out.
	defaultA2ASendsPerMinute = 120
)

// handleA2A routes an agent-to-agent message inbound to the target agent group's
// active sessions, after authorization + hop-depth + send-quota checks.
//
// An authorization failure is a hard error (a real misconfiguration to surface).
// The amplification limits (hop depth, quota) DROP the message (logged) rather than
// erroring, so a flooding agent cannot stall the delivery loop for everyone.
func (d *Delivery) handleA2A(sess registry.Session, msg contract.MessageOut) error {
	if d.newWriter == nil {
		return fmt.Errorf("host/delivery: a2a from %s refused (no inbound-writer wired)", sess.ID)
	}
	target := contract.AgentGroupID(deref(msg.PlatformID))
	if target == "" {
		return fmt.Errorf("host/delivery: a2a message from %s has no target agent group", sess.ID)
	}
	// Deny-by-default authorization: the sender needs an agent-destination grant.
	if !d.reg.IsAllowedDestination(sess.AgentGroupID, agentChannel, string(target)) {
		return fmt.Errorf("host/delivery: a2a from %s to %s not permitted (no agent-destination grant)", sess.AgentGroupID, target)
	}

	// Hop-depth bound: how deep is the SENDER already in an a2a chain?
	d.mu.Lock()
	senderHop := d.a2aHops[sess.ID]
	d.mu.Unlock()
	if senderHop >= d.a2aHopLimit {
		log.Printf("host/delivery: a2a from %s dropped: hop depth %d reached limit %d", sess.ID, senderHop, d.a2aHopLimit)
		return nil
	}
	// Send-quota bound per sender group.
	if !d.a2aQuota.allow(string(sess.AgentGroupID)) {
		log.Printf("host/delivery: a2a from %s dropped: send quota exceeded", sess.AgentGroupID)
		return nil
	}

	sessions, err := d.reg.ListSessions()
	if err != nil {
		return fmt.Errorf("host/delivery: a2a list sessions: %w", err)
	}
	wrote := 0
	for _, tgt := range sessions {
		if tgt.AgentGroupID != target {
			continue
		}
		if err := d.writeA2AInbound(tgt, sess, msg, senderHop+1); err != nil {
			return err
		}
		wrote++
	}
	if wrote == 0 {
		log.Printf("host/delivery: a2a from %s to %s: target has no active session; dropping", sess.AgentGroupID, target)
	}
	return nil
}

// writeA2AInbound writes one inbound message to a target session, stamping the
// sender's session id as provenance and recording the target's a2a chain depth so
// the next hop is bounded.
func (d *Delivery) writeA2AInbound(target, from registry.Session, msg contract.MessageOut, hop int) error {
	writer, err := d.newWriter(target.ID)
	if err != nil {
		return fmt.Errorf("host/delivery: a2a open inbound writer for %s: %w", target.ID, err)
	}
	defer writer.Close()

	src := string(from.ID)
	in := contract.MessageIn{
		ID:              d.nextA2AID(target.ID),
		Seq:             d.nextEvenSeq(),
		Kind:            contract.KindChat,
		Timestamp:       time.Now().UTC(),
		Status:          contract.StatusQueued,
		Trigger:         1, // a directed agent message engages the target
		Content:         msg.Content,
		SourceSessionID: &src,
	}
	if err := writer.WriteMessageIn(in); err != nil {
		return fmt.Errorf("host/delivery: a2a enqueue for %s: %w", target.ID, err)
	}
	d.mu.Lock()
	d.a2aHops[target.ID] = hop
	d.mu.Unlock()
	return nil
}

// nextA2AID returns a process-unique id for an a2a inbound message.
func (d *Delivery) nextA2AID(target contract.SessionID) contract.MessageID {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.a2aCtr++
	return contract.MessageID(fmt.Sprintf("a2a_%s_%d", target, d.a2aCtr))
}

// a2aQuota is a per-key fixed-window rate limiter (sends per minute). It bounds
// agent-to-agent fan-out so a compromised agent cannot flood its peers.
type a2aQuota struct {
	mu     sync.Mutex
	limit  int
	window map[string]*a2aWindow
}

type a2aWindow struct {
	start time.Time
	count int
}

func newA2AQuota(limit int) *a2aQuota {
	return &a2aQuota{limit: limit, window: make(map[string]*a2aWindow)}
}

// allow reports whether key may send now, consuming one unit if so. The window is
// a fixed one-minute bucket per key.
func (q *a2aQuota) allow(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	w := q.window[key]
	if w == nil || now.Sub(w.start) >= time.Minute {
		q.window[key] = &a2aWindow{start: now, count: 1}
		return true
	}
	if w.count >= q.limit {
		return false
	}
	w.count++
	return true
}
