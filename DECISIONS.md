# Decisions

Concise log of load-bearing decisions. The full numbered design table (D1–D53)
lives in `statesman-architecture.md` (Decisions Snapshot); this file records the
v1-defining choices and the deviations made while building it. Format: decision —
date — author — summary.

- **machine.json is the only source of truth** — 2026-06-13 — m+git@andri.dk — Machines are authored as a Stately/SCXML-subset `machine.json`; the loader (`schema.Load`) is the single parser, and codegen reads the same JSON. No Go DSL for structure.

- **Schema is structural-only; context and output are Go-side** — 2026-06-13 — m+git@andri.dk — The subset has no `context`/`output`/`internal`/`reenter` fields (they aren't in Stately's `machineSchema.json`). Context is a Go type (`TCtx`); `done.invoke` payloads come from Go actor return types. Internal-vs-external transition is derived from target presence: targetless ⇒ internal (action-only), any target ⇒ external.

- **Stricter-than-schema validation gates** — 2026-06-13 — m+git@andri.dk — `schema.Load` enforces rules the JSON Schema can't: `initial` required on compound states, `invoke.id` required, single `target` only, exact-or-`*` event descriptors (partial wildcards rejected), and a `#id`/`.child`/`sibling` target grammar. Document order is preserved for deterministic callsite numbering.

- **Deferred invoke spawn** — 2026-06-13 — m+git@andri.dk — Invoked actors spawn at macrostep quiescence for states still active, not on entry. A state entered and exited within one macrostep never fires its real side effect (e.g. an HTTP `chargeCard`).

- **Opaque applier/guard closures are the core↔codegen seam** — 2026-06-13 — m+git@andri.dk — The engine takes `applyAction func(int, TCtx, TEvt) AppliedEffect[TCtx]` and `evalGuard func(int, TCtx, TEvt) bool` keyed by callsite index (D36). Generated code supplies the dispatch tables; the runtime stays free of reflection and of any user types.

- **Strong subscriber delivery + StallObserver** — 2026-06-13 — m+git@andri.dk — Subscribers get a bounded queue with blocking on overflow (D25): a slow subscriber stalls the actor, by design, for audit semantics. `StallObserver` wraps a load-bearing transition observer and reports (via a watchdog timer) when it holds the actor past a threshold.

- **Panic policy: let it crash** — 2026-06-13 — m+git@andri.dk — No `recover()` in the actor loop (D39). Action/guard panics are programming errors; crash isolation is a distribution concern. Documented in generated godoc.

- **Context immutability is a convention** — 2026-06-13 — m+git@andri.dk — Go can't enforce value immutability (D41); the `ctx` parameter in action/guard methods is read-only and assigns return a new value via `ActionResult`. Mutating in place corrupts the published snapshot and breaks rollback.

- **`schema` package is public** — 2026-06-13 — m+git@andri.dk — Moved from `internal/schema` so generated code can call `schema.Load` directly via the embedded `machine.json`.

- **Invoke input via Impl mapper method** — 2026-06-13 — m+git@andri.dk — Stately JSON has no per-invoke `input` (it lives in the TS config as a function). Generated code exposes a typed `Impl` mapper method per input-taking invoke rather than modelling a static-value form.

- **Durable layer deferred to post-v1 roadmap** — 2026-06-13 — m+git@andri.dk — The persistence contract (`docs/persistence-contract.md`) and the `TransitionObserver` seam are designed and in place, but the SnapshotStore/EventLog/recovery implementation (Phase 6) ships after v1. v1 is in-memory.

- **Parity via a curated txtar corpus** — 2026-06-13 — m+git@andri.dk — Behavioral coverage breadth comes from data-driven `*.txtar` scenarios (`statesmantest.RunScenarios`), not a generated xstate harness. Each fixture is a `machine.json` + a send/advance/expect script run on virtual time.

- **`go test -race` is a non-negotiable CI gate** — 2026-06-13 — m+git@andri.dk — The whole runtime and its tests must pass `-race` from the first test (D31/D45).

- **No third-party runtime dependencies** — 2026-06-13 — m+git@andri.dk — The runtime and loader are stdlib-only. `golang.org/x/tools` is a codegen-only dependency (used by `cmd/statesman` and `statesmantest`'s scenario runner); it never enters a generated machine's runtime path.

- **Defaults confirmed from benchmarks** — 2026-06-13 — m+git@andri.dk — On an Apple M3, a synchronous send→settle round trip is ~3.1µs (63 allocs); `Snapshot()` is lock-free (~0.3ns, 0 allocs). Defaults stand: mailbox cap 256 (~0.8ms buffered work), subscriber buffer 16, always-loop limit 100.
