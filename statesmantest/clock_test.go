package statesmantest

import (
	"context"
	"testing"
	"time"
)

func TestManualTimerFiresOnAdvance(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	ts := NewManualTimerService(clk)
	fired := 0
	ts.Schedule(context.Background(), clk.Now().Add(5*time.Second), func() { fired++ })

	ts.Advance(3 * time.Second)
	if fired != 0 {
		t.Fatalf("timer fired early at +3s")
	}
	ts.Advance(2 * time.Second)
	if fired != 1 {
		t.Fatalf("timer should fire at +5s, got %d fires", fired)
	}
	ts.Advance(10 * time.Second)
	if fired != 1 {
		t.Fatalf("timer must fire once, got %d", fired)
	}
}

func TestManualTimerCancel(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	ts := NewManualTimerService(clk)
	fired := 0
	tm := ts.Schedule(context.Background(), clk.Now().Add(5*time.Second), func() { fired++ })

	if !tm.Cancel() {
		t.Fatal("Cancel before fire should return true")
	}
	ts.Advance(10 * time.Second)
	if fired != 0 {
		t.Fatal("cancelled timer must not fire")
	}
	if tm.Cancel() {
		t.Fatal("second Cancel should return false")
	}
}

func TestManualTimerReArm(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	ts := NewManualTimerService(clk)
	count := 0
	var arm func()
	arm = func() {
		ts.Schedule(context.Background(), clk.Now().Add(5*time.Second), func() {
			count++
			if count < 3 {
				arm() // re-arm from inside a fire (the `after` re-entry pattern)
			}
		})
	}
	arm()
	for i := 0; i < 3; i++ {
		ts.Advance(5 * time.Second)
	}
	if count != 3 {
		t.Fatalf("re-armed timer should fire 3 times, got %d", count)
	}
}

func TestManualTimerDeadlineOrder(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	ts := NewManualTimerService(clk)
	var order []int
	ts.Schedule(context.Background(), clk.Now().Add(10*time.Second), func() { order = append(order, 10) })
	ts.Schedule(context.Background(), clk.Now().Add(5*time.Second), func() { order = append(order, 5) })

	ts.Advance(10 * time.Second) // both due; must fire in deadline order
	if len(order) != 2 || order[0] != 5 || order[1] != 10 {
		t.Fatalf("fire order = %v, want [5 10]", order)
	}
}
