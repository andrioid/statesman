package statesman

import "sort"

// This file is the pure, side-effect-free core of the transition algorithm
// (docs/transition-algorithm.md §5–§7): the active-state configuration, LCCA /
// exit-set / entry-set computation, and transition selection with parallel
// preemption. Nothing here touches goroutines, time, appliers, or context —
// guards are abstracted behind a predicate so these functions are deterministic
// and unit-testable on a hand-built Definition. The microstep transaction that
// applies actions and accumulates context lives in microstep.go.

// configuration is the set of active state nodes (atomic leaves plus every
// ancestor up to the root, and all regions of active parallels).
type configuration struct {
	members map[*StateNode]struct{}
}

func newConfiguration() *configuration {
	return &configuration{members: make(map[*StateNode]struct{})}
}

func (c *configuration) add(n *StateNode)      { c.members[n] = struct{}{} }
func (c *configuration) remove(n *StateNode)   { delete(c.members, n) }
func (c *configuration) has(n *StateNode) bool { _, ok := c.members[n]; return ok }

// all returns every active node sorted by ascending document order.
func (c *configuration) all() []*StateNode {
	out := make([]*StateNode, 0, len(c.members))
	for n := range c.members {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocOrder < out[j].DocOrder })
	return out
}

// atomic returns active atomic/final leaves, sorted by document order.
func (c *configuration) atomic() []*StateNode {
	var out []*StateNode
	for _, n := range c.all() {
		if n.IsAtomic() {
			out = append(out, n)
		}
	}
	return out
}

// findLCCA returns the least common compound ancestor of states: the lowest
// ancestor that is a compound state or the root (parallel ancestors are never
// the LCCA). docs/transition-algorithm.md §6.2.
func findLCCA(states []*StateNode) *StateNode {
	if len(states) == 0 {
		return nil
	}
	for _, anc := range states[0].ProperAncestors() { // innermost first
		if anc.Kind != StateCompound && anc.Parent != nil {
			continue // only compound, or the root, qualifies
		}
		ok := true
		for _, s := range states[1:] {
			if !s.IsDescendant(anc) {
				ok = false
				break
			}
		}
		if ok {
			return anc
		}
	}
	return nil
}

// getTransitionDomain is the state to exit/re-enter for t: nil for a targetless
// (internal) transition, else the LCCA of source and targets. §6.2.
func getTransitionDomain(t *Transition) *StateNode {
	if len(t.Targets) == 0 {
		return nil
	}
	states := make([]*StateNode, 0, len(t.Targets)+1)
	states = append(states, t.Source)
	states = append(states, t.Targets...)
	return findLCCA(states)
}

// exitSetOf returns the active states a single transition would exit: the active
// proper descendants of its domain. §6.2.
func exitSetOf(cfg *configuration, t *Transition) map[*StateNode]struct{} {
	out := make(map[*StateNode]struct{})
	if len(t.Targets) == 0 {
		return out
	}
	dom := getTransitionDomain(t)
	for _, s := range cfg.all() {
		if s.IsDescendant(dom) {
			out[s] = struct{}{}
		}
	}
	return out
}

// computeExitSet returns all active states exited by the transition set, sorted
// reverse document order (innermost first), as the exit order. §6.3.
func computeExitSet(cfg *configuration, ts []*Transition) []*StateNode {
	set := make(map[*StateNode]struct{})
	for _, t := range ts {
		for s := range exitSetOf(cfg, t) {
			set[s] = struct{}{}
		}
	}
	out := make([]*StateNode, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocOrder > out[j].DocOrder })
	return out
}

// guardPredicate decides whether a transition's guard passes. It is supplied by
// the caller (the microstep wraps the generated evalGuard closure over the live
// context/event); selection itself stays pure.
type guardPredicate func(t *Transition) bool

// matchAtNode returns the first transition on s that fires for event, exact
// matches before the `*` catch-all (§6 of docs/schema-subset.md). Eventless
// transitions are never matched here.
func matchAtNode(s *StateNode, event string, pass guardPredicate) *Transition {
	evented := nodeEventedTransitions(s)
	for _, t := range evented {
		if !t.Eventless() && t.Event == event && pass(t) {
			return t
		}
	}
	for _, t := range evented {
		if !t.Eventless() && t.Event == "*" && pass(t) {
			return t
		}
	}
	return nil
}

// nodeEventedTransitions yields a node's evented edges for selection: its own
// `on`/`after` transitions followed by its invoke onDone/onError edges (which
// live under Invoke, keyed on done.invoke.<id> / error.invoke.<id>). Eventless
// `always` edges are handled separately and need no invoke fold.
func nodeEventedTransitions(s *StateNode) []*Transition {
	if len(s.Invokes) == 0 {
		return s.Transitions
	}
	out := make([]*Transition, 0, len(s.Transitions)+2*len(s.Invokes))
	out = append(out, s.Transitions...)
	for _, iv := range s.Invokes {
		out = append(out, iv.OnDone...)
		out = append(out, iv.OnError...)
	}
	return out
}

// selectTransitions selects the conflict-resolved transitions enabled by event:
// the first firing transition per atomic state, searched innermost-out. §5.2.
func selectTransitions(cfg *configuration, event string, pass guardPredicate) []*Transition {
	return selectBy(cfg, pass, func(s *StateNode) *Transition {
		return matchAtNode(s, event, pass)
	})
}

