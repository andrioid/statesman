package codegen

import (
	"fmt"

	"github.com/andrioid/statesman"
)

// Warnings returns advisory build-time diagnostics for a resolved machine. They
// are non-fatal (unlike Unresolved, which fails generate); the CLI prints them to
// stderr so they surface in `go generate` output.
//
// Current lint: a promise invoke whose state (or any ancestor) has no `after`
// timeout and whose invoke has no onError. A promise that fails or hangs then has
// no automatic edge out — the actor wedges silently. onDone does not count: it
// only fires on success, so it cannot rescue a failed or hung call.
func Warnings(res *Resolution, def *statesman.Definition) []string {
	var out []string
	var visit func(n *statesman.StateNode)
	visit = func(n *statesman.StateNode) {
		for _, iv := range n.Invokes {
			a := res.Actors[iv.Src]
			if a == nil || a.Kind != AdapterPromise {
				continue
			}
			if len(iv.OnError) > 0 || hasAfterSelfOrAncestor(n) {
				continue
			}
			out = append(out, fmt.Sprintf(
				"state %q: promise invoke %q has no onError and no after timeout; a failed or hung call cannot exit and will stall the actor",
				n.ID, iv.ID))
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(def.Root)
	return out
}

// hasAfterSelfOrAncestor reports whether n or any ancestor up to the root has an
// `after` transition — i.e. a timeout that would exit the invoking state and
// abort its invoke. An ancestor's `after` (an overall deadline wrapping a retry
// loop) rescues a hung child, so it suppresses the warning too.
func hasAfterSelfOrAncestor(n *statesman.StateNode) bool {
	if nodeHasAfter(n) {
		return true
	}
	for _, anc := range n.ProperAncestors() {
		if nodeHasAfter(anc) {
			return true
		}
	}
	return false
}

func nodeHasAfter(n *statesman.StateNode) bool {
	for _, t := range n.Transitions {
		if t.IsAfter {
			return true
		}
	}
	return false
}
