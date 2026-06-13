package statesman_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/schema"
	"github.com/andrioid/statesman/statesmantest"
)

type oEvt struct{ t string }

func (e oEvt) EventType() string { return e.t }

type oCtx struct{ Retries int }

func loadOrder(t *testing.T) *statesman.Definition {
	t.Helper()
	data, err := os.ReadFile("schema/testdata/order.json")
	if err != nil {
		t.Fatalf("read order.json: %v", err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatalf("load order.json: %v", err)
	}
	return def
}

// callsiteMaps extracts action/guard type -> Callsite so tests can wire appliers
// without hard-coding the loader's enumeration order.
func callsiteMaps(def *statesman.Definition) (actType, guardType map[int]string) {
	actType = map[int]string{}
	guardType = map[int]string{}
	visitT := func(t *statesman.Transition) {
		if t.Guard != nil {
			guardType[t.Guard.Callsite] = t.Guard.Type
		}
		for _, a := range t.Actions {
			actType[a.Callsite] = a.Type
		}
	}
	var visit func(n *statesman.StateNode)
	visit = func(n *statesman.StateNode) {
		for _, a := range n.Entry {
			actType[a.Callsite] = a.Type
		}
		for _, a := range n.Exit {
			actType[a.Callsite] = a.Type
		}
		for _, tr := range n.Transitions {
			visitT(tr)
		}
		for _, iv := range n.Invokes {
			for _, tr := range iv.OnDone {
				visitT(tr)
			}
			for _, tr := range iv.OnError {
				visitT(tr)
			}
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(def.Root)
	return actType, guardType
}

// orderAppliers dispatches by action/guard TYPE, so every callsite of a given
// action (e.g. incrementRetries wired on both the error and timeout edges)
// behaves identically — mirroring a codegen fallback method.
func orderAppliers(actType, guardType map[int]string) (func(int, oCtx, oEvt) statesman.AppliedEffect[oCtx], func(int, oCtx, oEvt) bool) {
	apply := func(cs int, ctx oCtx, _ oEvt) statesman.AppliedEffect[oCtx] {
		switch actType[cs] {
		case "incrementRetries":
			return statesman.AppliedEffect[oCtx]{Kind: statesman.EffectAssign, Ctx: oCtx{Retries: ctx.Retries + 1}}
		case "validateForm":
			return statesman.AppliedEffect[oCtx]{Kind: statesman.EffectSend, Target: "/order-1/inventory", Event: oEvt{"WATCH_SKUS"}}
		}
		return statesman.AppliedEffect[oCtx]{Kind: statesman.EffectNoop}
	}
	guard := func(cs int, ctx oCtx, _ oEvt) bool {
		switch guardType[cs] {
		case "hasRetriesLeft":
			return ctx.Retries < 3
		}
		return true
	}
	return apply, guard
}

func newOrderSync(t *testing.T) *statesmantest.Sync[oCtx, oEvt] {
	t.Helper()
	def := loadOrder(t)
	acts, guards := callsiteMaps(def)
	apply, guard := orderAppliers(acts, guards)
	return statesmantest.NewSync[oCtx, oEvt](func(o ...statesman.Option) *statesman.Machine[oCtx, oEvt] {
		o = append(o, statesman.WithDelays(map[string]time.Duration{"RetryDelay": 5 * time.Second}))
		return statesman.NewMachine[oCtx, oEvt](def, oCtx{}, apply, guard, o...)
	})
}

func hasState(snap statesman.Snapshot[oCtx], id statesman.StateID) bool {
	for _, s := range snap.ActiveStates {
		if s == id {
			return true
		}
	}
	return false
}

func TestRuntimeRetryLoop(t *testing.T) {
	ctx := context.Background()
	s := newOrderSync(t)
	if err := s.Start(ctx, "order-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !hasState(s.Snapshot(), "idle") {
		t.Fatalf("initial active = %v, want idle", s.Snapshot().ActiveStates)
	}

	mustSend(t, s, "SUBMIT")
	if !hasState(s.Snapshot(), "processing.charging") {
		t.Fatalf("after SUBMIT = %v, want processing.charging", s.Snapshot().ActiveStates)
	}

	mustSend(t, s, "error.invoke.charge")
	if !hasState(s.Snapshot(), "retrying") {
		t.Fatalf("after failure = %v, want retrying", s.Snapshot().ActiveStates)
	}
	if s.Snapshot().Context.Retries != 1 {
		t.Fatalf("Retries = %d, want 1", s.Snapshot().Context.Retries)
	}
	// PendingAfter should hold the RetryDelay timer while in retrying.
	if len(s.Snapshot().PendingAfter) != 1 {
		t.Fatalf("PendingAfter = %v, want one armed timer", s.Snapshot().PendingAfter)
	}

	if err := s.Advance(ctx, 5*time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !hasState(s.Snapshot(), "processing.charging") {
		t.Fatalf("after retry delay = %v, want processing.charging", s.Snapshot().ActiveStates)
	}

	mustSend(t, s, "done.invoke.charge")
	if !hasState(s.Snapshot(), "confirming") {
		t.Fatalf("after success = %v, want confirming", s.Snapshot().ActiveStates)
	}

	mustSend(t, s, "CONFIRM")
	snap := s.Snapshot()
	if !hasState(snap, "done") || snap.Status != statesman.StatusDone {
		t.Fatalf("final = %v status %v, want done/Done", snap.ActiveStates, snap.Status)
	}
	if snap.Context.Retries != 1 {
		t.Fatalf("final Retries = %d, want 1", snap.Context.Retries)
	}
	_ = s.M.Close()
}

func TestRuntimeTimeoutFiresGuardedEdge(t *testing.T) {
	ctx := context.Background()
	s := newOrderSync(t)
	if err := s.Start(ctx, "order-2"); err != nil {
		t.Fatalf("start: %v", err)
	}
	mustSend(t, s, "SUBMIT")
	// The 30s timeout on charging fires the same guarded edge as a transport error.
	if err := s.Advance(ctx, 30*time.Second); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !hasState(s.Snapshot(), "retrying") {
		t.Fatalf("after timeout = %v, want retrying", s.Snapshot().ActiveStates)
	}
	if s.Snapshot().Context.Retries != 1 {
		t.Fatalf("Retries = %d, want 1 after timeout", s.Snapshot().Context.Retries)
	}
	_ = s.M.Close()
}

type abortOnCharging struct{ calls int }

func (o *abortOnCharging) OnTransition(_ context.Context, _, after statesman.Snapshot[oCtx], _ oEvt) error {
	o.calls++
	for _, st := range after.ActiveStates {
		if st == "processing.charging" {
			return errors.New("durable write failed")
		}
	}
	return nil
}

func TestRuntimeObserverAbortRollsBack(t *testing.T) {
	ctx := context.Background()
	def := loadOrder(t)
	acts, guards := callsiteMaps(def)
	apply, guard := orderAppliers(acts, guards)
	m := statesman.NewMachine[oCtx, oEvt](def, oCtx{}, apply, guard)
	obs := &abortOnCharging{}
	m.AddObserver(obs)
	if err := m.Start(ctx, "order-3"); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = m.Settle(ctx)
	// idle committed fine.
	if !hasState(m.Snapshot(), "idle") {
		t.Fatalf("expected idle after start, got %v", m.Snapshot().ActiveStates)
	}
	// SUBMIT would enter charging; the observer aborts -> rollback to idle, Error.
	_ = m.Send(ctx, oEvt{"SUBMIT"})
	_ = m.Settle(ctx)
	snap := m.Snapshot()
	if snap.Status != statesman.StatusError {
		t.Fatalf("status = %v, want Error after observer abort", snap.Status)
	}
	if !hasState(snap, "idle") {
		t.Fatalf("rolled-back active = %v, want idle (pre-transition)", snap.ActiveStates)
	}
	var oerr *statesman.ObserverError
	if !errors.As(snap.ErrorReason, &oerr) {
		t.Fatalf("ErrorReason = %v, want *ObserverError", snap.ErrorReason)
	}
	if err := m.Send(ctx, oEvt{"SUBMIT"}); !errors.Is(err, statesman.ErrActorStopped) {
		t.Fatalf("send after terminal = %v, want ErrActorStopped", err)
	}
	_ = m.Close()
}

func TestRuntimeCloseThenSend(t *testing.T) {
	ctx := context.Background()
	s := newOrderSync(t)
	if err := s.Start(ctx, "order-4"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.M.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.M.Close(); err != nil {
		t.Fatalf("second close should be idempotent: %v", err)
	}
	if err := s.M.Send(ctx, oEvt{"SUBMIT"}); !errors.Is(err, statesman.ErrActorStopped) {
		t.Fatalf("send after close = %v, want ErrActorStopped", err)
	}
}

func TestRuntimeSubscriberReceivesSnapshots(t *testing.T) {
	ctx := context.Background()
	def := loadOrder(t)
	acts, guards := callsiteMaps(def)
	apply, guard := orderAppliers(acts, guards)
	m := statesman.NewMachine[oCtx, oEvt](def, oCtx{}, apply, guard)

	got := make(chan statesman.Snapshot[oCtx], 16)
	m.Subscribe(ctx, func(s statesman.Snapshot[oCtx]) { got <- s }) // before Start

	if err := m.Start(ctx, "order-5"); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = m.Settle(ctx)
	_ = m.Send(ctx, oEvt{"SUBMIT"})
	_ = m.Settle(ctx)

	recv := func() statesman.Snapshot[oCtx] {
		t.Helper()
		select {
		case s := <-got:
			return s
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for subscriber delivery")
			return statesman.Snapshot[oCtx]{}
		}
	}
	first := recv()  // idle
	second := recv() // processing.charging
	if !hasState(first, "idle") {
		t.Fatalf("first snapshot = %v, want idle", first.ActiveStates)
	}
	if !hasState(second, "processing.charging") {
		t.Fatalf("second snapshot = %v, want processing.charging", second.ActiveStates)
	}
	_ = m.Close()
}

func TestRuntimeCloseAfterDrain(t *testing.T) {
	ctx := context.Background()
	def := loadOrder(t)
	actType, guardType := callsiteMaps(def)
	apply, guard := orderAppliers(actType, guardType)
	m := statesman.NewMachine[oCtx, oEvt](def, oCtx{}, apply, guard)
	if err := m.Start(ctx, "order-6"); err != nil {
		t.Fatalf("start: %v", err)
	}
	_ = m.Settle(ctx)
	// Enqueue SUBMIT, then ask for a graceful drain: SUBMIT must be processed
	// before shutdown (FIFO before the drain sentinel).
	if err := m.Send(ctx, oEvt{"SUBMIT"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := m.CloseAfterDrain(ctx); err != nil {
		t.Fatalf("CloseAfterDrain: %v", err)
	}
	snap := m.Snapshot()
	if !hasState(snap, "processing.charging") {
		t.Fatalf("drained snapshot = %v, want SUBMIT processed to processing.charging", snap.ActiveStates)
	}
	if snap.Status != statesman.StatusStopped {
		t.Fatalf("status = %v, want Stopped after drain close", snap.Status)
	}
	if err := m.Send(ctx, oEvt{"SUBMIT"}); !errors.Is(err, statesman.ErrActorStopped) {
		t.Fatalf("send after drain-close = %v, want ErrActorStopped", err)
	}
}

func mustSend(t *testing.T, s *statesmantest.Sync[oCtx, oEvt], desc string) {
	t.Helper()
	if err := s.SendAndSettle(context.Background(), oEvt{desc}); err != nil {
		t.Fatalf("send %s: %v", desc, err)
	}
}
