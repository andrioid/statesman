package diagram

import (
	"context"
	"io"

	"github.com/andrioid/statesman"
)

// Live overlays a running machine's state on the terminal tree, repainting on
// every committed transition. It subscribes to m and renders each snapshot's
// active states, status, version, and armed timers onto m.Definition()'s tree.
//
// m must already be Started: Live uses the post-Start (channel) subscription
// path, so a typical caller does `go diagram.Live(ctx, m, w)` after m.Start.
//
// Live shows only this machine's own configuration: an invoked actor's internal
// state is not carried in the parent snapshot, so invokes render as static
// wiring. It blocks until ctx is cancelled or the machine reaches a terminal
// status, and always restores the terminal on return.
//
// opts (e.g. WithASCII, WithColor) override the live defaults; the snapshot's
// active/status/version/timers are always applied.
func Live[TCtx any, TEvt statesman.EventBase](ctx context.Context, m *statesman.Machine[TCtx, TEvt], w io.Writer, opts ...Option) error {
	def := m.Definition()
	screen := NewScreen(w)
	screen.Enter()
	defer screen.Leave()

	// Latest-wins: the subscriber callback runs on the actor's drain goroutine,
	// so it must not block on rendering. It only stashes the newest snapshot;
	// Live renders in its own loop, dropping intermediate frames under load.
	snaps := make(chan statesman.Snapshot[TCtx], 1)
	m.Subscribe(ctx, func(s statesman.Snapshot[TCtx]) { latestWins(snaps, s) })

	// Paint the current state at once: a snapshot published before Subscribe
	// registered (or the standing state of an already-running machine) would not
	// otherwise stream in until the next transition.
	screen.Frame(Text(def, snapshotOptions(m.Snapshot(), opts)...))
	for {
		select {
		case <-ctx.Done():
			return nil
		case s := <-snaps:
			screen.Frame(Text(def, snapshotOptions(s, opts)...))
			if s.Status.Terminal() {
				return nil
			}
		}
	}
}

// snapshotOptions derives render options from a snapshot, then applies the
// caller's opts last so they win (e.g. WithColor(false), WithASCII(true)).
func snapshotOptions[TCtx any](s statesman.Snapshot[TCtx], base []Option) []Option {
	opts := make([]Option, 0, len(base)+5)
	opts = append(opts,
		WithActive(s.ActiveStates),
		WithStatus(s.Status),
		WithVersion(s.Version),
		WithPending(s.PendingAfter),
		WithColor(true),
	)
	return append(opts, base...)
}

// latestWins pushes v onto ch, discarding any value already queued so the
// channel always holds the most recent snapshot.
func latestWins[T any](ch chan T, v T) {
	for {
		select {
		case ch <- v:
			return
		default:
			select {
			case <-ch:
			default:
			}
		}
	}
}
