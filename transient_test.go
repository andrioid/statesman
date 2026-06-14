package statesman_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/andrioid/statesman"
)

// netErr is a net.Error double: Timeout() reports whether the failure is a
// transport timeout (the only thing IsTransient keys on for net errors).
type netErr struct{ timeout bool }

func (netErr) Error() string   { return "net error" }
func (e netErr) Timeout() bool { return e.timeout }
func (netErr) Temporary() bool { return false }

func TestIsTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ctx deadline", context.DeadlineExceeded, true},
		{"wrapped ctx deadline", fmt.Errorf("dial: %w", context.DeadlineExceeded), true},
		{"os deadline", os.ErrDeadlineExceeded, true},
		{"net timeout", netErr{timeout: true}, true},
		{"wrapped net timeout", fmt.Errorf("read: %w", netErr{timeout: true}), true},
		{"net non-timeout", netErr{timeout: false}, false},
		{"ctx canceled", context.Canceled, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statesman.IsTransient(tc.err); got != tc.want {
				t.Fatalf("IsTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
