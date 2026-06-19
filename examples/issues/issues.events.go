// Package issues is a worked statesman example: a GitHub issue-triage coordinator
// that drives LLM agents invoked from shell. It collects an issue, classifies it,
// and — for bugs — investigates and fixes it via sub-machines, then summarises and
// syncs back to GitHub.
//
// collecting and syncing are network steps wrapped in the retry/timeout pattern
// (per-attempt + overall deadline + backoff, error and timeout funnelling into one
// retry fork). investigate and fix are invoked sub-machines (fromMachine), seeded
// from this machine's context. classify branches: only bugs are investigated/fixed.
//
// issues.machine.json defines the structure; this file the typed event and
// context; issues.behavior.go the behavior; issues.actors.go the shell adapters and
// sub-machine factories; issues.machine.gen.go (from `statesman generate`) wires
// them. Regenerate with `go generate ./...`.
package issues

import "github.com/andrioid/statesman"

// Event is the sealed per-machine union; every transition here is driven by a
// generated invoke done/error event (or an `after`/`always`), so there are no
// user-authored events — only the marker that seals the union.
type Event interface {
	statesman.EventBase
	issuesEvent()
}

// ContextFields accumulates the run's data: the issue identity and the artifacts
// each step produces. Number is the only required input (seed it before Start).
type ContextFields struct {
	Number    int
	Owner     string // resolved repo owner (env or git remote), saved on collect
	Repo      string // resolved repo name, saved on collect
	Title     string
	Body      string
	IssueID   string // GitHub node id, used as the comment subject on sync
	Category  string
	Findings  string
	Patch     string
	Summary   string
	Attempt   int    // collect/sync retry counter
	LastError string // most recent transient failure, for observability
}
