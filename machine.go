package statesman

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const defaultMailboxCap = 256

// Option configures a Machine at construction (used by generated constructors).
type Option func(*machineConfig)

type machineConfig struct {
	clock       Clock
	timers      TimerService
	alwaysLimit int
	mailboxCap  int
	delays      map[string]time.Duration
}

// WithClock sets the Clock (default WallClock).
func WithClock(c Clock) Option { return func(m *machineConfig) { m.clock = c } }

// WithTimerService sets the TimerService (default in-process, bound to the Clock).
func WithTimerService(ts TimerService) Option { return func(m *machineConfig) { m.timers = ts } }

// WithAlwaysLimit caps consecutive eventless microsteps before ErrAlwaysLoopExceeded.
func WithAlwaysLimit(n int) Option { return func(m *machineConfig) { m.alwaysLimit = n } }

// WithMailboxCap sets the bounded mailbox capacity (default 256).
func WithMailboxCap(n int) Option { return func(m *machineConfig) { m.mailboxCap = n } }

// WithDelays resolves symbolic `after` delay names (from delays.go) to durations.
func WithDelays(d map[string]time.Duration) Option {
	return func(m *machineConfig) { m.delays = d }
}

// Machine is the runtime actor handle. Construct unstarted (via a generated
// NewXxxMachine or newMachine), configure observers/subscribers, then Start. One
// goroutine owns all mutable runtime state; readers use the lock-free Snapshot.
type Machine[TCtx any, TEvt EventBase] struct {
	def         *Definition
	initCtx     TCtx
	applyAction func(int, TCtx, TEvt) AppliedEffect[TCtx]
	evalGuard   func(int, TCtx, TEvt) bool
	cfg         machineConfig

	transitionObs []TransitionObserver[TCtx, TEvt]
	actorObs      []ActorObserver
	timerObs      []TimerObserver
	preSubs       []*subscriber[TCtx]
	runners       map[string]InvokeRunner[TCtx, TEvt] // invoke id -> runner (pre-Start)

	startOnce sync.Once
	closeOnce sync.Once
	started   atomic.Bool

	addr          ActorAddress
	eng           *engine[TCtx, TEvt]
	snap          atomic.Pointer[Snapshot[TCtx]]
	mailbox       chan inboxItem[TEvt]
	subscribeCh   chan *subscriber[TCtx]
	unsubscribeCh chan *subscriber[TCtx]
	loopCtx       context.Context
	loopCancel    context.CancelFunc
	done          chan struct{}
	wg            sync.WaitGroup

	// actor-goroutine-owned (no external access):
	version      int
	localSubs    []*subscriber[TCtx]
	armed        map[*StateNode][]armedTimer
	children     map[string]*runningChild // invoke id -> running child actor
	invokeSpawns map[string]int           // invoke id -> total spawns (for restart count)
}

type inboxItem[TEvt EventBase] struct {
	evt        TEvt
	desc       string
	hasEvt     bool
	ack        chan bool // settle ping: actor replies whether the mailbox is empty
	drainClose bool      // CloseAfterDrain sentinel: drain remaining events then stop
}

type armedTimer struct {
	timer      Timer
	descriptor string
	deadline   time.Time
	state      *StateNode
}

// NewMachine builds an unstarted Machine. Generated constructors wrap this with
// the concrete appliers and the per-machine Definition; tests may call it
// directly. This is the core/codegen seam: the applyAction/evalGuard closures
// carry the opaque dispatch tables (decision 36).
func NewMachine[TCtx any, TEvt EventBase](
	def *Definition,
	initCtx TCtx,
	applyAction func(int, TCtx, TEvt) AppliedEffect[TCtx],
	evalGuard func(int, TCtx, TEvt) bool,
	opts ...Option,
) *Machine[TCtx, TEvt] {
	mc := machineConfig{alwaysLimit: defaultAlwaysLimit, mailboxCap: defaultMailboxCap}
	for _, o := range opts {
		o(&mc)
	}
	if mc.clock == nil {
		mc.clock = WallClock{}
	}
	if mc.timers == nil {
		mc.timers = NewInProcessTimerService(mc.clock)
	}
	return &Machine[TCtx, TEvt]{def: def, initCtx: initCtx, applyAction: applyAction, evalGuard: evalGuard, cfg: mc}
}

