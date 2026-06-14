package statesman

import (
	"context"
	"errors"
	"net"
	"os"
)

// IsTransient reports whether err is a retryable transport-level failure: a
// context or I/O deadline (context.DeadlineExceeded, os.ErrDeadlineExceeded) or a
// net.Error reporting a timeout. It unwraps via errors.Is / errors.As.
//
// It deliberately does NOT classify domain errors (HTTP 4xx/5xx, application
// failures) or context.Canceled — a cancel is an intentional stop, never a retry.
// Compose your own predicate around it for domain errors: an error.invoke.<id>
// guard is where to decide whether a failure is worth spending a retry on.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}