// selectEventlessTransitions selects conflict-resolved `always` transitions
// enabled in the current configuration. §5.1.
func selectEventlessTransitions(cfg *configuration, pass guardPredicate) []*Transition {
	return selectBy(cfg, pass, func(s *StateNode) *Transition {
		for _, t := range s.Transitions {
			if t.Eventless() && pass(t) {
				return t
			}
		}
		return nil
	})
}

// selectBy walks atomic states in document order, finds the first firing
// transition for each (self then ancestors), dedups, and resolves conflicts.
func selectBy(cfg *configuration, pass guardPredicate, at func(*StateNode) *Transition) []*Transition {
	seen := make(map[*Transition]struct{})
	var enabled []*Transition
	for _, st := range cfg.atomic() {
		for _, s := range append([]*StateNode{st}, st.ProperAncestors()...) {
			t := at(s)
			if t == nil {
				continue
			}
			if _, dup := seen[t]; !dup {
				seen[t] = struct{}{}
				enabled = append(enabled, t)
			}
			break // first match for this atomic state
		}
	}
	return removeConflictingTransitions(cfg, enabled)
}

// removeConflictingTransitions resolves parallel preemption: a deeper source
// preempts a shallower one, else the earlier document-order transition wins. §5.4.
func removeConflictingTransitions(cfg *configuration, enabled []*Transition) []*Transition {
	sort.SliceStable(enabled, func(i, j int) bool {
		if enabled[i].Source.DocOrder != enabled[j].Source.DocOrder {
			return enabled[i].Source.DocOrder < enabled[j].Source.DocOrder
		}
		return enabled[i].DocOrder < enabled[j].DocOrder
	})
	var filtered []*Transition
	for _, t1 := range enabled {
		es1 := exitSetOf(cfg, t1)
		preempted := false
		toRemove := make(map[*Transition]struct{})
		for _, t2 := range filtered {
			if intersects(es1, exitSetOf(cfg, t2)) {
				if t1.Source.IsDescendant(t2.Source) {
					toRemove[t2] = struct{}{}
				} else {
					preempted = true
					break
				}
			}
		}
		if preempted {
			continue
		}
		next := filtered[:0:0]
		for _, t2 := range filtered {
			if _, drop := toRemove[t2]; !drop {
				next = append(next, t2)
			}
		}
		filtered = append(next, t1)
	}
	return filtered
}

func intersects(a, b map[*StateNode]struct{}) bool {
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	for s := range small {
		if _, ok := large[s]; ok {
			return true
		}
	}
	return false
}

// computeEntrySet returns the states to enter for the transition set, the subset
// needing default (compound initial) entry, and resolves history. §7.1.
func computeEntrySet(ts []*Transition, hist map[StateID][]*StateNode) (toEnter, defaultEntry map[*StateNode]struct{}) {
	toEnter = make(map[*StateNode]struct{})
	defaultEntry = make(map[*StateNode]struct{})
	for _, t := range ts {
		for _, tgt := range t.Targets {
			addDescendantStatesToEnter(tgt, toEnter, defaultEntry, hist)
		}
		dom := getTransitionDomain(t)
		for _, tgt := range t.Targets {
			addAncestorStatesToEnter(tgt, dom, toEnter, defaultEntry, hist)
		}
	}
	return toEnter, defaultEntry
}

func addDescendantStatesToEnter(s *StateNode, toEnter, defaultEntry map[*StateNode]struct{}, hist map[StateID][]*StateNode) {
	if s.Kind == StateHistory {
		restore := hist[s.ID]
		if restore == nil {
			restore = s.HistoryTo
		}
		for _, hs := range restore {
			addDescendantStatesToEnter(hs, toEnter, defaultEntry, hist)
			addAncestorStatesToEnter(hs, s.Parent, toEnter, defaultEntry, hist)
		}
		return
	}
	toEnter[s] = struct{}{}
	switch s.Kind {
	case StateCompound:
		defaultEntry[s] = struct{}{}
		if s.Initial != nil {
			addDescendantStatesToEnter(s.Initial, toEnter, defaultEntry, hist)
			addAncestorStatesToEnter(s.Initial, s, toEnter, defaultEntry, hist)
		}
	case StateParallel:
		for _, child := range s.Children {
			if !hasDescendantInSet(child, toEnter) {
				addDescendantStatesToEnter(child, toEnter, defaultEntry, hist)
			}
		}
	}
}

func addAncestorStatesToEnter(s, ancestor *StateNode, toEnter, defaultEntry map[*StateNode]struct{}, hist map[StateID][]*StateNode) {
	for _, anc := range s.ProperAncestors() {
		if anc == ancestor {
			break
		}
		toEnter[anc] = struct{}{}
		if anc.Kind == StateParallel {
			for _, child := range anc.Children {
				if !hasDescendantInSet(child, toEnter) {
					addDescendantStatesToEnter(child, toEnter, defaultEntry, hist)
				}
			}
		}
	}
}

// hasDescendantInSet reports whether n or any descendant of n is already in set.
func hasDescendantInSet(n *StateNode, set map[*StateNode]struct{}) bool {
	if _, ok := set[n]; ok {
		return true
	}
	for _, c := range n.Children {
		if hasDescendantInSet(c, set) {
			return true
		}
	}
	return false
}

// orderedEntry returns the entry-set states sorted by ascending document order
// (ancestors before descendants), the entry order. §7.2.
func orderedEntry(set map[*StateNode]struct{}) []*StateNode {
	out := make([]*StateNode, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DocOrder < out[j].DocOrder })
	return out
}
