package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// Audit event stage values.
const (
	AuditSubmit   = "submit"
	AuditVerdict  = "verdict"
	AuditDecision = "decision"
	AuditApply    = "apply"
)

// AuditEntry is one append-only audit record. Entries are written as JSONL (one
// JSON object per line) so the log is greppable and crash-safe.
type AuditEntry struct {
	Time     time.Time           `json:"time"`
	Stage    string              `json:"stage"`
	ChangeID contract.ChangeID   `json:"changeId"`
	Kind     contract.ChangeKind `json:"kind,omitempty"`
	Detail   string              `json:"detail,omitempty"`
}

// AuditLog is an append-only JSONL audit sink. It is mutex-guarded; each Append
// writes one line and flushes. A nil *AuditLog is a valid no-op sink so callers
// need not branch.
type AuditLog struct {
	mu sync.Mutex
	f  *os.File
}

// NewAuditLog opens (creating/appending) the JSONL audit file at path.
func NewAuditLog(path string) (*AuditLog, error) {
	if path == "" {
		return nil, fmt.Errorf("host/gateway: AuditLog requires a path")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("host/gateway: AuditLog open: %w", err)
	}
	return &AuditLog{f: f}, nil
}

// Append writes one audit entry. A nil receiver is a no-op (audit disabled).
func (a *AuditLog) Append(e AuditEntry) error {
	if a == nil {
		return nil
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return a.f.Sync()
}

// Close closes the underlying file. A nil receiver is a no-op.
func (a *AuditLog) Close() error {
	if a == nil || a.f == nil {
		return nil
	}
	return a.f.Close()
}

// ReadAudit reads up to limit most-recent entries from a JSONL audit file at path
// (limit <= 0 returns all). It is used by the API's audit endpoint. A missing file
// returns an empty slice.
func ReadAudit(path string, limit int) ([]AuditEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditEntry{}, nil
		}
		return nil, err
	}
	var all []AuditEntry
	dec := json.NewDecoder(bytes.NewReader(b))
	for dec.More() {
		var e AuditEntry
		if err := dec.Decode(&e); err != nil {
			break
		}
		all = append(all, e)
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}
