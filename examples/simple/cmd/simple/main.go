// Command simple is a terminal slot machine driven by the statesman example
// machine. It looks random but holds a configured return-to-player percentage by
// switching between "giving" and "taking" mode states (see ../../).
//
// In a real terminal: press SPACE to spin, Q to quit. When stdin is not a
// terminal (piped, CI), it auto-plays a fixed number of spins and prints a
// summary.
//
// Run it: go run ./examples/simple/cmd/simple
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/simple"
)

// startBank is the player's display bankroll; the machine only tracks net
// (TotalWon - TotalBet), so the demo adds a starting balance for presentation.
const startBank = 100

func main() {
	rtp := flag.Float64("rtp", 0.92, "target return-to-player (0..1)")
	bet := flag.Int("bet", 1, "credits wagered per spin")
	seed := flag.Int64("seed", time.Now().UnixNano(), "RNG seed")
	autoSpins := flag.Int("spins", 12, "spins to auto-play when stdin is not a terminal")
	flag.Parse()

	ctx := context.Background()
	m := simple.NewSimpleMachine(simple.NewImpl(*seed, *bet, *rtp))
	if err := m.Start(ctx, "slot-1"); err != nil {
		panic(err)
	}
	defer m.Close()
	if err := m.Settle(ctx); err != nil {
		panic(err)
	}

	display := rand.New(rand.NewSource(time.Now().UnixNano()))
	fmt.Printf("🎰 statesman slot machine — target RTP %.0f%%, bet %d credit(s)\n", *rtp*100, *bet)

	if isTTY(os.Stdin) {
		fmt.Println("   SPACE = spin    Q = quit")
		playInteractive(ctx, m, display)
	} else {
		fmt.Printf("   (stdin is not a terminal — auto-playing %d spins)\n\n", *autoSpins)
		for i := 0; i < *autoSpins; i++ {
			spin(ctx, m, display, false)
		}
	}
	summary(m.Snapshot(), *rtp)
}

// playInteractive reads single keypresses in cbreak mode: SPACE spins, Q quits.
func playInteractive(ctx context.Context, m *statesman.Machine[simple.Context, simple.Event], display *rand.Rand) {
	restore := rawMode()
	defer restore()
	// Restore the terminal even if interrupted.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() { <-sig; restore(); os.Exit(0) }()

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		switch buf[0] {
		case ' ':
			spin(ctx, m, display, true)
		case 'q', 'Q', 3: // 3 = Ctrl-C if it reaches us
			return
		}
	}
}

// spin animates the reels (when animate), advances the machine one SPIN, then
// prints the settled result.
func spin(ctx context.Context, m *statesman.Machine[simple.Context, simple.Event], display *rand.Rand, animate bool) {
	if animate {
		for i := 0; i < 14; i++ {
			fmt.Printf("\r   [ %s %s %s ]   spinning…    ", face(display), face(display), face(display))
			time.Sleep(55 * time.Millisecond)
		}
	}
	if err := m.Send(ctx, simple.Spin{}); err != nil {
		return
	}
	if err := m.Settle(ctx); err != nil {
		return
	}
	printResult(m.Snapshot(), animate)
}

func printResult(snap statesman.Snapshot[simple.Context], inplace bool) {
	c := snap.Context
	result := "—       "
	if c.LastWin > 0 {
		result = fmt.Sprintf("WIN +%-3d", c.LastWin)
	}
	mode := string(snap.ActiveStates[0])
	rtp := 0.0
	if c.TotalBet > 0 {
		rtp = float64(c.TotalWon) / float64(c.TotalBet) * 100
	}
	line := fmt.Sprintf("  spin %3d   [ %s %s %s ]   %s   mode=%-6s  rtp=%5.1f%%  bank=%d",
		c.Spins, simple.Face(c.Reels[0]), simple.Face(c.Reels[1]), simple.Face(c.Reels[2]),
		result, mode, rtp, startBank+int(c.TotalWon-c.TotalBet))
	if inplace {
		fmt.Printf("\r%s\n", line)
		return
	}
	fmt.Println(line)
}

func summary(snap statesman.Snapshot[simple.Context], target float64) {
	c := snap.Context
	rtp := 0.0
	if c.TotalBet > 0 {
		rtp = float64(c.TotalWon) / float64(c.TotalBet) * 100
	}
	fmt.Printf("\n%d spins · wagered %d · paid %d · realized RTP %.1f%% (target %.0f%%) · bank %d\n",
		c.Spins, c.TotalBet, c.TotalWon, rtp, target*100, startBank+int(c.TotalWon-c.TotalBet))
}

func face(display *rand.Rand) string { return simple.Face(display.Intn(simple.SymbolCount())) }

// isTTY reports whether f is a real terminal. It probes stty (which fails on a
// pipe or /dev/null) rather than checking ModeCharDevice — /dev/null is also a
// character device, so the mode bit alone would misfire.
func isTTY(f *os.File) bool {
	c := exec.Command("stty", "-g")
	c.Stdin = f
	return c.Run() == nil
}

// rawMode puts the controlling terminal in cbreak/no-echo via stty (dependency
// free) and returns a restore func. Unix only; the auto-play path needs no TTY.
func rawMode() func() {
	stty := func(args ...string) {
		c := exec.Command("stty", args...)
		c.Stdin = os.Stdin
		_ = c.Run()
	}
	stty("cbreak", "-echo")
	return func() { stty("sane") }
}
