package statesman

// EventBase is the only constraint the core runtime places on events. Every
// event type implements it; each generated machine narrows it to a sealed Event
// interface (an unexported marker method) that only that package's types satisfy.
type EventBase interface {
	// EventType returns the wire/descriptor name used to match transitions,
	// e.g. "SUBMIT" or "done.invoke.charge".
	EventType() string
}

// Never is an uninhabited event type: it declares an unexported marker no type
// can satisfy, so a value of type Never cannot be constructed. A ref parametrized
// with Never (a promise or observable actor) is therefore not sendable — Send
// cannot be called because no argument exists. See docs and decision 45.
type Never interface {
	EventBase
	isNever()
}
