// Package simple is a worked statesman example: a slot machine that looks random
// but holds a configured return-to-player (RTP) percentage.
//
// The trick is two mode states — "giving" (spins biased to win) and "taking"
// (spins biased to lose) — that share identical faces and payouts, so no single
// spin reveals the mode. Guarded eventless (`always`) transitions form a feedback
// loop: when realized RTP climbs to the target the machine switches to taking;
// when it falls below, it switches back to giving. Over many spins the realized
// RTP converges on the target.
//
// simple.machine.json defines the structure; this file the typed event and
// context; simple.behavior.go the behavior (RNG + RTP control);
// simple.machine.gen.go (from `statesman generate`) wires them. cmd/simple runs
// it in the terminal — press SPACE to spin. Regenerate with `go generate ./...`.
package simple

import "github.com/andrioid/statesman"

// Event is the sealed per-machine event union. Spin gets its EventType from
// simple.machine.gen.go; the simpleEvent marker seals the union.
type Event interface {
	statesman.EventBase
	simpleEvent()
}

// Spin pulls the lever.
type Spin struct{}

func (Spin) simpleEvent() {}

// ContextFields is the machine's running game state. RTP is derived as
// TotalWon/TotalBet; the mode is the active state, not stored here.
type ContextFields struct {
	Spins    int
	TotalBet int64
	TotalWon int64
	LastWin  int    // payout of the most recent spin (0 = loss)
	Reels    [3]int // symbol indices of the most recent spin, for display
}
