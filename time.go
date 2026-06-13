package statesman

import (
	"context"
	"sync"
	"time"
)

// Clock sources the current time. Pluggable so tests can use virtual time.
type Clock interface {
	Now() time.Time
}

// TimerService schedules a one-shot callback at an absolute time. The fire
// callback runs on the service's own goroutine (or a fresh one); it must be
// cheap and non-blocking — typically it just Sends an event to a mailbox.
type TimerService interface {
	Schedule(ctx context.Context, at time.Time, fire func()) Timer
}

// Timer is a scheduled, cancellable fire. Cancel mirrors time.Timer.Stop:
// it returns false if the timer already fired (its event is in flight).
type Timer interface {
	Cancel() bool
	Deadline() time.Time
}

// WallClock is the production Clock backed by the OS clock.
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now() }

// InProcessTimerService is the default TimerService backed by time.AfterFunc.
// Timers do not survive a crash; on Restore the runtime re-arms PendingAfter
// entries. The ctx argument is accepted for interface symmetry with DB-backed
// services; in-process relies on the actor loop calling Timer.Cancel on state
// exit and shutdown (decision 52).
type InProcessTimerService struct {
	clock Clock
}

// NewInProcessTimerService returns a TimerService using clock for deadlines
// (defaults to WallClock when clock is nil).
func NewInProcessTimerService(clock Clock) *InProcessTimerService {
	if clock == nil {
		clock = WallClock{}
	}
	return &InProcessTimerService{clock: clock}
}

func (s *InProcessTimerService) Schedule(_ context.Context, at time.Time, fire func()) Timer {
	d := at.Sub(s.clock.Now())
	if d < 0 {
		d = 0
	}
	t := &inProcTimer{deadline: at}
	t.timer = time.AfterFunc(d, fire)
	return t
}

type inProcTimer struct {
	mu       sync.Mutex
	timer    *time.Timer
	deadline time.Time
}

func (t *inProcTimer) Cancel() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer == nil {
		return false
	}
	return t.timer.Stop()
}

func (t *inProcTimer) Deadline() time.Time { return t.deadline }
