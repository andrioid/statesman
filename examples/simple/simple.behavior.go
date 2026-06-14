package simple

import "math/rand"

// reelSymbols are the slot faces; payouts[i] is the bet multiplier for a line of
// three of symbol i. Rendering lives in cmd/simple — the machine only stores the
// indices in Context.Reels.
var (
	reelSymbols = []string{"🍒", "🔔", "🍋", "💎", "7️⃣"}
	payouts     = []int{3, 4, 5, 10, 20}
)

// Win probability per mode. Both modes draw from the same faces/payouts; only the
// win frequency differs, so a single spin never reveals the mode. Chosen so the
// expected return in "giving" sits well above any sane target and "taking" well
// below — the guarded always-edges (PayingTooMuch/PayingTooLittle) then steer the
// realized RTP to the configured target.
const (
	givingWinChance = 0.50
	takingWinChance = 0.04
)

// Impl is the slot machine's behavior. It owns the RNG (so spins look random) and
// the configured return-to-player target the modes converge toward.
type Impl struct {
	Rng       *rand.Rand
	Bet       int
	TargetRTP float64
}

// NewImpl builds an Impl with a seeded RNG: pass a fixed seed for deterministic
// tests, a time-based one for a live game.
func NewImpl(seed int64, bet int, targetRTP float64) Impl {
	return Impl{Rng: rand.New(rand.NewSource(seed)), Bet: bet, TargetRTP: targetRTP}
}

func (s Impl) SpinGivingOnSpin(ctx Context, evt Spin) ActionResult {
	return s.spin(ctx, givingWinChance)
}
func (s Impl) SpinTakingOnSpin(ctx Context, evt Spin) ActionResult {
	return s.spin(ctx, takingWinChance)
}

// spin draws a result with the given win probability and folds it into context.
func (s Impl) spin(ctx Context, winChance float64) ActionResult {
	reels, win := s.draw(winChance)
	return Assign{Fields: ContextFields{
		Spins:    ctx.Spins + 1,
		TotalBet: ctx.TotalBet + int64(s.Bet),
		TotalWon: ctx.TotalWon + int64(win),
		LastWin:  win,
		Reels:    reels,
	}}
}

// draw returns the reel faces and payout for one spin.
func (s Impl) draw(winChance float64) ([3]int, int) {
	if s.Rng.Float64() < winChance {
		sym := s.Rng.Intn(len(reelSymbols))
		return [3]int{sym, sym, sym}, s.Bet * payouts[sym]
	}
	return s.losingReels(), 0
}

// losingReels returns three faces that are not all equal.
func (s Impl) losingReels() [3]int {
	n := len(reelSymbols)
	for {
		r := [3]int{s.Rng.Intn(n), s.Rng.Intn(n), s.Rng.Intn(n)}
		if r[0] != r[1] || r[1] != r[2] {
			return r
		}
	}
}

// PayingTooMuchOnGiving leaves "giving" once realized return has caught up to the
// target — the machine has paid out enough for now. Guards must be side-effect
// free (they may be evaluated more than once during selection), so this only
// reads context.
func (s Impl) PayingTooMuchOnGiving(ctx Context) bool {
	return ctx.TotalBet > 0 && returnToPlayer(ctx) >= s.TargetRTP
}

// PayingTooLittleOnTaking leaves "taking" once realized return has fallen below
// the target — time to let the player win again.
func (s Impl) PayingTooLittleOnTaking(ctx Context) bool {
	return returnToPlayer(ctx) < s.TargetRTP
}

// returnToPlayer is realized RTP: total paid out / total wagered.
func returnToPlayer(ctx Context) float64 {
	if ctx.TotalBet == 0 {
		return 0
	}
	return float64(ctx.TotalWon) / float64(ctx.TotalBet)
}

// Face returns the display glyph for reel symbol index i, for renderers like
// cmd/simple. The machine stores indices in Context.Reels; faces are presentation.
func Face(i int) string { return reelSymbols[i] }

// SymbolCount is the number of distinct reel faces.
func SymbolCount() int { return len(reelSymbols) }
