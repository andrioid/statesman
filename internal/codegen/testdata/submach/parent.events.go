// Package submach is a codegen fixture exercising a machine invoke (fromMachine):
// the parent's `run` invoke targets the child sub-machine. It compiles standalone
// so the resolution pass can load it.
package submach

import "github.com/andrioid/statesman"

// Event is the sealed per-machine union; the run done/error events get their
// marker + EventType from parent.machine.gen.go.
type Event interface {
	statesman.EventBase
	parentEvent()
}

type ContextFields struct{}
