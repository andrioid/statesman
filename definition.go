package statesman

// Definition is the validated, structural machine model the engine walks. It is
// non-generic: guards and actions are referenced by Callsite index, never by Go
// type, which is the core/codegen boundary (decision 36) — the generated
// constructor provides closures keyed by the same Callsite ids.
//
// internal/schema builds a Definition from machine.json (Type strings filled,
// Callsite ids assigned in document order); `statesman generate` emits the same
// shape as Go and the matching dispatch closures.
type Definition struct {
	ID    string // machine id; must equal the package name (decision 50)
	Root  *StateNode
	nodes map[StateID]*StateNode

	// ActionCount / GuardCount are the number of distinct callsites enumerated,
	// so a runtime or codegen consumer can size its dispatch table.
	ActionCount int
	GuardCount  int
}

// Lookup returns the node with the given id, or nil.
func (d *Definition) Lookup(id StateID) *StateNode { return d.nodes[id] }

// NewDefinition assembles a Definition from a fully-built root node, indexing
// every node by ID for Lookup. actionCount/guardCount are the totals from the
// builder's callsite enumeration (they size the dispatch tables). Used by
// internal/schema and by generated code; the engine never builds one itself.
func NewDefinition(id string, root *StateNode, actionCount, guardCount int) *Definition {
	d := &Definition{
		ID:          id,
		Root:        root,
		nodes:       make(map[StateID]*StateNode),
		ActionCount: actionCount,
		GuardCount:  guardCount,
	}
	var index func(n *StateNode)
	index = func(n *StateNode) {
		d.nodes[n.ID] = n
		for _, c := range n.Children {
			index(c)
		}
	}
	index(root)
	return d
}

// StateKind classifies a state node.
type StateKind uint8

const (
	StateAtomic StateKind = iota
	StateCompound
	StateParallel
	StateFinal
	StateHistory
)

func (k StateKind) String() string {
	switch k {
	case StateAtomic:
		return "atomic"
	case StateCompound:
		return "compound"
	case StateParallel:
		return "parallel"
	case StateFinal:
		return "final"
	case StateHistory:
		return "history"
	default:
		return "unknown"
	}
}

// HistoryKind is shallow or deep, for history pseudo-states.
type HistoryKind uint8

const (
	HistoryShallow HistoryKind = iota
	HistoryDeep
)

// ActionRef references an entry/exit/transition action. Type is the JSON action
// type (for codegen name resolution); Params are the raw literal params (for
// stub type inference only — never read as a map at runtime). Callsite is the
// stable id the opaque applier dispatches on.
type ActionRef struct {
	Type     string
	Params   map[string]any
	Callsite int
}

// GuardRef references a transition guard. Same Callsite contract as ActionRef,
// in a separate id space.
type GuardRef struct {
	Type     string
	Params   map[string]any
	Callsite int
}

// Delay is a resolved `after` delay: literal milliseconds when Symbol == "",
// otherwise a symbolic delays.go const name resolved at codegen.
type Delay struct {
	Millis int64
	Symbol string
}

// Transition is one transition out of a state. Event == "" means eventless
// (`always`). Targets is empty for a targetless (internal, action-only)
// transition; in v1 it holds at most one explicit target (fan-out to multiple
// states happens only via parallel/initial entry closure).
type Transition struct {
	Source   *StateNode
	Event    string
	Guard    *GuardRef
	Targets  []*StateNode
	Actions  []ActionRef
	DocOrder int
	IsAfter  bool
	Delay    Delay // valid when IsAfter
}

// Eventless reports whether t is an `always` transition.
func (t *Transition) Eventless() bool { return t.Event == "" && !t.IsAfter }

// Internal reports whether t is targetless (action-only; no exit/entry).
func (t *Transition) Internal() bool { return len(t.Targets) == 0 }

// Invoke is an invoked actor declared on a state. ID is required (decision 10);
// Src names the Go actor symbol. OnDone/OnError are the transitions wired on the
// generated done.invoke.<id> / error.invoke.<id> events.
type Invoke struct {
	ID      string
	Src     string
	OnDone  []*Transition
	OnError []*Transition
}

// StateNode is a node in the machine tree.
type StateNode struct {
	ID          StateID
	Key         string // local key under Parent.Children
	Kind        StateKind
	Parent      *StateNode
	Children    []*StateNode // document order
	Initial     *StateNode   // compound: default child (nil otherwise)
	HistoryKind HistoryKind  // history nodes only
	HistoryTo   []*StateNode // history default target (nil => use shallow default)
	Entry       []ActionRef
	Exit        []ActionRef
	Invokes     []*Invoke
	Transitions []*Transition // on + always + after, in document order
	DocOrder    int
}

// IsAtomic reports whether n is a leaf for selection purposes (atomic or final).
func (n *StateNode) IsAtomic() bool { return n.Kind == StateAtomic || n.Kind == StateFinal }

// ProperAncestors returns n's ancestors from its parent up to and including the
// root, innermost first.
func (n *StateNode) ProperAncestors() []*StateNode {
	var out []*StateNode
	for p := n.Parent; p != nil; p = p.Parent {
		out = append(out, p)
	}
	return out
}

// IsDescendant reports whether n is a proper descendant of anc.
func (n *StateNode) IsDescendant(anc *StateNode) bool {
	if anc == nil {
		return false
	}
	for p := n.Parent; p != nil; p = p.Parent {
		if p == anc {
			return true
		}
	}
	return false
}
