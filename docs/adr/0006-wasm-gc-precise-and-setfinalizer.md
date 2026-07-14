# ADR 0006 — WASM GC switched to `-gc precise` (and the TinyGo `SetFinalizer` PR)

- Status: Accepted
- Date: 2026-07-13
- Scope: Gothic v3 WASM build recipe — TinyGo garbage collector selection
- Related: [0005](0005-typed-fetch-and-htmx-wasm.md) (Typed Fetch + HTMX, whose
  `Decode`/`MapAny` exposed the leak); Gothic ADR sequence 0001–0004

## Context

Every Gothic page compiles its `ClientSideState` to a TinyGo WebAssembly module.
The build recipe lives in one place — `tinygoWasmFlags` in
`cli/internal/build/wasm_build.go` — and is folded into `buildRecipeFingerprint()`
so any flag change automatically invalidates the per-page WASM cache.

Two facts about TinyGo's garbage collector set the stage:

- **The GC decides what is alive by scanning memory for pointers.** The only hard
  question a GC answers is *"is this machine word a pointer (an address of a live
  object) or just a number that happens to look like one?"*
- TinyGo offers several collectors via `-gc`:
  - `leaking` — never frees (fine only for very short-lived programs);
  - `conservative` — a real mark/sweep that **treats any word that *looks* like a
    heap pointer as one** (no compiler support needed, works everywhere);
  - `precise` — a mark/sweep that reads **pointer maps emitted by the compiler**,
    so it scans only *real* pointers.

**History (see also decision D-II.4 in the internal KB).** Gothic originally ran
on the TinyGo default `leaking` GC, which caused a heap-leak regression on the
core/manager WASM. The fix at the time was to switch to **`-gc conservative`** — a
collector that actually frees memory and works on every target. `precise` was not
evaluated then; the decision was simply "stop leaking with `leaking`."

**The regression this ADR responds to.** The Typed Fetch feature (ADR 0005) added
`Decode[T]` and `Response.MapAny()`, which build a large, transient
`map[string]any` tree when decoding a big JSON payload — full of integers (ids,
counts, sizes). Many of those integers, as raw bits, *look like valid heap
addresses*. Under `-gc conservative` the scanner therefore **false-pointer-pins**
that transient garbage: it never gets collected. Decoding a large payload
repeatedly grew WASM linear memory **~8–16 MB per decode with no upper bound**,
reaching >1.5 GB in ~100 decodes — a straight line to the WASM32 4 GB ceiling and
a `RuntimeError`. This was proven by `stress-fetch-largepayload.spec.ts`.

The leak is *not* in the JS bridge (the `_values` table, js.Func/Uint8Array counts
were all flat) — it is Go-heap retention caused specifically by conservative
scanning of integer-heavy data. It is inherent to the new large-decode workload,
which the framework did not have before this feature.

## Decision

Set the TinyGo build recipe to **`-gc precise`** (was `conservative`) in
`cli/internal/build/wasm_build.go`. This is framework-wide: every page's WASM is
built with precise GC.

The single-source `tinygoWasmFlags` var + `buildRecipeFingerprint()` machinery
(from the earlier conservative decision) is unchanged and is exactly what makes
the one-word flip auto-invalidate every cached binary — no stale WASM survives the
change.

**Related, but NOT shipped here:** a TinyGo PR that implements
`runtime.SetFinalizer` for the block GC
([tinygo-org/tinygo#5521](https://github.com/tinygo-org/tinygo/pull/5521),
*"runtime: implement SetFinalizer to fix syscall/js finalizeRef leak"*). It fixes
a **different** leak — boxed `js.Value` ref-table slots that never recycle because
stock TinyGo's `SetFinalizer` is a no-op, so `syscall/js.finalizeRef` never fires.
Gothic uses stock TinyGo, so that leak is still open today. We adopt precise GC
now (which we control via the build flag) and track the PR for the future.

## Rationale

- **Precise fixes the actual bug.** With pointer maps, the integer fields in the
  decoded `map[string]any` are known to be *data*, not pointers, so the transient
  tree is collected. Heap plateaus (~1.6 MB/decode late-slope in the stress test)
  instead of growing without bound.
- **No safety regression, validated empirically.** The theoretical risk of precise
  is the opposite of conservative's: conservative over-retains (safe but leaky);
  precise relies on the pointer maps being correct, and a wrong map could free a
  live object (crash). We validated against the entire release gate — **193 pass /
  4 skip / 0 fail**, including every existing leak/heap/codec/topic stress spec —
  with zero regressions. TinyGo's `syscall/js` bridge does not depend on
  conservative scanning (it keeps `js.Value` refs in a side table, not as raw heap
  pointers), so nothing in the runtime relies on conservative behavior.
- **It is the direction the ecosystem is moving,** and the precondition for PR
  #5521: that PR's own code documents that `SetFinalizer` only fires
  *deterministically* under the precise wasm GC (conservative stack scanning cannot
  reliably collect the dropped object), and its finalizer table stores object
  addresses **bitwise-NOT-encoded** *specifically to hide them from the conservative
  scanner* — the same false-pointer pathology this ADR fixes. Being on precise now
  means that when #5521 lands in a stock TinyGo release and Gothic bumps its pinned
  version, the finalizer fix works out of the box.
- **Cost is negligible.** Precise adds compiler-emitted pointer/stack maps —
  measured at **~1% larger raw `.wasm`** and **~0 after brotli** (what the browser
  actually downloads). Stress throughput was equal or *higher*
  (`wasm-fullstress` 50→582 ops/30s).

We considered leaving conservative and rewriting `Decode`/`MapAny` to stream JSON
straight into the target struct without an intermediate `map[string]any` (removing
the garbage entirely). Precise was chosen first because it is a one-line,
framework-wide fix with zero regressions and additional forward benefits; the
streaming rewrite remains available as a future optimization if ever needed.

## Consequences

- **Framework-wide, no API change.** Every page's WASM now builds with
  `-gc precise`. Users only need to rebuild. A single large decode was always fine;
  repeated large decodes no longer leak.
- **The build cache self-invalidated** on the flag change via
  `buildRecipeFingerprint()`; no manual cache clear was required.
- **Neither GC returns linear memory to the OS** (the WASM32 heap does not shrink);
  precise changes the *high-water* from "grows without bound" to "plateaus."
- **Open item — the `js.Value` / `finalizeRef` leak (Leak B).** Not fixed by
  precise alone; it needs `SetFinalizer` (PR #5521), which stock TinyGo lacks. When
  the PR merges into a stock TinyGo release, bump the CLI's pinned TinyGo version
  (`WasmHelper.Version`) to pick it up. Pointing `WasmBinary` at a local fork is
  **not** sufficient: the CLI pins `TINYGOROOT` to the managed stock toolchain, so
  a fork binary runs against stock runtime sources and fails. A future option is a
  toolchain-root override in `gothic.config.go` for teams that need Leak B closed
  before the merge.
- **Docs/KB updated:** the build-recipe reference (`09_build_pipeline.md`), the
  decision log (`16_adrs_and_decisions.md`, decision **D-TF.1** supersedes D-II.4),
  and this ADR now state `-gc precise` and the reason.