// AddObserver registers an observer; it type-asserts to the observer interfaces
// (the one intentional any, decision 46). Call before Start.
func (m *Machine[TCtx, TEvt]) AddObserver(o any) {
	if m.started.Load() {
		return
	}
	if to, ok := o.(TransitionObserver[TCtx, TEvt]); ok {
		m.transitionObs = append(m.transitionObs, to)
	}
	if ao, ok := o.(ActorObserver); ok {
		m.actorObs = append(m.actorObs, ao)
	}
	if tobs, ok := o.(TimerObserver); ok {
		m.timerObs = append(m.timerObs, tobs)
	}
}

// Address returns the actor's hierarchical address (valid after Start).
func (m *Machine[TCtx, TEvt]) Address() ActorAddress { return m.addr }

// Snapshot returns the latest published snapshot (lock-free).
func (m *Machine[TCtx, TEvt]) Snapshot() Snapshot[TCtx] {
	if s := m.snap.Load(); s != nil {
		return *s
	}
	return Snapshot[TCtx]{MachineID: m.def.ID, Context: m.initCtx, Status: StatusStarting}
}

// Definition returns the machine's structural model. It is immutable after
// construction, so callers (e.g. diagram.Live) may read the tree concurrently
// with the running actor.
func (m *Machine[TCtx, TEvt]) Definition() *Definition { return m.def }

// Start launches the actor goroutine at the given root address. Single-shot: a
// second call returns ErrAlreadyStarted (decision 29).
func (m *Machine[TCtx, TEvt]) Start(ctx context.Context, address string) error {
	err := ErrAlreadyStarted
	m.startOnce.Do(func() {
		err = nil
		m.started.Store(true)
		m.addr = ActorAddress("/" + address)
		m.eng = newEngine(m.def, m.initCtx, m.applyAction, m.evalGuard, m.cfg.alwaysLimit)
		m.mailbox = make(chan inboxItem[TEvt], m.cfg.mailboxCap)
		m.subscribeCh = make(chan *subscriber[TCtx])
		m.unsubscribeCh = make(chan *subscriber[TCtx])
		m.done = make(chan struct{})
		m.armed = make(map[*StateNode][]armedTimer)
		m.children = make(map[string]*runningChild)
		m.loopCtx, m.loopCancel = context.WithCancel(ctx)
		m.snap.Store(&Snapshot[TCtx]{MachineID: m.def.ID, Address: m.addr, Context: m.initCtx, Status: StatusStarting})
		m.localSubs = m.preSubs
		for _, s := range m.localSubs {
			m.startDrain(s)
		}
		for _, ao := range m.actorObs {
			ao.OnActorStart(m.addr)
		}
		m.wg.Add(1)
		go m.loop()
	})
	return err
}

// SetInitialContext overrides the initial context before Start. It is the seam
// MachineActor uses to seed a child sub-machine with input derived from its
// parent; a no-op once the machine has started.
func (m *Machine[TCtx, TEvt]) SetInitialContext(ctx TCtx) {
	if m.started.Load() {
		return
	}
	m.initCtx = ctx
}

// Send blocks until the event is enqueued, the actor stops, or ctx is cancelled.
func (m *Machine[TCtx, TEvt]) Send(ctx context.Context, evt TEvt) error {
	if !m.started.Load() || m.stopped() {
		return ErrActorStopped
	}
	select {
	case <-m.done:
		return ErrActorStopped
	case <-ctx.Done():
		return ctx.Err()
	case m.mailbox <- inboxItem[TEvt]{evt: evt, desc: evt.EventType(), hasEvt: true}:
		return nil
	}
}

// stopped reports whether shutdown has completed (done closed). Used to give
// Send/TrySend a deterministic ErrActorStopped instead of racing a buffered
// mailbox slot against the done signal.
func (m *Machine[TCtx, TEvt]) stopped() bool {
	select {
	case <-m.done:
		return true
	default:
		return false
	}
}

// TrySend enqueues without blocking, returning ErrMailboxFull when the mailbox
// is full or ErrActorStopped when stopped.
func (m *Machine[TCtx, TEvt]) TrySend(evt TEvt) error {
	if !m.started.Load() || m.stopped() {
		return ErrActorStopped
	}
	select {
	case <-m.done:
		return ErrActorStopped
	case m.mailbox <- inboxItem[TEvt]{evt: evt, desc: evt.EventType(), hasEvt: true}:
		return nil
	default:
		return ErrMailboxFull
	}
}

