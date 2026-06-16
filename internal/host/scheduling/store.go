// OWNER: T-084

package scheduling

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// This file adds the scheduling STORE: the lifecycle authority for scheduled
// tasks (list / cancel / pause / resume / update). It complements the pure
// validation + next-occurrence logic in scheduling.go.
//
// Why a separate store and not the inbound queue? A scheduled prompt is enqueued
// as an inbound row (status=scheduled), but the frozen contract's InboundWriter
// exposes only WriteMessageIn / MarkDelivered — there is no cancel / pause /
// update method, and adding one would be a contract change. More fundamentally,
// the queue cannot express a PAUSED state. So task lifecycle lives here, in a
// host-internal store the host owns, while the queue stays the delivery path.
//
// SECURITY NOTE — same invariant as the rest of scheduling: a Task carries ONLY a
// prompt, never a script/command. Managing a task (pausing, editing its prompt,
// rescheduling) can never become an execution path; the worst a malicious edit
// can do is change WHICH prompt the agent later reads, and any privileged action
// that prompt then requests still passes through the gateway. Every operation is
// also SESSION-SCOPED: a caller may only touch tasks belonging to its own
// session, so one sandbox can never list or cancel another session's tasks.

// TaskState is the lifecycle state of a scheduled task. A task is born Active,
// may toggle Active<->Paused, and terminates at Cancelled (terminal).
type TaskState string

const (
	// TaskActive: the task is live and will fire when it next comes due.
	TaskActive TaskState = "active"
	// TaskPaused: the task is suspended; the sweep must not fire it until resumed.
	TaskPaused TaskState = "paused"
	// TaskCancelled: the task is permanently retired (terminal); it never fires
	// again and is excluded from List.
	TaskCancelled TaskState = "cancelled"
)

// Task is a scheduled prompt tracked by the store: a future (optionally recurring)
// prompt plus its lifecycle state. Like ScheduledRequest it carries ONLY a prompt
// — there is intentionally NO script/command field.
type Task struct {
	ID         string
	SessionID  contract.SessionID
	Prompt     string
	NextRun    time.Time
	Recurrence string // "" (one-shot) or a named cadence / Go duration (see Validate)
	State      TaskState
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TaskUpdate carries the fields an update_task may change. A nil pointer leaves
// the corresponding field unchanged; a non-nil pointer sets it. The resulting task
// is re-validated, so an update that would empty the prompt or set a bad cadence
// is rejected.
type TaskUpdate struct {
	Prompt     *string
	NextRun    *time.Time
	Recurrence *string
}

// Sentinel errors so callers (and the host's system-action dispatch) can branch on
// the failure rather than string-match.
var (
	// ErrTaskNotFound: no task with that id exists for the session.
	ErrTaskNotFound = errors.New("host/scheduling: task not found")
	// ErrTaskCancelled: the operation is invalid because the task is already
	// cancelled (terminal). Returned by Pause/Resume/Update.
	ErrTaskCancelled = errors.New("host/scheduling: task is cancelled")
	// ErrInvalidTask: the task or update failed validation (empty id/prompt, bad
	// recurrence, no-op update).
	ErrInvalidTask = errors.New("host/scheduling: invalid task")
)

// Store is the task-lifecycle surface the host operates and the sandbox's
// task-management tools target (over the wire). It is satisfied by *MemStore; a
// durable backend can implement the same interface later with no caller changes.
type Store interface {
	// Add inserts a new task. It validates the task and rejects a duplicate id.
	Add(t Task) error
	// Get returns the session's task by id, if present (any state).
	Get(sessionID contract.SessionID, id string) (Task, bool)
	// List returns the session's live (non-cancelled) tasks, sorted by next run
	// then id.
	List(sessionID contract.SessionID) []Task
	// Cancel retires a task (terminal, idempotent) and returns it.
	Cancel(sessionID contract.SessionID, id string) (Task, error)
	// Pause suspends an active task (idempotent if already paused).
	Pause(sessionID contract.SessionID, id string) (Task, error)
	// Resume reactivates a paused task (idempotent if already active).
	Resume(sessionID contract.SessionID, id string) (Task, error)
	// Update changes a task's prompt / next run / recurrence, re-validating it.
	Update(sessionID contract.SessionID, id string, upd TaskUpdate) (Task, error)
}

// MemStore is an in-memory, concurrency-safe Store. State is held in a map keyed by
// (session, task id); every method takes the session id so operations are
// session-scoped at the type level.
type MemStore struct {
	mu    sync.Mutex
	tasks map[string]Task // key = sessionID + "\x00" + id
	now   func() time.Time
}

var _ Store = (*MemStore)(nil)

// NewMemStore constructs an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{
		tasks: make(map[string]Task),
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// key builds the composite map key. Using a NUL separator keeps distinct
// (session,id) pairs from ever colliding regardless of the id contents.
func key(sessionID contract.SessionID, id string) string {
	return string(sessionID) + "\x00" + id
}

// validateTask checks the invariants shared by Add and Update: non-empty id,
// non-empty session, and a prompt + recurrence that pass scheduling.Validate.
func validateTask(t Task) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidTask)
	}
	if strings.TrimSpace(string(t.SessionID)) == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidTask)
	}
	if err := Validate(ScheduledRequest{Prompt: t.Prompt, RunAt: t.NextRun, Recurrence: t.Recurrence}); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTask, err)
	}
	return nil
}

