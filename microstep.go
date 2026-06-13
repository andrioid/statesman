package statesman

// microstep.go is the generic engine that drives docs/transition-algorithm.md
// §4 (macrostep/microstep loop) and §8 (action accumulation, transactionality).
// It is generic over TCtx/TEvt but never names a per-machine ActionResult: it
// calls the opaque applier/guard closures the generated constructor injects
// (decision 36). compute works entirely on clones of the engine's live state, so
// an observer-aborted microstep needs no rollback — the live state was untouched
// until commit.

// EffectKind tags an AppliedEffect.
type EffectKind uint8

const (
	EffectNoop EffectKind = iota
	EffectAssign
	EffectSend
	EffectSpawn
)

// AppliedEffect is the closure's return: the engine's view of one action result.
// Assign carries the new context (the closure preserves child refs, decision 32);
// Send/Spawn are outbox intents fired by the actor loop after publish.
type AppliedEffect[TCtx any] struct {
	Kind   EffectKind
	Ctx    TCtx         // EffectAssign
	Target ActorAddress // EffectSend / EffectSpawn
	Event  EventBase    // EffectSend
	Spawn  any          // EffectSpawn descriptor (wired in Phase 4)
}

type queuedEvent[TEvt EventBase] struct {
	descriptor string
	payload    TEvt
	hasPayload bool
}

// Microstep is one committed-or-stageable transition transaction. The actor loop
// runs observers on (PrevActive/PrevContext → NextActive/NextContext), then
// commits (publish + drain Effects) or aborts.
type Microstep[TCtx any] struct {
	Transitions      []*Transition
	Exited           []*StateNode // reverse document order (exit order)
	Entered          []*StateNode // document order (entry order)
	PrevActive       []StateID
	NextActive       []StateID
	PrevContext      TCtx
	NextContext      TCtx
	Effects          []AppliedEffect[TCtx] // Send/Spawn outbox, in order
	TriggeredByEvent bool
	ReachedFinal     bool

	// staged engine state, swapped in by commit.
	cfg             *configuration
	hist            map[StateID][]*StateNode
	internalEnqueue []string // done.state.* descriptors
}

// engine holds the live interpreter state. Unexported; the Machine wraps it.
type engine[TCtx any, TEvt EventBase] struct {
	def  *Definition
	cfg  *configuration
	ctx  TCtx
	hist map[StateID][]*StateNode

	internalQueue   []queuedEvent[TEvt]
	pendingExternal *queuedEvent[TEvt]

	applyAction func(callsite int, ctx TCtx, evt TEvt) AppliedEffect[TCtx]
	evalGuard   func(callsite int, ctx TCtx, evt TEvt) bool

	alwaysLimit int
	alwaysRun   int
	status      ActorStatus
	errReason   error
	lastEvt     TEvt // triggering event of the most recently staged microstep
}

// triggerEvent returns the event that triggered the most recently staged
// microstep (zero for the initial entry and eventless/internal microsteps). The
// actor loop passes it to observers.
func (e *engine[TCtx, TEvt]) triggerEvent() TEvt { return e.lastEvt }

const defaultAlwaysLimit = 100

func newEngine[TCtx any, TEvt EventBase](
	def *Definition,
	initCtx TCtx,
	applyAction func(int, TCtx, TEvt) AppliedEffect[TCtx],
	evalGuard func(int, TCtx, TEvt) bool,
	alwaysLimit int,
) *engine[TCtx, TEvt] {
	if alwaysLimit <= 0 {
		alwaysLimit = defaultAlwaysLimit
	}
	return &engine[TCtx, TEvt]{
		def:         def,
		cfg:         newConfiguration(),
		ctx:         initCtx,
		hist:        make(map[StateID][]*StateNode),
		applyAction: applyAction,
		evalGuard:   evalGuard,
		alwaysLimit: alwaysLimit,
		status:      StatusStarting,
	}
}

