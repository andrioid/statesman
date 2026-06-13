// Package statesman is a Go statechart runtime for long-running backend
// workflows: it consumes Stately-compatible machine.json and runs it as an
// actor tree with first-class persistence hooks.
//
// A machine is authored as machine.json (a Stately/SCXML subset), loaded by the
// schema package into a Definition, and run as a Machine: a single-goroutine
// actor whose state is published through a lock-free Snapshot. The statesman CLI
// (cmd/statesman) generates a typed facade — States constants, event types, the
// Implementations behavior interface, and a NewXxxMachine constructor — from the
// JSON plus the package's own Go types, so action/guard signatures carry concrete
// event and context types rather than any.
//
// See README.md for a quickstart, statesman-architecture.md for design and
// patterns, docs/ for the normative transition, schema, and persistence specs,
// and DECISIONS.md for the decision log.
package statesman
