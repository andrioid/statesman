// Package fix is a statesman sub-machine invoked by the issues coordinator. It
// drives an edit -> test -> re-edit loop for one issue, bounded by MaxAttempts, and
// leaves the patch + outcome in its terminal Context for the parent to read.
//
// Number/Findings/MaxAttempts are seeded by the parent via the coordinator's
// FixInput mapper. The internal promise invokes (edit, test) carry onError but no
// timeout — a real deployment should add `after` edges so a hung agent/test is
// bounded; omitted here to keep the loop legible.
package fix

import "github.com/andrioid/statesman"

// Event is the sealed per-machine union; the edit/test done/error events get their
// marker + EventType from fix.machine.gen.go.
type Event interface {
	statesman.EventBase
	fixEvent()
}

// ContextFields: Number/Findings/MaxAttempts are seeded by the parent; Patch and
// Attempt are the running outputs the parent can read from the terminal Context.
type ContextFields struct {
	Number      int
	Findings    string
	Patch       string
	Attempt     int
	MaxAttempts int
}
