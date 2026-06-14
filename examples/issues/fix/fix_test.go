package fix

import (
	"context"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

func active(snap statesman.Snapshot[Context], id statesman.StateID) bool {
	for _, s := range snap.ActiveStates {
		if s == id {
			return true
		}
	}
	return false
}

func newSync(t *testing.T) *statesmantest.Sync[Context, Event] {
	t.Helper()
	return statesmantest.NewSync(func(o ...statesman.Option) *statesman.Machine[Context, Event] {
		return NewFixMachine(Impl{}, o...)
	})
}

// The edit->test loop re-edits once (tests fail then pass) and lands in `done`
// with the attempt counter advanced and the patch recorded.
func TestFixRetriesThenPasses(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 7, MaxAttempts: 2}})
	s.M.RegisterInvoke("edit", statesmantest.FakeActor[Context, Event](
		EditDone{Output: EditResult{Patch: "diff-1"}},
		EditDone{Output: EditResult{Patch: "diff-2"}}))
	s.M.RegisterInvoke("test", statesmantest.FakeActor[Context, Event](
		TestDone{Output: TestResult{Passed: false}}, // attempt 0 fails -> re-edit
		TestDone{Output: TestResult{Passed: true}})) // attempt 1 passes
	if err := s.Start(ctx, "fix-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Done) {
		t.Fatalf("final = %v/%v, want done/Done", snap.ActiveStates, snap.Status)
	}
	if snap.Context.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1 (one re-edit)", snap.Context.Attempt)
	}
	if snap.Context.Patch != "diff-2" {
		t.Fatalf("Patch = %q, want diff-2", snap.Context.Patch)
	}
}

// With a one-attempt budget and a failing test, the loop gives up in `failed`.
func TestFixExhaustsBudget(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 7, MaxAttempts: 1}})
	s.M.RegisterInvoke("edit", statesmantest.FakeActor[Context, Event](
		EditDone{Output: EditResult{Patch: "diff-1"}}))
	s.M.RegisterInvoke("test", statesmantest.FakeActor[Context, Event](
		TestDone{Output: TestResult{Passed: false}}))
	if err := s.Start(ctx, "fix-2"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if snap := s.Snapshot(); !active(snap, States.Failed) {
		t.Fatalf("active = %v, want failed", snap.ActiveStates)
	}
}
