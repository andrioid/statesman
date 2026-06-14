package diagram

import (
	"fmt"
	"strings"

	"github.com/andrioid/statesman"
)

// Mermaid renders def as a Mermaid stateDiagram-v2 string. Hierarchy is emitted
// as nested `state X { … }` containers; every transition is emitted flat
// afterward (Mermaid ids are global, so cross-boundary edges resolve regardless
// of nesting). Invoke activities have no native Mermaid form, so onDone/onError
// become labeled edges and the activity itself is preserved as a `%%` comment.
func Mermaid(def *statesman.Definition) string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	m := mermaid{b: &b}
	for _, c := range def.Root.Children {
		m.container(c, "    ")
	}
	if def.Root.Initial != nil {
		fmt.Fprintf(&b, "    [*] --> %s\n", mermaidID(def.Root.Initial))
	}
	m.edges(def.Root)
	return b.String()
}

type mermaid struct {
	b *strings.Builder
}

// mermaidID is the globally-unique node id: the dotted StateID with dots mapped
// to underscores (Mermaid rejects dots in ids).
func mermaidID(n *statesman.StateNode) string {
	return strings.ReplaceAll(string(n.ID), ".", "_")
}

// container declares a node and, for composites, recurses into a `state { … }`
// block. Atomic states are declared as bare ids so isolated nodes still appear
// and box membership is correct.
func (m *mermaid) container(n *statesman.StateNode, indent string) {
	id := mermaidID(n)
	switch n.Kind {
	case statesman.StateCompound:
		fmt.Fprintf(m.b, "%sstate %s {\n", indent, id)
		if n.Initial != nil {
			fmt.Fprintf(m.b, "%s    [*] --> %s\n", indent, mermaidID(n.Initial))
		}
		for _, c := range n.Children {
			m.container(c, indent+"    ")
		}
		fmt.Fprintf(m.b, "%s}\n", indent)
	case statesman.StateParallel:
		fmt.Fprintf(m.b, "%sstate %s {\n", indent, id)
		for i, c := range n.Children {
			if i > 0 {
				fmt.Fprintf(m.b, "%s    --\n", indent)
			}
			m.container(c, indent+"    ")
		}
		fmt.Fprintf(m.b, "%s}\n", indent)
	case statesman.StateHistory:
		kind := "shallow"
		if n.HistoryKind == statesman.HistoryDeep {
			kind = "deep"
		}
		fmt.Fprintf(m.b, "%sstate %s\n", indent, id)
		fmt.Fprintf(m.b, "%snote right of %s : history (%s)\n", indent, id, kind)
	default: // atomic, final
		fmt.Fprintf(m.b, "%sstate %s\n", indent, id)
	}
}

// edges walks the tree in document order and emits every transition, invoke
// outcome edge, and final marker as flat Mermaid statements.
func (m *mermaid) edges(n *statesman.StateNode) {
	src := mermaidID(n)
	for _, t := range n.Transitions {
		m.transitionEdges(t)
	}
	for _, iv := range n.Invokes {
		fmt.Fprintf(m.b, "    %%%% invoke %s: %s\n", iv.ID, iv.Src)
		for _, t := range iv.OnDone {
			m.invokeEdges(t, "onDone "+iv.ID)
		}
		for _, t := range iv.OnError {
			m.invokeEdges(t, "onError "+iv.ID)
		}
	}
	if n.Kind == statesman.StateFinal {
		fmt.Fprintf(m.b, "    %s --> [*]\n", src)
	}
	for _, c := range n.Children {
		m.edges(c)
	}
}

func (m *mermaid) transitionEdges(t *statesman.Transition) {
	if t.Internal() {
		fmt.Fprintf(m.b, "    %%%% internal %s%s on %s\n", mermaidEventLabel(t), guardLabel(t), mermaidID(t.Source))
		return
	}
	src := mermaidID(t.Source)
	label := mermaidEventLabel(t) + guardLabel(t)
	for _, tgt := range t.Targets {
		fmt.Fprintf(m.b, "    %s --> %s : %s\n", src, mermaidID(tgt), label)
	}
}

func (m *mermaid) invokeEdges(t *statesman.Transition, label string) {
	src := mermaidID(t.Source)
	full := label + guardLabel(t)
	for _, tgt := range t.Targets {
		fmt.Fprintf(m.b, "    %s --> %s : %s\n", src, mermaidID(tgt), full)
	}
}

// mermaidEventLabel renders the triggering event: a delay for `after`, "always"
// for eventless, otherwise the event descriptor.
func mermaidEventLabel(t *statesman.Transition) string {
	if t.IsAfter {
		return "after(" + delayLabel(t.Delay) + ")"
	}
	if t.Eventless() {
		return "always"
	}
	return t.Event
}
