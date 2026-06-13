package orderpkg

import (
	"context"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

// TestGeneratedOrderRetry drives the GENERATED order machine through the same
// fail->retry->backoff->success scenario as the hand-wired Phase-4 test, proving
// the codegen output is behaviorally equivalent.
func TestGeneratedOrderRetry(t *testing.T) {
	ctx := context.Background()
	rec := &statesmantest.CommandRecorder[InventoryCommand]{}
	s := statesmantest.NewSync(func(o ...statesman.Option) *statesman.Machine[Context, Event] {
		return NewOrderMachine(orderImpl{}, o...)
	})
	// Override the real adapters with scenario doubles.
	s.M.RegisterInvoke("charge", statesmantest.FakeActor[Context, Event](
		ChargeError{}, // attempt 1 fails
		ChargeDone{Output: ChargeResult{ID: "ch_1"}}, // attempt 2 succeeds
	))
	s.M.RegisterInvoke("inventory", statesmantest.FakeCallback[Context, Event, InventoryCommand](rec))

	if err := s.Start(ctx, "order-1"); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := s.SendAndSettle(ctx, Submit{Form: FormData{SKUs: []string{"sku-1"}}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !active(s.Snapshot(), States.Retrying) {
		t.Fatalf("after submit+fail = %v, want retrying", s.Snapshot().ActiveStates)
	}
	if s.Snapshot().Context.Retries != 1 {
		t.Fatalf("Retries = %d, want 1", s.Snapshot().Context.Retries)
	}
	if cmds := rec.Commands(); len(cmds) != 1 || cmds[0].EventType() != "WATCH_SKUS" {
		t.Fatalf("inventory commands = %v, want one WATCH_SKUS", cmds)
	}

	if err := s.Advance(ctx, 5*time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !active(s.Snapshot(), States.Confirming) {
		t.Fatalf("after backoff+success = %v, want confirming", s.Snapshot().ActiveStates)
	}

	if err := s.SendAndSettle(ctx, Confirm{}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if snap := s.Snapshot(); snap.Status != statesman.StatusDone {
		t.Fatalf("final status = %v, want Done", snap.Status)
	}
	_ = s.M.Close()
}

func active(snap statesman.Snapshot[Context], id statesman.StateID) bool {
	for _, s := range snap.ActiveStates {
		if s == id {
			return true
		}
	}
	return false
}
