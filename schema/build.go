package schema

import (
	"fmt"

	"github.com/andrioid/statesman"
)

// targetSpec records the raw `target` string of a transition so the resolve
// pass (resolve.go) can turn it into a node once the whole tree exists.
type targetSpec struct {
	has  bool
	raw  string
	path string
}

type builder struct {
	nodeDTO map[*statesman.StateNode]*dtoState
	tspec   map[*statesman.Transition]targetSpec
	byKey   map[string][]*statesman.StateNode
	ordered []*statesman.StateNode // pre-order, ascending DocOrder
}

// build assembles a validated *statesman.Definition from the parsed dto tree.
func build(root *dtoState) (*statesman.Definition, error) {
	if !root.hasID || root.id == "" {
		return nil, errf("$.id", "machine root requires a non-empty \"id\"")
	}
	b := &builder{
		nodeDTO: map[*statesman.StateNode]*dtoState{},
		tspec:   map[*statesman.Transition]targetSpec{},
		byKey:   map[string][]*statesman.StateNode{},
	}

	rootNode, err := b.buildNode(root, root.id, "", nil, true)
	if err != nil {
		return nil, err
	}
	b.assignDocOrder(rootNode)
	if err := b.buildContents(); err != nil {
		return nil, err
	}
	if err := b.resolveTargets(); err != nil {
		return nil, err
	}
	actionCount, guardCount := b.assignCallsites()
	return statesman.NewDefinition(root.id, rootNode, actionCount, guardCount), nil
}

// buildNode builds the node shell (Key, ID, Kind, Parent, Children) recursively.
// The root's Key is the machine id and its ID is empty; descendants take the
// dotted path. Contents (transitions, actions, initial, …) are filled later.
func (b *builder) buildNode(dto *dtoState, key, parentID string, parent *statesman.StateNode, root bool) (*statesman.StateNode, error) {
	if !root && dto.hasID && dto.id != key {
		return nil, errf(dto.path+".id", "id %q must equal the state key %q (rule: id-equals-key)", dto.id, key)
	}

	var id statesman.StateID
	switch {
	case root:
		id = "" // root ID is empty; the machine id is the root Key.
	case parentID == "":
		id = statesman.StateID(key)
	default:
		id = statesman.StateID(parentID + "." + key)
	}

	kind, err := kindOf(dto)
	if err != nil {
		return nil, err
	}
	n := &statesman.StateNode{ID: id, Key: key, Kind: kind, Parent: parent}
	b.nodeDTO[n] = dto
	b.byKey[key] = append(b.byKey[key], n)

	for _, ch := range dto.states {
		cn, err := b.buildNode(ch.state, ch.key, string(id), n, false)
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, cn)
	}
	return n, nil
}

func kindOf(dto *dtoState) (statesman.StateKind, error) {
	if dto.hasType {
		switch dto.typ {
		case "parallel":
			return statesman.StateParallel, nil
		case "final":
			return statesman.StateFinal, nil
		case "history":
			return statesman.StateHistory, nil
		default:
			return 0, errf(dto.path+".type", "unknown state type %q (want parallel|history|final)", dto.typ)
		}
	}
	if dto.hasStates && len(dto.states) > 0 {
		return statesman.StateCompound, nil
	}
	return statesman.StateAtomic, nil
}

