package statesman

import "testing"

// buildTree returns root{ a{ a1 }, b } for helper tests.
func buildTree() (root, a, a1, b *StateNode) {
	root = &StateNode{ID: "", Key: "", Kind: StateCompound, DocOrder: 0}
	a = &StateNode{ID: "a", Key: "a", Kind: StateCompound, Parent: root, DocOrder: 1}
	a1 = &StateNode{ID: "a.a1", Key: "a1", Kind: StateAtomic, Parent: a, DocOrder: 2}
	b = &StateNode{ID: "b", Key: "b", Kind: StateFinal, Parent: root, DocOrder: 3}
	root.Children = []*StateNode{a, b}
	a.Children = []*StateNode{a1}
	return
}

func TestIsDescendant(t *testing.T) {
	root, a, a1, b := buildTree()
	cases := []struct {
		name string
		n    *StateNode
		anc  *StateNode
		want bool
	}{
		{"a1 under root", a1, root, true},
		{"a1 under a", a1, a, true},
		{"a1 not under b", a1, b, false},
		{"root not under a", root, a, false},
		{"node not under itself", a, a, false},
		{"nil ancestor", a1, nil, false},
	}
	for _, c := range cases {
		if got := c.n.IsDescendant(c.anc); got != c.want {
			t.Errorf("%s: IsDescendant=%v want %v", c.name, got, c.want)
		}
	}
}

func TestProperAncestors(t *testing.T) {
	root, a, a1, _ := buildTree()
	got := a1.ProperAncestors()
	want := []*StateNode{a, root} // innermost first
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ancestor[%d]=%v want %v", i, got[i].ID, want[i].ID)
		}
	}
	if len(root.ProperAncestors()) != 0 {
		t.Error("root should have no proper ancestors")
	}
}

func TestIsAtomic(t *testing.T) {
	root, a, a1, b := buildTree()
	if a1.IsAtomic() != true || b.IsAtomic() != true {
		t.Error("atomic and final are atomic for selection")
	}
	if root.IsAtomic() || a.IsAtomic() {
		t.Error("compound is not atomic")
	}
}
