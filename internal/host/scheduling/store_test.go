// OWNER: T-084

package scheduling

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nivardsec/ironclaw/internal/contract"
)

// fixedClock returns a store whose clock is pinned to base+offsets, so CreatedAt
// and UpdatedAt are deterministic across mutations.
func fixedClock(base time.Time) (*MemStore, *int) {
	s := NewMemStore()
	tick := 0
	s.now = func() time.Time { return base.Add(time.Duration(tick) * time.Second) }
	return s, &tick
}

func sampleTask(id string, sess contract.SessionID, runAt time.Time) Task {
	return Task{ID: id, SessionID: sess, Prompt: "check the deploy", NextRun: runAt, Recurrence: "daily"}
}

func TestAddValidatesAndStamps(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s, _ := fixedClock(base)

	if err := s.Add(sampleTask("t1", "sessA", base.Add(time.Hour))); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.Get("sessA", "t1")
	if !ok {
		t.Fatal("Get after Add: not found")
	}
	if got.State != TaskActive {
		t.Fatalf("default state = %q, want %q", got.State, TaskActive)
	}
	if !got.CreatedAt.Equal(base) || !got.UpdatedAt.Equal(base) {
		t.Fatalf("timestamps not stamped: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}

	// Duplicate id within the session is rejected.
	if err := s.Add(sampleTask("t1", "sessA", base)); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("duplicate Add err = %v, want ErrInvalidTask", err)
	}
}

func TestAddRejectsInvalid(t *testing.T) {
	s := NewMemStore()
	bad := map[string]Task{
		"empty id":         {ID: "", SessionID: "s", Prompt: "p"},
		"empty session":    {ID: "t", SessionID: "", Prompt: "p"},
		"empty prompt":     {ID: "t", SessionID: "s", Prompt: ""},
		"bad recurrence":   {ID: "t", SessionID: "s", Prompt: "p", Recurrence: "fortnightly"},
		"negative cadence": {ID: "t", SessionID: "s", Prompt: "p", Recurrence: "-5m"},
	}
	for name, task := range bad {
		t.Run(name, func(t *testing.T) {
			if err := s.Add(task); !errors.Is(err, ErrInvalidTask) {
				t.Fatalf("Add(%+v) err = %v, want ErrInvalidTask", task, err)
			}
		})
	}
}

func TestListIsSessionScopedSortedAndExcludesCancelled(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s := NewMemStore()
	// Two sessions; sessA has three tasks out of run order plus one to cancel.
	mustAdd(t, s, sampleTask("b", "sessA", base.Add(2*time.Hour)))
	mustAdd(t, s, sampleTask("a", "sessA", base.Add(1*time.Hour)))
	mustAdd(t, s, sampleTask("c", "sessA", base.Add(3*time.Hour)))
	mustAdd(t, s, sampleTask("z", "sessB", base.Add(30*time.Minute)))

	if _, err := s.Cancel("sessA", "c"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got := s.List("sessA")
	if len(got) != 2 {
		t.Fatalf("List(sessA) len = %d, want 2 (cancelled excluded)", len(got))
	}
	// Sorted by NextRun: a (1h) before b (2h).
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("List order = [%s %s], want [a b]", got[0].ID, got[1].ID)
	}
	// Session isolation: sessB's task never appears under sessA.
	for _, task := range got {
		if task.SessionID != "sessA" {
			t.Fatalf("List(sessA) leaked task from %s", task.SessionID)
		}
	}
}

func TestPauseResumeCancelTransitions(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s, tick := fixedClock(base)
	mustAdd(t, s, sampleTask("t1", "s", base.Add(time.Hour)))

	*tick = 5
	paused, err := s.Pause("s", "t1")
	if err != nil || paused.State != TaskPaused {
		t.Fatalf("Pause = (%+v, %v), want state=paused", paused, err)
	}
	if !paused.UpdatedAt.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("UpdatedAt = %v, want clock tick", paused.UpdatedAt)
	}
	// Pause is idempotent.
	if again, err := s.Pause("s", "t1"); err != nil || again.State != TaskPaused {
		t.Fatalf("idempotent Pause = (%+v, %v)", again, err)
	}

	resumed, err := s.Resume("s", "t1")
	if err != nil || resumed.State != TaskActive {
		t.Fatalf("Resume = (%+v, %v), want state=active", resumed, err)
	}

	cancelled, err := s.Cancel("s", "t1")
	if err != nil || cancelled.State != TaskCancelled {
		t.Fatalf("Cancel = (%+v, %v), want state=cancelled", cancelled, err)
	}
	// Cancel is idempotent; pause/resume/update on a cancelled task are refused.
	if _, err := s.Cancel("s", "t1"); err != nil {
		t.Fatalf("idempotent Cancel err = %v", err)
	}
	if _, err := s.Pause("s", "t1"); !errors.Is(err, ErrTaskCancelled) {
		t.Fatalf("Pause cancelled err = %v, want ErrTaskCancelled", err)
	}
	if _, err := s.Resume("s", "t1"); !errors.Is(err, ErrTaskCancelled) {
		t.Fatalf("Resume cancelled err = %v, want ErrTaskCancelled", err)
	}
	prompt := "new"
	if _, err := s.Update("s", "t1", TaskUpdate{Prompt: &prompt}); !errors.Is(err, ErrTaskCancelled) {
		t.Fatalf("Update cancelled err = %v, want ErrTaskCancelled", err)
	}
}

