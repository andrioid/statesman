package main

import (
	"errors"
	"strings"
	"testing"
)

// watchFrame must never blank a valid diagram because of an in-progress edit:
// once a good render exists, a parse error keeps it on screen and appends the
// error rather than replacing it.
func TestWatchFrameKeepsLastGood(t *testing.T) {
	good := "machine\n├─ a\n└─ b"

	// First successful load: adopt it as the displayed and remembered render.
	frame, newGood, haveGood := watchFrame(good, nil, "", false)
	if frame != good || newGood != good || !haveGood {
		t.Fatalf("on success: frame=%q good=%q have=%v", frame, newGood, haveGood)
	}

	// Subsequent broken edit: keep the last good render, surface the error,
	// and do not lose the remembered render.
	parseErr := errors.New("line 14: unexpected }")
	frame, newGood, haveGood = watchFrame("", parseErr, newGood, haveGood)
	if !strings.Contains(frame, good) {
		t.Errorf("broken edit dropped the last good render:\n%s", frame)
	}
	if !strings.Contains(frame, parseErr.Error()) {
		t.Errorf("broken edit hid the error:\n%s", frame)
	}
	if newGood != good || !haveGood {
		t.Errorf("broken edit forgot the last good render: good=%q have=%v", newGood, haveGood)
	}
}

// Before any valid load, a parse error shows guidance and remembers nothing.
func TestWatchFrameNoGoodYet(t *testing.T) {
	frame, newGood, haveGood := watchFrame("", errors.New("boom"), "", false)
	if newGood != "" || haveGood {
		t.Errorf("should remember nothing: good=%q have=%v", newGood, haveGood)
	}
	if !strings.Contains(frame, "boom") {
		t.Errorf("error not shown: %q", frame)
	}
}
