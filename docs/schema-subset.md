# statesman: Stately schema subset (normative)

What v1 loads from `machine.json`, field by field, against the **actual**
[`machineSchema.json`](https://github.com/statelyai/schema/blob/main/machineSchema.json)
(vendored at `internal/schema/testdata/machineSchema.json`). Pins the boundary the
[transition algorithm](./transition-algorithm.md) assumes (it defers JSON shape
here) and the loader (`internal/schema`) implements.

> **Status:** normative (TODO.md Phase 0 gate). Grounded in the schema as fetched
> 2026-06-13 — a **purely structural** schema (no types, **no `context`**).

## 1. Validation layers

A `machine.json` passes three gates, in order:

1. **Structural** — validates against the vendored `machineSchema.json`
   (JSON-Schema draft 2020-12). The schema is `additionalProperties: false`
   throughout, so unknown fields are rejected here, for free.
2. **Subset** — statesman load-time constraints *stricter* than the schema (§5).
   Violations are load errors with a JSON path.
3. **Name resolution** — at `statesman generate`: identifiers resolve to Go
   symbols by convention (D22, architecture
   [Naming normalization](../statesman-architecture.md#naming-normalization)).
   Failures are `CodegenError`, not load errors.

This document defines gates 1–2. Gate 3 is the architecture's naming section.

## 2. The schema at a glance

Root object = a state node plus a root-only `version`. Every nested state (under
`states`) is the recursive `State` definition — identical fields minus `version`.
Four `$defs`: `Action`, `Transition`, `Guard`, `Invoke` (+ free-form `Meta`).

```
State := { id?, description?, type?, target?, history?, initial?,
           entry?, exit?, on?, after?, always?, invoke?, meta?, states? }
Action     := { type (req), params? }
Guard      := { type (req), params? }
Transition := { target?, actions?, guard?, description?, meta? }
Invoke     := { id?, src (req), onDone?, onError?, meta? }
```

## 3. State node fields

| Field | Schema | v1 behavior |
|---|---|---|
| `id` | string | Optional in schema; the **state key** (its property name under `states`) is the identity statesman uses. Loaded into `StateID` as the dotted path. If `id` is present it must match the key or it's a load error (§5). |
| `description` | string | **Loaded-but-ignored** (Studio round-trip only). |
| `type` | `parallel`/`history`/`final` | Absent ⇒ **atomic** (no `states`) or **compound** (has `states`). All four kinds supported (D3). |
| `target` | string | **History states only** — the default target if no stored history value (`transition-algorithm.md` §7.3). Ignored on non-history states. |
| `history` | `shallow`/`deep` | History states only. **Absent ⇒ `shallow`** (xstate default). Load error on a non-history state. |
| `initial` | string | Compound states: the default child key. **Required on compound** (§5) — a bare child key, never an object, so initial transitions carry **no actions** in v1 (`transition-algorithm.md` §7.2 `initialTransitionActions` is always empty). Ignored on atomic/parallel/final/history. |
| `entry`, `exit` | `Action[]` | Ordered entry/exit action refs. Each `type` → Go method (D22); `params` → `<Name>Params` (§4). |
| `on` | `{ desc: Transition \| Transition[] }` | Event transitions. Key = event descriptor (§6). Array value = ordered candidates (document order). |
| `after` | `{ delay: Transition \| Transition[] }` | Delayed transitions. Key = delay descriptor (§7). Desugars to a timed event (`transition-algorithm.md` §3). |
| `always` | `Transition \| Transition[]` | Eventless transitions; the only truly eventless construct (`transition-algorithm.md` §3, §5.1). |
| `invoke` | `Invoke[]` | Invoked actors, spawned at macrostep end (`transition-algorithm.md` §7.4). |
| `meta` | `Meta` | **Loaded-but-ignored.** |
| `states` | `{ key: State }` | Child states. Presence (without `type`) ⇒ compound. |
| `version` | string | **Root only; loaded-but-ignored.** |

## 4. Action / Guard / params

- `Action` and `Guard` are `{ type: string (required), params?: object }`.
- `type` → Go symbol by normalization (D22). The action/guard is **parameterized
  iff** a `<Name>Params` struct exists in the package (architecture: authoring).
- `params` is `additionalProperties: {}` — arbitrary literal config. It is the
  **only** place the schema carries literal values, so it is the sole input to
  `statesman stub`'s type inference (architecture stub limit #2): string→`string`,
  number→`int64`, bool→`bool`; anything else → `statesman.Unspecified` + TODO.
- statesman never reads `params` at runtime as a map — values are surfaced only
  through the typed `<Name>Params` struct (no `map[string]any` on any surface).

## 5. Subset constraints (statesman is stricter than the schema)

Load errors (gate 2), each with a JSON path:

1. **`invoke.id` is required.** The schema marks it optional, but addressing
   needs a static id (D10; architecture
   [Actor addressing](../statesman-architecture.md#actor-addressing)). Missing ⇒
   error.
2. **`initial` is required on every compound state.** No silent "first child"
   default — entry must be unambiguous.
3. **Single `target` only.** The schema's `target` is one string; statesman does
   not synthesize SCXML multi-target. Fan-out to multiple states happens only via
   parallel/`initial` entry closure (`transition-algorithm.md` §7.1).
4. **Event descriptors: exact or `*` only** (§6). Partial/namespace wildcards
   (`error.*`) are a load error in v1 — be explicit.
5. **History `type` ⇒ parent must be compound or parallel**, and `target` (its
   default) must resolve to a sibling (§8).
6. **`id`, if present, must equal the state's key.** Two names of record is a
   load error.
7. **No `internal`/`reenter`** exists in the schema, so the internal-vs-external
   distinction is derived from `target` presence, not configured
   (`transition-algorithm.md` §6.1). Nothing to validate; documented so reviewers
   know it is intentional, not missing.

## 6. Event descriptor matching

`on` keys and the synthetic keys the engine generates:

- **Exact** `EventType()` string equality — the common case.
- **Generated keys** (not user-authored as events, but valid `on` keys):
  `done.invoke.<id>`, `error.invoke.<id>` (supervision, D11/D18),
  `done.state.<id>` (final bubbling), and `after`'s `xstate.after(<delay>)#<id>`.
- **`*`** — catch-all, lowest priority **within a node**: selected only if no
  exact/generated descriptor at that node matches the event
  (`transition-algorithm.md` §5). One `*` entry per node.
- Partial wildcards (`a.b.*`) — **rejected** in v1 (§5.4).

## 7. Delay descriptors (`after` keys)

A delay key is one of:

- **Integer milliseconds** — `"30000"` ⇒ `30 * time.Millisecond * 1000`. Pure
  literal, no Go symbol needed.
- **Symbolic name** — `"RetryDelay"` ⇒ resolves to a `time.Duration` const in
  `delays.go` by convention (architecture authoring; stub emits
  `const RetryDelay = 0 // TODO`). Unresolved ⇒ `CodegenError` (gate 3).

Either way the delay is `Clock`-timed and durable in `PendingAfter` (D52).

## 8. Target resolution

A transition/history `target` string resolves to exactly one state node:

- `#<id>` — absolute, by a state's `id`/key anywhere in the tree.
- `.<child>[.<grandchild>…]` — relative descendant of the transition's `source`.
- `<sibling>` — a sibling under the same parent as `source`.

Unresolvable, ambiguous, or out-of-tree targets are load errors. (This is the
xstate target grammar reduced to the three forms v1 accepts; richer forms are a
load error, not a silent guess.)

## 9. Reconciliations (consequences of a structural-only schema)

- **No `context` in the schema.** Context shape and initial values are **Go-only**
  — `ContextFields` + the constructor (architecture authoring). There is no JSON
  initial context, so stub type-inference applies to action/guard `params`
  literals only (§4), never to context.
- **No `output`/donedata on `final`.** `done.invoke.<id>` payloads come from the
  Go actor (promise return / observable value / `fromMachine` final `Context`);
  `done.state.<id>` carries no payload in v1 (`transition-algorithm.md` §7.5).
- **No initial-transition actions.** `initial` is a bare string (§3), so the
  algorithm's initial-transition action hook is always empty in v1.

## 10. Conformance fixtures

`internal/schema/testdata/` holds the vendored schema and `machine.json`
fixtures, one per subset feature plus rejection cases:

- `order.json` — the worked example (`transition-algorithm.md` §11): compound
  `processing`, `invoke` with `onError`/`onDone`, `after`, guards. Must pass all
  three gates.
- Rejection fixtures (must fail gate 2 with the cited rule): `no-invoke-id.json`
  (rule 1), `compound-no-initial.json` (rule 2), `partial-wildcard.json` (rule 4),
  `bad-target.json` (§8).

The loader's table tests (Phase 1) assert each fixture's gate outcome.
