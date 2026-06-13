package statesman_test

import (
	"context"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

// TestAdaptersOrderRetryWithFakeActor drives the full §11 retry loop with real
// invoke spawning: charge is a FakeActor scripting fail-then-succeed, inventory a
// fake callback recording the SendTo from validateForm.
func TestAdaptersOrderRetryWithFakeActor(t *testing.T) {
	ctx := context.Background()
	rec := &statesmantest.CommandRecorder[oEvt]{}
	s := newOrderSync(t)
	s.M.RegisterInvoke("charge", statesmantest.FakeActor[oCtx, oEvt](
		oEvt{"error.invoke.charge"}, // attempt 1 fails
		oEvt{"done.invoke.charge"},  // attempt 2 succeeds
	))
	s.M.RegisterInvoke("inventory", statesmantest.FakeCallback[oCtx, oEvt, oEvt](rec))

	if err := s.Start(ctx, "order-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !hasState(s.Snapshot(), "idle") {
		t.Fatalf("initial = %v, want idle", s.Snapshot().ActiveStates)
	}

	// SUBMIT enters charging, spawns charge (attempt 1 fails synchronously), and
	// the guarded onError edge lands in retrying with Retries incremented.
	mustSend(t, s, "SUBMIT")
	if !hasState(s.Snapshot(), "retrying") {
		t.Fatalf("after SUBMIT+fail = %v, want retrying", s.Snapshot().ActiveStates)
	}
	if s.Snapshot().Context.Retries != 1 {
		t.Fatalf("Retries = %d, want 1", s.Snapshot().Context.Retries)
	}
	// validateForm forwarded the SKUs to the inventory callback.
	cmds := rec.Commands()
	if len(cmds) != 1 || cmds[0].EventType() != "WATCH_SKUS" {
		t.Fatalf("inventory commands = %v, want one WATCH_SKUS", cmds)
	}

	// Backoff elapses: re-enter charging, attempt 2 succeeds -> confirming.
	if err := s.Advance(ctx, 5*time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !hasState(s.Snapshot(), "confirming") {
		t.Fatalf("after backoff+success = %v, want confirming", s.Snapshot().ActiveStates)
	}

	mustSend(t, s, "CONFIRM")
	if snap := s.Snapshot(); snap.Status != statesman.StatusDone || !hasState(snap, "done") {
		t.Fatalf("final = %v/%v, want done/Done", snap.ActiveStates, snap.Status)
	}
	_ = s.M.Close()
}

// TestAdaptersRealPromise exercises the real PromiseActor goroutine path (async
// completion bridged to done.invoke.charge), on the wall clock.
func TestAdaptersRealPromise(t *testing.T) {
	ctx := context.Background()
	def := loadOrder(t)
	actType, guardType := callsiteMaps(def)
	apply, guard := orderAppliers(actType, guardType)
	m := statesman.NewMachine[oCtx, oEvt](def, oCtx{}, apply, guard)

	type chargeIn struct{ amount int }
	type chargeOut struct{ id string }
	m.RegisterInvoke("charge", statesman.PromiseActor[oCtx, oEvt, chargeIn, chargeOut](
		func(_ context.Context, _ chargeIn) (chargeOut, error) { return chargeOut{id: "ch_1"}, nil },
		func(oCtx) chargeIn { return chargeIn{amount: 100} },
		func(chargeOut) oEvt { return oEvt{"done.invoke.charge"} },
		func(error) oEvt { return oEvt{"error.invoke.charge"} },
	))
	// inventory: a no-op real callback that blocks until cancelled.
	m.RegisterInvoke("inventory", statesman.CallbackActor[oCtx, oEvt, oEvt](
		func(ctx context.Context, _ func(oEvt), _ <-chan oEvt) error { <-ctx.Done(); return nil },
		nil, nil,
	))

	if err := m.Start(ctx, "order-real"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := m.Send(ctx, oEvt{"SUBMIT"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// The promise resolves asynchronously; wait for the bridged done event to
	// drive charging -> confirming.
	waitForState(t, m, "confirming", 2*time.Second)
	_ = m.Close()
}

func waitForState(t *testing.T, m *statesman.Machine[oCtx, oEvt], id statesman.StateID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if hasState(m.Snapshot(), id) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("state %q not reached within %v; active = %v", id, timeout, m.Snapshot().ActiveStates)
}