// Subscribe registers a snapshot listener with strong delivery (decision 25).
// Before Start the listener is queued; after Start it is handed to the actor.
func (m *Machine[TCtx, TEvt]) Subscribe(ctx context.Context, fn func(Snapshot[TCtx]), opts ...SubscribeOption) {
	o := subscribeOptions{buffer: defaultSubscriberBuffer}
	for _, op := range opts {
		op(&o)
	}
	s := &subscriber[TCtx]{ch: make(chan Snapshot[TCtx], o.buffer), fn: fn, ctx: ctx}
	if !m.started.Load() {
		m.preSubs = append(m.preSubs, s)
		return
	}
	select {
	case m.subscribeCh <- s:
	case <-m.done:
	}
}

// Close initiates bottom-up shutdown and waits for the actor to drain (decision
// 40). Idempotent; safe to defer.
func (m *Machine[TCtx, TEvt]) Close() error {
	if !m.started.Load() {
		return nil
	}
	m.closeOnce.Do(func() { m.loopCancel() })
	<-m.done
	return nil
}

// CloseAfterDrain processes the events already in the mailbox, then shuts down
// (decision 40). If ctx expires first it falls back to a hard Close and returns
// ctx.Err(). Idempotent with Close.
func (m *Machine[TCtx, TEvt]) CloseAfterDrain(ctx context.Context) error {
	if !m.started.Load() || m.stopped() {
		return nil
	}
	select {
	case m.mailbox <- inboxItem[TEvt]{drainClose: true}:
	case <-m.done:
		return nil
	case <-ctx.Done():
		return m.hardClose(ctx.Err())
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return m.hardClose(ctx.Err())
	}
}

func (m *Machine[TCtx, TEvt]) hardClose(cause error) error {
	m.closeOnce.Do(func() { m.loopCancel() })
	<-m.done
	return cause
}

