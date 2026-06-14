package diagram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/schema"
)

// In overlay mode an armed `after` timer shows its remaining time on the owning
// state's delayed transition. nowFn is pinned so the countdown is deterministic.
func TestOverlayTimerCountdown(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "order.machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	saved := nowFn
	nowFn = func() time.Time { return now }
	defer func() { nowFn = saved }()

	out := Text(def,
		WithActive([]statesman.StateID{"processing.charging"}),
		WithPending([]statesman.ScheduledTimer{{
			StateID:  "processing.charging",
			Deadline: now.Add(12 * time.Second),
		}}),
	)
	if !strings.Contains(out, "(12s left)") {
		t.Errorf("expected countdown on charging's after edge:\n%s", out)
	}
}
