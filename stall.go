package statesman

import (
	"context"
	"time"
)

// StallReport describes a transition observer that held the actor goroutine past
// a StallObserver's threshold. Transition observers run synchronously at the
// commit point (decision 25/26): while one runs, the actor takes no further
// transitions and no subscriber receives the snapshot. A slow or deadlocked
// observer therefore stalls the entire machine, and StallReport surfaces that as
// a metric rather than an invisible latency cliff.
type StallReport struct {
	Observer string        // the StallObserver's Name
	Held     time.Duration // threshold that elapsed before the watchdog fired
	Version  int           // Snapshot.Version of the transition being observed
}

// StallObserver wraps a load-bearing TransitionObserver (e.g. a durable writer)
// and reports when its OnTransition does not return within Threshold. The
// watchdog is armed on the TimerService before the inner observer runs and
// cancelled when it returns, so OnStall fires even when the inner observer is
// still blocked — surfacing true deadlocks, not just post-hoc slowness.
//
// OnStall runs on the TimerService goroutine, concurrently with the still-running
// inner observer; keep it cheap and thread-safe (typically a metrics counter).
// The inner observer's return value — including an abort error (decision 26) — is
// passed through unchanged, so wrapping never alters transition semantics.
//
// Clock and Timers default to WallClock and a fresh InProcessTimerService; inject
// the statesmantest virtual pair for deterministic tests.
type StallObserver[TCtx any, TEvt EventBase] struct {
	Name      string
	Threshold time.Duration
	Inner     TransitionObserver[TCtx, TEvt]
	OnStall   func(StallReport)

	Clock  Clock
	Timers TimerService
}

// OnTransition arms the watchdog, delegates to the inner observer, then cancels
// the watchdog. It satisfies TransitionObserver so a StallObserver can be passed
// to AddObserver directly.
func (s *StallObserver[TCtx, TEvt]) OnTransition(ctx context.Context, before, after Snapshot[TCtx], evt TEvt) error {
	clock := s.Clock
	if clock == nil {
		clock = WallClock{}
	}
	timers := s.Timers
	if timers == nil {
		timers = NewInProcessTimerService(clock)
	}
	// Capture by value: the fire callback runs on another goroutine and must not
	// read fields that the actor could mutate.
	name, held, version := s.Name, s.Threshold, after.Version
	onStall := s.OnStall
	timer := timers.Schedule(ctx, clock.Now().Add(s.Threshold), func() {
		if onStall != nil {
			onStall(StallReport{Observer: name, Held: held, Version: version})
		}
	})
	err := s.Inner.OnTransition(ctx, before, after, evt)
	timer.Cancel()
	return err
}
