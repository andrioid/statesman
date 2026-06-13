package orderpkg

import (
	"context"
	"time"
)

// ChargeInput / ChargeResult are the promise adapter's input/output.
type ChargeInput struct {
	UserID string
	Amount int64
}

type ChargeResult struct{ ID string }

// ChargeCard is a promise adapter: (ctx, input) -> (output, error).
func ChargeCard(ctx context.Context, in ChargeInput) (ChargeResult, error) {
	return ChargeResult{ID: "ch_demo"}, nil
}

// WatchInventory is a callback adapter: (ctx, emit, receive) -> error.
func WatchInventory(ctx context.Context, emit func(InventoryEvent), receive <-chan InventoryCommand) error {
	<-ctx.Done()
	return nil
}

// RetryDelay is the backoff before re-charging (delays.go convention).
const RetryDelay = 5 * time.Second
