package statesman

import "context"

const defaultSubscriberBuffer = 16

// SubscribeOption configures a subscription.
type SubscribeOption func(*subscribeOptions)

type subscribeOptions struct {
	buffer int
}

// WithBuffer sizes a subscriber's bounded delivery queue. The default is 16. A
// full queue blocks the actor (strong delivery; decision 25) — size for the
// subscriber's worst-case drain latency, or use a latest-wins proxy for lossy
// feeds.
func WithBuffer(n int) SubscribeOption {
	return func(o *subscribeOptions) {
		if n > 0 {
			o.buffer = n
		}
	}
}

// subscriber is one registered snapshot listener. ch is written only by the
// actor goroutine and closed only by the actor goroutine; the drain goroutine
// only reads it, so there is no send-on-closed race. ctx unsubscribes the
// listener when cancelled.
type subscriber[TCtx any] struct {
	ch  chan Snapshot[TCtx]
	fn  func(Snapshot[TCtx])
	ctx context.Context
}
