# statesman: Implementation TODO

Concrete, ordered implementation plan for the design in
[`statesman-architecture.md`](./statesman-architecture.md). The architecture doc
owns the *design and patterns* (the "what" and "why"); this file owns the
*build sequence* (the "in what order, with what acceptance criteria"). Decision
numbers (`D#`) and section links below point back into the architecture doc.

> **Status (2026-06-13): v1 complete.** Phases 0–5 and 7 are done and verified
> race-clean (`go build ./... && go vet ./... && go test -race ./...` → 5 packages
> ok, gofmt clean). The engine, actor runtime, adapters, codegen/CLI, txtar
> scenario corpus, `StallObserver`, benchmarks, and docs (README, DECISIONS) all
> ship. **Phase 6 (durable persistence) is deferred to the post-v1 roadmap** — its
> contract (`docs/persistence-contract.md`) and the `TransitionObserver` seam
> exist, but the on-disk store/recovery is not implemented; v1 is in-memory.
> **Phase 6 below is a forward, reviewable plan (not yet built).** The other
> per-task checkboxes record the original plan; this status is authoritative.

## Legend & conventions

- `- [ ]` open · `- [x]` done · `- [~]` in progress · `- [!]` blocked.
- Each phase lists **Goal → Tasks → Acceptance → Depends on**. A phase is done
  only when its Acceptance bullets are demonstrably met (tests/commands named).
- **Each layer builds without the one above it** (architecture overview): core
  has no I/O, durable is optional, codegen targets a proven core API.
- **`go test -race` clean is a non-negotiable CI gate from the first test on**
  (D31). Every phase that ships runtime code ships race-clean tests.
- Subagents/contributors: do not run project-wide lint/format/build as part of a
  task; the owner runs gates once across the union of changed files at phase end.

## Module layout (target)

```
github.com/andrioid/statesman             # module path (Go 1.26)
  ./                 package statesman     # core runtime, no I/O (engine, machine, actors, observers, StallObserver)
  ./schema           package schema        # Stately machine.json loader (public; generated code calls schema.Load)
  ./cmd/statesman    package main          # the `statesman` CLI (init/stub/generate)
  ./internal/codegen package codegen        # go/types resolution + emitters (+ testdata/orderpkg worked example)
  ./statesmantest    package statesmantest # Sync runner, ManualClock, FakeActor, txtar scenario corpus
  ./docs                                   # normative specs (transition, schema-subset, persistence)
  ./durable          package durable       # persistence reference impls — ROADMAP (Phase 6, post-v1)
```

- Module path **`github.com/andrioid/statesman`**, **Go 1.26** (pinned in
  `go.mod`). Resolved before Phase 0.
- One machine per package (D50); `order` is the canonical example/integration
  target throughout.

---

## Phase 0 — Foundations & normative specs (THE GATE)

