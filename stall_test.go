package statesman_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

type stallEvt struct{}

func (stallEvt) EventType() string { return "x" }

// stallInner is a programmable TransitionObserver: fn runs inside OnTransition
// (used to advance virtual time across the watchdog deadline) and err is the
// returned result (used to assert passthrough).
type stallInner struct {
	fn  func()
	err error
}

func (i stallInner) OnTransition(context.Context, statesman.Snapshot[struct{}], statesman.Snapshot[struct{}], stallEvt) error {
	if i.fn != nil {
		i.fn()
	}
	return i.err
}

func newStallObserver(clk *statesmantest.ManualClock, ts *statesmantest.ManualTimerService, inner stallInner, onStall func(statesman.StallReport)) *statesman.StallObserver[struct{}, stallEvt] {
	return &statesman.StallObserver[struct{}, stallEvt]{
		Name:      "durable",
		Threshold: time.Second,
		Inner:     inner,
		OnStall:   onStall,
		Clock:     clk,
		Timers:    ts,
	}
}

func TestStallObserverFiresWhenInnerExceedsThreshold(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	var got statesman.StallReport
	var calls int
	// The inner observer "works" long enough to cross the 1s deadline.
	inner := stallInner{fn: func() { ts.Advance(2 * time.Second) }}
	so := newStallObserver(clk, ts, inner, func(r statesman.StallReport) {
		calls++
		got = r
	})

	after := statesman.Snapshot[struct{}]{Version: 7}
	if err := so.OnTransition(context.Background(), statesman.Snapshot[struct{}]{}, after, stallEvt{}); err != nil {
		t.Fatalf("OnTransition err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("OnStall calls = %d, want 1", calls)
	}
	if got.Observer != "durable" || got.Held != time.Second || got.Version != 7 {
		t.Fatalf("report = %+v, want {durable 1s 7}", got)
	}
}

func TestStallObserverSilentWhenInnerFast(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	var calls int
	so := newStallObserver(clk, ts, stallInner{}, func(statesman.StallReport) { calls++ })

	if err := so.OnTransition(context.Background(), statesman.Snapshot[struct{}]{}, statesman.Snapshot[struct{}]{Version: 3}, stallEvt{}); err != nil {
		t.Fatalf("OnTransition err = %v", err)
	}
	// The watchdog was cancelled when the fast inner returned; advancing past the
	// deadline must not fire it.
	ts.Advance(5 * time.Second)
	if calls != 0 {
		t.Fatalf("OnStall calls = %d, want 0 (watchdog cancelled)", calls)
	}
}

func TestStallObserverPassesInnerError(t *testing.T) {
	clk := statesmantest.NewManualClock(time.Unix(0, 0))
	ts := statesmantest.NewManualTimerService(clk)
	sentinel := errors.New("observer abort")
	so := newStallObserver(clk, ts, stallInner{err: sentinel}, nil)

	err := so.OnTransition(context.Background(), statesman.Snapshot[struct{}]{}, statesman.Snapshot[struct{}]{}, stallEvt{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel passthrough", err)
	}
}