// Add inserts a new task. The task must validate and its id must be free within
// the session. CreatedAt/UpdatedAt and a defaulted Active state are stamped here.
func (s *MemStore) Add(t Task) error {
	if err := validateTask(t); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(t.SessionID, t.ID)
	if _, exists := s.tasks[k]; exists {
		return fmt.Errorf("%w: duplicate task id %q for session %s", ErrInvalidTask, t.ID, t.SessionID)
	}
	now := s.now()
	if t.State == "" {
		t.State = TaskActive
	}
	t.NextRun = t.NextRun.UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	s.tasks[k] = t
	return nil
}

// Get returns the session's task by id (any state).
func (s *MemStore) Get(sessionID contract.SessionID, id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[key(sessionID, id)]
	return t, ok
}

// List returns the session's live (Active or Paused) tasks, sorted by next run
// then id for a stable, deterministic order. Cancelled tasks are excluded.
func (s *MemStore) List(sessionID contract.SessionID) []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.tasks {
		if t.SessionID == sessionID && t.State != TaskCancelled {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].NextRun.Equal(out[j].NextRun) {
			return out[i].NextRun.Before(out[j].NextRun)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// mutate looks up a session's task and applies fn under the lock, persisting and
// returning the result. fn may return an error to abort without persisting.
func (s *MemStore) mutate(sessionID contract.SessionID, id string, fn func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(sessionID, id)
	t, ok := s.tasks[k]
	if !ok {
		return Task{}, fmt.Errorf("%w: %q for session %s", ErrTaskNotFound, id, sessionID)
	}
	if err := fn(&t); err != nil {
		return Task{}, err
	}
	t.UpdatedAt = s.now()
	s.tasks[k] = t
	return t, nil
}

// Cancel retires a task. It is terminal and idempotent: cancelling an
// already-cancelled task is a no-op that returns the task.
func (s *MemStore) Cancel(sessionID contract.SessionID, id string) (Task, error) {
	return s.mutate(sessionID, id, func(t *Task) error {
		t.State = TaskCancelled
		return nil
	})
}

// Pause suspends an active task so the sweep will not fire it. It is idempotent for
// an already-paused task and refuses a cancelled one.
func (s *MemStore) Pause(sessionID contract.SessionID, id string) (Task, error) {
	return s.mutate(sessionID, id, func(t *Task) error {
		if t.State == TaskCancelled {
			return fmt.Errorf("%w: cannot pause %q", ErrTaskCancelled, id)
		}
		t.State = TaskPaused
		return nil
	})
}

// Resume reactivates a paused task. It is idempotent for an already-active task and
// refuses a cancelled one.
func (s *MemStore) Resume(sessionID contract.SessionID, id string) (Task, error) {
	return s.mutate(sessionID, id, func(t *Task) error {
		if t.State == TaskCancelled {
			return fmt.Errorf("%w: cannot resume %q", ErrTaskCancelled, id)
		}
		t.State = TaskActive
		return nil
	})
}

// Update changes a task's prompt, next run, and/or recurrence. At least one field
// must be supplied; a cancelled task cannot be updated; and the resulting task is
// re-validated so an edit can never produce an empty prompt or bad cadence.
func (s *MemStore) Update(sessionID contract.SessionID, id string, upd TaskUpdate) (Task, error) {
	if upd.Prompt == nil && upd.NextRun == nil && upd.Recurrence == nil {
		return Task{}, fmt.Errorf("%w: update changes nothing", ErrInvalidTask)
	}
	return s.mutate(sessionID, id, func(t *Task) error {
		if t.State == TaskCancelled {
			return fmt.Errorf("%w: cannot update %q", ErrTaskCancelled, id)
		}
		next := *t
		if upd.Prompt != nil {
			next.Prompt = *upd.Prompt
		}
		if upd.NextRun != nil {
			next.NextRun = upd.NextRun.UTC()
		}
		if upd.Recurrence != nil {
			next.Recurrence = *upd.Recurrence
		}
		if err := validateTask(next); err != nil {
			return err
		}
		*t = next
		return nil
	})
}

// --- Wire-action bridge ----------------------------------------------------

// Task-management action discriminators carried as contract.SystemAction.Action.
// They MIRROR the sandbox tool names in internal/sandbox/tools/tasks.go: the
// sandbox cannot import this package, so the strings are duplicated across the seam
// and pinned by tests on both sides (the same approach schedule_task's recurrence
// rule uses). Like ActionScheduleTask they are NON-privileged — they only manage
// future prompts and execute nothing — so the host applies them directly here
// rather than routing them through the gateway.
const (
	ActionListTasks  = "list_tasks"
	ActionCancelTask = "cancel_task"
	ActionPauseTask  = "pause_task"
	ActionResumeTask = "resume_task"
	ActionUpdateTask = "update_task"
)

// ManagePayload is the contract.SystemAction.Payload of a task-management action.
// TaskID identifies the target for cancel/pause/resume/update; Prompt/RunAt/
// Recurrence carry the new values for update_task (RunAt is RFC3339). list_tasks
// needs no payload.
type ManagePayload struct {
	TaskID     string `json:"task_id,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	RunAt      string `json:"run_at,omitempty"`
	Recurrence string `json:"recurrence,omitempty"`
}

// IsManageAction reports whether action is one of the task-management actions
// (used by the host's system-action dispatch to route it to ApplyManage).
func IsManageAction(action string) bool {
	switch action {
	case ActionListTasks, ActionCancelTask, ActionPauseTask, ActionResumeTask, ActionUpdateTask:
		return true
	default:
		return false
	}
}

// ApplyManage applies a task-management action to the store on behalf of
// sessionID. For ActionListTasks it returns the session's live tasks; the other
// actions return the single affected task. It executes nothing privileged — it is
// the host's entry point for a non-privileged task-management system message.
//
// This is the host-side bridge; wiring it into the delivery loop (so an inbound
// task-management SystemAction reaches it) is a small, separate integration step
// owned by host/delivery, mirroring how schedule_task is dispatched there.
func ApplyManage(s Store, sessionID contract.SessionID, action string, p ManagePayload) ([]Task, error) {
	switch action {
	case ActionListTasks:
		return s.List(sessionID), nil
	case ActionCancelTask:
		t, err := s.Cancel(sessionID, p.TaskID)
		return wrapOne(t, err)
	case ActionPauseTask:
		t, err := s.Pause(sessionID, p.TaskID)
		return wrapOne(t, err)
	case ActionResumeTask:
		t, err := s.Resume(sessionID, p.TaskID)
		return wrapOne(t, err)
	case ActionUpdateTask:
		upd, err := payloadToUpdate(p)
		if err != nil {
			return nil, err
		}
		t, err := s.Update(sessionID, p.TaskID, upd)
		return wrapOne(t, err)
	default:
		return nil, fmt.Errorf("host/scheduling: unknown task-management action %q", action)
	}
}

// payloadToUpdate converts the update fields of a ManagePayload into a TaskUpdate.
// Only the fields actually present in the payload are set; an empty RunAt/
// Recurrence/Prompt string means "not supplied" (leave unchanged). RunAt is parsed
// as RFC3339.
func payloadToUpdate(p ManagePayload) (TaskUpdate, error) {
	var upd TaskUpdate
	if p.Prompt != "" {
		prompt := p.Prompt
		upd.Prompt = &prompt
	}
	if strings.TrimSpace(p.RunAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(p.RunAt))
		if err != nil {
			return TaskUpdate{}, fmt.Errorf("%w: run_at must be RFC3339: %v", ErrInvalidTask, err)
		}
		ut := t.UTC()
		upd.NextRun = &ut
	}
	if p.Recurrence != "" {
		rec := p.Recurrence
		upd.Recurrence = &rec
	}
	return upd, nil
}

// wrapOne adapts a single-task result to the []Task return shape.
func wrapOne(t Task, err error) ([]Task, error) {
	if err != nil {
		return nil, err
	}
	return []Task{t}, nil
}

// MarshalManagePayload renders a ManagePayload as JSON (used by callers building a
// system-action body and convenient for tests).
func MarshalManagePayload(p ManagePayload) (json.RawMessage, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return b, nil
}