func TestMutationsAreSessionScoped(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s := NewMemStore()
	mustAdd(t, s, sampleTask("t1", "owner", base.Add(time.Hour)))

	// A different session must not be able to touch owner's task.
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"cancel", func() error { _, err := s.Cancel("intruder", "t1"); return err }},
		{"pause", func() error { _, err := s.Pause("intruder", "t1"); return err }},
		{"resume", func() error { _, err := s.Resume("intruder", "t1"); return err }},
		{"get", func() error {
			if _, ok := s.Get("intruder", "t1"); ok {
				return errors.New("found")
			}
			return ErrTaskNotFound
		}},
	} {
		t.Run(op.name, func(t *testing.T) {
			if err := op.run(); !errors.Is(err, ErrTaskNotFound) {
				t.Fatalf("%s cross-session err = %v, want ErrTaskNotFound", op.name, err)
			}
		})
	}
	// The owner's task is untouched (still active).
	if got, _ := s.Get("owner", "t1"); got.State != TaskActive {
		t.Fatalf("owner task state = %q, want active", got.State)
	}
}

func TestUpdateAppliesAndValidates(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s := NewMemStore()
	mustAdd(t, s, sampleTask("t1", "s", base.Add(time.Hour)))

	newRun := base.Add(48 * time.Hour)
	newPrompt := "different prompt"
	newRec := "weekly"
	got, err := s.Update("s", "t1", TaskUpdate{Prompt: &newPrompt, NextRun: &newRun, Recurrence: &newRec})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Prompt != newPrompt || !got.NextRun.Equal(newRun) || got.Recurrence != newRec {
		t.Fatalf("Update applied wrong: %+v", got)
	}

	// No-op update (nothing supplied) is rejected.
	if _, err := s.Update("s", "t1", TaskUpdate{}); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("empty Update err = %v, want ErrInvalidTask", err)
	}
	// An update that empties the prompt is rejected (re-validation).
	empty := "   "
	if _, err := s.Update("s", "t1", TaskUpdate{Prompt: &empty}); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("blank-prompt Update err = %v, want ErrInvalidTask", err)
	}
	// A bad recurrence is rejected.
	bad := "fortnightly"
	if _, err := s.Update("s", "t1", TaskUpdate{Recurrence: &bad}); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("bad-recurrence Update err = %v, want ErrInvalidTask", err)
	}
}

func TestOperationsOnMissingTask(t *testing.T) {
	s := NewMemStore()
	prompt := "p"
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"cancel", func() error { _, err := s.Cancel("s", "nope"); return err }},
		{"pause", func() error { _, err := s.Pause("s", "nope"); return err }},
		{"resume", func() error { _, err := s.Resume("s", "nope"); return err }},
		{"update", func() error { _, err := s.Update("s", "nope", TaskUpdate{Prompt: &prompt}); return err }},
	} {
		t.Run(op.name, func(t *testing.T) {
			if err := op.run(); !errors.Is(err, ErrTaskNotFound) {
				t.Fatalf("%s missing err = %v, want ErrTaskNotFound", op.name, err)
			}
		})
	}
}

