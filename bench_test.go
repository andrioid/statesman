package statesman_test

import (
	"context"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/schema"
)

type benchCtx struct{}

type benchEvt struct{}

func (benchEvt) EventType() string { return "GO" }

// pingJSON is a two-state ping-pong: every GO toggles a<->b, so each Send drives
// exactly one external transition through the full mailbox -> microstep ->
// snapshot-publish path.
const pingJSON = `{
  "id": "ping",
  "initial": "a",
  "states": {
    "a": { "on": { "GO": { "target": "b" } } },
    "b": { "on": { "GO": { "target": "a" } } }
  }
}`

func benchNoop(int, benchCtx, benchEvt) statesman.AppliedEffect[benchCtx] {
	return statesman.AppliedEffect[benchCtx]{Kind: statesman.EffectNoop}
}

func benchTrue(int, benchCtx, benchEvt) bool { return true }

func startPing(b *testing.B) (*statesman.Machine[benchCtx, benchEvt], context.Context) {
	b.Helper()
	def, err := schema.Load([]byte(pingJSON))
	if err != nil {
		b.Fatalf("load: %v", err)
	}
	m := statesman.NewMachine[benchCtx, benchEvt](def, benchCtx{}, benchNoop, benchTrue)
	ctx := context.Background()
	if err := m.Start(ctx, "ping"); err != nil {
		b.Fatalf("start: %v", err)
	}
	if err := m.Settle(ctx); err != nil {
		b.Fatalf("settle: %v", err)
	}
	return m, ctx
}

// BenchmarkSendSettle measures one synchronous transition round trip: the
// dominant per-event cost and the basis for the mailbox-capacity default.
func BenchmarkSendSettle(b *testing.B) {
	m, ctx := startPing(b)
	defer m.Close()
	evt := benchEvt{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.Send(ctx, evt); err != nil {
			b.Fatalf("send: %v", err)
		}
		if err := m.Settle(ctx); err != nil {
			b.Fatalf("settle: %v", err)
		}
	}
}

// BenchmarkSnapshot measures the lock-free reader path (atomic.Pointer load plus
// struct copy) that every Snapshot() caller and subscriber pays.
func BenchmarkSnapshot(b *testing.B) {
	m, _ := startPing(b)
	defer m.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Snapshot()
	}
}
