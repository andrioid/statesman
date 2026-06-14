package statesman

import (
	"context"
	"io"
	"strings"
	"time"
)

// --- Actor capability interfaces (composed à la io) -----------------

type Sender[T any] interface {
	Send(ctx context.Context, evt T) error
}

type Snapshotter[T any] interface {
	Snapshot() Snapshot[T]
}

type Subscriber[T any] interface {
	Subscribe(ctx context.Context, fn func(T), opts ...SubscribeOption)
}

type Addressable interface {
	Address() ActorAddress
}

// ActorRef is the uniform handle to any actor. *Machine satisfies it. Promise and
// observable refs use Never for TEvt, making them non-sendable (decision 45).
type ActorRef[TCtx any, TEvt EventBase] interface {
	Sender[TEvt]
	Snapshotter[TCtx]
	Subscriber[Snapshot[TCtx]]
	Addressable
	io.Closer
}

// --- Invoke runner seam ---------------------------------------------

// InvokeRunner starts one invoked actor. It delivers parent events through emit
// — intermediate callback emissions and the terminal done.invoke.<id> /
// error.invoke.<id>. addr is the child's hierarchical address. It returns a
// handle to cancel the child and (for sendable children) route commands to it.
// Generated code builds these from the typed adapter constructors below;
// statesmantest.FakeActor builds scripted ones.
type InvokeRunner[TCtx any, TEvt EventBase] func(ctx context.Context, parentCtx TCtx, addr ActorAddress, emit func(TEvt)) RunningInvoke

// RunningInvoke is a live child's control surface.
type RunningInvoke struct {
	Cancel  func()          // stop the child (also cancelled via ctx)
	Deliver func(EventBase) // route a SendTo command; nil if not sendable
}

type runningChild struct {
	id      string
	address ActorAddress
	cancel  func()
	deliver func(EventBase)
}

// RegisterInvoke wires the runner for an invoke id. Call before Start; generated
// constructors do this from the machine's declared invokes.
func (m *Machine[TCtx, TEvt]) RegisterInvoke(id string, r InvokeRunner[TCtx, TEvt]) {
	if m.started.Load() {
		return
	}
	if m.runners == nil {
		m.runners = make(map[string]InvokeRunner[TCtx, TEvt])
	}
	m.runners[id] = r
}

// reconcileInvokes spawns invokes for states active at macrostep quiescence and
// stops those whose owning state has exited (deferred-invoke, §7.4). Runs on the
// actor goroutine after each settled macrostep.
func (m *Machine[TCtx, TEvt]) reconcileInvokes() {
	desired := make(map[string]struct{})
	for _, n := range m.eng.cfg.all() {
		for _, iv := range n.Invokes {
			desired[iv.ID] = struct{}{}
		}
	}
	for id, ch := range m.children {
		if _, ok := desired[id]; !ok {
			ch.cancel()
			delete(m.children, id)
		}
	}
	for id := range desired {
		if _, ok := m.children[id]; ok {
			continue
		}
		runner := m.runners[id]
		if runner == nil {
			continue // no runner registered (e.g. a not-yet-wired invoke); skip
		}
		childCtx, cancel := context.WithCancel(m.loopCtx)
		addr := m.addr.Child(id)
		emit := func(e TEvt) {
			select {
			case m.mailbox <- inboxItem[TEvt]{evt: e, desc: e.EventType(), hasEvt: true}:
			case <-m.done:
			}
		}
		ri := runner(childCtx, m.eng.ctx, addr, emit)
		stop := func() {
			cancel()
			if ri.Cancel != nil {
				ri.Cancel()
			}
		}
		m.children[id] = &runningChild{id: id, address: addr, cancel: stop, deliver: ri.Deliver}
		if m.invokeSpawns == nil {
			m.invokeSpawns = make(map[string]int)
		}
		m.invokeSpawns[id]++
	}
}

// restartsSnapshot returns the per-invoke restart count (spawns beyond the first)
// for the next published snapshot, or nil when nothing has restarted. Runs on the
// actor goroutine; the returned map is frozen — never mutated after publication —
// so lock-free readers of the snapshot see a stable value.
func (m *Machine[TCtx, TEvt]) restartsSnapshot() map[string]int {
	var out map[string]int
	for id, n := range m.invokeSpawns {
		if n <= 1 {
			continue
		}
		if out == nil {
			out = make(map[string]int, len(m.invokeSpawns))
		}
		out[id] = n - 1
	}
	return out
}

func (m *Machine[TCtx, TEvt]) childByAddress(addr ActorAddress) *runningChild {
	for _, ch := range m.children {
		if ch.address == addr {
			return ch
		}
	}
	return nil
}

// --- Typed adapter constructors -------------------------------------

// PromiseActor adapts a one-shot (ctx, In) -> (Out, error) into an InvokeRunner.
// input derives the call argument from the parent context; onDone/onError map the
// outcome to the parent's done.invoke.<id> / error.invoke.<id> event. The ref is
// non-sendable (Deliver nil).
func PromiseActor[TCtx any, TEvt EventBase, In, Out any](
	fn func(context.Context, In) (Out, error),
	input func(TCtx) In,
	onDone func(Out) TEvt,
	onError func(error) TEvt,
) InvokeRunner[TCtx, TEvt] {
	return func(ctx context.Context, parentCtx TCtx, _ ActorAddress, emit func(TEvt)) RunningInvoke {
		in := input(parentCtx)
		go func() {
			out, err := fn(ctx, in)
			if ctx.Err() != nil {
				return // cancelled on state exit; outcome discarded
			}
			if err != nil {
				emit(onError(err))
				return
			}
			emit(onDone(out))
		}()
		return RunningInvoke{Cancel: func() {}}
	}
}

