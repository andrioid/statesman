package issues

import "time"

// after-delay symbols referenced by issues.machine.json. Attempt timeouts bound a
// single agent/gh call; deadlines bound the whole retry loop for a step; backoffs
// are the fixed wait between attempts (swap for a statesman.BackoffActor invoke to
// get exponential/jitter).
const (
	CollectAttemptTimeout = 30 * time.Second
	CollectDeadline       = 3 * time.Minute
	CollectBackoff        = 5 * time.Second
	ClassifyTimeout       = 60 * time.Second
	SummariseTimeout      = 60 * time.Second
	SyncAttemptTimeout    = 20 * time.Second
	SyncDeadline          = 2 * time.Minute
	SyncBackoff           = 3 * time.Second
)
