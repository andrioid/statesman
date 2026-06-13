// Package statesmantest provides deterministic test infrastructure for
// statesman machines: a virtual clock and timer service, and (later) the Sync
// scenario runner and FakeActor doubles.
package statesmantest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/andrioid/statesman"
)

// ManualClock is a virtual clock advanced explicitly by tests via the paired
// ManualTimerService.Advance. It satisfies statesman.Clock.
type ManualClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewManualClock returns a clock starting at start (or the Unix epoch if zero).
func NewManualClock(start time.Time) *ManualClock {
	if start.IsZero() {
		start = time.Unix(0, 0)
	}
	return &ManualClock{now: start}
}

func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *ManualClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// ManualTimerService fires scheduled callbacks deterministically when the
// virtual clock is advanced. It satisfies statesman.TimerService.
type ManualTimerService struct {
	mu     sync.Mutex
	clock  *ManualClock
	timers []*manualTimer
}

// NewManualTimerService returns a timer service bound to clock.
func NewManualTimerService(clock *ManualClock) *ManualTimerService {
	return &ManualTimerService{clock: clock}
}

func (s *ManualTimerService) Schedule(_ context.Context, at time.Time, fire func()) statesman.Timer {
	t := &manualTimer{deadline: at, fire: fire}
	s.mu.Lock()
	s.timers = append(s.timers, t)
	s.mu.Unlock()
	return t
}

// Advance moves virtual time forward by d and fires every timer now due, in
// deadline order. Timers are fired with the service unlocked so a fire callback
// may schedule further timers (e.g. a re-armed `after`) without deadlocking;
// those newly-armed timers fire on a subsequent Advance.
func (s *ManualTimerService) Advance(d time.Duration) {
	s.mu.Lock()
	now := s.clock.Now().Add(d)
	s.clock.set(now)
	snapshot := append([]*manualTimer(nil), s.timers...)
	s.mu.Unlock()

	var due []*manualTimer
	for _, t := range snapshot {
		if t.markFiredIfDue(now) {
			due = append(due, t)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].deadline.Before(due[j].deadline) })
	for _, t := range due {
		t.fire()
	}
}

type manualTimer struct {
	mu        sync.Mutex
	deadline  time.Time
	fire      func()
	cancelled bool
	fired     bool
}

func (t *manualTimer) markFiredIfDue(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancelled || t.fired || t.deadline.After(now) {
		return false
	}
	t.fired = true
	return true
}

func (t *manualTimer) Cancel() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fired || t.cancelled {
		return false
	}
	t.cancelled = true
	return true
}

func (t *manualTimer) Deadline() time.Time { return t.deadline }
