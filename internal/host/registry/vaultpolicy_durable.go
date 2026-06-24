package registry

// DurableVaultPolicyStore persists per-group vault policy across a control-plane
// restart. Like SQLRegistry it is host-internal — the sandbox never sees the
// policy store — so it is NOT bound by the frozen queue contract and owns its own
// SQLCipher database and schema.
//
// Design mirrors durable.go: an in-memory VaultPolicyStore is the working set and
// the single source of truth for reads (Allows/Get), so the deny-by-default
// decision logic and validation are reused verbatim and stay exercised by the
// existing vaultpolicy_test.go. Every mutation is mirrored write-through to the
// encrypted DB as a single-row upsert/delete, and the whole policy set is reloaded
// into a fresh in-memory store on Open.
//
// Durability is write-through best-effort: the in-memory mutation is applied first
// (it validates and normalizes), then persisted. If the persist fails the call
// returns the error; the in-memory state is momentarily ahead of disk and the lost
// change is simply absent after a restart (the DB is authoritative on reload).

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"

	_ "github.com/mutecomm/go-sqlcipher/v4" // registers the "sqlite3" SQLCipher driver

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// vaultPolicySchema is the host-internal DDL for the durable policy store. One row
// per agent group holds that group's normalized rules as JSON; a group with no row
// is denied everything (deny-by-default), exactly as an empty in-memory store.
const vaultPolicySchema = `
CREATE TABLE IF NOT EXISTS vault_policies (
    agent_group_id TEXT PRIMARY KEY,
    rules_json     TEXT NOT NULL
);
`

// DurableVaultPolicyStore is a VaultPolicyStore backed by an encrypted SQLCipher
// database. Reads (Allows/Get) answer from the in-memory working set; mutations
// (Set/Delete) write through to disk.
type DurableVaultPolicyStore struct {
	mem *VaultPolicyStore
	db  *sql.DB
}

// OpenDurableVaultPolicyStore opens (creating if absent) the encrypted vault-policy
// database at path, keyed by key, and loads its contents into the working set.
// Opening with the wrong key fails (SQLITE_NOTADB on the forced page read) rather
// than silently returning an empty — i.e. fully-permissive-by-omission — store. The
// key is a secret derived from the host master (see keys.DeriveSubKey); never log
// it.
func OpenDurableVaultPolicyStore(path string, key [32]byte) (*DurableVaultPolicyStore, error) {
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
		return nil, fmt.Errorf("host/registry: open encrypted vault-policy db %q (wrong key?): %w", path, err)
	}
	if _, err := db.Exec(vaultPolicySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("host/registry: apply vault-policy schema: %w", err)
	}

	s := &DurableVaultPolicyStore{mem: NewVaultPolicyStore(), db: db}
	if err := s.load(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// load hydrates the in-memory working set from the encrypted DB. A row that fails
// validation (e.g. a corrupt or tampered policy) fails the open rather than being
// silently dropped or admitted — fail-closed for an authorization store.
func (s *DurableVaultPolicyStore) load() error {
	rows, err := s.db.Query(`SELECT agent_group_id, rules_json FROM vault_policies`)
	if err != nil {
		return fmt.Errorf("host/registry: load vault policies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, rj string
		if err := rows.Scan(&id, &rj); err != nil {
			return fmt.Errorf("host/registry: scan vault policy: %w", err)
		}
		var rules []VaultRule
		if err := json.Unmarshal([]byte(rj), &rules); err != nil {
			return fmt.Errorf("host/registry: decode vault policy for %q: %w", id, err)
		}
		// Re-validate and normalize through the in-memory store so a tampered or
		// stale row cannot install a malformed (and thus unevaluable) rule set.
		if err := s.mem.Set(VaultPolicy{AgentGroupID: contract.AgentGroupID(id), Rules: rules}); err != nil {
			return fmt.Errorf("host/registry: invalid persisted vault policy for %q: %w", id, err)
		}
	}
	return rows.Err()
}

// Set installs or replaces a group's policy: validate+normalize in memory first,
// then persist the normalized form as a single-row upsert. Call site: the gateway
// apply step for an approved vault-policy change.
func (s *DurableVaultPolicyStore) Set(p VaultPolicy) error {
	if err := s.mem.Set(p); err != nil {
		return err
	}
	// Persist exactly what the in-memory store holds (normalized), so a reload
	// reconstructs the identical decision.
	norm, _ := s.mem.Get(p.AgentGroupID)
	blob, err := json.Marshal(norm.Rules)
	if err != nil {
		return fmt.Errorf("host/registry: encode vault policy for %q: %w", p.AgentGroupID, err)
	}
	if _, err := s.db.Exec(`
		INSERT INTO vault_policies (agent_group_id, rules_json) VALUES (?,?)
		ON CONFLICT(agent_group_id) DO UPDATE SET rules_json=excluded.rules_json`,
		string(p.AgentGroupID), string(blob)); err != nil {
		return fmt.Errorf("host/registry: persist vault policy for %q: %w", p.AgentGroupID, err)
	}
	return nil
}

// Delete removes a group's policy in memory and on disk (idempotent). Unlike the
// in-memory store's Delete it returns the persistence error so a failed write-
// through is not silently swallowed. Gateway-apply only.
func (s *DurableVaultPolicyStore) Delete(group contract.AgentGroupID) error {
	s.mem.Delete(group)
	if _, err := s.db.Exec(`DELETE FROM vault_policies WHERE agent_group_id=?`, string(group)); err != nil {
		return fmt.Errorf("host/registry: delete vault policy for %q: %w", group, err)
	}
	return nil
}

// Get returns a group's stored (normalized) policy from the working set.
func (s *DurableVaultPolicyStore) Get(group contract.AgentGroupID) (VaultPolicy, bool) {
	return s.mem.Get(group)
}

// Allows answers the deny-by-default authorization decision from the working set,
// identically to the in-memory store.
func (s *DurableVaultPolicyStore) Allows(group contract.AgentGroupID, credential, host string) bool {
	return s.mem.Allows(group, credential, host)
}

// Close closes the underlying database handle.
func (s *DurableVaultPolicyStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
