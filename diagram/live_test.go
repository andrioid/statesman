package diagram_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/diagram"
	"github.com/andrioid/statesman/schema"
)

type lightEvt struct{ t string }

func (e lightEvt) EventType() string { return e.t }

// safeBuf is an io.Writer the test reads concurrently with Live's render loop.
type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Live must repaint the active state as the machine transitions, end to end:
// subscribe to a running machine, drive an event, and see the overlay move.
func TestLiveOverlaysActiveState(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "light.machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatal(err)
	}

	apply := func(int, struct{}, lightEvt) statesman.AppliedEffect[struct{}] {
		return statesman.AppliedEffect[struct{}]{Kind: statesman.EffectNoop}
	}
	guard := func(int, struct{}, lightEvt) bool { return true }
	m := statesman.NewMachine[struct{}, lightEvt](def, struct{}{}, apply, guard)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf safeBuf
	done := make(chan error, 1)
	// Live requires a started machine: Start first, then watch. Spawning Live
	// after Start gives a clean happens-before and the channel subscription path.
	if err := m.Start(context.Background(), "light-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { done <- diagram.Live(ctx, m, &buf) }()

	// The initial idle frame proves Live subscribed; only then drive the event.
	eventually(t, &buf, "◆ idle")
	if err := m.Send(context.Background(), lightEvt{"GO"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	eventually(t, &buf, "◆ running")

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Live did not return after ctx cancel")
	}
	_ = m.Close()
}

func eventually(t *testing.T, buf *safeBuf, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q in:\n%s", want, buf.String())
}
