package statesman

import "testing"

// mk builds a node with a key and kind; children/initial/transitions are wired
// by the caller, then finalize assigns Parent, dotted ID, and pre-order DocOrder.
func mk(key string, kind StateKind, children ...*StateNode) *StateNode {
	return &StateNode{Key: key, Kind: kind, Children: children}
}

func finalize(root *StateNode) *Definition {
	ord := 0
	var walk func(n, parent *StateNode)
	walk = func(n, parent *StateNode) {
		n.Parent = parent
		n.DocOrder = ord
		ord++
		switch {
		case parent == nil:
			n.ID = "" // root is the machine; children take root-relative dotted paths
		case parent.ID == "":
			n.ID = StateID(n.Key)
		default:
			n.ID = parent.ID + "." + StateID(n.Key)
		}
		for _, c := range n.Children {
			walk(c, n)
		}
	}
	walk(root, nil)
	return NewDefinition("test", root, 0, 0)
}

// treeA: root{ s1{a,b}, p[r1{r1a,r1b}, r2{r2a,r2b}], s2 }
func treeA() (nodes map[string]*StateNode) {
	a := mk("a", StateAtomic)
	b := mk("b", StateAtomic)
	s1 := mk("s1", StateCompound, a, b)
	s1.Initial = a
	r1a := mk("r1a", StateAtomic)
	r1b := mk("r1b", StateAtomic)
	r1 := mk("r1", StateCompound, r1a, r1b)
	r1.Initial = r1a
	r2a := mk("r2a", StateAtomic)
	r2b := mk("r2b", StateAtomic)
	r2 := mk("r2", StateCompound, r2a, r2b)
	r2.Initial = r2a
	p := mk("p", StateParallel, r1, r2)
	s2 := mk("s2", StateAtomic)
	root := mk("root", StateCompound, s1, p, s2)
	root.Initial = s1
	finalize(root)
	return map[string]*StateNode{
		"root": root, "s1": s1, "a": a, "b": b, "p": p,
		"r1": r1, "r1a": r1a, "r1b": r1b, "r2": r2, "r2a": r2a, "r2b": r2b, "s2": s2,
	}
}

func cfgOf(ns ...*StateNode) *configuration {
	c := newConfiguration()
	for _, n := range ns {
		c.add(n)
	}
	return c
}

func always(t *testing.T) guardPredicate { t.Helper(); return func(*Transition) bool { return true } }

func TestFindLCCA(t *testing.T) {
	n := treeA()
	cases := []struct {
		name   string
		states []*StateNode
		want   *StateNode
	}{
		{"siblings under s1", []*StateNode{n["a"], n["b"]}, n["s1"]},
		{"across regions -> root", []*StateNode{n["r1a"], n["r2a"]}, n["root"]},
		{"within one region", []*StateNode{n["r1a"], n["r1b"]}, n["r1"]},
		{"deep vs shallow -> root", []*StateNode{n["a"], n["s2"]}, n["root"]},
	}
	for _, c := range cases {
		if got := findLCCA(c.states); got != c.want {
			t.Errorf("%s: findLCCA=%v want %v", c.name, idOf(got), idOf(c.want))
		}
	}
}

func idOf(n *StateNode) string {
	if n == nil {
		return "<nil>"
	}
	return string(n.ID)
}

func TestExitSet(t *testing.T) {
	n := treeA()
	cfg := cfgOf(n["root"], n["s1"], n["a"])
	tr := &Transition{Source: n["a"], Event: "E", Targets: []*StateNode{n["b"]}}
	got := computeExitSet(cfg, []*Transition{tr})
	if len(got) != 1 || got[0] != n["a"] {
		t.Fatalf("exit set = %v, want [a]", ids(got))
	}
	// domain of a->b is s1, which must NOT be exited.
	if dom := getTransitionDomain(tr); dom != n["s1"] {
		t.Errorf("domain = %v want s1", idOf(dom))
	}
}

func TestEntrySetParallelFansOut(t *testing.T) {
	n := treeA()
	// Enter the parallel state p: expect p + both regions + each region's initial.
	tr := &Transition{Source: n["s2"], Event: "E", Targets: []*StateNode{n["p"]}}
	toEnter, def := computeEntrySet([]*Transition{tr}, nil)
	want := []*StateNode{n["p"], n["r1"], n["r1a"], n["r2"], n["r2a"]}
	assertSet(t, "entry", toEnter, want)
	assertSet(t, "defaultEntry", def, []*StateNode{n["r1"], n["r2"]})
}

func TestInitialEntry(t *testing.T) {
	n := treeA()
	toEnter := map[*StateNode]struct{}{}
	def := map[*StateNode]struct{}{}
	addDescendantStatesToEnter(n["root"], toEnter, def, nil)
	assertSet(t, "initial", toEnter, []*StateNode{n["root"], n["s1"], n["a"]})
	assertSet(t, "defaultEntry", def, []*StateNode{n["root"], n["s1"]})
}