func TestApplyManageDispatch(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s := NewMemStore()
	mustAdd(t, s, sampleTask("t1", "s", base.Add(time.Hour)))
	mustAdd(t, s, sampleTask("t2", "s", base.Add(2*time.Hour)))

	// list_tasks returns the live tasks.
	got, err := ApplyManage(s, "s", ActionListTasks, ManagePayload{})
	if err != nil || len(got) != 2 {
		t.Fatalf("ApplyManage list = (%d tasks, %v), want 2", len(got), err)
	}

	// pause_task toggles state.
	got, err = ApplyManage(s, "s", ActionPauseTask, ManagePayload{TaskID: "t1"})
	if err != nil || len(got) != 1 || got[0].State != TaskPaused {
		t.Fatalf("ApplyManage pause = (%+v, %v)", got, err)
	}

	// update_task parses run_at and applies fields.
	newRun := base.Add(72 * time.Hour)
	got, err = ApplyManage(s, "s", ActionUpdateTask, ManagePayload{
		TaskID: "t2", Prompt: "edited", RunAt: newRun.Format(time.RFC3339), Recurrence: "hourly",
	})
	if err != nil {
		t.Fatalf("ApplyManage update: %v", err)
	}
	if got[0].Prompt != "edited" || !got[0].NextRun.Equal(newRun) || got[0].Recurrence != "hourly" {
		t.Fatalf("ApplyManage update applied wrong: %+v", got[0])
	}

	// cancel_task then confirm it drops out of list.
	if _, err := ApplyManage(s, "s", ActionCancelTask, ManagePayload{TaskID: "t1"}); err != nil {
		t.Fatalf("ApplyManage cancel: %v", err)
	}
	live, _ := ApplyManage(s, "s", ActionListTasks, ManagePayload{})
	for _, task := range live {
		if task.ID == "t1" {
			t.Fatal("cancelled task still listed")
		}
	}

	// A bad run_at is rejected.
	if _, err := ApplyManage(s, "s", ActionUpdateTask, ManagePayload{TaskID: "t2", RunAt: "tomorrow"}); !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("ApplyManage bad run_at err = %v, want ErrInvalidTask", err)
	}
	// An unknown action is rejected.
	if _, err := ApplyManage(s, "s", "frobnicate", ManagePayload{}); err == nil {
		t.Fatal("ApplyManage unknown action: want error")
	}
}

func TestIsManageAction(t *testing.T) {
	for _, a := range []string{ActionListTasks, ActionCancelTask, ActionPauseTask, ActionResumeTask, ActionUpdateTask} {
		if !IsManageAction(a) {
			t.Fatalf("IsManageAction(%q) = false, want true", a)
		}
	}
	for _, a := range []string{"", "schedule_task", "persona", "list"} {
		if IsManageAction(a) {
			t.Fatalf("IsManageAction(%q) = true, want false", a)
		}
	}
}

// TestManageActionNamesArePinned guards the cross-seam wire strings: the sandbox's
// task tools (which cannot import this package) emit these exact action names, so a
// rename here without the mirror is a silent break. The literals are the contract.
func TestManageActionNamesArePinned(t *testing.T) {
	pins := map[string]string{
		ActionListTasks:  "list_tasks",
		ActionCancelTask: "cancel_task",
		ActionPauseTask:  "pause_task",
		ActionResumeTask: "resume_task",
		ActionUpdateTask: "update_task",
	}
	for got, want := range pins {
		if got != want {
			t.Fatalf("action name = %q, want %q", got, want)
		}
	}
}

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	base := time.Date(2026, 6, 16, 9, 0, 0, 0, time.UTC)
	s := NewMemStore()
	mustAdd(t, s, sampleTask("t1", "s", base.Add(time.Hour)))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Pause("s", "t1")
			_ = s.List("s")
			_, _ = s.Resume("s", "t1")
			_, _ = s.Get("s", "t1")
		}()
	}
	wg.Wait()
}

func mustAdd(t *testing.T, s *MemStore, task Task) {
	t.Helper()
	if err := s.Add(task); err != nil {
		t.Fatalf("Add(%s): %v", task.ID, err)
	}
}
