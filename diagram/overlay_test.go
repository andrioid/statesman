package diagram_test

import (
	"strings"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/diagram"
)

// WithActive supplies only atomic leaves; the overlay must close the set upward
// so the active leaf and every ancestor on its path are marked, while siblings
// off the path are not.
func TestOverlayClosesUpward(t *testing.T) {
	def := load(t, "order")
	out := diagram.Text(def, diagram.WithActive([]statesman.StateID{"processing.charging"}))

	for _, want := range []string{"◆ charging", "◆ processing", "· idle", "· done"} {
		if !strings.Contains(out, want) {
			t.Errorf("overlay missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "◆ idle") {
		t.Errorf("inactive state idle marked active:\n%s", out)
	}
}

// A history pseudo-state never appears in ActiveStates, so it is never marked
// active even when its parent compound is on the active path.
func TestOverlayHistoryNeverActive(t *testing.T) {
	def := load(t, "player")
	out := diagram.Text(def, diagram.WithActive([]statesman.StateID{"paused.idle"}))

	if !strings.Contains(out, "◆ paused") || !strings.Contains(out, "◆ idle") {
		t.Errorf("active path not marked:\n%s", out)
	}
	if !strings.Contains(out, "· hist") || strings.Contains(out, "◆ hist") {
		t.Errorf("history pseudo-state should never be active:\n%s", out)
	}
}

func TestOverlayHeaderStatus(t *testing.T) {
	def := load(t, "order")
	out := diagram.Text(def,
		diagram.WithActive([]statesman.StateID{"idle"}),
		diagram.WithStatus(statesman.StatusRunning),
		diagram.WithVersion(7),
	)
	header := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(header, "running") || !strings.Contains(header, "v7") {
		t.Errorf("header missing status/version: %q", header)
	}
}
