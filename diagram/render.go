package diagram

import (
	"time"

	"github.com/andrioid/statesman"
)

// Option configures a terminal render (Text and Live). Mermaid takes no options:
// its output is purely structural so it stays stable as a documentation artifact.
type Option func(*options)

type options struct {
	overlay      bool
	activeLeaves []statesman.StateID
	status       statesman.ActorStatus
	hasStatus    bool
	version      int
	hasVersion   bool
	pending      []statesman.ScheduledTimer
	color        bool
	ascii        bool
	verbose      bool
}

// WithActive marks the active leaf states (from Snapshot.ActiveStates) for
// overlay highlighting. ActiveStates holds only atomic leaves, so the renderer
// closes the set upward: every ancestor on an active path is highlighted too.
func WithActive(ids []statesman.StateID) Option {
	return func(o *options) {
		o.activeLeaves = ids
		o.overlay = true
	}
}

// WithStatus annotates the header with the actor's lifecycle status.
func WithStatus(s statesman.ActorStatus) Option {
	return func(o *options) {
		o.status = s
		o.hasStatus = true
		o.overlay = true
	}
}

// WithVersion annotates the header with the snapshot version (monotonic per actor).
func WithVersion(v int) Option {
	return func(o *options) {
		o.version = v
		o.hasVersion = true
	}
}

// WithPending supplies armed `after` timers so the overlay can show a countdown
// on the owning state's delayed transitions.
func WithPending(t []statesman.ScheduledTimer) Option {
	return func(o *options) { o.pending = t }
}

// WithColor toggles ANSI styling. Off by default so piped/golden output is clean.
func WithColor(on bool) Option { return func(o *options) { o.color = on } }

// WithASCII swaps the Unicode glyph set for an ASCII-only one.
func WithASCII(on bool) Option { return func(o *options) { o.ascii = on } }

// WithVerbose adds entry/exit and transition actions to the terminal output.
func WithVerbose(on bool) Option { return func(o *options) { o.verbose = on } }

// nowFn is overridable in tests so timer countdowns are deterministic.
var nowFn = time.Now

// glyphset is the symbol vocabulary for the terminal renderer. Two instances
// exist (Unicode default, ASCII fallback) so output degrades cleanly on dumb
// terminals.
type glyphset struct {
	branch, lastBranch, vert, space string
	arrow                           string
	initial, final, parallel        string
	invoke, done, fail, after       string
	active, inactive, sep           string
}

func glyphs(ascii bool) glyphset {
	if ascii {
		return glyphset{
			branch: "+- ", lastBranch: "\\- ", vert: "|  ", space: "   ",
			arrow:   "->",
			initial: "->", final: "(final)", parallel: "(parallel)",
			invoke: "invoke", done: "done", fail: "error", after: "after",
			active: "*", inactive: " ", sep: "=== || ===",
		}
	}
	return glyphset{
		branch: "├─ ", lastBranch: "└─ ", vert: "│  ", space: "   ",
		arrow:   "→",
		initial: "◇→", final: "●", parallel: "∥",
		invoke: "⟳", done: "✓", fail: "✗", after: "⏱",
		active: "◆", inactive: "·", sep: "╪────────",
	}
}

const (
	ansiReset  = "\x1b[0m"
	ansiActive = "\x1b[1;36m" // bold cyan: active states
	ansiDim    = "\x1b[2m"    // dim: inactive states in overlay
	ansiError  = "\x1b[1;31m" // bold red: error status
)

// activeSet expands the atomic active leaves into the full active configuration
// by walking each leaf's ancestors, so an active compound/parallel highlights
// along with its active descendants.
func activeSet(def *statesman.Definition, leaves []statesman.StateID) map[statesman.StateID]bool {
	set := make(map[statesman.StateID]bool, len(leaves)*2)
	for _, id := range leaves {
		set[id] = true
		n := def.Lookup(id)
		if n == nil {
			continue
		}
		for _, a := range n.ProperAncestors() {
			set[a.ID] = true
		}
	}
	return set
}

// delayLabel renders an `after` delay: a symbolic name verbatim, otherwise the
// literal duration (30000ms → "30s").
func delayLabel(d statesman.Delay) string {
	if d.Symbol != "" {
		return d.Symbol
	}
	return (time.Duration(d.Millis) * time.Millisecond).String()
}

// guardLabel renders a transition guard as " [type]", or "" when unguarded.
func guardLabel(t *statesman.Transition) string {
	if t.Guard == nil {
		return ""
	}
	return " [" + t.Guard.Type + "]"
}
