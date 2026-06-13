package statesmantest

import (
	"context"
	"sync"

	"github.com/andrioid/statesman"
)

// FakeActor builds an InvokeRunner that emits one scripted parent event per spawn
// — one outcome per invoke attempt, in order (reusing the replay machinery,
// decision 53). Running past the script (or a nil-equivalent) emits nothing: a
// Pending actor that only resolves via timeout or cancellation. Outcomes are
// delivered synchronously at spawn, so scenarios settle deterministically.
func FakeActor[TCtx any, TEvt statesman.EventBase](outcomes ...TEvt) statesman.InvokeRunner[TCtx, TEvt] {
	var mu sync.Mutex
	var attempt int
	return func(_ context.Context, _ TCtx, _ statesman.ActorAddress, emit func(TEvt)) statesman.RunningInvoke {
		mu.Lock()
		var out TEvt
		has := attempt < len(outcomes)
		if has {
			out = outcomes[attempt]
			attempt++
		}
		mu.Unlock()
		if has {
			emit(out)
		}
		return statesman.RunningInvoke{Cancel: func() {}}
	}
}

// CommandRecorder captures the SendTo commands routed to a fake callback child.
type CommandRecorder[Cmd statesman.EventBase] struct {
	mu   sync.Mutex
	cmds []Cmd
}

func (r *CommandRecorder[Cmd]) add(c Cmd) {
	r.mu.Lock()
	r.cmds = append(r.cmds, c)
	r.mu.Unlock()
}

// Commands returns a copy of the commands received so far.
func (r *CommandRecorder[Cmd]) Commands() []Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Cmd(nil), r.cmds...)
}

// FakeCallback builds a sendable InvokeRunner that records the SendTo commands it
// receives (no emissions). Stands in for a callback actor in scenarios.
func FakeCallback[TCtx any, TEvt statesman.EventBase, Cmd statesman.EventBase](rec *CommandRecorder[Cmd]) statesman.InvokeRunner[TCtx, TEvt] {
	return func(_ context.Context, _ TCtx, _ statesman.ActorAddress, _ func(TEvt)) statesman.RunningInvoke {
		return statesman.RunningInvoke{
			Cancel: func() {},
			Deliver: func(e statesman.EventBase) {
				if c, ok := e.(Cmd); ok {
					rec.add(c)
				}
			},
		}
	}
}
