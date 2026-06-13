package statesman

import (
	"errors"
	"testing"
)

// ev is a test event carrying an arbitrary descriptor (stands in for generated
// events incl. done.invoke.* / error.invoke.* / xstate.after(..)# keys).
type ev struct{ t string }

func (e ev) EventType() string { return e.t }

type orderCtx struct{ Retries int }

// orderMachine hand-builds the spec §11 retry chart with callsites assigned:
// action 0=validateForm (emits a Send), action 1=incrementRetries; guard 0=hasRetriesLeft.
func orderMachine() *Definition {
	idle := mk("idle", StateAtomic)
	charging := mk("charging", StateAtomic)
	retrying := mk("retrying", StateAtomic)
	confirming := mk("confirming", StateAtomic)
	done := mk("done", StateFinal)
	errored := mk("errored", StateFinal)
	root := mk("order", StateCompound, idle, charging, retrying, confirming, done, errored)
	root.Initial = idle
	finalize(root)

	idle.Transitions = []*Transition{{
		Source: idle, Event: "SUBMIT", Targets: []*StateNode{charging},
		Actions: []ActionRef{{Type: "validateForm", Callsite: 0}}, DocOrder: 0,
	}}
	charging.Transitions = []*Transition{
		{Source: charging, Event: "error.invoke.charge",
			Guard:   &GuardRef{Type: "hasRetriesLeft", Callsite: 0},
			Targets: []*StateNode{retrying},
			Actions: []ActionRef{{Type: "incrementRetries", Callsite: 1}}, DocOrder: 0},
		{Source: charging, Event: "error.invoke.charge", Targets: []*StateNode{errored}, DocOrder: 1},
		{Source: charging, Event: "done.invoke.charge", Targets: []*StateNode{confirming}, DocOrder: 2},
	}
	retrying.Transitions = []*Transition{{
		Source: retrying, Event: "xstate.after(5s)#retrying", Targets: []*StateNode{charging}, DocOrder: 0,
	}}
	confirming.Transitions = []*Transition{{
		Source: confirming, Event: "CONFIRM", Targets: []*StateNode{done}, DocOrder: 0,
	}}
	return NewDefinition("order", root, 2, 1)
}

func orderAppliers() (func(int, orderCtx, ev) AppliedEffect[orderCtx], func(int, orderCtx, ev) bool) {
	apply := func(cs int, ctx orderCtx, _ ev) AppliedEffect[orderCtx] {
		switch cs {
		case 0: // validateForm -> Send to the inventory watcher
			return AppliedEffect[orderCtx]{Kind: EffectSend, Target: "/order/inventory", Event: ev{"WATCH_SKUS"}}
		case 1: // incrementRetries
			return AppliedEffect[orderCtx]{Kind: EffectAssign, Ctx: orderCtx{Retries: ctx.Retries + 1}}
		}
		return AppliedEffect[orderCtx]{Kind: EffectNoop}
	}
	guard := func(cs int, ctx orderCtx, _ ev) bool {
		return ctx.Retries < 3 // hasRetriesLeft
	}
	return apply, guard
}

func TestOrderRetryTrace(t *testing.T) {
	apply, guard := orderAppliers()
	e := newEngine[orderCtx, ev](orderMachine(), orderCtx{}, apply, guard, 0)

	commitStep := func() *Microstep[orderCtx] {
		ms, q := e.step()
		if q {
			return nil
		}
		e.commit(ms)
		return ms
	}

	init := e.start()
	e.commit(init)
	assertActive(t, "start", init.NextActive, "idle")

	feed := func(desc string) *Microstep[orderCtx] {
		e.offer(ev{desc})
		ms := commitStep()
		// drain any cascading eventless/internal microsteps.
		for {
			more := commitStep()
			if more == nil {
				break
			}
			ms = more
		}
		return ms
	}

	ms := feed("SUBMIT")
	assertActive(t, "after SUBMIT", ms.NextActive, "charging")
	if len(ms.Effects) != 1 || ms.Effects[0].Kind != EffectSend || ms.Effects[0].Event.EventType() != "WATCH_SKUS" {
		t.Fatalf("SUBMIT should emit one Send (WATCH_SKUS), got %+v", ms.Effects)
	}

	ms = feed("error.invoke.charge")
	assertActive(t, "after error", ms.NextActive, "retrying")
	if e.ctx.Retries != 1 {
		t.Fatalf("Retries = %d, want 1 after first failure", e.ctx.Retries)
	}

	ms = feed("xstate.after(5s)#retrying")
	assertActive(t, "after retry delay", ms.NextActive, "charging")

	ms = feed("done.invoke.charge")
	assertActive(t, "after success", ms.NextActive, "confirming")

	ms = feed("CONFIRM")
	assertActive(t, "after confirm", ms.NextActive, "done")
	if e.status != StatusDone {
		t.Fatalf("status = %v, want done", e.status)
	}
	if e.ctx.Retries != 1 {
		t.Fatalf("final Retries = %d, want 1", e.ctx.Retries)
	}
}

func TestOrderRetryExhaustion(t *testing.T) {
	apply, guard := orderAppliers()
	e := newEngine[orderCtx, ev](orderMachine(), orderCtx{}, apply, guard, 0)
	e.commit(e.start())

	feed := func(desc string) {
		e.offer(ev{desc})
		for {
			ms, q := e.step()
			if q {
				return
			}
			e.commit(ms)
		}
	}

	feed("SUBMIT")
	// Three failures consume the budget (Retries 0->1->2->3), each followed by the
	// backoff that re-enters charging.
	for i := 0; i < 3; i++ {
		feed("error.invoke.charge")
		feed("xstate.after(5s)#retrying")
	}
	if e.ctx.Retries != 3 {
		t.Fatalf("Retries = %d, want 3 before exhaustion", e.ctx.Retries)
	}
	// Fourth failure: guard 3<3 is false, so the fall-through edge to errored fires.
	feed("error.invoke.charge")
	assertActive(t, "exhausted", e.cfg.atomicIDs(), "errored")
	if e.status != StatusDone {
		t.Fatalf("status = %v, want done (errored is final)", e.status)
	}
}

func TestAlwaysLoopGuard(t *testing.T) {
	a := mk("a", StateAtomic)
	b := mk("b", StateAtomic)
	root := mk("m", StateCompound, a, b)
	root.Initial = a
	finalize(root)
	a.Transitions = []*Transition{{Source: a, Event: "", Targets: []*StateNode{b}}}
	b.Transitions = []*Transition{{Source: b, Event: "", Targets: []*StateNode{a}}}
	def := NewDefinition("m", root, 0, 0)

	apply := func(int, orderCtx, ev) AppliedEffect[orderCtx] { return AppliedEffect[orderCtx]{Kind: EffectNoop} }
	guard := func(int, orderCtx, ev) bool { return true }
	e := newEngine[orderCtx, ev](def, orderCtx{}, apply, guard, 5)
	e.commit(e.start())
	for {
		ms, q := e.step()
		if q {
			break
		}
		e.commit(ms)
	}
	if e.status != StatusError {
		t.Fatalf("status = %v, want error on runaway always loop", e.status)
	}
	if !errors.Is(e.errReason, ErrAlwaysLoopExceeded) {
		t.Fatalf("errReason = %v, want ErrAlwaysLoopExceeded", e.errReason)
	}
}

func assertActive(t *testing.T, label string, got []StateID, want ...StateID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: active=%v want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: active=%v want %v", label, got, want)
		}
	}
}