func TestSelectParallelBothFire(t *testing.T) {
	r1a := mk("x1", StateAtomic)
	y1 := mk("y1", StateAtomic)
	r1 := mk("r1", StateCompound, r1a, y1)
	r1.Initial = r1a
	x2 := mk("x2", StateAtomic)
	y2 := mk("y2", StateAtomic)
	r2 := mk("r2", StateCompound, x2, y2)
	r2.Initial = x2
	p := mk("p", StateParallel, r1, r2)
	root := mk("root", StateCompound, p)
	root.Initial = p
	finalize(root)
	r1a.Transitions = []*Transition{{Source: r1a, Event: "E", Targets: []*StateNode{y1}}}
	x2.Transitions = []*Transition{{Source: x2, Event: "E", Targets: []*StateNode{y2}}}
	cfg := cfgOf(root, p, r1, r1a, r2, x2)
	got := selectTransitions(cfg, "E", always(t))
	if len(got) != 2 {
		t.Fatalf("expected both regions to fire, got %d: %v", len(got), srcIDs(got))
	}
}

func TestSelectPreemptionDeeperWins(t *testing.T) {
	x1 := mk("x1", StateAtomic)
	y1 := mk("y1", StateAtomic)
	r1 := mk("r1", StateCompound, x1, y1)
	r1.Initial = x1
	x2 := mk("x2", StateAtomic)
	r2 := mk("r2", StateCompound, x2)
	r2.Initial = x2
	p := mk("p", StateParallel, r1, r2)
	out := mk("out", StateAtomic)
	root := mk("root", StateCompound, p, out)
	root.Initial = p
	finalize(root)
	tX1 := &Transition{Source: x1, Event: "E", Targets: []*StateNode{y1}}
	tR2 := &Transition{Source: r2, Event: "E", Targets: []*StateNode{out}} // exits all of p
	x1.Transitions = []*Transition{tX1}
	r2.Transitions = []*Transition{tR2}
	cfg := cfgOf(root, p, r1, x1, r2, x2)
	got := selectTransitions(cfg, "E", always(t))
	if len(got) != 1 || got[0] != tX1 {
		t.Fatalf("deeper x1 transition should preempt r2; got %v", srcIDs(got))
	}
}

func TestMatchExactBeforeWildcard(t *testing.T) {
	a := mk("a", StateAtomic)
	b := mk("b", StateAtomic)
	s := mk("s", StateAtomic)
	root := mk("root", StateCompound, s, a, b)
	root.Initial = s
	finalize(root)
	wild := &Transition{Source: s, Event: "*", Targets: []*StateNode{a}, DocOrder: 0}
	exact := &Transition{Source: s, Event: "E", Targets: []*StateNode{b}, DocOrder: 1}
	s.Transitions = []*Transition{wild, exact} // wildcard declared first
	if got := matchAtNode(s, "E", always(t)); got != exact {
		t.Error("exact match must win over earlier wildcard")
	}
	if got := matchAtNode(s, "OTHER", always(t)); got != wild {
		t.Error("wildcard must catch unmatched event")
	}
}

func TestEventlessChildShadowsAncestor(t *testing.T) {
	leaf := mk("leaf", StateAtomic)
	other := mk("other", StateAtomic)
	c := mk("c", StateCompound, leaf, other)
	c.Initial = leaf
	root := mk("root", StateCompound, c)
	root.Initial = c
	finalize(root)
	tParent := &Transition{Source: c, Event: "", Targets: []*StateNode{other}}
	tLeaf := &Transition{Source: leaf, Event: "", Targets: []*StateNode{other}}
	c.Transitions = []*Transition{tParent}
	leaf.Transitions = []*Transition{tLeaf}
	cfg := cfgOf(root, c, leaf)
	got := selectEventlessTransitions(cfg, always(t))
	if len(got) != 1 || got[0] != tLeaf {
		t.Fatalf("child eventless transition must shadow ancestor; got %v", srcIDs(got))
	}
}

func TestHistoryRestore(t *testing.T) {
	a := mk("a", StateAtomic)
	b := mk("b", StateAtomic)
	h := mk("h", StateHistory)
	comp := mk("comp", StateCompound, a, b, h)
	comp.Initial = a
	h.HistoryKind = HistoryShallow
	h.HistoryTo = []*StateNode{a} // default
	root := mk("root", StateCompound, comp)
	root.Initial = comp
	finalize(root)

	// With a stored value, restore it.
	toEnter := map[*StateNode]struct{}{}
	addDescendantStatesToEnter(h, toEnter, map[*StateNode]struct{}{}, map[StateID][]*StateNode{h.ID: {b}})
	assertSet(t, "history stored", toEnter, []*StateNode{b})

	// With no stored value, use the default target.
	toEnter2 := map[*StateNode]struct{}{}
	addDescendantStatesToEnter(h, toEnter2, map[*StateNode]struct{}{}, nil)
	assertSet(t, "history default", toEnter2, []*StateNode{a})
}

func assertSet(t *testing.T, label string, got map[*StateNode]struct{}, want []*StateNode) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: size %d want %d (%v)", label, len(got), len(want), setIDs(got))
		return
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("%s: missing %s (got %v)", label, w.ID, setIDs(got))
		}
	}
}

func ids(ns []*StateNode) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = string(n.ID)
	}
	return out
}

func setIDs(m map[*StateNode]struct{}) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, string(n.ID))
	}
	return out
}

func srcIDs(ts []*Transition) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = string(t.Source.ID)
	}
	return out
}
