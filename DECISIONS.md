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

- **Machine files are machine-named and dot-separated** — 2026-06-13 — m+git@andri.dk — A package holds `<id>.machine.json` (definition), `<id>.machine.gen.go` (generated facade — the `.gen.` token plus the `// Code generated … DO NOT EDIT.` header mark it untouchable), and the authored `<id>.events.go` / `<id>.behavior.go` / `<id>.actors.go` / `<id>.delays.go`. Replaces the generic `machine.json` / `machine_gen.go` / `types.go` / `impl.go` / `actors.go` / `delays.go`. Dot over snake_case because Go reads trailing `_test.go` / `_<GOOS>` / `_<GOARCH>` filename suffixes as build constraints; dots are inert (cf. `*.pb.go`). `actors`/`delays` keep their domain names (ubiquitous language); only the vague `types`/`impl` were renamed.

- **Machine identity is the filename, not the directory** — 2026-06-13 — m+git@andri.dk — Dropped the `id == package/dirname` convention (it was documented but never enforced in code; the prefix already comes from the JSON `id`). `loadDef` globs the single `*.machine.json` per package and requires its filename prefix to equal the machine `id`. One machine per package still holds — the unprefixed package-scoped singletons (`Context`/`Event`/`States`/`ActionResult`/`Implementations`) are what forbids two — so machines can now live in any directory the app developer chooses.

- **`statesman stub` emits the Impl behavior skeleton** — 2026-06-13 — m+git@andri.dk — Closes the D51 gap (the skeleton was documented but unimplemented). `stub` now appends `type Impl struct{}` plus one panicking method per action/guard/invoke-input callsite to `<id>.behavior.go`, sharing the signature computation with the generated `Implementations` interface. Strictly additive: it skips methods a present `Impl` already defines, so it converges across re-runs as the machine grows.

- **`statesman diagram` renders machines as Mermaid or a terminal tree** — 2026-06-13 — m+git@andri.dk — A fourth CLI verb plus a public `statesman/diagram` package render a `Definition` to a Mermaid `stateDiagram-v2` string (docs) or a Unicode/ANSI outline (terminal), from one tree walk. No image rendering: turning Mermaid into pixels is left to whatever already renders it (an editor preview, GitHub, or a native renderer like `mmdr`), so statesman keeps zero render dependencies. `diagram --watch` re-renders on the `machine.json`'s mtime (zero-dep polling, immune to atomic-rename saves; keeps the last valid diagram on screen when an in-progress edit fails to parse). `diagram.Live` overlays a running machine's active states/status/version/armed timers via `Subscribe`; it shows only that machine's own configuration (a child actor's internals aren't in the parent snapshot). Added `Machine.Definition()` so Live can read the immutable tree.

- **Loader accepts a root `$schema` meta-keyword** — 2026-06-13 — m+git@andri.dk — `schema.Load` whitelists `$schema` at the machine root (loaded-but-ignored, like `version`), so a verbatim Stately Studio export — which emits `$schema` for editor field-suggestion and validation — round-trips. It is not a `machineSchema.json` property (the schema is `additionalProperties:false`), but it is the JSON Schema dialect meta-keyword, not machine data. Rejected anywhere but the root.
