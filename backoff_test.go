package statesman_test

import (
	"context"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

// TestBackoffActorFiresOnVirtualTime: the scheduled onDone is emitted only once
// the (virtual) clock advances past the computed delay, and the delay is read
// from the parent context.
func TestBackoffActorFiresOnVirtualTime(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	runner := statesman.BackoffActor[oCtx, oEvt](clk, ts,
		func(c oCtx) time.Duration { return time.Duration(c.Retries) * time.Second },
		func() oEvt { return oEvt{"done.invoke.backoff"} },
	)

	var got []oEvt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner(ctx, oCtx{Retries: 3}, "/m/backoff", func(e oEvt) { got = append(got, e) })

	ts.Advance(2 * time.Second) // before the 3s deadline
	if len(got) != 0 {
		t.Fatalf("fired early at 2s: %v", got)
	}
	ts.Advance(2 * time.Second) // now past 3s
	if len(got) != 1 || got[0].EventType() != "done.invoke.backoff" {
		t.Fatalf("got %v, want one done.invoke.backoff", got)
	}
}

// TestBackoffActorCancelSuppressesFire: cancelling the running invoke stops the
// timer, so advancing past the deadline emits nothing.
func TestBackoffActorCancelSuppressesFire(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	runner := statesman.BackoffActor[oCtx, oEvt](clk, ts,
		func(oCtx) time.Duration { return time.Second },
		func() oEvt { return oEvt{"done.invoke.backoff"} },
	)

	var got []oEvt
	ri := runner(context.Background(), oCtx{}, "/m/backoff", func(e oEvt) { got = append(got, e) })
	ri.Cancel()

	ts.Advance(5 * time.Second)
	if len(got) != 0 {
		t.Fatalf("fired after cancel: %v", got)
	}
}

// TestBackoffActorCtxCancelSuppressesEmit: even if the timer fires, a cancelled
// child ctx (state exited) suppresses the emit.
func TestBackoffActorCtxCancelSuppressesEmit(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	runner := statesman.BackoffActor[oCtx, oEvt](clk, ts,
		func(oCtx) time.Duration { return time.Second },
		func() oEvt { return oEvt{"done.invoke.backoff"} },
	)

	var got []oEvt
	ctx, cancel := context.WithCancel(context.Background())
	runner(ctx, oCtx{}, "/m/backoff", func(e oEvt) { got = append(got, e) })
	cancel() // state exited before the timer fires

	ts.Advance(5 * time.Second)
	if len(got) != 0 {
		t.Fatalf("emitted despite cancelled ctx: %v", got)
	}
}