// CallbackActor adapts a long-running (ctx, emit, receive) -> error callback. Its
// emit subset events flow to the parent; SendTo commands of type Cmd are routed
// to receive. fn returning nil emits onDone; non-nil emits onError (if provided).
func CallbackActor[TCtx any, TEvt EventBase, Cmd EventBase](
	fn func(ctx context.Context, emit func(TEvt), receive <-chan Cmd) error,
	onDone func() TEvt,
	onError func(error) TEvt,
) InvokeRunner[TCtx, TEvt] {
	return func(ctx context.Context, _ TCtx, _ ActorAddress, emit func(TEvt)) RunningInvoke {
		recv := make(chan Cmd, 16)
		go func() {
			err := fn(ctx, emit, recv)
			if ctx.Err() != nil {
				return
			}
			switch {
			case err != nil && onError != nil:
				emit(onError(err))
			case err == nil && onDone != nil:
				emit(onDone())
			}
		}()
		deliver := func(e EventBase) {
			c, ok := e.(Cmd)
			if !ok {
				return
			}
			select {
			case recv <- c:
			case <-ctx.Done():
			}
		}
		return RunningInvoke{Cancel: func() {}, Deliver: deliver}
	}
}

// ObservableActor adapts a subscription (ctx, next) -> error. Each emitted Out is
// mapped to a parent event; completion/error map to onDone/onError. Non-sendable.
func ObservableActor[TCtx any, TEvt EventBase, Out any](
	subscribe func(ctx context.Context, next func(Out)) error,
	onNext func(Out) TEvt,
	onDone func() TEvt,
	onError func(error) TEvt,
) InvokeRunner[TCtx, TEvt] {
	return func(ctx context.Context, _ TCtx, _ ActorAddress, emit func(TEvt)) RunningInvoke {
		go func() {
			err := subscribe(ctx, func(o Out) {
				if onNext != nil {
					emit(onNext(o))
				}
			})
			if ctx.Err() != nil {
				return
			}
			switch {
			case err != nil && onError != nil:
				emit(onError(err))
			case err == nil && onDone != nil:
				emit(onDone())
			}
		}()
		return RunningInvoke{Cancel: func() {}}
	}
}

// MachineActor adapts a nested child Machine (fromMachine). newChild builds a
// fresh child per spawn (so a re-entered invoke restarts cleanly); input seeds the
// child's initial context from the parent before Start, the machine analogue of a
// promise's input mapper. The child runs at the invoke address; its terminal
// status maps to the parent's done/error event (the done payload is the child's
// final Context). The child is sendable: SendTo commands of the child's event type
// route to its mailbox.
func MachineActor[TCtx any, TEvt EventBase, CCtx any, CEvt EventBase](
	newChild func() *Machine[CCtx, CEvt],
	input func(TCtx) CCtx,
	onDone func(Snapshot[CCtx]) TEvt,
	onError func(Snapshot[CCtx]) TEvt,
) InvokeRunner[TCtx, TEvt] {
	return func(ctx context.Context, parentCtx TCtx, addr ActorAddress, emit func(TEvt)) RunningInvoke {
		child := newChild()
		if input != nil {
			child.SetInitialContext(input(parentCtx))
		}
		child.Subscribe(ctx, func(s Snapshot[CCtx]) {
			switch s.Status {
			case StatusDone:
				if onDone != nil {
					emit(onDone(s))
				}
			case StatusError:
				if onError != nil {
					emit(onError(s))
				}
			}
		})
		_ = child.Start(ctx, strings.TrimPrefix(string(addr), "/"))
		deliver := func(e EventBase) {
			if ce, ok := e.(CEvt); ok {
				_ = child.Send(ctx, ce)
			}
		}
		return RunningInvoke{Cancel: func() { _ = child.Close() }, Deliver: deliver}
	}
}

// BackoffActor adapts a context-reading delay into an InvokeRunner for dynamic
// (exponential / jittered) backoff that the static `after` delays cannot express
// (resolveDelay sees no context). On spawn it schedules a timer for
// delay(parentCtx) from clock.Now() via timers; on fire it emits onDone — wire
// that to the backoff state's done.invoke.<id> edge. State exit cancels the child,
// which cancels the timer, and no event is emitted.
//
// Pass the same Clock/TimerService the machine runs on (the ManualTimerService in
// a Sync test, the in-process or DB-backed one in production) so the wait advances
// on that clock. Wire it with RegisterInvoke: the schema's `invoke.src` carries no
// signature for codegen to detect, so generated constructors do not emit it.
//
// Backoff is an invoke, not a durable `after`, so the remaining wait is not
// recorded in Snapshot.PendingAfter. The attempt counter you read in delay lives
// in Context (durable), so a restart recomputes the delay from the persisted count.
func BackoffActor[TCtx any, TEvt EventBase](
	clock Clock,
	timers TimerService,
	delay func(TCtx) time.Duration,
	onDone func() TEvt,
) InvokeRunner[TCtx, TEvt] {
	return func(ctx context.Context, parentCtx TCtx, _ ActorAddress, emit func(TEvt)) RunningInvoke {
		timer := timers.Schedule(ctx, clock.Now().Add(delay(parentCtx)), func() {
			if ctx.Err() != nil {
				return // cancelled on state exit; no event for a stopped backoff
			}
			emit(onDone())
		})
		return RunningInvoke{Cancel: func() { timer.Cancel() }}
	}
}
