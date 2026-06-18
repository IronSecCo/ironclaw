package registry

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4" // registers the "sqlite3" SQLCipher driver

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// SQLRegistry is a durable, encrypted Registry backend. It is host-internal (the
// sandbox never sees the registry), so it is NOT bound by the frozen queue
// contract — it owns its own SQLCipher database and schema.
//
// Design: an in-memory MemRegistry is the working set and the single source of
// truth for reads, so all of the precedence (owner > global-admin > scoped-admin >
// member) and session-partitioning logic is reused verbatim and stays exercised by
// the existing tests. Every mutation is mirrored write-through to the encrypted DB
// as a single-row upsert/delete, and the whole state is reloaded into a fresh
// MemRegistry on Open. This swaps in behind the Registry interface with no caller
// changes.
//
// Durability is write-through best-effort: the in-memory mutation is applied
// first (it also generates IDs), then persisted. If the persist fails the call
// returns the error; the in-memory state is momentarily ahead of disk and the lost
// change is simply absent after a restart (the DB is authoritative on reload).
type SQLRegistry struct {
	mem *MemRegistry
	db  *sql.DB
}

var _ Registry = (*SQLRegistry)(nil)

// registrySchema is the host-internal DDL. It is owned here (not the frozen
// contract). Global roles store an empty scope_key so (user_id, role, scope_key)
// can be a real primary key (SQLite treats NULLs as distinct, which would defeat
// grant-idempotency for global roles).
const registrySchema = `
CREATE TABLE IF NOT EXISTS agent_groups (
    id     TEXT PRIMARY KEY,
    name   TEXT,
    folder TEXT
);
CREATE TABLE IF NOT EXISTS messaging_groups (
    id                    TEXT PRIMARY KEY,
    channel_type          TEXT,
    platform_id           TEXT,
    instance              TEXT,
    is_group              INTEGER,
    unknown_sender_policy TEXT
);
CREATE TABLE IF NOT EXISTS wirings (
    id                     TEXT PRIMARY KEY,
    messaging_group_id     TEXT,
    agent_group_id         TEXT,
    engage_mode            TEXT,
    engage_pattern         TEXT,
    sender_scope           TEXT,
    ignored_message_policy TEXT,
    session_mode           TEXT,
    priority               INTEGER
);
CREATE TABLE IF NOT EXISTS sessions (
    id                 TEXT PRIMARY KEY,
    agent_group_id     TEXT,
    messaging_group_id TEXT,
    thread_id          TEXT,
    container_status   TEXT,
    last_active        TEXT
);
CREATE TABLE IF NOT EXISTS users (
    id           TEXT PRIMARY KEY,
    kind         TEXT,
    display_name TEXT
);
CREATE TABLE IF NOT EXISTS roles (
    user_id        TEXT,
    role           TEXT,
    scope_key      TEXT,  -- '' => global role
    agent_group_id TEXT,  -- NULL => global role
    PRIMARY KEY (user_id, role, scope_key)
);
CREATE TABLE IF NOT EXISTS members (
    user_id        TEXT,
    agent_group_id TEXT,
    PRIMARY KEY (user_id, agent_group_id)
);
CREATE TABLE IF NOT EXISTS destinations (
    agent_group_id TEXT,
    channel_type   TEXT,
    platform_id    TEXT,
    PRIMARY KEY (agent_group_id, channel_type, platform_id)
);
`