**Goal:** close the three pre-code gaps named in
[What this is not yet](./statesman-architecture.md#what-this-is-not-yet) and
stand up an empty, building module. No runtime behavior yet.

- [~] Initialize module: `go.mod` (`github.com/andrioid/statesman`, Go 1.26),
      root `statesman` package (`doc.go`) — builds & vets clean. CI workflow
      still TODO (CI platform not chosen).
- [x] **`docs/transition-algorithm.md` — normative.** Approved 2026-06-13
      (deferred-invoke decided). SCXML Appendix-D reduction to the Stately subset,
      making precise everything the 12-step
      [Transition step](./statesman-architecture.md#transition-step-deterministic)
      sketch omits:
  - candidate-transition selection over compound + parallel regions;
  - exit-set / entry-set computation (LCCA — least common compound ancestor);
  - document-order conflict resolution across parallel regions;
  - internal vs. external transitions;
  - shallow vs. deep **history** restoration (D3);
  - `always` (eventless) ordering vs. `after`, and the loop guard (D38);
  - initial-state entry, final-state / `onDone` bubbling in parallel regions.
  - Worked example: trace the `order` retry loop end-to-end against the spec.
- [x] **`docs/schema-subset.md` — pins the Stately surface** against the vendored
      `machineSchema.json` (3 validation gates; states/`initial`/`entry`/`exit`/
      `on`/`after`/`always`/`invoke`+`onDone`/`onError`, `{type,params}`). Found
      the schema is structural-only (no `context`, no `internal` flag, no final
      `output`) → corrections folded back into `transition-algorithm.md`.
- [x] **`docs/persistence-contract.md`** — durability window (observer-before-
      publish, abort-on-write-failure), `OutboxIntent` wire format, EventLog
      records, and the at-least-once startup recovery reconciliation (D8, D27, D42).
- [x] Vendored `internal/schema/testdata/machineSchema.json` + `order.json`
      fixture (validates against the schema, gate 1). Rejection/feature fixtures
      deferred to Phase 1 loader tests.

**Acceptance:** all three docs reviewed by ≥1 engineer (per AGENTS.md
architecture rule); `go test -race ./...` green on an empty module; the `order`
retry loop is traceable on paper against `transition-algorithm.md`.

**Depends on:** nothing.

---

## Phase 1 — Core types & schema loader

**Goal:** the generic vocabulary and an in-memory machine definition the engine
will walk. No actor loop yet.

- [ ] Core generic surface
      ([Generics in the core runtime](./statesman-architecture.md#generics-in-the-core-runtime)):
      `EventBase`, `Never` (uninhabited; D45), `StateID`, `ActorAddress`,
      `ActorStatus`, `Snapshot[TCtx]`, `ChildRef`, `ScheduledTimer`.
- [ ] Error model (D-[Error model](./statesman-architecture.md#error-model)):
      `ErrActorStopped`, `ErrAlreadyStarted`, `ErrAlwaysLoopExceeded`,
      `ErrMailboxFull`, `ObserverError`, `CodegenError`.
- [ ] `internal/schema`: loader for the Phase-0 subset → an internal
      `*Definition` tree (states, transitions, invokes, after/always, guard &
      action refs). Reject out-of-subset input with a clear error.
- [ ] Definition validation: missing `invoke.id` → error (addressing rule);
      malformed targets, unreachable states, parallel/history well-formedness.

**Acceptance:** loader round-trips every fixture from Phase 0 into a
`*Definition`; table tests cover one chart per subset feature + rejection cases;
race-clean.

**Depends on:** Phase 0 (schema subset, transition spec for definition shape).

---

## Phase 2 — Transition engine (pure, single-actor)

**Goal:** a deterministic, side-effect-free reducer implementing
`docs/transition-algorithm.md`. No goroutines, no time, no I/O — a function from
(state, context, event) to (next state, accumulated actions, outbox intents).

- [ ] State configuration model (active-state set) for compound + parallel +
      history; entry/exit set computation (LCCA).
- [ ] Guard evaluation in document order; transition selection per the spec.
- [ ] Action accumulation: run actions in order, fold `Assign` into a new
      context, collect side-effect intents to an outbox (D44, composite effects).
- [ ] `always` processing with the configurable loop guard (D38) →
      `ErrAlwaysLoopExceeded`.
- [ ] Pre-transition snapshot / rollback point (step 4) and the full-rollback
      path as a pure result variant (consumed by the actor loop in Phase 3).
- [ ] **Opaque applier boundary** (D36): engine takes
      `func(actionIndex int, ctx TCtx, evt TEvt) appliedEffect` and never names
      `ActionResult`. Define `appliedEffect` and the callsite→method dispatch
      contract the generated constructor will satisfy.

**Acceptance:** engine reproduces every paper trace in
`docs/transition-algorithm.md` as a unit test (hierarchy, parallel, history,
guards, `always`, loop-guard trip); 100% of subset branches covered; race-clean
(pure code, trivially so).

**Depends on:** Phase 1.

---

## Phase 3 — Actor runtime (goroutine, mailbox, snapshot, lifecycle)

**Goal:** wrap the Phase-2 engine in the one-goroutine-per-actor runtime with
lock-free reads. Still no adapters; a "machine with no children" runs end-to-end.

- [ ] `Machine[TCtx,TEvt]` + unexported `runningActor`: actor loop draining
      internal queue then mailbox (D24); `Send` with ctx + `ErrActorStopped`
      after close; `TrySend` → `ErrMailboxFull`.
- [ ] Lock-free `Snapshot()` via `atomic.Pointer[Snapshot]` (D28); publication
      ordering per [Observer ordering](./statesman-architecture.md#observer-ordering-decision-27).
- [ ] Subscribers: COW `atomic.Pointer[[]subscriber]`, strong delivery / bounded
      queue / block-on-overflow (D25); `WithBuffer`, `WithLatestWins`.
- [ ] Observer interfaces + `AddObserver(any)` type-assert at registration (D46);
      sync-observer abort → full rollback → terminal `Error` (D26).
- [ ] Lifecycle: single-shot `Start` via `sync.Once` (D29, D33); idempotent
      `Close` + `done` chan; `CloseAfterDrain(ctx)` (D40).
- [ ] `Clock` / `TimerService` / `Timer` ports; `WallClock` +
      `InProcessTimerService`; state-scoped `after` via ctx tree (D52);
      `Timer.Cancel() bool` race surfacing (D30).
- [ ] `statesmantest`: `Sync` runner (`SendAndSettle`/`Settle`/`Advance`),
      `ManualClock`, `ManualTimerService` (D53).

**Acceptance:** a hand-written childless machine (the `order` chart minus
invokes) runs under `Sync`+`ManualClock`: scenario tests for transitions,
guards, `after` retry/backoff timing, sync-observer rollback, mailbox
close/drain semantics; `go test -race` clean (the gate, D31).

**Depends on:** Phase 2.

---

## Phase 4 — Adapters, supervision, addressing

**Goal:** "everything is an actor" — the four adapters, the actor tree, and
schema-driven supervision.

- [ ] `ActorSpec` + typed constructors (D-[ActorSpec](./statesman-architecture.md#actorspec--the-central-polymorphism)):
      `PromiseActor`, `CallbackActor`, `ObservableActor`, `MachineActor`.
- [ ] `ActorRef[TCtx,TEvt]` composed capabilities (`Sender`/`Snapshotter`/
      `Subscriber`/`Addressable`/`io.Closer`); per-adapter ref shapes incl.
      non-sendable `[…,Never]` (D45).
- [ ] Adapter runtimes: `fromPromise` (one-shot → `done/error.invoke.<id>`),
      `fromCallback` (emit/receive subsets, sendable), `fromObservable`,
      `fromMachine` (nested child actor).
- [ ] Actor tree: parent ctx → child ctx `WithCancel`; bottom-up shutdown;
      activity cancellation via `ctx.Done()` (D55); hierarchical addressing
      (D10) — invoke `id`, spawn caller-supplied name.
- [ ] Supervision (D11): `onDone`/`onError`; event-key split
      `done/error.invoke.<id>` vs. `done/error.actor.<address>`; log+stop when no
      `onError`. Terminal actors never restart (retry = state re-entry = fresh
      actor).
- [ ] Outbox drain wiring: `SendTo<Target>` / `Spawn<Target>` fire after publish.

**Acceptance:** full `order` example runs under `Sync` with `FakeActor` doubles
(D53): retry-then-confirm scenario from
[Testability](./statesman-architecture.md#testability) passes; timeout (`after`)
and `CANCEL` edges exercised; cancellation aborts in-flight fake activity;
race-clean.

**Depends on:** Phase 3.

---

## Phase 5 — Codegen + CLI

**Goal:** replace the hand-written `order` machine with generated code, proving
the core API is a viable codegen target. Build last among the runtime layers,
per the design (codegen targets a *proven* core).

- [ ] `internal/codegen` go/types resolution pass: load user `.go` + `machine.json`,
      compute the **unresolved set** shared by `stub` and `generate` (D9, D47).
- [ ] Naming normalization + hard-fail rules (D22): word-boundary PascalCase;
      reject digit-leading / reserved-word / colliding / unresolved names;
      special-case `done.invoke.X`→`XDone`.
- [ ] Emitters (D14–D20, D32, D43, D45): typed `States` constants; sealed `Event`
      `EventType()` helpers; per-target `ActionResult` variants
      (`Assign`/`SendTo<T>`/`Spawn<T>`/`Noop`); `Implementations` with
      per-callsite narrowed methods + union fallback for 2+ transitions;
      codegen-owned `Context` embedding `ContextFields` + typed child refs;
      `Replay<Name>` helpers; `New<M>Machine` + `Restore<M>` constructors closing
      over the opaque applier (D36).
- [ ] Adapter-kind + subset detection from signatures via go/types (D34): promise
      / callback (emit+receive subsets) / observable / machine; cross-package
      `fromMachine` ref typing.
- [ ] `cmd/statesman` (D49): `init <name>` (runnable `idle→done` + `//go:generate`),
      `stub` (append unresolved set to conventional files, idempotent, gofmt;
      `Impl` skeleton once `machine_gen.go` exists — D47/D48/D51),
      `generate ./...` (import-topological, leaves-first; `--strict` makes
      surviving `Unspecified` a hard error — D48/D50).
- [ ] `statesman.Unspecified` (`= any`) alias + the `generate` warning surfacing
      survivors.

**Acceptance:** `statesman init order2` produces a package where `go test ./...`
is green first run; regenerating the `order` example yields code byte-compatible
in behavior with the Phase-4 hand-written version (same scenarios pass);
golden-file tests for emitters; codegen-error tests for each hard-fail rule
(D22); cross-package `fromMachine` (order→payment) types and compiles.

**Depends on:** Phase 4 (stable core API surface).

---

## Phase 6 — `durable` package (persistence)

**Goal:** the optional persistence layer implementing
`docs/persistence-contract.md` — durable snapshots, at-least-once outbox effects,
deterministic resume, durable `after` timers, and version-CAS concurrency.

**Status:** planned, not started (post-v1). The data shapes are pinned by the
contract; the one non-additive prerequisite is the core seam (6a). Everything
else is a layer *above* the runtime. Sub-phases are ordered; 6a lands first.

### 6a — Core seam for the outbox (prerequisite; touches `package statesman`)

Today `TransitionObserver.OnTransition(ctx, before, after, evt)` runs at the
durability window ❶ (before publish) but does **not** receive the committed
outbox — `Microstep.Effects` drain at ❸, *after* the observer returns. The
contract (§4) requires the snapshot **and** its `[]OutboxIntent` to persist
atomically, so the seam must hand the durable observer the committed effects and
then signal each as it fires. This is the only change inside the core package.

- [ ] Define `OutboxKind` (`SendTo`/`Spawn`) and a codec-free committed-effect
      view in core (`Seq int`, `Kind OutboxKind`, `Target ActorAddress`,
      `Event TEvt`). Core stays serialization-free; `durable` encodes `Event` into
      `OutboxIntent.Payload` (contract §4).
- [ ] Add an optional durable-observer interface the actor loop type-asserts
      (keep `TransitionObserver` minimal): `OnCommit(ctx, before, after Snapshot[TCtx],
      evt TEvt, effects []CommittedEffect[TEvt]) error` at ❶, and
      `OnEffectFired(ctx, addr ActorAddress, version, seq int)` at ❸ per drained intent.
- [ ] Thread `Seq` onto effects in `execMicrostep`/`drainOutbox`; invoke the new
      hooks; preserve abort-on-error rollback (D26) for `OnCommit` exactly as the
      existing observer path does (return error ⇒ no commit, no effects, terminal Error).
- [ ] Core race-clean unit test: a fake durable observer captures `(after, effects)`
      at ❶ and fired `(version, seq)` at ❸ across the order retry chart.

### 6b — Snapshot & outbox serialization (`durable`)

- [ ] `snapshotEnvelope` wire DTO (contract §3): `MachineID`, `Address`,
      `ActiveStates []string`, `Context json.RawMessage`, `PendingAfter`, `Children`,
      `Status` (string), `ErrorReason` (string), `Version`, `Outbox []OutboxIntent`.
- [ ] Pluggable codec (JSON default) used for both the envelope and each
      `OutboxIntent.Payload`.
- [ ] Reflection child-ref (de)serialization (D35): walk `Context`, replace each
      typed `ActorRef` field with `ChildRef{Address,TypeName}` on save and resolve
      back on restore; inline `invoke` children, reference `spawn` children by
      address (D12).
- [ ] `SnapshotMarshaler`/`SnapshotUnmarshaler` opt-out on `Context` — when present,
      skip the reflection path entirely.
- [ ] `ErrorReason error` ↔ string round-trip carrier.

### 6c — Stores (`durable`)

- [ ] `SnapshotStore` (`Load`/`Save`/`Delete`, `[]byte`-typed) with version-CAS
      `Save` (D42): succeed only if stored version == `version-1` (or row absent for
      the first); conflict → `ErrVersionConflict` (caller reloads, never blind-retries).
- [ ] In-memory `SnapshotStore` reference impl (map + mutex) for tests.
- [ ] SQL `SnapshotStore` reference impl (`database/sql`):
      `actor_snapshot(address PK, version, blob, status, updated_at)`; CAS via
      conditional UPSERT (`WHERE version = excluded.version - 1`).
- [ ] `EventLog` (`Append`/`Read(fromSeq)`) + in-memory and SQL impls:
      `actor_event_log(address, seq, version, kind, event_type, payload, at,
      PK(address,seq))`.

### 6d — Persistent timers (`durable`)

- [ ] `PersistentTimerService` satisfying core `TimerService` (D7). Armed timers
      are already captured in the snapshot's `PendingAfter`; on restore, re-arm each
      entry at its original deadline against the live `Clock` — a past deadline fires
      immediately (§7).

### 6e — `durable.Runtime` wrapper (`durable`)

- [ ] `Runtime[TCtx,TEvt]` registers the 6a durable observer on a `Machine`: at ❶,
      `Save(envelope incl. outbox, version)` + `EventLog.Append(transition record)`
      in one backend transaction where supported; at ❸, `EventLog.Append(effect-fired{version,seq})`.
- [ ] Startup recovery reconciliation (§5): `Load` → read fired seqs from `EventLog`
      for the snapshot version → re-fire `outbox ∖ fired` in `Seq` order
      (at-least-once) → re-arm `PendingAfter`.
- [ ] Restore seeds engine configuration from `ActiveStates` without re-running
      entry actions; the macrostep loop replays `always`/internal work
      (transition-algorithm.md §10) so a resumed actor is indistinguishable from one
      that never crashed (modulo re-fired effects).

### 6f — Typed restore / replay (codegen seam)

- [ ] `Restore<M>` constructor + `Replay<Name>` activity-result injection so a
      completed invoke is not re-run on resume (D19, D33) — emitter work in
      `internal/codegen`, consumed by `durable.Runtime`.
- [ ] Rehydrate embedded (`invoke`) children from the parent blob; resolve
      independent (`spawn`) children via `SnapshotStore.Load` (D12).

**Acceptance:**
- Crash/restore integration tests: kill in the ❷→❸ window (committed, mid-drain),
  restart, assert undrained effects re-fire in `Seq` order **exactly once** with
  `Replay` injection; kill before ❷, assert restore from the prior version and a
  deterministic re-run.
- Version-CAS conflict test (two writers / stale failover) returns `ErrVersionConflict`.
- `after` timers survive restore via `PendingAfter` (past deadline fires immediately).
- Reflection round-trip test for a `Context` holding typed child refs, plus a
  `SnapshotMarshaler` opt-out test.
- In-memory and SQL stores pass the same store-contract suite; `go test -race ./durable/...` green.

**Depends on:** Phase 5 (generated `Restore`/`Replay`) + Phase 0 persistence
contract. **6a modifies `package statesman`** and must land (and stay race-clean
under the existing suite) before 6b–6f.

---

## Phase 7 — Test corpus, parity, hardening (final phase)

**Goal:** the cleanup/verification phase — only started once Phases 1–6
demonstrably work. Per AGENTS.md, docs/tests/benchmarks land here, not up front.

- [ ] Scenario runner + `.txtar` data fixtures with a generated `TestScenarios(t)`
      (D53); fixtures double as the parity corpus.
- [ ] **Semantic parity corpus** run against xstate / the Stately schema
      validation suite — the missing piece called out in
      [What this is not yet](./statesman-architecture.md#what-this-is-not-yet).
- [ ] Benchmarks: mailbox bound, subscriber backpressure, codegen speed (the
      "Benchmarked" gap); set defaults from real numbers.
- [ ] `StallObserver` reference impl + goroutine-id assertions in `-debug`/`-race`
      builds (observer-deadlock detection).
- [ ] Godoc on generated code: panic policy (D39), context immutability (D41),
      observer-no-`Send` rule.
- [ ] `DECISIONS.md` entries for any implementation-time decisions (per AGENTS.md).
- [ ] README + quickstart against the `order` example.

**Acceptance:** parity corpus passes; benchmarks recorded; full
`go test -race ./...` green across the module.

**Depends on:** Phases 1–6.

---

## Milestones

- **M0 — Specs locked** (end Phase 0): transition algorithm + schema subset +
  persistence contract reviewed; empty module builds.
- **M1 — Engine proven** (end Phase 2): pure reducer matches every spec trace.
- **M2 — Core runs** (end Phase 3): childless machine runs race-clean under `Sync`.
- **M3 — Example green, hand-written** (end Phase 4): full `order` retry-then-
  confirm passes with `FakeActor`.
- **M4 — Codegen replaces hand-written** (end Phase 5): `init` package green
  first run; regenerated `order` passes M3 scenarios.
- **M5 — Durable** (end Phase 6): crash/restore re-fires effects exactly once.
- **M6 — v1** (end Phase 7): parity corpus + benchmarks; v1 ships.

## Cross-cutting invariants (hold in every phase)

- Type safety prime directive: no `any`/`interface{}`/`map[string]any` on a
  user-facing surface — the two sanctioned exceptions are `AddObserver(any)`
  (D46) and `statesman.Unspecified` (greppable, warned).
- `go test -race` clean is the CI gate (D31).
- Context treated as immutable by convention (D41); engine relies on it for
  rollback and lock-free publication.
- WET over DRY, return-early, functional style, path aliases — per AGENTS.md.

## Implementation-time open questions (resolve in-phase, not deferred to v2)

Distinct from the [v2 candidates](./statesman-architecture.md#open-questions-v2-candidates).
These must be answered while building the named phase:

- **P2:** exact tie-break when multiple parallel regions select conflicting
  transitions on one event — pin in `transition-algorithm.md`.
- **P5:** the callsite→method dispatch table representation behind the opaque
  applier (D36) — array-indexed vs. map; how `--strict` interacts with fallback
  methods.
- **P5:** how go/types resolves adapter **subset** interfaces (D34) when the
  `emit`/`receive` parameter types are themselves still stubs.
- **P6:** reflection traversal of `Context` for nested/embedded child refs and
  interaction with user `SnapshotMarshaler` opt-out (D35).
- **P6 (seam):** extend `TransitionObserver` vs. a separate optional
  durable-observer interface for handing over the committed `[]CommittedEffect`
  and the per-effect `OnEffectFired` signal; where `Seq` is assigned and how it
  threads from `Microstep.Effects` through `drainOutbox`.
- **P6 (atomicity):** snapshot row + transition `EventRecord` atomicity when the
  backend lacks multi-table transactions — embed the transition record in the
  snapshot row, accept a two-write window, or require a txn-capable store.
