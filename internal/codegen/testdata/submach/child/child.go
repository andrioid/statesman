// Package child is a minimal sub-machine fixture: just the typed surface a parent
// references through a machine invoke (Context, Event). The codegen test only
// type-checks the parent against this surface, so no real machine is needed.
package child

import "github.com/andrioid/statesman"

type Event interface {
	statesman.EventBase
	childEvent()
}

type Done struct{}

func (Done) childEvent()       {}
func (Done) EventType() string { return "done" }

type ContextFields struct{ Result string }

type Context struct{ ContextFields }