// OpenSQLRegistry opens (creating if absent) the encrypted registry database at
// path, keyed by key, and loads its contents into the working set. Opening with
// the wrong key fails (SQLITE_NOTADB on the forced page read) rather than silently
// returning an empty registry.
func OpenSQLRegistry(path string, key [32]byte) (*SQLRegistry, error) {
	q := url.Values{}
	// Raw-key mode (no KDF) via the DSN so every pooled connection is keyed before
	// any page is read.
	q.Set("_pragma_key", `x'`+hex.EncodeToString(key[:])+`'`)
	q.Set("_busy_timeout", "5000")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("host/registry: open %q: %w", path, err)
	}
	// Host-local single writer; one connection keeps writes serialized and avoids
	// multiple keyed handles racing on the file.
	db.SetMaxOpenConns(1)
	// Force a real page read so a wrong key fails here rather than at first query.
	if _, err := db.Exec("SELECT count(*) FROM sqlite_master;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("host/registry: open encrypted db %q (wrong key?): %w", path, err)
	}
	if _, err := db.Exec(registrySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("host/registry: apply schema: %w", err)
	}

	r := &SQLRegistry{mem: NewMemRegistry(), db: db}
	if err := r.load(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

// Close closes the underlying database handle.
func (r *SQLRegistry) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

// scopeKey maps a (nullable) role scope to its storage key: "" for a global role,
// else the agent-group id.
func scopeKey(ag *contract.AgentGroupID) string {
	if ag == nil {
		return ""
	}
	return string(*ag)
}

// load reads every table into a fresh MemRegistry working set, rebuilding the
// derived indexes (messaging-group triple index, destination set).
func (r *SQLRegistry) load() error {
	m := r.mem

	rows, err := r.db.Query(`SELECT id, name, folder FROM agent_groups`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g AgentGroup
		if err := rows.Scan(&g.ID, &g.Name, &g.Folder); err != nil {
			rows.Close()
			return err
		}
		m.agentGroups[g.ID] = g
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT id, channel_type, platform_id, instance, is_group, unknown_sender_policy FROM messaging_groups`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			mg     MessagingGroup
			grp    int
			policy string
		)
		if err := rows.Scan(&mg.ID, &mg.ChannelType, &mg.PlatformID, &mg.Instance, &grp, &policy); err != nil {
			rows.Close()
			return err
		}
		mg.IsGroup = grp != 0
		mg.UnknownSenderPolicy = contract.UnknownSenderPolicy(policy)
		m.messagingGroups[mg.ID] = mg
		m.mgIndex[mgKey(mg.ChannelType, mg.PlatformID, mg.Instance)] = mg.ID
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT id, messaging_group_id, agent_group_id, engage_mode, engage_pattern, sender_scope, ignored_message_policy, session_mode, priority FROM wirings`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			w               Wiring
			em, ss, imp, sm string
		)
		if err := rows.Scan(&w.ID, &w.MessagingGroupID, &w.AgentGroupID, &em, &w.EngagePattern, &ss, &imp, &sm, &w.Priority); err != nil {
			rows.Close()
			return err
		}
		w.EngageMode = contract.EngageMode(em)
		w.SenderScope = contract.SenderScope(ss)
		w.IgnoredMessagePolicy = contract.IgnoredMessagePolicy(imp)
		w.SessionMode = contract.SessionMode(sm)
		m.wirings[w.ID] = w
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT id, agent_group_id, messaging_group_id, thread_id, container_status, last_active FROM sessions`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			s      Session
			thread sql.NullString
			active string
		)
		if err := rows.Scan(&s.ID, &s.AgentGroupID, &s.MessagingGroupID, &thread, &s.ContainerStatus, &active); err != nil {
			rows.Close()
			return err
		}
		if thread.Valid {
			t := thread.String
			s.ThreadID = &t
		}
		if active != "" {
			if t, err := time.Parse(time.RFC3339Nano, active); err == nil {
				s.LastActive = t
			}
		}
		m.sessions[s.ID] = s
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT id, kind, display_name FROM users`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Kind, &u.DisplayName); err != nil {
			rows.Close()
			return err
		}
		m.users[u.ID] = u
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT user_id, role, agent_group_id FROM roles`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			role Role
			ag   sql.NullString
		)
		if err := rows.Scan(&role.UserID, &role.Role, &ag); err != nil {
			rows.Close()
			return err
		}
		if ag.Valid && ag.String != "" {
			id := contract.AgentGroupID(ag.String)
			role.AgentGroupID = &id
		}
		m.roles = append(m.roles, role)
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT user_id, agent_group_id FROM members`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mem Member
		if err := rows.Scan(&mem.UserID, &mem.AgentGroupID); err != nil {
			rows.Close()
			return err
		}
		m.members = append(m.members, mem)
	}
	rows.Close()

	rows, err = r.db.Query(`SELECT agent_group_id, channel_type, platform_id FROM destinations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var (
			ag, ch, pl string
		)
		if err := rows.Scan(&ag, &ch, &pl); err != nil {
			rows.Close()
			return err
		}
		agID := contract.AgentGroupID(ag)
		set, ok := m.dests[agID]
		if !ok {
			set = make(map[string]struct{})
			m.dests[agID] = set
		}
		set[destKey(ch, pl)] = struct{}{}
	}
	rows.Close()

	return nil
}

// --- mutations: apply to the working set, then mirror to the DB ---

// GetOrCreateMessagingGroup implements Registry.
func (r *SQLRegistry) GetOrCreateMessagingGroup(channelType, platformID, instance string, isGroup bool, policy contract.UnknownSenderPolicy) (MessagingGroup, error) {
	mg, err := r.mem.GetOrCreateMessagingGroup(channelType, platformID, instance, isGroup, policy)
	if err != nil {
		return mg, err
	}
	_, err = r.db.Exec(`
        INSERT INTO messaging_groups (id, channel_type, platform_id, instance, is_group, unknown_sender_policy)
        VALUES (?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
            channel_type=excluded.channel_type, platform_id=excluded.platform_id,
            instance=excluded.instance, is_group=excluded.is_group,
            unknown_sender_policy=excluded.unknown_sender_policy`,
		string(mg.ID), mg.ChannelType, mg.PlatformID, mg.Instance, boolToInt(mg.IsGroup), string(mg.UnknownSenderPolicy))
	if err != nil {
		return MessagingGroup{}, fmt.Errorf("host/registry: persist messaging group: %w", err)
	}
	return mg, nil
}

// PutAgentGroup implements Registry.
func (r *SQLRegistry) PutAgentGroup(g AgentGroup) error {
	if err := r.mem.PutAgentGroup(g); err != nil {
		return err
	}
	_, err := r.db.Exec(`
        INSERT INTO agent_groups (id, name, folder) VALUES (?,?,?)
        ON CONFLICT(id) DO UPDATE SET name=excluded.name, folder=excluded.folder`,
		string(g.ID), g.Name, g.Folder)
	return wrapPersist("agent group", err)
}

// PutWiring implements Registry. The ID is assigned here (when empty) so the same
// value lands in both the working set and the DB.
func (r *SQLRegistry) PutWiring(w Wiring) error {
	if w.ID == "" {
		w.ID = "wr_" + randID()
	}
	if err := r.mem.PutWiring(w); err != nil {
		return err
	}
	_, err := r.db.Exec(`
        INSERT INTO wirings (id, messaging_group_id, agent_group_id, engage_mode, engage_pattern, sender_scope, ignored_message_policy, session_mode, priority)
        VALUES (?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
            messaging_group_id=excluded.messaging_group_id, agent_group_id=excluded.agent_group_id,
            engage_mode=excluded.engage_mode, engage_pattern=excluded.engage_pattern,
            sender_scope=excluded.sender_scope, ignored_message_policy=excluded.ignored_message_policy,
            session_mode=excluded.session_mode, priority=excluded.priority`,
		w.ID, string(w.MessagingGroupID), string(w.AgentGroupID), string(w.EngageMode), w.EngagePattern,
		string(w.SenderScope), string(w.IgnoredMessagePolicy), string(w.SessionMode), w.Priority)
	return wrapPersist("wiring", err)
}

// PutUser implements Registry.
func (r *SQLRegistry) PutUser(u User) error {
	if err := r.mem.PutUser(u); err != nil {
		return err
	}
	_, err := r.db.Exec(`
        INSERT INTO users (id, kind, display_name) VALUES (?,?,?)
        ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, display_name=excluded.display_name`,
		string(u.ID), u.Kind, u.DisplayName)
	return wrapPersist("user", err)
}

// GrantRole implements Registry.
func (r *SQLRegistry) GrantRole(role Role) error {
	if err := r.mem.GrantRole(role); err != nil {
		return err
	}
	var ag any
	if role.AgentGroupID != nil {
		ag = string(*role.AgentGroupID)
	}
	_, err := r.db.Exec(`
        INSERT INTO roles (user_id, role, scope_key, agent_group_id) VALUES (?,?,?,?)
        ON CONFLICT(user_id, role, scope_key) DO NOTHING`,
		string(role.UserID), role.Role, scopeKey(role.AgentGroupID), ag)
	return wrapPersist("role", err)
}

// RevokeRole implements Registry.
func (r *SQLRegistry) RevokeRole(role Role) error {
	if err := r.mem.RevokeRole(role); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM roles WHERE user_id=? AND role=? AND scope_key=?`,
		string(role.UserID), role.Role, scopeKey(role.AgentGroupID))
	return wrapPersist("revoke role", err)
}

// AddMember implements Registry.
func (r *SQLRegistry) AddMember(m Member) error {
	if err := r.mem.AddMember(m); err != nil {
		return err
	}
	_, err := r.db.Exec(`
        INSERT INTO members (user_id, agent_group_id) VALUES (?,?)
        ON CONFLICT(user_id, agent_group_id) DO NOTHING`,
		string(m.UserID), string(m.AgentGroupID))
	return wrapPersist("member", err)
}

// RemoveMember implements Registry.
func (r *SQLRegistry) RemoveMember(m Member) error {
	if err := r.mem.RemoveMember(m); err != nil {
		return err
	}
	_, err := r.db.Exec(`DELETE FROM members WHERE user_id=? AND agent_group_id=?`,
		string(m.UserID), string(m.AgentGroupID))
	return wrapPersist("remove member", err)
}

// ResolveSession implements Registry. The (possibly new, possibly touched) session
// is persisted so LastActive and creation survive a restart.
func (r *SQLRegistry) ResolveSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, error) {
	s, err := r.mem.ResolveSession(agentGroupID, messagingGroupID, threadID, mode)
	if err != nil {
		return s, err
	}
	if err := r.upsertSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

// AddDestination implements Registry.
func (r *SQLRegistry) AddDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) error {
	if err := r.mem.AddDestination(agentGroupID, channelType, platformID); err != nil {
		return err
	}
	_, err := r.db.Exec(`
        INSERT INTO destinations (agent_group_id, channel_type, platform_id) VALUES (?,?,?)
        ON CONFLICT(agent_group_id, channel_type, platform_id) DO NOTHING`,
		string(agentGroupID), channelType, platformID)
	return wrapPersist("destination", err)
}

