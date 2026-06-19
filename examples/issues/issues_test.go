package issues

import (
	"context"
	"errors"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/issues/fix"
	"github.com/andrioid/statesman/examples/issues/investigate"
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
		return NewIssuesMachine(Impl{}, o...)
	})
}

// A bug runs the whole pipeline: collect -> classify(bug) -> investigate -> fix ->
// summarise -> sync -> finished, accumulating each step's artifact in context. All
// invokes (promises and the investigate/fix sub-machines) are faked, so the chain
// settles synchronously on Start.
func TestCoordinatorBugFlow(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 42}})
	s.M.RegisterInvoke("collect", statesmantest.FakeActor[Context, Event](
		CollectDone{Output: CollectResult{Title: "crash on save", Body: "panic..."}}))
	s.M.RegisterInvoke("classify", statesmantest.FakeActor[Context, Event](
		ClassifyDone{Output: ClassifyResult{Category: "bug"}}))
	s.M.RegisterInvoke("investigate", statesmantest.FakeActor[Context, Event](
		InvestigateDone{Output: investigate.Context{ContextFields: investigate.ContextFields{Findings: "nil deref"}}}))
	s.M.RegisterInvoke("fix", statesmantest.FakeActor[Context, Event](
		FixDone{Output: fix.Context{ContextFields: fix.ContextFields{Patch: "the-diff"}}}))
	s.M.RegisterInvoke("summarise", statesmantest.FakeActor[Context, Event](
		SummariseDone{Output: SummariseResult{Comment: "fixed in #99"}}))
	s.M.RegisterInvoke("sync", statesmantest.FakeActor[Context, Event](
		SyncDone{Output: SyncResult{Posted: true}}))

	if err := s.Start(ctx, "issue-42"); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Finished) {
		t.Fatalf("final = %v/%v, want finished/Done", snap.ActiveStates, snap.Status)
	}
	c := snap.Context
	if c.Category != "bug" || c.Findings != "nil deref" || c.Patch != "the-diff" || c.Summary != "fixed in #99" {
		t.Fatalf("context not fully populated through the pipeline: %+v", c)
	}
}

// A non-bug skips investigate/fix: classify routes straight to summarising, so the
// faked investigate/fix outcomes never run and Findings/Patch stay empty.
func TestCoordinatorNonBugSkipsFixing(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 7}})
	s.M.RegisterInvoke("collect", statesmantest.FakeActor[Context, Event](
		CollectDone{Output: CollectResult{Title: "how do I?", Body: "question"}}))
	s.M.RegisterInvoke("classify", statesmantest.FakeActor[Context, Event](
		ClassifyDone{Output: ClassifyResult{Category: "question"}}))
	// Registered but must never fire (the branch skips them).
	s.M.RegisterInvoke("investigate", statesmantest.FakeActor[Context, Event](
		InvestigateDone{Output: investigate.Context{ContextFields: investigate.ContextFields{Findings: "SHOULD NOT APPEAR"}}}))
	s.M.RegisterInvoke("fix", statesmantest.FakeActor[Context, Event](
		FixDone{Output: fix.Context{ContextFields: fix.ContextFields{Patch: "SHOULD NOT APPEAR"}}}))
	s.M.RegisterInvoke("summarise", statesmantest.FakeActor[Context, Event](
		SummariseDone{Output: SummariseResult{Comment: "closing as question"}}))
	s.M.RegisterInvoke("sync", statesmantest.FakeActor[Context, Event](
		SyncDone{Output: SyncResult{Posted: true}}))

	if err := s.Start(ctx, "issue-7"); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Finished) {
		t.Fatalf("final = %v/%v, want finished/Done", snap.ActiveStates, snap.Status)
	}
	if c := snap.Context; c.Findings != "" || c.Patch != "" {
		t.Fatalf("investigate/fix should have been skipped, got Findings=%q Patch=%q", c.Findings, c.Patch)
	}
	if snap.Context.Summary != "closing as question" {
		t.Fatalf("Summary = %q", snap.Context.Summary)
	}
}

// A transient collect failure retries: the error edge's IsTransient override sends
// it to backoff; advancing the backoff re-invokes collect, which succeeds. The
// restart is visible in the snapshot, the attempt counter advanced, and the failure
// recorded.
func TestCoordinatorCollectRetry(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 9}})
	s.M.RegisterInvoke("collect", statesmantest.FakeActor[Context, Event](
		CollectError{Err: context.DeadlineExceeded},                // attempt 1: transient
		CollectDone{Output: CollectResult{Title: "t", Body: "b"}})) // attempt 2: ok
	s.M.RegisterInvoke("classify", statesmantest.FakeActor[Context, Event](
		ClassifyDone{Output: ClassifyResult{Category: "question"}}))
	s.M.RegisterInvoke("summarise", statesmantest.FakeActor[Context, Event](
		SummariseDone{Output: SummariseResult{Comment: "done"}}))
	s.M.RegisterInvoke("sync", statesmantest.FakeActor[Context, Event](
		SyncDone{Output: SyncResult{Posted: true}}))

	if err := s.Start(ctx, "issue-9"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if snap := s.Snapshot(); !active(snap, States.CollectingBackingOff) {
		t.Fatalf("after transient fail = %v, want collecting.backingOff", snap.ActiveStates)
	}

	if err := s.Advance(ctx, CollectBackoff); err != nil {
		t.Fatalf("advance: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Finished) {
		t.Fatalf("after retry = %v/%v, want finished/Done", snap.ActiveStates, snap.Status)
	}
	if snap.Context.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1", snap.Context.Attempt)
	}
	if snap.Context.LastError == "" {
		t.Fatalf("LastError should record the transient failure")
	}
	if snap.InvokeRestarts["collect"] != 1 {
		t.Fatalf("collect restarts = %d, want 1", snap.InvokeRestarts["collect"])
	}
}

// A non-transient collect failure does not retry: the error edge routes straight
// to the terminal `failed` state, and that edge records the cause so an operator
// (or the issuesctl dry-run) can see why the run failed.
func TestCoordinatorCollectFailRecordsError(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.SetInitialContext(Context{ContextFields: ContextFields{Number: 13}})
	s.M.RegisterInvoke("collect", statesmantest.FakeActor[Context, Event](
		CollectError{Err: errors.New("could not resolve to an Issue with the number of 13")}))

	if err := s.Start(ctx, "issue-13"); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Failed) {
		t.Fatalf("final = %v/%v, want failed/Done", snap.ActiveStates, snap.Status)
	}
	if snap.Context.LastError == "" {
		t.Fatal("terminal collect failure must record LastError")
	}
}
