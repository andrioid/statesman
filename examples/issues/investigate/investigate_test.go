package investigate

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
		return NewInvestigateMachine(Impl{}, o...)
	})
}

// A successful analysis lands in `done` with the agent's findings in context — the
// payload the parent reads from the invoke's done snapshot.
func TestInvestigateProducesFindings(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.RegisterInvoke("analyse", statesmantest.FakeActor[Context, Event](
		AnalyseDone{Output: AnalyseResult{Findings: "nil deref at x.go:10"}}))
	if err := s.Start(ctx, "inv-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	snap := s.Snapshot()
	if snap.Status != statesman.StatusDone || !active(snap, States.Done) {
		t.Fatalf("final = %v/%v, want done/Done", snap.ActiveStates, snap.Status)
	}
	if snap.Context.Findings != "nil deref at x.go:10" {
		t.Fatalf("Findings = %q", snap.Context.Findings)
	}
}

// An analyse error routes to `failed` (also a final state, distinguished by id).
func TestInvestigateFailsOnError(t *testing.T) {
	ctx := context.Background()
	s := newSync(t)
	s.M.RegisterInvoke("analyse", statesmantest.FakeActor[Context, Event](AnalyseError{}))
	if err := s.Start(ctx, "inv-2"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if snap := s.Snapshot(); !active(snap, States.Failed) {
		t.Fatalf("active = %v, want failed", snap.ActiveStates)
	}
}
