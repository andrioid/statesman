package statesman

import (
	"errors"
	"fmt"
)

// Sentinel errors. See docs/transition-algorithm.md and the architecture error
// model. Compared with errors.Is.
var (
	// ErrActorStopped is returned by Send when the actor has reached a terminal
	// status; Send never panics on a stopped actor.
	ErrActorStopped = errors.New("statesman: actor stopped")
	// ErrAlreadyStarted is returned by a second Start on the same *Machine.
	ErrAlreadyStarted = errors.New("statesman: machine already started")
	// ErrAlwaysLoopExceeded is set as ErrorReason when an `always` chain exceeds
	// the configured iteration cap (WithAlwaysLimit, default 100).
	ErrAlwaysLoopExceeded = errors.New("statesman: always-transition loop limit exceeded")
	// ErrMailboxFull is returned by the non-blocking TrySend variant; Send blocks.
	ErrMailboxFull = errors.New("statesman: mailbox full")
)

// ObserverError wraps an error returned by a sync observer so callers can tell a
// persistence failure from an application observer failure. A sync observer
// error aborts the transition with full rollback (decision 26).
type ObserverError struct {
	Cause        error
	ObserverType string
}

func (e *ObserverError) Error() string {
	return fmt.Sprintf("statesman: observer %s failed: %v", e.ObserverType, e.Cause)
}

func (e *ObserverError) Unwrap() error { return e.Cause }

// CodegenError is a structured build-time error from `statesman generate`. It is
// never a runtime error — it surfaces only while generating code.
type CodegenError struct {
	File     string
	JSONPath string
	Message  string
}

func (e *CodegenError) Error() string {
	if e.JSONPath == "" {
		return fmt.Sprintf("%s: %s", e.File, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.File, e.JSONPath, e.Message)
}
