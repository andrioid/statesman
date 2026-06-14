package diagram

import (
	"fmt"
	"strings"
	"time"

	"github.com/andrioid/statesman"
)

// Text renders def as a Unicode/ANSI outline tree for the terminal. With
// WithActive (and friends) it overlays a live snapshot: active states are
// marked and, with WithColor, highlighted.
func Text(def *statesman.Definition, opts ...Option) string {
	o := options{}
	for _, f := range opts {
		f(&o)
	}
	r := &textRender{b: &strings.Builder{}, o: &o, g: glyphs(o.ascii), def: def}
	if o.overlay {
		r.act = activeSet(def, o.activeLeaves)
	}
	r.header(def.Root)
	r.attrs(def.Root, "  ")
	r.children(def.Root, "")
	return r.b.String()
}

type textRender struct {
	b   *strings.Builder
	o   *options
	g   glyphset
	act map[statesman.StateID]bool
	def *statesman.Definition
}

// header is the root line: machine id, its initial child, and (in overlay) the
// actor status and snapshot version.
func (r *textRender) header(root *statesman.StateNode) {
	s := r.def.ID
	if root.Initial != nil {
		s += "  " + r.g.initial + " " + root.Initial.Key
	}
	if r.o.hasStatus {
		st := r.o.status.String()
		if r.o.color && r.o.status == statesman.StatusError {
			st = ansiError + st + ansiReset
		}
		s += "    " + st
		if r.o.hasVersion {
			s += fmt.Sprintf(" · v%d", r.o.version)
		}
	}
	r.b.WriteString(s + "\n")
}

func (r *textRender) children(n *statesman.StateNode, prefix string) {
	for i, c := range n.Children {
		// Parallel regions are AND-composed (all active at once); a rule between
		// them is the cue that distinguishes them from XOR compound children.
		if n.Kind == statesman.StateParallel && i > 0 {
			r.b.WriteString(prefix + r.g.sep + "\n")
		}
		r.node(c, prefix, i == len(n.Children)-1)
	}
}

func (r *textRender) node(n *statesman.StateNode, prefix string, isLast bool) {
	branch, cont := r.g.branch, r.g.vert
	if isLast {
		branch, cont = r.g.lastBranch, r.g.space
	}
	r.line(prefix+branch, n)
	childPrefix := prefix + cont
	r.attrs(n, childPrefix+"  ")
	r.children(n, childPrefix)
}

func (r *textRender) line(prefix string, n *statesman.StateNode) {
	label := r.label(n)
	if r.o.overlay {
		// History is a pseudo-state: it never appears in ActiveStates, so it is
		// never highlighted.
		on := r.act[n.ID] && n.Kind != statesman.StateHistory
		marker := r.g.inactive
		if on {
			marker = r.g.active
		}
		label = marker + " " + label
		if r.o.color {
			style := ansiDim
			if on {
				style = ansiActive
			}
			label = style + label + ansiReset
		}
	}
	r.b.WriteString(prefix + label + "\n")
}

func (r *textRender) label(n *statesman.StateNode) string {
	if n.Kind == statesman.StateHistory {
		h := "(H)"
		if n.HistoryKind == statesman.HistoryDeep {
			h = "(H*)"
		}
		s := n.Key + " " + h
		if len(n.HistoryTo) == 0 {
			return s + " (default)"
		}
		var ts []string
		for _, t := range n.HistoryTo {
			ts = append(ts, string(t.ID))
		}
		return s + " " + r.g.arrow + " " + strings.Join(ts, ",")
	}
	s := n.Key
	switch n.Kind {
	case statesman.StateCompound:
		if n.Initial != nil {
			s += "  " + r.g.initial + " " + n.Initial.Key
		}
	case statesman.StateParallel:
		s += "  " + r.g.parallel
	case statesman.StateFinal:
		s += "  " + r.g.final
	}
	return s
}

func (r *textRender) attrs(n *statesman.StateNode, p string) {
	if r.o.verbose {
		for _, a := range n.Entry {
			r.b.WriteString(p + "entry / " + a.Type + "\n")
		}
	}
	for _, t := range n.Transitions {
		r.transition(t, p)
	}
	for _, iv := range n.Invokes {
		r.invoke(iv, p)
	}
	if r.o.verbose {
		for _, a := range n.Exit {
			r.b.WriteString(p + "exit / " + a.Type + "\n")
		}
	}
}

func (r *textRender) transition(t *statesman.Transition, p string) {
	ev := r.eventText(t)
	guard := guardLabel(t)
	actions := r.actionSuffix(t.Actions)
	if t.Internal() {
		r.b.WriteString(p + ev + guard + actions + " (internal)\n")
		return
	}
	timer := ""
	if t.IsAfter {
		timer = r.timerSuffix(t.Source)
	}
	for _, tgt := range t.Targets {
		r.b.WriteString(p + ev + guard + " " + r.g.arrow + " " + targetLabel(t.Source, tgt) + actions + timer + "\n")
	}
}

func (r *textRender) invoke(iv *statesman.Invoke, p string) {
	r.b.WriteString(p + r.g.invoke + " " + iv.ID + " (" + iv.Src + ")\n")
	ep := p + "  "
	r.invokeEdges(iv.OnDone, r.g.done, ep)
	r.invokeEdges(iv.OnError, r.g.fail, ep)
}

func (r *textRender) invokeEdges(ts []*statesman.Transition, mark, p string) {
	for _, t := range ts {
		guard := guardLabel(t)
		actions := r.actionSuffix(t.Actions)
		for _, tgt := range t.Targets {
			r.b.WriteString(p + mark + " " + r.g.arrow + " " + targetLabel(t.Source, tgt) + guard + actions + "\n")
		}
	}
}

func (r *textRender) eventText(t *statesman.Transition) string {
	if t.IsAfter {
		return r.g.after + " " + delayLabel(t.Delay)
	}
	if t.Eventless() {
		return "always"
	}
	return t.Event
}

func (r *textRender) actionSuffix(acts []statesman.ActionRef) string {
	if !r.o.verbose || len(acts) == 0 {
		return ""
	}
	var as []string
	for _, a := range acts {
		as = append(as, a.Type)
	}
	return " / " + strings.Join(as, ", ")
}

// timerSuffix renders the remaining time on an armed `after` timer for owner,
// when overlaying a live snapshot.
func (r *textRender) timerSuffix(owner *statesman.StateNode) string {
	if !r.o.overlay || len(r.o.pending) == 0 {
		return ""
	}
	now := nowFn()
	for _, ti := range r.o.pending {
		if ti.StateID != owner.ID {
			continue
		}
		rem := ti.Deadline.Sub(now)
		if rem < 0 {
			rem = 0
		}
		return " (" + rem.Truncate(time.Millisecond).String() + " left)"
	}
	return ""
}

// targetLabel renders a transition target relative to its source: a sibling by
// its local key, otherwise its absolute dotted id.
func targetLabel(src, tgt *statesman.StateNode) string {
	if tgt == src {
		return "self"
	}
	if tgt.Parent != nil && tgt.Parent == src.Parent {
		return tgt.Key
	}
	if tgt.ID == "" {
		return "(root)"
	}
	return string(tgt.ID)
}
