package statesman

import "context"

// TransitionObserver is notified synchronously at the observer point of each
// committed-or-aborted microstep (before publish; docs/persistence-contract.md
// §1). Returning an error aborts the transition with full rollback and takes the
// actor terminal (decision 26). The durable layer registers as one of these.
//
// Observers run on the actor goroutine and MUST NOT call Send on the actor they
// observe — a synchronous self-Send deadlocks the loop. Emit side effects via
// ActionResult (the outbox) instead (decision 26; architecture §observer-induced
// deadlock).
type TransitionObserver[TCtx any, TEvt EventBase] interface {
	OnTransition(ctx context.Context, before, after Snapshot[TCtx], evt TEvt) error
}

// ActorObserver is notified of actor lifecycle edges. Non-blocking; the runtime
// calls these on the actor goroutine.
type ActorObserver interface {
	OnActorStart(addr ActorAddress)
	OnActorStop(addr ActorAddress, reason error)
}

// TimerObserver is notified when `after` timers are scheduled and fire.
type TimerObserver interface {
	OnTimerScheduled(t ScheduledTimer)
	OnTimerFired(t ScheduledTimer)
}
