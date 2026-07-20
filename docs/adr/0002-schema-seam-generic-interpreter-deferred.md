# ADR 0002 — Schema seam reserved; generic interpreter DEFERRED

- Status: Accepted
- Date: 2026-07-08
- Scope: Gothic v3.0.0 WASM re-architecture, Part II
- Related: [0001](0001-custom-codec-not-protobuf.md), [0003](0003-two-tier-protocol.md), [0004](0004-static-full-go-core.md)

## Context

Gothic introduced a **schema seam** alongside the WireVersion byte: for each
topic struct the codegen produces a schema descriptor literal and a `schemaId`
content hash, and the generated topic constructors carry a registration slot for
them (`SchemaID` / `SchemaDescriptorLit` in `buildWasmTopicFuncData` /
`buildServerTopicFuncData`, `wasm_codec.go`). The obvious next step would be a
**generic interpreter**: a runtime that reads the descriptor and can
encode/decode *any* topic value without per-type generated code — enabling
generic tooling (inspectors, bridges, logging) over arbitrary topics.

Building that interpreter now is tempting but premature. It is real complexity
with no first consumer, and it carries sharp edges that only bite once something
actually interprets the schema.

## Decision

Reserve the seam; **defer** the interpreter. In v3.0:

- The schema descriptor, the `schemaId` content hash, and the registration slot
  are **generated and carried**, but **nothing interprets them**. They exist so
  that (a) version/schema skew is *detectable* and (b) a future interpreter has a
  stable attachment point that does not require re-generating existing topics.
- The **unlock condition** for building the interpreter is a *real first
  consumer*. The recommended first consumer is a **dev-mode topic-state
  inspector**: it has genuine DX value, it exercises the interpreter for real
  (not a toy), and — being dev-only — its version-skew and data-exposure risks
  are negligible. We do not build the interpreter speculatively; we build it when
  that consumer is greenlit.

The six sharp edges the interpreter must handle, each with its mitigation, are
recorded now so the deferral is honest:

1. **Schema / version skew on hot reload.** A reloaded manager and a still-live
   consumer may disagree on layout. Mitigation: the `schemaId` **content hash** —
   mismatched schemas are detectable before decode.
2. **Encoder ↔ schema drift.** The generated encoder and the descriptor could
   diverge. Mitigation: the round-trip test from ADR 0001 (extended to assert
   the descriptor matches the encoded layout when the interpreter lands).
3. **Interpreter performance.** A naive reflective interpreter is slow.
   Mitigation: byte-level schema-driven *skip* (advance without materializing)
   plus lazy JSON only for fields actually inspected.
4. **Internal generic value is not JSON.** The interpreter's in-memory generic
   representation must not be conflated with a JSON projection; JSON is an
   on-demand *view*, produced lazily, never the data-plane format (see ADR 0003).
5. **Security.** Interpreting a topic must be **opt-in per topic** — a topic is
   only exposable to generic tooling when its owner marks it so. No blanket
   reflection over all topic state.
6. **Schema expressiveness.** The descriptor format must be able to express the
   full wire surface — maps and nested slices included — or the interpreter will
   silently mis-handle the very structs ADR 0001's tests cover.

## Consequences

- v3.0 ships the seam as *inert metadata*: small constant cost in generated code,
  zero runtime behavior, no interpreter attack surface.
- Skew is detectable today (via `schemaId`) even though it is not yet *acted on*.
- When the dev-mode inspector is greenlit, the interpreter can be added without a
  wire break or re-generation of existing topics, because the attachment point is
  already reserved.
- Until then, generic cross-topic tooling is intentionally impossible — the only
  way to touch topic bytes is the per-type generated codec. This is a feature: it
  keeps the v3.0 surface small and the security story trivial.
