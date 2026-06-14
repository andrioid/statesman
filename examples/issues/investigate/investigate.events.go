// Package investigate is a statesman sub-machine invoked by the issues coordinator.
// It runs code analysis for one issue and leaves its findings in the terminal
// Context, which the parent reads from the investigate invoke's done payload
// (Snapshot[investigate.Context]).
//
// investigate.machine.json defines the structure; this file the typed event and
// context; investigate.behavior.go the behavior; investigate.machine.gen.go (from
// `statesman generate`) wires them. Number is seeded by the parent via the
// coordinator's InvestigateInput mapper.
package investigate

import "github.com/andrioid/statesman"

// Event is the sealed per-machine union; the analyse done/error events get their
// marker + EventType from investigate.machine.gen.go.
type Event interface {
	statesman.EventBase
	investigateEvent()
}

// ContextFields: Number is the issue to analyse (seeded by the parent); Findings
// is the output the parent reads from the terminal Context.
type ContextFields struct {
	Number   int
	Findings string
}
