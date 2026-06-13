package statesmantest

import (
	"context"
	"time"

	"github.com/andrioid/statesman"
)

// Sync wraps a Machine with a virtual clock + timer service for deterministic
// scenario tests: each step blocks until the actor settles, and time only moves
// on Advance (decision 53).
type Sync[TCtx any, TEvt statesman.EventBase] struct {
	M      *statesman.Machine[TCtx, TEvt]
	Clock  *ManualClock
	Timers *ManualTimerService
}

// NewSync builds a Machine with the virtual clock/timer injected. build is a thin
// closure over a generated constructor, e.g.:
//
//	NewSync(func(o ...statesman.Option) *statesman.Machine[Ctx, Event] {
//	    return order.NewOrderMachine(impl, o...)
//	})
func NewSync[TCtx any, TEvt statesman.EventBase](build func(opts ...statesman.Option) *statesman.Machine[TCtx, TEvt]) *Sync[TCtx, TEvt] {
	clk := NewManualClock(time.Unix(0, 0))
	ts := NewManualTimerService(clk)
	m := build(statesman.WithClock(clk), statesman.WithTimerService(ts))
	return &Sync[TCtx, TEvt]{M: m, Clock: clk, Timers: ts}
}

// Start starts the underlying machine at address and settles the initial entry.
func (s *Sync[TCtx, TEvt]) Start(ctx context.Context, address string) error {
	if err := s.M.Start(ctx, address); err != nil {
		return err
	}
	return s.M.Settle(ctx)
}

// SendAndSettle sends evt and blocks until the resulting macrostep settles.
func (s *Sync[TCtx, TEvt]) SendAndSettle(ctx context.Context, evt TEvt) error {
	if err := s.M.Send(ctx, evt); err != nil {
		return err
	}
	return s.M.Settle(ctx)
}

// Advance moves virtual time forward, firing due `after` timers, then settles.
func (s *Sync[TCtx, TEvt]) Advance(ctx context.Context, d time.Duration) error {
	s.Timers.Advance(d)
	return s.M.Settle(ctx)
}

// Snapshot returns the machine's current snapshot.
func (s *Sync[TCtx, TEvt]) Snapshot() statesman.Snapshot[TCtx] { return s.M.Snapshot() }
