package statesman

// StateID is the dotted path identifying a state node, e.g. "processing.charging".
// Generated code exposes these as typed constants on the per-machine States var.
type StateID string

// ActorAddress is a hierarchical actor path anchored at a caller-supplied root,
// e.g. "/order-123/payment". Addresses are stable across restarts.
type ActorAddress string

// Child returns the address of a child actor named name under a.
func (a ActorAddress) Child(name string) ActorAddress {
	return ActorAddress(string(a) + "/" + name)
}

// ActorStatus is the lifecycle status of an actor. Done, Error, and Stopped are
// terminal; a terminal actor never restarts.
type ActorStatus uint8

const (
	StatusStarting ActorStatus = iota
	StatusRunning
	StatusDone
	StatusError
	StatusStopped
)

// Terminal reports whether the status is one an actor never leaves.
func (s ActorStatus) Terminal() bool {
	return s == StatusDone || s == StatusError || s == StatusStopped
}

func (s ActorStatus) String() string {
	switch s {
	case StatusStarting:
		return "starting"
	case StatusRunning:
		return "running"
	case StatusDone:
		return "done"
	case StatusError:
		return "error"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}
