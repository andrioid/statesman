package schema

import (
	"strconv"
	"strings"

	"github.com/andrioid/statesman"
)

// resolveTargets turns every transition's raw `target` string and every history
// node's default `target` into the node(s) they name (§8 of schema-subset.md).
// It runs after the whole tree exists so absolute/relative/sibling lookups can
// see every node.
func (b *builder) resolveTargets() error {
	for _, n := range b.ordered {
		for _, t := range n.Transitions {
			if err := b.resolveTransition(t); err != nil {
				return err
			}
		}
		for _, iv := range n.Invokes {
			for _, t := range iv.OnDone {
				if err := b.resolveTransition(t); err != nil {
					return err
				}
			}
			for _, t := range iv.OnError {
				if err := b.resolveTransition(t); err != nil {
					return err
				}
			}
		}
		if n.Kind == statesman.StateHistory {
			dto := b.nodeDTO[n]
			if dto.hasTarget && dto.target != "" {
				node, err := b.resolveTarget(n, dto.target, dto.path+".target")
				if err != nil {
					return err
				}
				n.HistoryTo = []*statesman.StateNode{node}
			}
		}
	}
	return nil
}

func (b *builder) resolveTransition(t *statesman.Transition) error {
	spec := b.tspec[t]
	if !spec.has || spec.raw == "" {
		return nil // targetless => empty Targets (internal/action-only)
	}
	node, err := b.resolveTarget(t.Source, spec.raw, spec.path+".target")
	if err != nil {
		return err
	}
	t.Targets = []*statesman.StateNode{node}
	return nil
}

// resolveTarget implements the three accepted target forms:
//   - "#<id>"   absolute: the node whose Key (id-tail) or dotted ID equals <id>.
//   - ".<a>[.<b>…]" relative: a descendant of src reached by child keys.
//   - "<sibling>"   a sibling of src under the same parent.
//
// Anything unresolvable or ambiguous is a gate-2 error citing the target.
func (b *builder) resolveTarget(src *statesman.StateNode, raw, path string) (*statesman.StateNode, error) {
	switch {
	case strings.HasPrefix(raw, "#"):
		id := raw[1:]
		if id == "" {
			return nil, errf(path, "target %q is missing an id after '#' (rule: target-resolution)", raw)
		}
		cands := append([]*statesman.StateNode(nil), b.byKey[id]...)
		for _, n := range b.ordered {
			if string(n.ID) == id && !containsNode(cands, n) {
				cands = append(cands, n)
			}
		}
		switch len(cands) {
		case 0:
			return nil, errf(path, "target %q does not resolve to any state (rule: target-resolution)", raw)
		case 1:
			return cands[0], nil
		default:
			return nil, errf(path, "target %q is ambiguous: %d states share that id (rule: target-resolution)", raw, len(cands))
		}

	case strings.HasPrefix(raw, "."):
		cur := src
		for _, seg := range strings.Split(raw[1:], ".") {
			if seg == "" {
				return nil, errf(path, "target %q has an empty path segment (rule: target-resolution)", raw)
			}
			child := findChild(cur, seg)
			if child == nil {
				return nil, errf(path, "target %q does not resolve: %q is not a descendant of %q (rule: target-resolution)", raw, seg, src.Key)
			}
			cur = child
		}
		return cur, nil

	default:
		if src.Parent == nil {
			return nil, errf(path, "target %q cannot resolve: source state %q has no siblings (rule: target-resolution)", raw, src.Key)
		}
		child := findChild(src.Parent, raw)
		if child == nil {
			return nil, errf(path, "target %q does not resolve to a sibling of %q (rule: target-resolution)", raw, src.Key)
		}
		return child, nil
	}
}

func containsNode(set []*statesman.StateNode, n *statesman.StateNode) bool {
	for _, s := range set {
		if s == n {
			return true
		}
	}
	return false
}

// validateEventDescriptor enforces gate-2 rule 4: an `on` key must be an exact
// event or exactly "*"; partial/namespace wildcards (e.g. "error.*") are rejected.
func validateEventDescriptor(desc, path string) error {
	if desc == "*" {
		return nil
	}
	if strings.Contains(desc, "*") {
		return errf(path, "event descriptor %q: partial wildcards are not allowed, use an exact event or \"*\" (rule: event-descriptor-wildcard)", desc)
	}
	return nil
}

// parseDelay implements §7: an all-digits key is literal milliseconds; anything
// else is a symbolic delay name resolved at codegen.
func parseDelay(key, path string) (statesman.Delay, error) {
	if key == "" {
		return statesman.Delay{}, errf(path, "after delay key must be non-empty")
	}
	if isAllDigits(key) {
		n, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return statesman.Delay{}, errf(path, "after delay %q overflows int64", key)
		}
		return statesman.Delay{Millis: n}, nil
	}
	return statesman.Delay{Symbol: key}, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// assignCallsites enumerates action and guard callsites deterministically. It
// walks nodes in ascending DocOrder and, per node, assigns ids in the fixed
// order: entry actions, then each transition (guard then actions) in DocOrder,
// then exit actions, then each invoke's onDone then onError edges (guard then
// actions). Actions and guards are independent 0-based sequences.
func (b *builder) assignCallsites() (actionCount, guardCount int) {
	assignEdge := func(t *statesman.Transition) {
		if t.Guard != nil {
			t.Guard.Callsite = guardCount
			guardCount++
		}
		for i := range t.Actions {
			t.Actions[i].Callsite = actionCount
			actionCount++
		}
	}

	for _, n := range b.ordered {
		for i := range n.Entry {
			n.Entry[i].Callsite = actionCount
			actionCount++
		}
		for _, t := range n.Transitions {
			assignEdge(t)
		}
		for i := range n.Exit {
			n.Exit[i].Callsite = actionCount
			actionCount++
		}
		for _, iv := range n.Invokes {
			for _, t := range iv.OnDone {
				assignEdge(t)
			}
			for _, t := range iv.OnError {
				assignEdge(t)
			}
		}
	}
	return actionCount, guardCount
}