// Settle blocks until the actor has processed every event currently enqueued
// and returned to idle, or the actor stops. It enqueues a no-op ping after the
// current mailbox contents; FIFO ordering means the ping is handled only once
// prior events' macrosteps complete. With a ManualClock this makes scenarios
// deterministic (decision 53). Returns nil once settled or the actor is terminal.
func (m *Machine[TCtx, TEvt]) Settle(ctx context.Context) error {
	if !m.started.Load() {
		return ErrActorStopped
	}
	for {
		ack := make(chan bool, 1)
		select {
		case <-m.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case m.mailbox <- inboxItem[TEvt]{ack: ack}:
		}
		select {
		case idle := <-ack:
			// A spawned invoke (e.g. FakeActor) may have appended its outcome
			// before this ping; the actor reports emptiness authoritatively, so
			// loop until it is truly drained.
			if idle {
				return nil
			}
		case <-m.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// --- actor goroutine ------------------------------------------------

func (m *Machine[TCtx, TEvt]) loop() {
	defer m.wg.Done()
	if !m.execMicrostep(m.eng.start()) || m.eng.status.Terminal() || !m.runMacrostep() {
		m.shutdown()
		return
	}
	m.reconcileInvokes()
	for {
		select {
		case <-m.loopCtx.Done():
			m.shutdown()
			return
		case s := <-m.subscribeCh:
			m.localSubs = append(m.localSubs, s)
			m.startDrain(s)
		case s := <-m.unsubscribeCh:
			m.removeSub(s)
		case it := <-m.mailbox:
			if it.ack != nil {
				// Report, on the actor goroutine, whether the mailbox is now empty
				// — authoritative for "settled" (a test-goroutine len() check races
				// the actor dequeuing a cascade event).
				it.ack <- len(m.mailbox) == 0
				continue
			}
			if it.drainClose {
				m.drainAndClose()
				return
			}
			if it.hasEvt {
				m.eng.offer(it.evt)
			} else {
				m.eng.offerDescriptor(it.desc)
			}
			if !m.runMacrostep() {
				m.shutdown()
				return
			}
			m.reconcileInvokes()
		}
	}
}

// drainAndClose processes the events already queued in the mailbox (non-blocking)
// then shuts down — the CloseAfterDrain path (decision 40). Events arriving after
// the queue momentarily empties are not waited for.
func (m *Machine[TCtx, TEvt]) drainAndClose() {
	for {
		select {
		case it := <-m.mailbox:
			if it.ack != nil {
				it.ack <- true // draining toward close; report settled
				continue
			}
			if it.drainClose {
				continue
			}
			if it.hasEvt {
				m.eng.offer(it.evt)
			} else {
				m.eng.offerDescriptor(it.desc)
			}
			if !m.runMacrostep() {
				m.shutdown()
				return
			}
		default:
			m.shutdown()
			return
		}
	}
}

// runMacrostep drives microsteps to quiescence; returns false when the actor
// became terminal (Done, observer abort, or always-loop guard).
func (m *Machine[TCtx, TEvt]) runMacrostep() bool {
	for {
		ms, quiescent := m.eng.step()
		if quiescent {
			if m.eng.status == StatusError {
				m.publishTerminalError(m.eng.errReason)
				return false
			}
			return true
		}
		if !m.execMicrostep(ms) {
			return false
		}
		if m.eng.status.Terminal() {
			return false
		}
	}
}

// execMicrostep runs observers on the pending transition, then (on success)
// commits, publishes, arms/cancels timers, drains the outbox, and notifies
// subscribers. Returns false on observer abort (terminal).
func (m *Machine[TCtx, TEvt]) execMicrostep(ms *Microstep[TCtx]) bool {
	before := *m.snap.Load()
	status := StatusRunning
	if ms.ReachedFinal {
		status = StatusDone
	}
	pending := m.plannedPending(ms)
	after := Snapshot[TCtx]{
		MachineID:      m.def.ID,
		Address:        m.addr,
		ActiveStates:   ms.NextActive,
		Context:        ms.NextContext,
		PendingAfter:   pending,
		Status:         status,
		Version:        m.version + 1,
		InvokeRestarts: m.restartsSnapshot(),
	}
	evt := m.eng.triggerEvent()
	for _, o := range m.transitionObs {
		if err := o.OnTransition(m.loopCtx, before, after, evt); err != nil {
			m.publishObserverAbort(before, err)
			return false
		}
	}
	m.eng.commit(ms)
	m.version++
	m.snap.Store(&after)
	m.cancelAfterTimers(ms.Exited)
	m.armAfterTimers(ms.Entered)
	m.drainOutbox(ms)
	m.notifySubs(after)
	return true
}

// plannedPending computes the PendingAfter timers the next snapshot will hold:
// timers surviving this microstep's exits plus those its entries arm. Pure — the
// real scheduling happens post-publish in armAfterTimers.
func (m *Machine[TCtx, TEvt]) plannedPending(ms *Microstep[TCtx]) []ScheduledTimer {
	exited := make(map[*StateNode]struct{}, len(ms.Exited))
	for _, n := range ms.Exited {
		exited[n] = struct{}{}
	}
	var out []ScheduledTimer
	for n, arms := range m.armed {
		if _, gone := exited[n]; gone {
			continue
		}
		for _, a := range arms {
			out = append(out, ScheduledTimer{StateID: n.ID, Descriptor: a.descriptor, Deadline: a.deadline})
		}
	}
	now := m.cfg.clock.Now()
	for _, n := range ms.Entered {
		for _, t := range n.Transitions {
			if !t.IsAfter {
				continue
			}
			out = append(out, ScheduledTimer{StateID: n.ID, Descriptor: t.Event, Deadline: now.Add(m.resolveDelay(t.Delay))})
		}
	}
	return out
}

func (m *Machine[TCtx, TEvt]) armAfterTimers(entered []*StateNode) {
	now := m.cfg.clock.Now()
	for _, n := range entered {
		for _, t := range n.Transitions {
			if !t.IsAfter {
				continue
			}
			desc := t.Event
			deadline := now.Add(m.resolveDelay(t.Delay))
			st := ScheduledTimer{StateID: n.ID, Descriptor: desc, Deadline: deadline}
			tm := m.cfg.timers.Schedule(m.loopCtx, deadline, func() {
				select {
				case m.mailbox <- inboxItem[TEvt]{desc: desc}:
				case <-m.done:
				}
				for _, o := range m.timerObs {
					o.OnTimerFired(st)
				}
			})
			m.armed[n] = append(m.armed[n], armedTimer{timer: tm, descriptor: desc, deadline: deadline, state: n})
			for _, o := range m.timerObs {
				o.OnTimerScheduled(st)
			}
		}
	}
}

func (m *Machine[TCtx, TEvt]) cancelAfterTimers(exited []*StateNode) {
	for _, n := range exited {
		for _, a := range m.armed[n] {
			a.timer.Cancel()
		}
		delete(m.armed, n)
	}
}

func (m *Machine[TCtx, TEvt]) resolveDelay(d Delay) time.Duration {
	if d.Symbol != "" {
		return m.cfg.delays[d.Symbol]
	}
	return time.Duration(d.Millis) * time.Millisecond
}

// drainOutbox fires Send/Spawn effects after publish (decision 27). SendTo routes
// the command to the target child's deliver hook (a callback's receive subset or
// a child machine's mailbox); a target with no live child is dropped.
func (m *Machine[TCtx, TEvt]) drainOutbox(ms *Microstep[TCtx]) {
	for _, eff := range ms.Effects {
		if eff.Kind != EffectSend {
			continue // dynamic Spawn handled via reconcile/registration
		}
		// Target is the invoke id (what codegen knows statically) or a full child
		// address (what hand-wired callers may use).
		ch := m.children[string(eff.Target)]
		if ch == nil {
			ch = m.childByAddress(eff.Target)
		}
		if ch != nil && ch.deliver != nil {
			ch.deliver(eff.Event)
		}
	}
}

func (m *Machine[TCtx, TEvt]) publishObserverAbort(before Snapshot[TCtx], cause error) {
	fail := before
	fail.Status = StatusError
	fail.ErrorReason = &ObserverError{Cause: cause, ObserverType: "transition"}
	m.version++
	fail.Version = m.version
	m.snap.Store(&fail)
	m.notifySubs(fail)
}

func (m *Machine[TCtx, TEvt]) publishTerminalError(reason error) {
	cur := *m.snap.Load()
	cur.Status = StatusError
	cur.ErrorReason = reason
	m.version++
	cur.Version = m.version
	m.snap.Store(&cur)
	m.notifySubs(cur)
}

func (m *Machine[TCtx, TEvt]) shutdown() {
	m.loopCancel()
	// Bottom-up: stop children before the parent stops accepting their events.
	for id, ch := range m.children {
		ch.cancel()
		delete(m.children, id)
	}
	for _, arms := range m.armed {
		for _, a := range arms {
			a.timer.Cancel()
		}
	}
	m.armed = nil
	cur := m.snap.Load()
	if !cur.Status.Terminal() {
		s := *cur
		s.Status = StatusStopped
		m.version++
		s.Version = m.version
		m.snap.Store(&s)
		m.notifySubs(s)
		cur = &s
	}
	var reason error
	if cur.Status == StatusError {
		reason = cur.ErrorReason
	}
	for _, ao := range m.actorObs {
		ao.OnActorStop(m.addr, reason)
	}
	for _, s := range m.localSubs {
		close(s.ch)
	}
	m.localSubs = nil
	close(m.done)
}

func (m *Machine[TCtx, TEvt]) notifySubs(snap Snapshot[TCtx]) {
	for _, s := range m.localSubs {
		select {
		case s.ch <- snap:
		case <-m.done:
		}
	}
}

func (m *Machine[TCtx, TEvt]) removeSub(s *subscriber[TCtx]) {
	for i, x := range m.localSubs {
		if x == s {
			m.localSubs = append(m.localSubs[:i], m.localSubs[i+1:]...)
			break
		}
	}
	close(s.ch)
}

// startDrain runs a subscriber's delivery goroutine: it calls fn for each
// snapshot until its ctx is cancelled (then requests removal and drains to the
// close) or the channel is closed by the actor on removal/shutdown.
func (m *Machine[TCtx, TEvt]) startDrain(s *subscriber[TCtx]) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case snap, ok := <-s.ch:
				if !ok {
					return
				}
				s.fn(snap)
			case <-s.ctx.Done():
				select {
				case m.unsubscribeCh <- s:
				case <-m.done:
				}
				for range s.ch { // drain until the actor closes the channel
				}
				return
			}
		}
	}()
}
