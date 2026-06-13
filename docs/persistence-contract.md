# statesman: Persistence contract (normative)

Pins the durability guarantees the architecture's
[Observer ordering](../statesman-architecture.md#observer-ordering-decision-27)
diagram only shapes (D8, D27): **when** a snapshot is durable relative to
effects, the **on-snapshot outbox** wire format, and the **startup recovery**
reconciliation. Implemented by `statesman/durable` (Phase 6); the core only
provides the observer window and typed replay.

> **Status:** normative (TODO.md Phase 0 gate). Drives the `durable` package and
> the `transition-algorithm.md` restore path (§10 there).

## 1. The window: where durability sits in a microstep

From `transition-algorithm.md` §4.2/§8.4, one microstep runs:

```
pre = snapshot pre-state              # rollback point
exit / transition / enter actions     # build pending ctx + config + outbox
run sync observers  ───────────────►  ❶ durable WRITE happens here
publish (atomic.Store; Version++)  ─►  ❷ commit point
drain outbox (SendTo / Spawn fire) ─►  ❸ effects fire
notify subscribers
```

The durable layer is a **`TransitionObserver`** (D27/D46). Its `OnTransition`
runs at ❶, **before** publish, with `(pre, next)` snapshots. The contract:

- **Snapshot is durable before it is published.** If the durable write fails,
  `OnTransition` returns error ⇒ the engine **aborts with full rollback** (D26):
  the transition never commits, the actor goes terminal `Error`, **no effect
  fires**. A persisted snapshot therefore always reflects a transition the
  in-memory actor also took — never the reverse.
- **Effects fire after the commit point ❷.** So a crash in the window ❷→❸ leaves
  a durable snapshot whose outbox effects **did not all fire**. Recovery (§5)
  re-fires them. This is the deliberate at-least-once posture: re-fire on
  recovery, demand idempotency or `Replay` (architecture line 827).

## 2. What is persisted, and at what granularity

Mirrors the invoke/spawn split (D12; architecture
[Granularity](../statesman-architecture.md#granularity-invoke-vs-spawn)):

| Unit | Storage | Keyed by |
|---|---|---|
| Root actor | one snapshot row | root `ActorAddress` |
| `invoke` child | **embedded** in the parent snapshot blob | — |
| dynamic `spawn` child | **independent** snapshot row; parent stores a `ChildRef` | child `ActorAddress` |

Each row holds the **snapshot blob + outbox blob + version** (§3, §4). The
`EventLog` (§6) holds the per-transition record stream.

## 3. Snapshot wire format

`Snapshot[TCtx]` (architecture
[Generics](../statesman-architecture.md#generics-in-the-core-runtime)) serializes
at the storage boundary; types are known again at the typed `Restore<M>` callsite.

- **Default: reflection-driven** (D35; architecture
  [Child ref serialization](../statesman-architecture.md#child-ref-serialization)).
  On `Save`, the runtime walks `Context` via reflection and replaces each typed
  `ActorRef` field with `ChildRef{Address, TypeName}`; embedded (`invoke`) child
  snapshots are inlined, spawned children become an address reference. Encoding is
  JSON by default.
- **Opt-out: `SnapshotMarshaler`/`SnapshotUnmarshaler`** on `Context` — full
  control of the wire format (encrypt PII, non-JSON encoding). When present, the
  reflection path is skipped entirely.
- **Stored fields:** `MachineID`, `Address`, `ActiveStates` (`[]StateID`),
  `Context` (per above), `PendingAfter` (`[]ScheduledTimer` — deadline + descriptor
  per timer, for re-arm §7), `Children` (`[]ChildRef`), `Status`, `ErrorReason`
  (string form), `Version`.

## 4. Outbox wire format

The outbox is persisted **alongside** the snapshot in the same row, committed in
the same `Save` (so snapshot and its pending effects are atomic together).

```go
type OutboxIntent struct {
    Seq     int64        // 0-based index within this transition's outbox
    Kind    OutboxKind   // SendTo | Spawn
    Target  ActorAddress // recipient (SendTo) or new child address (Spawn)
    Payload []byte       // serialized event (SendTo) or spawn descriptor (Spawn)
    EventType string     // EventBase.EventType() of the payload, for matching/audit
}
```

- The outbox blob is the ordered `[]OutboxIntent` for the **just-committed**
  transition only (it is rebuilt each microstep).
- `Payload` is encoded with the same codec as the snapshot.
- A drained intent is recorded in the `EventLog` (§6) as an `effect-fired` record
  carrying `(Address, Version, Seq)` — this is how recovery tells fired from
  unfired (§5).

## 5. Startup recovery reconciliation

`durable.Runtime`, on startup for an address:

```
snap, ok := SnapshotStore.Load(addr)
if !ok: nothing to recover
firedSeqs := EventLog.Read(addr, fromSeq=snap.Version's first record)
             |> filter Kind==effect-fired && Version==snap.Version
             |> collect Seq
undrained := [ i for i in snap.outbox if i.Seq not in firedSeqs ]
restore actor from snap (transition-algorithm.md §10)
for i in undrained (in Seq order): re-fire i      # at-least-once
```

- **Crash before ❷ (commit):** no durable snapshot for that Version exists; the
  actor restores from the prior committed snapshot and the transition re-runs
  from its triggering event — deterministic (`transition-algorithm.md`).
- **Crash in ❷→❸ (after commit, mid-drain):** the snapshot at the new Version is
  durable; `firedSeqs` covers the intents that fired before the crash; recovery
  re-fires only `undrained`. Re-fired `SendTo`/`Spawn` must be idempotent, or the
  target activity uses `Replay<Name>` injection so a completed activity is not
  re-run (architecture
  [Typed activity-result injection](../statesman-architecture.md#typed-activity-result-injection-on-restore)).
- **Effects are re-fired in `Seq` order**, preserving the within-transition
  ordering the outbox guaranteed (`transition-algorithm.md` §8.3).

## 6. EventLog

```go
type EventRecord struct {
    Seq       int64       // monotonic per address
    Version   int         // the actor Version this record belongs to
    Kind      RecordKind  // transition | effect-fired
    EventType string      // triggering event (transition) or payload type (effect)
    Payload   []byte      // triggering event (transition records); nil for effect-fired
    At        time.Time   // Clock time
}
```

- One `transition` record per committed microstep (written in the same observer
  window ❶ as the snapshot, same transaction where the store supports it).
- One `effect-fired` record per drained outbox intent (written at ❸).
- `Read(addr, fromSeq)` returns records in `Seq` order for recovery and audit.

## 7. Timers

`PendingAfter` is the durable record of armed `after` timers (D52). On `Restore`,
`durable.PersistentTimerService` (or the in-process default) re-arms each entry at
its original deadline against the live `Clock`; ctx-scoping is re-established per
the restored configuration (`transition-algorithm.md` §10). A deadline already
past at restore fires immediately (the event enqueues on the mailbox as normal).

## 8. Optimistic concurrency

`SnapshotStore.Save(addr, data, version)` is a **compare-and-swap on `Version`**
(D42): it succeeds only if the stored version is `version-1` (or the row is
absent for `version`==first). A conflict (two writers, or a stale process after a
failover) returns a conflict error; the caller must reload and not retry blindly.
`Version` is monotonic per actor, +1 per committed transition (architecture
[Generics](../statesman-architecture.md#generics-in-the-core-runtime)).

## 9. Guarantees summary

- **Snapshot ⊑ memory:** every durable snapshot reflects a transition the actor
  actually committed (observer-before-publish + abort-on-write-failure).
- **At-least-once effects:** every outbox intent fires at least once across
  crashes; idempotency or `Replay` makes it effectively once.
- **Deterministic resume:** restore + the macrostep loop reproduce all
  `always`/internal work and re-arm timers, so a resumed actor is
  indistinguishable from one that never crashed, modulo re-fired effects.
- **No partial commits:** a failed durable write rolls the whole transition back
  to its pre-state (D26) — partial commits are worse than visible failure for
  backend workflows.

## 10. Out of scope (this contract)

- Cross-process routing / distributed delivery — v2 (`ActorTransport`).
- Storage-engine specifics (SQL DDL, Postgres `LISTEN/NOTIFY`, Redis ZSET) — the
  `durable` reference impls choose these; this contract only constrains ordering,
  format, and recovery semantics.
