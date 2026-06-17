// OWNER: AGENT1

package registry

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// MemRegistry is an in-memory, mutex-guarded Registry. It is the development and
// test backend; a durable backend replaces it without touching callers.
type MemRegistry struct {
	mu sync.Mutex

	agentGroups     map[contract.AgentGroupID]AgentGroup
	messagingGroups map[contract.MessagingGroupID]MessagingGroup
	// mgIndex maps the (channelType, platformID, instance) triple to a messaging
	// group ID for get-or-create.
	mgIndex  map[string]contract.MessagingGroupID
	wirings  map[string]Wiring
	sessions map[contract.SessionID]Session
	users    map[contract.UserID]User
	roles    []Role
	members  []Member
	// dests maps an agent group to the set of allowed destination coordinate keys
	// (channelType\x00platformID).
	dests map[contract.AgentGroupID]map[string]struct{}
}

// NewMemRegistry constructs an empty MemRegistry.
func NewMemRegistry() *MemRegistry {
	return &MemRegistry{
		agentGroups:     make(map[contract.AgentGroupID]AgentGroup),
		messagingGroups: make(map[contract.MessagingGroupID]MessagingGroup),
		mgIndex:         make(map[string]contract.MessagingGroupID),
		wirings:         make(map[string]Wiring),
		sessions:        make(map[contract.SessionID]Session),
		users:           make(map[contract.UserID]User),
		dests:           make(map[contract.AgentGroupID]map[string]struct{}),
	}
}

// destKey is the composite key for a destination coordinate.
func destKey(channelType, platformID string) string {
	return channelType + "\x00" + platformID
}

// mgKey is the composite key for the messaging-group index.
func mgKey(channelType, platformID, instance string) string {
	if instance == "" {
		instance = channelType
	}
	return channelType + "\x00" + platformID + "\x00" + instance
}