// upsertSession mirrors a session row.
func (r *SQLRegistry) upsertSession(s Session) error {
	var thread any
	if s.ThreadID != nil {
		thread = *s.ThreadID
	}
	_, err := r.db.Exec(`
        INSERT INTO sessions (id, agent_group_id, messaging_group_id, thread_id, container_status, last_active)
        VALUES (?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
            agent_group_id=excluded.agent_group_id, messaging_group_id=excluded.messaging_group_id,
            thread_id=excluded.thread_id, container_status=excluded.container_status,
            last_active=excluded.last_active`,
		string(s.ID), string(s.AgentGroupID), string(s.MessagingGroupID), thread,
		s.ContainerStatus, s.LastActive.UTC().Format(time.RFC3339Nano))
	return wrapPersist("session", err)
}

// --- reads: delegate to the in-memory working set ---

func (r *SQLRegistry) ListWirings(mgID contract.MessagingGroupID) ([]Wiring, error) {
	return r.mem.ListWirings(mgID)
}
func (r *SQLRegistry) GetMessagingGroup(id contract.MessagingGroupID) (MessagingGroup, bool) {
	return r.mem.GetMessagingGroup(id)
}
func (r *SQLRegistry) ListMessagingGroups() []MessagingGroup { return r.mem.ListMessagingGroups() }
func (r *SQLRegistry) GetAgentGroup(id contract.AgentGroupID) (AgentGroup, bool) {
	return r.mem.GetAgentGroup(id)
}
func (r *SQLRegistry) ListAgentGroups() []AgentGroup           { return r.mem.ListAgentGroups() }
func (r *SQLRegistry) GetUser(id contract.UserID) (User, bool) { return r.mem.GetUser(id) }
func (r *SQLRegistry) ListSessions() ([]Session, error)        { return r.mem.ListSessions() }
func (r *SQLRegistry) GetSession(id contract.SessionID) (Session, bool) {
	return r.mem.GetSession(id)
}
func (r *SQLRegistry) FindSession(agentGroupID contract.AgentGroupID, messagingGroupID contract.MessagingGroupID, threadID *string, mode contract.SessionMode) (Session, bool) {
	return r.mem.FindSession(agentGroupID, messagingGroupID, threadID, mode)
}
func (r *SQLRegistry) IsAllowedDestination(agentGroupID contract.AgentGroupID, channelType, platformID string) bool {
	return r.mem.IsAllowedDestination(agentGroupID, channelType, platformID)
}

// ListDestinations reads from the in-memory working set (kept in sync with the
// destinations table on every AddDestination and rebuilt on Open).
func (r *SQLRegistry) ListDestinations(agentGroupID contract.AgentGroupID) []Destination {
	return r.mem.ListDestinations(agentGroupID)
}
func (r *SQLRegistry) CanAccess(userID contract.UserID, agentGroupID contract.AgentGroupID) (bool, string) {
	return r.mem.CanAccess(userID, agentGroupID)
}
func (r *SQLRegistry) IsKnownSender(userID contract.UserID, agentGroupID contract.AgentGroupID) bool {
	return r.mem.IsKnownSender(userID, agentGroupID)
}

// --- small helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func wrapPersist(what string, err error) error {
	if err != nil {
		return fmt.Errorf("host/registry: persist %s: %w", what, err)
	}
	return nil
}
