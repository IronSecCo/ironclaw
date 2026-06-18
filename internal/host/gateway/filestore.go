package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/IronSecCo/ironclaw/internal/contract"
)

// fileRecord is the on-disk JSON shape for one change's lifecycle. It is a
// superset of storedChange so a FileStore survives a restart with full state.
type fileRecord struct {
	Req      contract.ChangeRequest `json:"req"`
	Status   string                 `json:"status"`
	Decision *contract.Decision     `json:"decision,omitempty"`
}

// FileStore is a durable contract.ChangeStore: each change is one JSON file under
// a directory, named "<id>.json". State survives restart — Pending() is rebuilt
// from disk on construction. It is mutex-guarded and safe for concurrent use.
type FileStore struct {
	dir string
	mu  sync.Mutex
	// cache mirrors disk so reads do not re-stat the directory.
	cache map[contract.ChangeID]*fileRecord
}

// NewFileStore opens (creating if needed) the directory and loads any existing
// change records into the in-memory cache.
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("host/gateway: FileStore requires a directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("host/gateway: FileStore mkdir: %w", err)
	}
	fs := &FileStore{dir: dir, cache: make(map[contract.ChangeID]*fileRecord)}
	if err := fs.load(); err != nil {
		return nil, err
	}
	return fs, nil
}

// load reads all *.json records from disk into the cache.
func (fs *FileStore) load() error {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return fmt.Errorf("host/gateway: FileStore read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			return fmt.Errorf("host/gateway: FileStore read %s: %w", e.Name(), err)
		}
		var rec fileRecord
		if err := json.Unmarshal(b, &rec); err != nil {
			return fmt.Errorf("host/gateway: FileStore parse %s: %w", e.Name(), err)
		}
		fs.cache[rec.Req.ID] = &rec
	}
	return nil
}

// path returns the on-disk file path for a change id. The id is sanitized so it
// cannot escape the directory (defense-in-depth; ids are gateway-generated hex).
func (fs *FileStore) path(id contract.ChangeID) string {
	clean := strings.ReplaceAll(string(id), string(os.PathSeparator), "_")
	clean = strings.ReplaceAll(clean, "..", "_")
	return filepath.Join(fs.dir, clean+".json")
}

// persist atomically writes a record to disk (write temp + rename).
func (fs *FileStore) persist(rec *fileRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	final := fs.path(rec.Req.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// Put inserts a change as pending. An existing change with the same ID is left
// untouched (idempotent submit).
func (fs *FileStore) Put(req contract.ChangeRequest) error {
	if req.ID == "" {
		return fmt.Errorf("host/gateway: FileStore.Put requires a non-empty ChangeID")
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.cache[req.ID]; ok {
		return nil
	}
	rec := &fileRecord{Req: req, Status: string(statusPending)}
	fs.cache[req.ID] = rec
	return fs.persist(rec)
}

// SetDecision records a decision and moves the change to approved or rejected.
func (fs *FileStore) SetDecision(id contract.ChangeID, d contract.Decision) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	rec, ok := fs.cache[id]
	if !ok {
		return fmt.Errorf("host/gateway: unknown change %q", id)
	}
	dc := d
	rec.Decision = &dc
	switch d.Outcome {
	case OutcomeApprove:
		rec.Status = string(statusApproved)
	case OutcomeReject:
		rec.Status = string(statusRejected)
	default:
		return fmt.Errorf("host/gateway: unknown decision outcome %q", d.Outcome)
	}
	return fs.persist(rec)
}

// MarkApplied marks a change applied (terminal success state).
func (fs *FileStore) MarkApplied(id contract.ChangeID) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	rec, ok := fs.cache[id]
	if !ok {
		return fmt.Errorf("host/gateway: unknown change %q", id)
	}
	rec.Status = string(statusApplied)
	return fs.persist(rec)
}

// Pending returns all changes still awaiting a decision, ordered by id.
func (fs *FileStore) Pending() ([]contract.ChangeRequest, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []contract.ChangeRequest
	for _, rec := range fs.cache {
		if rec.Status == string(statusPending) {
			out = append(out, rec.Req)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Status returns the lifecycle status string for a change. The second return is
// false if the change is unknown.
func (fs *FileStore) Status(id contract.ChangeID) (string, bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	rec, ok := fs.cache[id]
	if !ok {
		return "", false
	}
	return rec.Status, true
}

// History returns all non-pending changes (applied + rejected + approved) with
// their decision, ordered by id. It is used by the API's change-history endpoint.
func (fs *FileStore) History() []HistoryEntry {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []HistoryEntry
	for _, rec := range fs.cache {
		if rec.Status == string(statusPending) {
			continue
		}
		out = append(out, HistoryEntry{Request: rec.Req, Status: rec.Status, Decision: rec.Decision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Request.ID < out[j].Request.ID })
	return out
}

// HistoryEntry is one applied/rejected change with its recorded decision.
type HistoryEntry struct {
	Request  contract.ChangeRequest `json:"request"`
	Status   string                 `json:"status"`
	Decision *contract.Decision     `json:"decision,omitempty"`
}
