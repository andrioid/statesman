package statesman

import "time"

// Snapshot is the immutable, typed view of an actor's state at a transition
// boundary. Serialization happens at the storage boundary; types are known again
// at the typed Restore callsite, so Context is the concrete type, not raw JSON.
type Snapshot[TCtx any] struct {
	MachineID    string
	Address      ActorAddress
	ActiveStates []StateID // atomic active states, typed (not raw strings)
	Context      TCtx
	PendingAfter []ScheduledTimer
	Children     []ChildRef
	Status       ActorStatus
	ErrorReason  error // non-nil iff Status == StatusError
	// Version is monotonic per actor, incremented by 1 on each completed
	// transition. The durable layer uses it for optimistic concurrency.
	Version int
}

// ChildRef is the serialized stand-in for a typed child actor ref held inside a
// persisted Context (reflection-driven default; see docs/persistence-contract.md).
type ChildRef struct {
	Address  ActorAddress
	TypeName string
}

// ScheduledTimer is the durable record of an armed `after` timer. On Restore the
// runtime re-arms each entry at its original deadline against the live Clock.
type ScheduledTimer struct {
	// StateID is the owning state; the timer is cancelled when that state exits.
	StateID StateID
	// Descriptor is the synthetic event descriptor the fire enqueues,
	// e.g. "xstate.after(5s)#retrying".
	Descriptor string
	Deadline   time.Time
}
