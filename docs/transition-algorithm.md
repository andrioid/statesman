# statesman: Transition algorithm (normative)

The precise reduction of the SCXML interpretation algorithm to statesman's
Stately subset. This is the contract the core engine
([architecture: Generics in the core runtime](../statesman-architecture.md#generics-in-the-core-runtime),
[Transition step](../statesman-architecture.md#transition-step-deterministic))
must implement. Where the architecture doc sketches ordering, this document is
authoritative.

> **Status:** normative (TODO.md Phase 0 gate); reviewed & approved 2026-06-13.
> Invoke timing (§7.4) decided: **deferred-invoke**.

## 1. Scope & sources

- **Source algorithm:** W3C SCXML Recommendation, Appendix D ("Algorithm for
  SCXML Interpretation") — the standard `mainEventLoop` / `microstep` /
  `selectTransitions` / `computeExitSet` / `enterStates` procedures. We
  transcribe them with SCXML's names so a reviewer can check line-for-line, then
  reduce to our subset and re-map executable content onto statesman's
  action/effect model.
- **Subset boundary:** which `machine.json` constructs exist at all is pinned in
  the sibling spec `docs/schema-subset.md`. This document assumes a loaded,
  validated `*Definition` and never re-litigates JSON shape; it cross-references
  `schema-subset.md` for the internal-vs-external rule (the schema has no
  `internal` flag), event-descriptor matching, and history defaults.
- **Out of scope:** persistence/durability ordering relative to effects — see
  `docs/persistence-contract.md`. This document defines *when a snapshot is
  built and published*; the durable layer decides *when it hits disk* within the
  observer window (§8.4).

## 2. Data model

The loaded definition is a tree of state nodes plus a flat transition list.

### 2.1 State node

| Field | Meaning |
|---|---|
| `id` | unique `StateID` (dotted path, e.g. `processing.charging`) |
| `kind` | `atomic` · `compound` · `parallel` · `final` · `history` |
| `parent` | parent node (nil for root) |
| `children` | ordered (document order) |
| `initial` | for `compound`: the child entered by default |
| `historyKind` | for `history`: `shallow` · `deep` |
| `historyDefault` | for `history`: default target transition if no stored value |
| `entry`, `exit` | ordered lists of action refs |
| `invokes` | ordered list of invoke specs (`fromMachine/Promise/Callback/Observable`), each with a static `invoke.id` |
| `afters` | ordered list of `(delay, transition)` — desugared per §3 |
| `transitions` | `on` + `always`, in document order |

**Document order** is the depth-first pre-order index assigned at load; it is the
sole tie-breaker for selection and the basis for entry/exit ordering.

### 2.2 Transition record

| Field | Meaning |
|---|---|
| `source` | owning state node |
| `event` | event descriptor, or **nil** ⇒ eventless (`always`) |
| `guard` | guard ref, or nil ⇒ always passes |
| `targets` | effective target state set; **empty** ⇒ targetless. JSON `target` is a single ref (`schema-subset.md`); fan-out to multiple states happens only via parallel/`initial` entry closure (§7.1), never two explicit targets |
| (internal/external) | **derived, not a JSON field**: targetless ⇒ internal (action-only); any `target` ⇒ external (§6.1). The Stately schema has no `internal`/`reenter` flag |
| `actions` | ordered action refs |
| `docOrder` | document-order index within `source` |

### 2.3 Interpreter state

- `configuration` — set of active state nodes (atomic states + every ancestor up
  to root, plus all regions of active parallels). The published
  `Snapshot.ActiveStates` is the **atomic** subset, normalized to `StateID`.
- `context` (`TCtx`) — immutable; replaced by accumulated `Assign` (D32/D41).
- `historyValue` — `map[StateID][]StateID`: per history node, the states to
  restore (§6.4, §7.3).
- `internalQueue` — FIFO of internal events (self-sends, `done.state.*`; §3).
- `externalQueue` — the bounded mailbox (`after` fires, parent/child events,
  host `Send`; D24).
- `statesToInvoke` — set accumulated during a macrostep, drained at its end (§7.4).
- `alwaysRunLength` — consecutive eventless microsteps, for the loop guard (§9).

## 3. Reduction of `after`, `always`, done/error to events

statesman has exactly **one** truly eventless construct; everything else is an
ordinary event transition. This is the key simplification over raw SCXML.

| Construct | Reduction |
|---|---|
| `always` | **Eventless** transition (`event == nil`). Selected by `selectEventlessTransitions` (§5.1). |
| `after: {D: target}` | Desugars to: on entry of the owning state, schedule a `Clock`-timed timer (D52) that, on fire, enqueues a synthetic event `xstate.after(D)#<stateID>` onto the **external** queue. The transition is an ordinary evented transition keyed by that descriptor. Timer is ctx-scoped to the state and cancelled on exit (§6.3). |
| `invoke` done | Child final ⇒ parent receives **external** event `done.invoke.<id>` carrying the child's output (D18). |
| `invoke` error | Child error ⇒ parent receives **external** event `error.invoke.<id>` (D11, supervision). |
| compound/parallel final reached | Engine enqueues **internal** event `done.state.<parentID>` (§7.5). |

Consequence: `after` and invoke outcomes flow through the mailbox exactly like
host events, so a timeout, a transport failure, and a `CANCEL` are three
ordinary edges (architecture: [Timeouts](../statesman-architecture.md#timeouts)).
Only `always` and `done.state.*` are internal.

## 4. Top-level loop (macrostep / microstep)

Normative `mainEventLoop`, reduced. A **microstep** takes one (conflict-resolved)
transition set. A **macrostep** is the maximal run of microsteps that consumes no
external event — it runs eventless and internal transitions to quiescence, then
blocks for one external event.

```
interpret(def, entryMode):           # entryMode = Fresh | Restore(snapshot)
    initialize configuration (§4.1)
    runMacrostepToQuiescence()       # settles always/internal + initial invokes
    while running:
        externalEvent = externalQueue.dequeue()   # blocks (mailbox)
        if isCloseSignal(externalEvent): break
        enabled = selectTransitions(externalEvent) # §5.2
        if not enabled.isEmpty():
            microstep(enabled, triggeredByEvent=true)   # §4.2, resets alwaysRunLength
        runMacrostepToQuiescence()

runMacrostepToQuiescence():
    loop:
        enabled = selectEventlessTransitions()         # §5.1
        if enabled.isEmpty():
            if internalQueue.isEmpty(): break           # macrostep done
            ev = internalQueue.dequeue()
            enabled = selectTransitions(ev)
            if enabled.isEmpty(): continue
            microstep(enabled, triggeredByEvent=true)   # resets alwaysRunLength
        else:
            guardAlwaysLoop()                           # §9
            microstep(enabled, triggeredByEvent=false)  # increments alwaysRunLength
    drainStatesToInvoke()                               # §7.4 — invokes start here
```

Selection priority within a macrostep: **eventless > internal event > (loop ends,
wait for) external event**. This is SCXML's exact ordering; the architecture's
"internal queue first, then mailbox" (step 1) and "process `always`" (step 12)
are this loop flattened — reconciled in §8.5.

### 4.1 Initial configuration

- **Fresh:** enter the root's `initial` child set via `enterStates` with a
  synthetic transition whose targets are the root initial (§7). Recurse into
  compound `initial` and all parallel regions.
- **Restore(snapshot):** seed `configuration` directly from
  `snapshot.ActiveStates` (re-expanded to include ancestors), set `context`,
  `historyValue`, and `alwaysRunLength=0`; re-arm every `PendingAfter` timer
  (§10). Entry actions are **not** re-run; invokes are re-established per
  `persistence-contract.md` (replay vs. fresh). `Start` is single-shot for both
  paths (D29/D33).

### 4.2 `microstep`

```
microstep(enabled, triggeredByEvent):
    pre = snapshotPreState()                 # rollback point (arch step 4)
    exitStates(enabled)                      # §6  (arch step 5)
    executeTransitionActions(enabled)        # §8.1 (arch step 6)
    enterStates(enabled)                     # §7  (arch step 7)
    if runSyncObservers(pre, next) == error: # arch step 8
        rollbackToTerminalError(pre, err)    # §8.4 — actor becomes terminal
        return
    publish(next)                            # atomic.Store; Version++ (arch step 9)
    drainOutbox()                            # SendTo/Spawn fire (arch step 10)
    notifySubscribers()                      # arch step 11
    if triggeredByEvent: alwaysRunLength = 0
```

`exitStates` + `executeTransitionActions` + `enterStates` build the *pending*
next configuration and context without publishing; publication is atomic at
`publish` (D28). Effects fire only after publish (D27).

## 5. Transition selection

### 5.1 `selectEventlessTransitions`

```
enabled = {}
for state in atomicStates(configuration) sorted by docOrder:
    found = false
    for s in [state] + properAncestors(state):     # innermost → outermost
        for t in s.transitions sorted by docOrder:
            if t.event == nil and guardPasses(t.guard, context):
                enabled.add(t); found = true; break
        if found: break
return removeConflictingTransitions(enabled)         # §5.4
```

First matching eventless transition **per atomic state**, searched innermost-out,
document order within a node. A child's eventless transition shadows an
ancestor's for that atomic state.

### 5.2 `selectTransitions(event)`

Identical to §5.1 except the inner test is
`eventMatches(t.event, event) and guardPasses(t.guard, context)`.

### 5.3 Event matching & guards

- **Matching** (`eventMatches`): exact `EventType()` string equality, plus the
  generated keys `done.invoke.<id>`, `error.invoke.<id>`, `done.state.<id>`, and
  the `xstate.after(D)#<id>` descriptor. Wildcard support (`*`) is deferred to
  and pinned by `schema-subset.md`.
- **Guards** are pure predicates over `(context[, event[, params]])` (D15/D16),
  evaluated in document order during selection — **before** any state is exited.
  A guard MUST NOT mutate context (D41). Guard order is the retry budget's
  mechanism (architecture: [Retries](../statesman-architecture.md#retries)).

### 5.4 `removeConflictingTransitions` (parallel preemption)

When parallel regions enable transitions for the same event, disjoint ones all
fire (document order); intersecting ones are resolved — deeper source preempts
shallower, else earlier document order wins.

```
filtered = {}
for t1 in enabled sorted by docOrder:
    preempted = false
    toRemove = {}
    for t2 in filtered:
        if computeExitSet([t1]) ∩ computeExitSet([t2]) != ∅:
            if isDescendant(t1.source, t2.source):
                toRemove.add(t2)          # t1 deeper ⇒ preempts t2
            else:
                preempted = true; break    # t2 wins
    if not preempted:
        filtered = (filtered \ toRemove) ∪ {t1}
return filtered
```

## 6. Exit set

### 6.1 Internal vs. external transitions

The Stately `Transition` schema has **no `internal`/`reenter` field**
(`schema-subset.md`), so internal-vs-external is **derived from the target**, not
configured:

- **Targetless** (`target` omitted) ⇒ **internal**: runs only its `actions`,
  exits and enters nothing, preserves the source's `exit`/`entry` and invokes.
  The pure action/assign case.
- **Targeted** (`target` present) ⇒ **external**: exits and re-enters down to the
  LCCA (§6.2), re-running `exit`/`entry` and restarting invokes. A self-transition
  (`target == source`) is external — it re-enters, re-running entry and (per §7.4)
  re-invoking. The retry loop relies on exactly this (§11).

statesman therefore does **not** support SCXML's internal transition *with a
descendant target* (change state without re-entering the source) — it is
unrepresentable in this schema. If a future schema gains `reenter`, this is the
one place the distinction widens.

### 6.2 `getTransitionDomain` / `findLCCA` / `computeExitSet`

```
getTransitionDomain(t):
    if t.targets == ∅: return nil               # targetless ⇒ internal, no exit
    return findLCCA([t.source] + t.targets)      # targeted ⇒ external

findLCCA(states):                                          # least common COMPOUND ancestor
    for anc in properAncestors(states[0]) where anc.kind in {compound, root}:
        if all(isDescendant(s, anc) for s in states[1:]):
            return anc
    # parallel ancestors are never the LCCA; the compound/root above them is

computeExitSet(transitions):
    out = {}
    for t in transitions where t.targets != ∅:
        domain = getTransitionDomain(t)
        for s in configuration where isDescendant(s, domain):
            out.add(s)
    return out
```

`findLCCA` returns only `compound`/`root` nodes (SCXML semantics). A parallel
region is entered/exited as a unit through its compound/root parent, never
selected as the LCCA itself.

### 6.3 `exitStates`

```
exitStates(enabled):
    toExit = computeExitSet(enabled)
    for s in toExit sorted by reverse docOrder:        # innermost-first
        recordHistory(s)                                # §6.4 — BEFORE removal
        cancelAfterTimers(s)                            # ctx-cancel state-scoped timers (D52)
        stopInvokes(s)                                  # cancel child ctx ⇒ ctx.Done() (D55)
        applyActions(s.exit)                            # fold Assign into pending context
        configuration.remove(s)
        statesToInvoke.discardIfPresent(s)
```

Exit order is reverse document order (deepest, latest first). History is recorded
*before* the state leaves the configuration. Stopping invokes cancels the child
ctx, which is how an in-flight activity aborts (architecture:
[Cancellation](../statesman-architecture.md#cancellation)) — the *why* (timeout
vs. failure vs. `CANCEL`) is the edge that was taken, never a ctx cause.

### 6.4 History recording

On exit of a state `s` that contains a `history` child `h`:
- `shallow` ⇒ `historyValue[h.id] = ` immediate children of `s` that are in the
  configuration.
- `deep` ⇒ `historyValue[h.id] = ` atomic descendants of `s` that are in the
  configuration.

## 7. Entry set

### 7.1 `computeEntrySet`

For each transition, seed `statesToEnter` with `getEffectiveTargetStates`
(resolving history targets, §7.3), then close upward and downward:

- **`addDescendantStatesToEnter(s)`**: if `s` is `history`, expand to stored
  values (or its `historyDefault`); if `s` is `compound`, also enter its
  `initial` child (recursively, marking it for default-entry); if `s` is
  `parallel`, also enter **all** its regions (recursively).
- **`addAncestorStatesToEnter(s, ancestor)`**: add every state between `s` and
  the transition domain so the path into `s` is complete; entering a `parallel`
  on the path pulls in all sibling regions not already entered.

### 7.2 `enterStates`

```
enterStates(enabled):
    (toEnter, defaultEntry) = computeEntrySet(enabled)
    for s in toEnter sorted by docOrder:               # ancestors-first
        configuration.add(s)
        applyActions(s.entry)                           # fold Assign into pending context
        scheduleAfterTimers(s)                          # arm Clock timers, ctx-scoped (§3, D52)
        statesToInvoke.add(s)                           # invokes DEFERRED — §7.4
        if s in defaultEntry: applyActions(s.initialTransitionActions)
        if s.kind == final: onFinalEntered(s)           # §7.5
```

Entry order is document order (ancestors before descendants), the inverse of
exit. `Assign` from entry actions folds into the same pending context the exit
and transition actions built (§8.1).

### 7.3 History restoration

`getEffectiveTargetStates` for a `history` target `h`: if `historyValue[h.id]`
exists, use it; else use `h.historyDefault`'s targets. Shallow restores the
recorded immediate children (each then re-defaults downward via §7.1); deep
restores the exact recorded atomic descendants.

### 7.4 Invoke timing (deferred)

Invokes are **deferred to macrostep end**, not spawned on entry: `enterStates`
records each entered state in `statesToInvoke`; `drainStatesToInvoke()` (§4)
spawns invokes only for states still in the configuration once the macrostep
reaches quiescence. A state exited before quiescence is removed from
`statesToInvoke` (§6.3) and never invokes.

This is SCXML's `<invoke>` timing and **refines architecture step 7** (which
reads as spawn-on-entry). It matters for backend workflows: an `invoke` is
usually a real side effect (e.g. `chargeCard` HTTP), so spawning it for a
**transient** state (entered then left within one macrostep by an `always`
transition) and immediately cancelling would waste a request and could
double-fire a non-idempotent call.

`after` timers are **not** deferred — they are cheap and ctx-cancelled on exit,
so they arm on entry per §7.2.

### 7.5 Final states & `done` generation

`onFinalEntered(s)`:
- If `s.parent == root` ⇒ the machine is `Done`; it emits the terminal event its
  parent observes — `done.invoke.<id>` when this machine is itself an invoke
  child (D18). The payload is the **actor's** output (a `fromPromise` return, a
  `fromObservable` latest value, or a `fromMachine` child's final `Context`), not
  a schema field — the Stately schema has **no `output`/donedata** on `final`
  (`schema-subset.md`).
- Else enqueue **internal** `done.state.<s.parent.id>` (no payload in v1; the
  schema has no final output). If `s.parent.parent` is `parallel` and *all* its
  regions are now in a final state, also enqueue internal
  `done.state.<grandparent.id>`.

`onDone` is just an ordinary transition keyed on `done.state.<id>` /
`done.invoke.<id>` (D11). No special path.

## 8. Action application, effects, transactionality

### 8.1 Accumulation within a microstep

Across `exitStates` → `executeTransitionActions` → `enterStates`, actions run in
that order. Each action method returns one `ActionResult` variant (D17/D44):

- `Assign{Fields}` ⇒ fold into the **pending** context (replace, preserving child
  refs per D32/D37). Later assigns see earlier assigns' result.
- `SendTo<Target>` / `Spawn<Target>` ⇒ append an intent to the **outbox** (not yet
  fired).
- `Noop` ⇒ nothing.

The engine never inspects `ActionResult` directly: it calls the opaque applier
`func(actionIndex int, ctx TCtx, evt TEvt) appliedEffect` injected by the
generated constructor (D36). `appliedEffect` carries either a new context (assign)
or an outbox intent. This is the sole core/codegen type boundary.

### 8.2 Triggering event at callsites

The `evt` passed to an action/guard is the event that triggered the current
microstep. For eventless (`always`) microsteps there is no triggering event;
codegen wires those callsites to the `shape=context` method form (no `evt`
param). Per-callsite narrowing (D15) guarantees the concrete type.

### 8.3 Outbox ordering

The outbox preserves append order. It is drained (§4.2, after `publish`) by
firing each `SendTo` (a `Send` to the target ref) and completing each `Spawn`.
Drain-after-publish (D27) is what makes effects re-fire safely on crash recovery
(`persistence-contract.md`).

### 8.4 Observers & rollback

`runSyncObservers` runs every registered `TransitionObserver` with `(pre, next)`
snapshots, **before** publish — this is the durable layer's write window (D27).

- All return nil ⇒ proceed to `publish`.
- Any returns error ⇒ **full rollback** (D26): discard pending context/config
  changes, build a failure snapshot from `pre` with `Status=Error` and
  `ErrorReason` set, `atomic.Store` it, emit `error.invoke.<id>` /
  `error.actor.<address>` to the parent (a root surfaces via snapshot/observers
  only), notify subscribers, and **terminate the actor** (it never resumes;
  retry = a fresh actor, §11). Outbox is **not** drained on the abort path —
  effects never fire for an aborted transition.

### 8.5 Reconciliation with architecture's 12-step list

| Arch step ([ref](../statesman-architecture.md#transition-step-deterministic)) | This spec |
|---|---|
| 1 receive (internal-first, then mailbox) | §4 loop priority: eventless > internal > external |
| 2 evaluate guards (document order) | §5.1/§5.2 selection |
| 3 compute exit/entry/actions | §6 + §7.1 |
| 4 pre-snapshot (rollback point) | §4.2 `pre` |
| 5 exit actions (+ stop spawned) | §6.3 |
| 6 transition actions | §8.1 `executeTransitionActions` |
| 7 entry actions (+ invoke) | §7.2 entry; **invoke deferred to macrostep end** §7.4 |
| 8 sync observers (+ abort path) | §8.4 |
| 9 publish | §4.2 `publish` |
| 10 drain outbox | §8.3 |
| 11 notify subscribers | §4.2 |
| 12 process `always` (loop guard) | §4 macrostep loop + §9 |

Two refinements this spec makes explicit beyond the list: (a) invoke timing —
deferred to macrostep end (§7.4); (b) the internal `done.state.*` queue
interleaves with `always` inside the macrostep loop (the list's step 12 is the
whole loop, not just `always`).

## 9. Loop guard & termination

```
guardAlwaysLoop():
    alwaysRunLength += 1
    if alwaysRunLength > limit:    # default 100, WithAlwaysLimit (D38)
        terminate actor: Status=Error, ErrorReason=ErrAlwaysLoopExceeded
```

`alwaysRunLength` increments per eventless microstep and resets to 0 whenever a
microstep is triggered by an event (internal or external) — i.e. each consumed
event is "progress". A macrostep terminates normally when no eventless transition
is enabled and the internal queue is empty. The machine reaches `Done` when the
root enters a final state (§7.5) or `Error` via observer abort (§8.4) or the loop
guard. Terminal actors do not restart (architecture:
[Actor lifecycle](../statesman-architecture.md#actor-lifecycle)).

## 10. Restore-specific semantics

On `Restore(snapshot)` (§4.1):
- `configuration` is seeded from `snapshot.ActiveStates`; entry actions are not
  re-run (the state was already entered before the crash).
- Every `snapshot.PendingAfter` timer is re-armed against the live `Clock`/
  `TimerService` with its original deadline; ctx-scoping is re-established per
  the restored configuration (§6.3, D52).
- Invokes for active states are re-established: replayed via `Replay<Name>`
  (pre-resolved, fires `done.invoke.<id>` without re-running the side effect) or
  freshly spawned, per `persistence-contract.md`. Restore then enters the normal
  macrostep loop, so any `always`/internal work resumes deterministically.

## 11. Worked example — the `order` retry loop

Chart used (self-contained; matches architecture
[Retries](../statesman-architecture.md#retries)):

- root `invoke watchInventory` (callback), live for the whole machine.
- `idle` *(initial)* — `on SUBMIT → charging` *(action `validateForm`)*.
- `charging` — `invoke chargeCard`; `on done.invoke.charge → confirming`;
  `on error.invoke.charge [hasRetriesLeft] → retrying` *(action
  `incrementRetries`)*, `[else] → errored`; `after RetryTimeout(30s)` same guarded
  pair.
- `retrying` — `after RetryDelay(5s) → charging`.
- `confirming → done`; `done` *(final)*; `errored` *(final)*.
- `hasRetriesLeft(ctx) = ctx.Retries < 3`.

Trace (deferred-invoke per §7.4; `ManualClock`; microstep granularity):

| # | Trigger (queue) | Selected transition | Exit set | Entry set | Effects (post-publish) | Config (atomic) | Retries |
|---|---|---|---|---|---|---|---|
| 0 | `Start` | root initial → `idle` | — | root, `idle` | spawn `watchInventory` (drain) | `idle` | 0 |
| 1 | `SUBMIT` (ext) | `idle → charging` | `idle` | `charging` | `validateForm` ⇒ `SendToInventory{WatchSKUs}` fired; spawn `chargeCard#1` (drain) | `charging` | 0 |
| 2 | `error.invoke.charge` (ext) | `charging →[Retries 0<3] retrying`, action `incrementRetries` | `charging` (stop `chargeCard#1` [terminal], cancel `after 30s` via ctx) | `retrying` (arm `after 5s`) | `Assign Retries=1` | `retrying` | 1 |
| 3 | `xstate.after(5s)#retrying` (ext, on `Advance(5s)`) | `retrying → charging` | `retrying` (after-timer already fired) | `charging` (arm `after 30s`) | spawn **fresh** `chargeCard#2` (drain) | `charging` | 1 |
| 4 | `done.invoke.charge{Output}` (ext) | `charging → confirming` | `charging` (cancel `after 30s`; `chargeCard#2` terminal) | `confirming` | — | `confirming` | 1 |
| 5 | (`confirming` immediate) `confirming → done` | `confirming` | `done` (final) | `onFinalEntered`: root final ⇒ machine `Done`; emit terminal event | `done` | 1 |

What the trace exercises: external-event selection (§5.2), guard-as-budget in
document order (§5.3, step 2), exit/entry sets with invoke stop/start (§6.3/§7),
`after` arm-on-entry + ctx-cancel-on-exit + re-arm per attempt (§3, step 2→3),
`Assign` fold persisting `Retries` across a crash (step 2), fresh-actor-on-retry
(step 3, terminal actors never restart), and final → `done` (§7.5). Crash after
step 2 then `Restore` resumes at `retrying` with `Retries=1` and the 5s timer
re-armed (§10) — the whole reason the loop lives in the chart, not a Go `for`.

## 12. Deferred to sibling specs

- Event descriptors / wildcards, history defaults, the internal-vs-external rule
  (no `internal` flag), and the exact loadable construct set →
  `docs/schema-subset.md`.
- Snapshot durability point within the observer window (§8.4), outbox wire
  format, and crash-recovery outbox reconciliation → `docs/persistence-contract.md`.