// guardPass returns a predicate evaluating guards against the live context and
// the supplied triggering event (zero for eventless/internal microsteps).
func (e *engine[TCtx, TEvt]) guardPass(evt TEvt) guardPredicate {
	return func(t *Transition) bool {
		if t.Guard == nil {
			return true
		}
		return e.evalGuard(t.Guard.Callsite, e.ctx, evt)
	}
}

// start computes the initial-configuration entry as a microstep (entry actions
// run; invokes are deferred to the actor loop). Not committed.
func (e *engine[TCtx, TEvt]) start() *Microstep[TCtx] {
	toEnter := make(map[*StateNode]struct{})
	defEntry := make(map[*StateNode]struct{})
	addDescendantStatesToEnter(e.def.Root, toEnter, defEntry, e.hist)
	var zero TEvt
	return e.apply(nil, nil, toEnter, zero, false)
}

// offer queues an external event (from the mailbox) for the next step.
func (e *engine[TCtx, TEvt]) offer(evt TEvt) {
	e.pendingExternal = &queuedEvent[TEvt]{descriptor: evt.EventType(), payload: evt, hasPayload: true}
}

// offerDescriptor queues a synthetic external event identified only by its
// descriptor (an `after` fire or a child done/error notification with no
// user-constructed payload). Action/guard callsites on such edges are
// context-shape, so the zero TEvt they receive is never inspected.
func (e *engine[TCtx, TEvt]) offerDescriptor(desc string) {
	e.pendingExternal = &queuedEvent[TEvt]{descriptor: desc}
}

// step computes the next pending microstep without committing, or reports the
// macrostep quiescent. Priority: eventless > internal event > external. §4.
func (e *engine[TCtx, TEvt]) step() (ms *Microstep[TCtx], quiescent bool) {
	var zero TEvt
	if ts := selectEventlessTransitions(e.cfg, e.guardPass(zero)); len(ts) > 0 {
		if e.alwaysRun >= e.alwaysLimit {
			e.status = StatusError
			e.errReason = ErrAlwaysLoopExceeded
			return nil, true
		}
		e.alwaysRun++
		return e.microstepFor(ts, zero, false), false
	}
	for len(e.internalQueue) > 0 {
		qe := e.internalQueue[0]
		e.internalQueue = e.internalQueue[1:]
		if ts := selectTransitions(e.cfg, qe.descriptor, e.guardPass(qe.payload)); len(ts) > 0 {
			return e.microstepFor(ts, qe.payload, true), false
		}
		// unmatched internal event is discarded; try the next one.
	}
	if e.pendingExternal != nil {
		qe := *e.pendingExternal
		e.pendingExternal = nil
		if ts := selectTransitions(e.cfg, qe.descriptor, e.guardPass(qe.payload)); len(ts) > 0 {
			return e.microstepFor(ts, qe.payload, true), false
		}
		// unhandled external event is dropped.
	}
	return nil, true
}

func (e *engine[TCtx, TEvt]) microstepFor(ts []*Transition, evt TEvt, triggered bool) *Microstep[TCtx] {
	exitSet := computeExitSet(e.cfg, ts)
	toEnter, _ := computeEntrySet(ts, e.hist)
	return e.apply(exitSet, ts, toEnter, evt, triggered)
}

