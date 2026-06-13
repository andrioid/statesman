package simple

import (
	"context"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

// TestRTPConverges drives many spins through the generated machine and asserts
// the giving/taking feedback loop holds realized RTP near the configured target
// and that both modes are actually exercised. Deterministic via a seeded RNG.
func TestRTPConverges(t *testing.T) {
	const (
		seed   = 42
		bet    = 1
		target = 0.92
		spins  = 500
	)
	ctx := context.Background()
	impl := NewImpl(seed, bet, target)
	s := statesmantest.NewSync(func(o ...statesman.Option) *statesman.Machine[Context, Event] {
		return NewSimpleMachine(impl, o...)
	})
	if err := s.Start(ctx, "slot-1"); err != nil {
		t.Fatalf("start: %v", err)
	}

	seen := map[statesman.StateID]bool{}
	for i := 0; i < spins; i++ {
		if err := s.SendAndSettle(ctx, Spin{}); err != nil {
			t.Fatalf("spin %d: %v", i, err)
		}
		seen[s.Snapshot().ActiveStates[0]] = true
	}
	snap := s.Snapshot()

	if snap.Context.Spins != spins {
		t.Fatalf("Spins = %d, want %d", snap.Context.Spins, spins)
	}
	if snap.Context.TotalBet != int64(spins*bet) {
		t.Fatalf("TotalBet = %d, want %d", snap.Context.TotalBet, spins*bet)
	}
	rtp := float64(snap.Context.TotalWon) / float64(snap.Context.TotalBet)
	if rtp < 0.82 || rtp > 1.04 {
		t.Fatalf("realized RTP = %.3f, want near target %.2f (the feedback loop is broken)", rtp, target)
	}
	if !seen[States.Giving] || !seen[States.Taking] {
		t.Fatalf("modes used = %v, want both giving and taking", seen)
	}
}

// TestSingleSpinCounts checks one spin updates the bet/spin counters regardless of
// outcome (a spin always wagers exactly Bet).
func TestSingleSpinCounts(t *testing.T) {
	ctx := context.Background()
	s := statesmantest.NewSync(func(o ...statesman.Option) *statesman.Machine[Context, Event] {
		return NewSimpleMachine(NewImpl(1, 5, 0.9), o...)
	})
	if err := s.Start(ctx, "slot-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.SendAndSettle(ctx, Spin{}); err != nil {
		t.Fatalf("spin: %v", err)
	}
	snap := s.Snapshot()
	if snap.Context.Spins != 1 || snap.Context.TotalBet != 5 {
		t.Fatalf("after one spin: spins=%d bet=%d, want 1 and 5", snap.Context.Spins, snap.Context.TotalBet)
	}
	if snap.Context.LastWin != 0 && snap.Context.LastWin < 5 {
		t.Fatalf("LastWin = %d, want 0 or a bet-multiple payout", snap.Context.LastWin)
	}
}