// GetOrCreateMessagingGroup implements Registry.
func (r *MemRegistry) GetOrCreateMessagingGroup(channelType, platformID, instance string, isGroup bool, policy contract.UnknownSenderPolicy) (MessagingGroup, error) {
	if channelType == "" || platformID == "" {
		return MessagingGroup{}, fmt.Errorf("host/registry: messaging group requires channelType and platformID")
	}
	if instance == "" {
		instance = channelType
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := mgKey(channelType, platformID, instance)
	if id, ok := r.mgIndex[key]; ok {
		return r.messagingGroups[id], nil
	}
	id := contract.MessagingGroupID("mg_" + randID())
	mg := MessagingGroup{
		ID:                  id,
		ChannelType:         channelType,
		PlatformID:          platformID,
		Instance:            instance,
		IsGroup:             isGroup,
		UnknownSenderPolicy: policy,
	}
	r.messagingGroups[id] = mg
	r.mgIndex[key] = id
	return mg, nil
}

// ListWirings implements Registry. Results are sorted by descending priority,
// ties broken by wiring ID for determinism.
func (r *MemRegistry) ListWirings(mgID contract.MessagingGroupID) ([]Wiring, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Wiring
	for _, w := range r.wirings {
		if w.MessagingGroupID == mgID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GetMessagingGroup implements Registry.
func (r *MemRegistry) GetMessagingGroup(id contract.MessagingGroupID) (MessagingGroup, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mg, ok := r.messagingGroups[id]
	return mg, ok
}

// PutAgentGroup implements Registry.
func (r *MemRegistry) PutAgentGroup(g AgentGroup) error {
	if g.ID == "" {
		return fmt.Errorf("host/registry: agent group requires an ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentGroups[g.ID] = g
	return nil
}

// GetAgentGroup implements Registry.
func (r *MemRegistry) GetAgentGroup(id contract.AgentGroupID) (AgentGroup, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.agentGroups[id]
	return g, ok
}

// PutWiring implements Registry.
func (r *MemRegistry) PutWiring(w Wiring) error {
	if w.ID == "" {
		w.ID = "wr_" + randID()
	}
	if w.MessagingGroupID == "" || w.AgentGroupID == "" {
		return fmt.Errorf("host/registry: wiring requires messaging and agent group IDs")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wirings[w.ID] = w
	return nil
}

// PutUser implements Registry.
func (r *MemRegistry) PutUser(u User) error {
	if u.ID == "" {
		return fmt.Errorf("host/registry: user requires an ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	return nil
}

// GetUser implements Registry.
func (r *MemRegistry) GetUser(id contract.UserID) (User, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	return u, ok
}

// scopeEqual reports whether two role scopes (nil = global) are the same.
func scopeEqual(a, b *contract.AgentGroupID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// roleMatches reports whether two roles are identical (user, role, scope).
func roleMatches(a, b Role) bool {
	return a.UserID == b.UserID && a.Role == b.Role && scopeEqual(a.AgentGroupID, b.AgentGroupID)
}

// GrantRole implements Registry.
func (r *MemRegistry) GrantRole(role Role) error {
	if role.UserID == "" || (role.Role != RoleOwner && role.Role != RoleAdmin) {
		return fmt.Errorf("host/registry: role requires a user and a valid role (owner|admin)")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.roles {
		if roleMatches(existing, role) {
			return nil
		}
	}
	r.roles = append(r.roles, role)
	return nil
}

// RevokeRole implements Registry.
func (r *MemRegistry) RevokeRole(role Role) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.roles[:0]
	for _, existing := range r.roles {
		if roleMatches(existing, role) {
			continue
		}
		out = append(out, existing)
	}
	r.roles = out
	return nil
}

// AddMember implements Registry.
func (r *MemRegistry) AddMember(m Member) error {
	if m.UserID == "" || m.AgentGroupID == "" {
		return fmt.Errorf("host/registry: member requires user and agent group IDs")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.members {
		if existing == m {
			return nil
		}
	}
	r.members = append(r.members, m)
	return nil
}

// RemoveMember implements Registry.
func (r *MemRegistry) RemoveMember(m Member) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.members[:0]
	for _, existing := range r.members {
		if existing == m {
			continue
		}
		out = append(out, existing)
	}
	r.members = out
	return nil
}

// sessionKey derives the partition key for a session under the given mode.
//
//   - shared:       one session per (agent group, messaging group), thread ignored.
//   - per-thread:   one session per (agent group, messaging group, thread).
//   - agent-shared: one session per agent group across ALL its messaging groups
//     and threads (a single durable conversation for the agent).
func sessionKey(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) string {
	switch mode {
	case contract.SessionAgentShared:
		return "ag\x00" + string(agentGroupID)
	case contract.SessionPerThread:
		t := ""
		if threadID != nil {
			t = *threadID
		}
		return "pt\x00" + string(agentGroupID) + "\x00" + string(messagingGroupID) + "\x00" + t
	default: // SessionShared (and any unknown mode falls back to shared)
		return "sh\x00" + string(agentGroupID) + "\x00" + string(messagingGroupID)
	}
}

// findSessionLocked returns an existing session whose partition key (under mode)
// equals the requested key. The caller holds the mutex.
func (r *MemRegistry) findSessionLocked(key string, mode contract.SessionMode) (Session, bool) {
	for _, s := range r.sessions {
		if sessionKey(s.AgentGroupID, s.MessagingGroupID, s.ThreadID, mode) == key {
			return s, true
		}
	}
	return Session{}, false
}

// ResolveSession implements Registry.
func (r *MemRegistry) ResolveSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, error) {
	if agentGroupID == "" || messagingGroupID == "" {
		return Session{}, fmt.Errorf("host/registry: ResolveSession requires agent and messaging group IDs")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sessionKey(agentGroupID, messagingGroupID, threadID, mode)
	if s, ok := r.findSessionLocked(key, mode); ok {
		s.LastActive = time.Now().UTC()
		r.sessions[s.ID] = s
		return s, nil
	}
	var tid *string
	if threadID != nil && mode == contract.SessionPerThread {
		t := *threadID
		tid = &t
	}
	s := Session{
		ID:               contract.SessionID("ses_" + randID()),
		AgentGroupID:     agentGroupID,
		MessagingGroupID: messagingGroupID,
		ThreadID:         tid,
		ContainerStatus:  "new",
		LastActive:       time.Now().UTC(),
	}
	r.sessions[s.ID] = s
	return s, nil
}

// FindSession implements Registry.
func (r *MemRegistry) FindSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sessionKey(agentGroupID, messagingGroupID, threadID, mode)
	return r.findSessionLocked(key, mode)
}

// ListSessions implements Registry.
func (r *MemRegistry) ListSessions() ([]Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// GetSession implements Registry.
func (r *MemRegistry) GetSession(id contract.SessionID) (Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	return s, ok
}

// AddDestination implements Registry.
func (r *MemRegistry) AddDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) error {
	if agentGroupID == "" || channelType == "" || platformID == "" {
		return fmt.Errorf("host/registry: destination requires agent group, channel, and platform")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.dests[agentGroupID]
	if !ok {
		set = make(map[string]struct{})
		r.dests[agentGroupID] = set
	}
	set[destKey(channelType, platformID)] = struct{}{}
	return nil
}

// IsAllowedDestination implements Registry.
func (r *MemRegistry) IsAllowedDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	set, ok := r.dests[agentGroupID]
	if !ok {
		return false
	}
	_, ok = set[destKey(channelType, platformID)]
	return ok
}

// ListDestinations implements Registry. It returns the agent group's destinations
// sorted by (channel, platform) for stable output; empty (never nil) when none.
func (r *MemRegistry) ListDestinations(agentGroupID contract.AgentGroupID) []Destination {
	r.mu.Lock()
	defer r.mu.Unlock()
	set := r.dests[agentGroupID]
	out := make([]Destination, 0, len(set))
	for key := range set {
		ct, pid, _ := strings.Cut(key, "\x00")
		out = append(out, Destination{AgentGroupID: agentGroupID, ChannelType: ct, PlatformID: pid})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelType != out[j].ChannelType {
			return out[i].ChannelType < out[j].ChannelType
		}
		return out[i].PlatformID < out[j].PlatformID
	})
	return out
}

// CanAccess implements Registry with precedence owner > global-admin >
// scoped-admin > member.
func (r *MemRegistry) CanAccess(userID contract.UserID, agentGroupID contract.AgentGroupID) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Owner (always global) — highest precedence.
	for _, role := range r.roles {
		if role.UserID == userID && role.Role == RoleOwner {
			return true, "owner"
		}
	}
	// 2. Global admin (admin with no scope).
	for _, role := range r.roles {
		if role.UserID == userID && role.Role == RoleAdmin && role.AgentGroupID == nil {
			return true, "global-admin"
		}
	}
	// 3. Scoped admin (admin scoped to this agent group).
	for _, role := range r.roles {
		if role.UserID == userID && role.Role == RoleAdmin && role.AgentGroupID != nil && *role.AgentGroupID == agentGroupID {
			return true, "scoped-admin"
		}
	}
	// 4. Member of this agent group.
	for _, m := range r.members {
		if m.UserID == userID && m.AgentGroupID == agentGroupID {
			return true, "member"
		}
	}
	return false, "no-access"
}

// IsKnownSender implements Registry.
func (r *MemRegistry) IsKnownSender(userID contract.UserID, agentGroupID contract.AgentGroupID) bool {
	allowed, _ := r.CanAccess(userID, agentGroupID)
	return allowed
}

// randID returns a short random hex identifier.
func randID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read on a healthy system does not fail; fall back to a timestamp.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