// apply builds the staged microstep on clones of the live state. §6/§7/§8.1.
func (e *engine[TCtx, TEvt]) apply(exitSet []*StateNode, ts []*Transition, toEnter map[*StateNode]struct{}, evt TEvt, triggered bool) *Microstep[TCtx] {
	e.lastEvt = evt
	stageCfg := e.cfg.clone()
	stageHist := cloneHistory(e.hist)
	stageCtx := e.ctx
	var effects []AppliedEffect[TCtx]

	fold := func(callsite int) {
		eff := e.applyAction(callsite, stageCtx, evt)
		switch eff.Kind {
		case EffectAssign:
			stageCtx = eff.Ctx
		case EffectSend, EffectSpawn:
			effects = append(effects, eff)
		}
	}

	for _, s := range exitSet { // reverse document order
		recordHistoryInto(s, stageHist, e.cfg)
		for _, a := range s.Exit {
			fold(a.Callsite)
		}
		stageCfg.remove(s)
	}
	for _, t := range ts {
		for _, a := range t.Actions {
			fold(a.Callsite)
		}
	}

	entered := orderedEntry(toEnter)
	var doneStates []string
	reachedFinal := false
	for _, s := range entered {
		stageCfg.add(s)
		for _, a := range s.Entry {
			fold(a.Callsite)
		}
		if s.Kind != StateFinal {
			continue
		}
		if s.Parent == nil || s.Parent == e.def.Root {
			reachedFinal = true
			continue
		}
		doneStates = append(doneStates, "done.state."+string(s.Parent.ID))
		if gp := s.Parent.Parent; gp != nil && gp.Kind == StateParallel && allRegionsFinal(gp, stageCfg) {
			doneStates = append(doneStates, "done.state."+string(gp.ID))
		}
	}

	return &Microstep[TCtx]{
		Transitions:      ts,
		Exited:           exitSet,
		Entered:          entered,
		PrevActive:       e.cfg.atomicIDs(),
		NextActive:       stageCfg.atomicIDs(),
		PrevContext:      e.ctx,
		NextContext:      stageCtx,
		Effects:          effects,
		TriggeredByEvent: triggered,
		ReachedFinal:     reachedFinal,
		cfg:              stageCfg,
		hist:             stageHist,
		internalEnqueue:  doneStates,
	}
}

// commit applies a staged microstep to the live engine state.
func (e *engine[TCtx, TEvt]) commit(ms *Microstep[TCtx]) {
	e.cfg = ms.cfg
	e.hist = ms.hist
	e.ctx = ms.NextContext
	for _, d := range ms.internalEnqueue {
		e.internalQueue = append(e.internalQueue, queuedEvent[TEvt]{descriptor: d})
	}
	switch {
	case ms.ReachedFinal:
		e.status = StatusDone
	case e.status == StatusStarting:
		e.status = StatusRunning
	}
	if ms.TriggeredByEvent {
		e.alwaysRun = 0
	}
}

func (c *configuration) clone() *configuration {
	n := newConfiguration()
	for k := range c.members {
		n.members[k] = struct{}{}
	}
	return n
}

func (c *configuration) atomicIDs() []StateID {
	at := c.atomic()
	out := make([]StateID, len(at))
	for i, n := range at {
		out[i] = n.ID
	}
	return out
}

func cloneHistory(h map[StateID][]*StateNode) map[StateID][]*StateNode {
	out := make(map[StateID][]*StateNode, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
}

// recordHistoryInto records the to-restore states for any history child of s,
// reading the configuration before s is removed. §6.4.
func recordHistoryInto(s *StateNode, hist map[StateID][]*StateNode, cfg *configuration) {
	for _, h := range s.Children {
		if h.Kind != StateHistory {
			continue
		}
		var rec []*StateNode
		if h.HistoryKind == HistoryDeep {
			for _, n := range cfg.all() {
				if n.IsAtomic() && n.IsDescendant(s) {
					rec = append(rec, n)
				}
			}
		} else {
			for _, c := range s.Children {
				if cfg.has(c) {
					rec = append(rec, c)
				}
			}
		}
		hist[h.ID] = rec
	}
}

// allRegionsFinal reports whether every region of a parallel state has an active
// final descendant. §7.5.
func allRegionsFinal(gp *StateNode, cfg *configuration) bool {
	for _, r := range gp.Children {
		final := false
		for _, n := range cfg.all() {
			if n.Kind == StateFinal && (n == r || n.IsDescendant(r)) {
				final = true
				break
			}
		}
		if !final {
			return false
		}
	}
	return true
}