// assignDocOrder numbers nodes by depth-first pre-order (root = 0) and records
// the ordered slice used by every later pass.
func (b *builder) assignDocOrder(root *statesman.StateNode) {
	i := 0
	var walk func(n *statesman.StateNode)
	walk = func(n *statesman.StateNode) {
		n.DocOrder = i
		i++
		b.ordered = append(b.ordered, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// buildContents fills entry/exit, initial, history kind, transitions, and
// invokes for every node, applying the gate-2 constraints that do not need
// resolved targets.
func (b *builder) buildContents() error {
	for _, n := range b.ordered {
		dto := b.nodeDTO[n]

		entry, err := b.actions(dto.entry)
		if err != nil {
			return err
		}
		n.Entry = entry
		exit, err := b.actions(dto.exit)
		if err != nil {
			return err
		}
		n.Exit = exit

		if n.Kind == statesman.StateCompound {
			if !dto.hasInitial || dto.initial == "" {
				return errf(dto.path, "compound state requires \"initial\" (rule: compound-requires-initial)")
			}
			child := findChild(n, dto.initial)
			if child == nil {
				return errf(dto.path+".initial", "initial %q does not name a child state (rule: compound-requires-initial)", dto.initial)
			}
			n.Initial = child
		}

		if n.Kind == statesman.StateHistory {
			if n.Parent == nil || (n.Parent.Kind != statesman.StateCompound && n.Parent.Kind != statesman.StateParallel) {
				return errf(dto.path, "history state must be a child of a compound or parallel state (rule: history-parent)")
			}
			switch {
			case !dto.hasHistory:
				n.HistoryKind = statesman.HistoryShallow
			case dto.history == "shallow":
				n.HistoryKind = statesman.HistoryShallow
			case dto.history == "deep":
				n.HistoryKind = statesman.HistoryDeep
			default:
				return errf(dto.path+".history", "history must be \"shallow\" or \"deep\", got %q", dto.history)
			}
		} else if dto.hasHistory {
			return errf(dto.path+".history", "\"history\" is only valid on history states (rule: history-on-history)")
		}

		trs, err := b.transitions(n, dto)
		if err != nil {
			return err
		}
		n.Transitions = trs

		ivs, err := b.invokes(n, dto)
		if err != nil {
			return err
		}
		n.Invokes = ivs
	}
	return nil
}

// transitions concatenates a node's edges in the fixed order on -> after ->
// always, assigning ascending DocOrder within the node.
func (b *builder) transitions(n *statesman.StateNode, dto *dtoState) ([]*statesman.Transition, error) {
	var out []*statesman.Transition
	doc := 0

	for _, ev := range dto.on {
		if err := validateEventDescriptor(ev.desc, ev.path); err != nil {
			return nil, err
		}
		for _, tr := range ev.transitions {
			t, err := b.transition(n, tr, ev.desc, false, statesman.Delay{}, doc)
			if err != nil {
				return nil, err
			}
			out = append(out, t)
			doc++
		}
	}

	for _, ev := range dto.after {
		delay, err := parseDelay(ev.desc, ev.path)
		if err != nil {
			return nil, err
		}
		event := fmt.Sprintf("xstate.after(%s)#%s", ev.desc, n.ID)
		for _, tr := range ev.transitions {
			t, err := b.transition(n, tr, event, true, delay, doc)
			if err != nil {
				return nil, err
			}
			out = append(out, t)
			doc++
		}
	}

	for _, tr := range dto.always {
		t, err := b.transition(n, tr, "", false, statesman.Delay{}, doc)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
		doc++
	}

	return out, nil
}

func (b *builder) transition(src *statesman.StateNode, dto dtoTransition, event string, isAfter bool, delay statesman.Delay, doc int) (*statesman.Transition, error) {
	guard, err := b.guard(dto.guard)
	if err != nil {
		return nil, err
	}
	acts, err := b.actions(dto.actions)
	if err != nil {
		return nil, err
	}
	t := &statesman.Transition{
		Source:   src,
		Event:    event,
		Guard:    guard,
		Actions:  acts,
		DocOrder: doc,
		IsAfter:  isAfter,
		Delay:    delay,
	}
	b.tspec[t] = targetSpec{has: dto.hasTarget, raw: dto.target, path: dto.path}
	return t, nil
}

// invokes builds a node's invoked actors with their onDone/onError edges. The
// invoking state is the Source of those edges.
func (b *builder) invokes(n *statesman.StateNode, dto *dtoState) ([]*statesman.Invoke, error) {
	if len(dto.invoke) == 0 {
		return nil, nil
	}
	out := make([]*statesman.Invoke, 0, len(dto.invoke))
	for _, iv := range dto.invoke {
		if !iv.hasID || iv.id == "" {
			return nil, errf(iv.path, "invoke \"id\" is required and must be non-empty (rule: invoke-id-required)")
		}
		if !iv.hasSrc || iv.src == "" {
			return nil, errf(iv.path, "invoke \"src\" is required and must be non-empty (rule: invoke-src-required)")
		}
		doneEvent := "done.invoke." + iv.id
		errEvent := "error.invoke." + iv.id

		onDone := make([]*statesman.Transition, 0, len(iv.onDone))
		for i, tr := range iv.onDone {
			t, err := b.transition(n, tr, doneEvent, false, statesman.Delay{}, i)
			if err != nil {
				return nil, err
			}
			onDone = append(onDone, t)
		}
		onError := make([]*statesman.Transition, 0, len(iv.onError))
		for i, tr := range iv.onError {
			t, err := b.transition(n, tr, errEvent, false, statesman.Delay{}, i)
			if err != nil {
				return nil, err
			}
			onError = append(onError, t)
		}
		out = append(out, &statesman.Invoke{ID: iv.id, Src: iv.src, OnDone: onDone, OnError: onError})
	}
	return out, nil
}

func (b *builder) actions(in []dtoAction) ([]statesman.ActionRef, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]statesman.ActionRef, 0, len(in))
	for _, a := range in {
		if !a.hasType || a.typ == "" {
			return nil, errf(a.path, "action \"type\" must be non-empty (rule: action-type-required)")
		}
		out = append(out, statesman.ActionRef{Type: a.typ, Params: a.params, Callsite: -1})
	}
	return out, nil
}

func (b *builder) guard(g *dtoGuard) (*statesman.GuardRef, error) {
	if g == nil {
		return nil, nil
	}
	if !g.hasType || g.typ == "" {
		return nil, errf(g.path, "guard \"type\" must be non-empty (rule: guard-type-required)")
	}
	return &statesman.GuardRef{Type: g.typ, Params: g.params, Callsite: -1}, nil
}

func findChild(n *statesman.StateNode, key string) *statesman.StateNode {
	for _, c := range n.Children {
		if c.Key == key {
			return c
		}
	}
	return nil
}
