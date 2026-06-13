# statesman

Statemanager for Go statechart for long-running backend workflows. Author a machine as
Stately-compatible `machine.json`, generate a typed Go facade from it, and run it
as an actor with a lock-free snapshot and deterministic, virtual-time tests.

- **Typed, not stringly-typed.** Generated `States` constants, event types, and an
  `Implementations` interface whose action/guard methods carry the concrete event
  and context types — no `any` on the surfaces you touch.
- **Stdlib-only runtime.** No third-party dependencies on a running machine's path.
- **Deterministic tests.** Virtual clock + `Sync` harness; transitions settle
  synchronously, time only moves on `Advance`.
- **`-race`-clean.** The whole runtime and test suite pass `go test -race`.

> **Status: v1.** Hierarchy, parallel regions, history, guards, eventless
> (`always`) transitions, delayed (`after`) transitions, invoked actors
> (promise/callback/observable/machine), supervision, and codegen are implemented.
> Durable persistence is designed (`docs/persistence-contract.md`) and exposed via
> the `TransitionObserver` seam, but the on-disk store/recovery (Phase 6) is on the
> roadmap — v1 is in-memory.

## Install

```sh
go get github.com/andrioid/statesman
go install github.com/andrioid/statesman/cmd/statesman@latest
```

Requires Go 1.26+.

## Quickstart

```sh
statesman init checkout      # scaffold a runnable checkout/ package (idle -> done)
```

`init` writes `checkout/`: `gen.go` (with a `//go:generate statesman generate`
directive), `machine.json`, `types.go` (event/context stubs), and
`machine_gen.go` (generated facade) — and it compiles immediately. Then iterate:

1. Edit `checkout/machine.json` (or paste the export from Stately Studio).
2. Re-scaffold the new symbols and regenerate:

   ```sh
   statesman stub ./checkout       # append stubs for new events/actors/fields + an Impl skeleton (idempotent)
   go generate ./checkout/         # re-runs `statesman generate` -> machine_gen.go
   ```

3. Fill in the `Implementations` methods on the `Impl` skeleton `statesman stub`
   wrote to `checkout/impl.go` (emitted once the machine has actions, guards, or
   invokes to implement).

Drive it from a test with the deterministic harness:

```go
impl := checkout.Impl{ /* ... */ }
s := statesmantest.NewSync(func(o ...statesman.Option) *statesman.Machine[checkout.Context, checkout.Event] {
    return checkout.NewCheckoutMachine(impl, o...)
})
ctx := context.Background()
_ = s.Start(ctx, "checkout-1")

_ = s.SendAndSettle(ctx, checkout.Submit{ /* ... */ })
s.Advance(ctx, checkout.RetryDelay)        // fire `after` timers on virtual time

snap := s.Snapshot()                       // typed: snap.ActiveStates, snap.Context
```

## How it works

```
machine.json  --schema.Load-->  *Definition  --NewXxxMachine-->  Machine[TCtx,TEvt]
     |                                                                |
     +-- statesman generate --> machine_gen.go (States, events,       +-- Snapshot[TCtx] (atomic, lock-free)
         Implementations interface, constructor, dispatch tables)
```

- **One goroutine owns mutable state.** Every transition runs on the actor loop;
  readers call `Snapshot()` (an `atomic.Pointer` load) and never block the actor.
- **Engine is pure.** Selection, LCCA exit/entry, and the microstep/macrostep loop
  are an SCXML-subset reduction (see `docs/transition-algorithm.md`); actions and
  guards are opaque closures keyed by callsite index, so the engine carries no
  user types.
- **Effects via an outbox.** Actions return an `ActionResult`; assigns produce a
  new context value, sends/spawns are drained after the snapshot publishes.

## Packages

| Path               | Role                                                                                                                                                     |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `.` (`statesman`)  | Runtime core: `Machine`, `Snapshot`, `NewMachine`, observers, `StallObserver`, clock/timer interfaces                                                    |
| `schema`           | `Load([]byte) (*Definition, error)` — the only `machine.json` parser; stricter-than-schema validation                                                    |
| `cmd/statesman`    | CLI: `init`, `stub`, `generate`                                                                                                                          |
| `statesmantest`    | `Sync` harness, `ManualClock`/`ManualTimerService`, `FakeActor`, `RunScenarios` corpus runner                                                            |
| `internal/codegen` | `go/types`-driven resolution + emitters (not a public API)                                                                                               |
| `docs/`            | Normative specs: transition algorithm, schema subset, persistence contract                                                                               |
| `examples/simple`  | Worked slot-machine example (RTP-controlled via giving/taking mode states): machine + `cmd/simple` terminal demo (`go run ./examples/simple/cmd/simple`) |

## Testing

- **`Sync`** — `SendAndSettle` / `Advance` / `Settle` block until the actor
  quiesces, so assertions are race-free without sleeps.
- **`ManualClock` + `ManualTimerService`** — virtual time; `after` timers fire on
  `Advance`.
- **`FakeActor` / `CommandRecorder` / `FakeCallback`** — actor doubles for invoke
  contract tests without real side effects.
- **`RunScenarios(t, dir)`** — the curated behavioral corpus: each
  `testdata/scenarios/*.txtar` holds a `machine.json` and a `send`/`advance`/
  `expect` script, run on virtual time. Add machines as data, not Go code.

## Production notes

- **Strong subscriber delivery.** Subscribers get a bounded, blocking queue: a
  slow subscriber stalls the actor by design (audit semantics). Wrap load-bearing
  transition observers in a `StallObserver` to surface "held the actor for >Nms"
  as a metric; use a latest-wins proxy for lossy feeds.
- **Observers must not `Send`.** A synchronous self-`Send` from an observer
  deadlocks the loop — emit effects via `ActionResult`.
- **Panics are fatal.** Action/guard methods must not panic; the actor loop has no
  `recover()` (panics are programming errors).
- **Context is read-only** in action/guard methods; mutate by returning a new
  value via `ActionResult`.

## Performance

Apple M3, `go test -bench`:

| Benchmark                                            | ns/op | allocs/op |
| ---------------------------------------------------- | ----- | --------- |
| `SendSettle` (one synchronous transition round trip) | ~3100 | 63        |
| `Snapshot` (lock-free reader)                        | ~0.3  | 0         |

## Documentation

- `statesman-architecture.md` — design, patterns, full decision table
- `docs/transition-algorithm.md` — the SCXML-subset transition reduction
- `docs/schema-subset.md` — the accepted `machine.json` subset and validation gates
- `docs/persistence-contract.md` — the durability window and recovery contract
- `DECISIONS.md` — decision log
- `TODO.md` — phased build sequence and roadmap
