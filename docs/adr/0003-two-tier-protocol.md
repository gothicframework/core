# ADR 0003 — Two-tier protocol: binary data-plane, JSON/string control-plane

- Status: Accepted
- Date: 2026-07-08
- Scope: Gothic v3.0.0 WASM re-architecture, Part II
- Related: [0001](0001-custom-codec-not-protobuf.md), [0002](0002-schema-seam-generic-interpreter-deferred.md), [0004](0004-static-full-go-core.md)

## Context

The topic system carries two very different kinds of traffic:

- **Data-plane:** reactive per-field state broadcasts. Every `Set` on a topic
  field pushes that field's value to every subscribed component. This is
  high-frequency and can be high-volume — the stress cases exercise ~5 MB
  payloads and ~150k-item collections.
- **Control-plane:** lifecycle and coordination messages — register / unregister
  a component, ping the manager, online acknowledgements, config. These are
  low-frequency, small, and human-debuggable.

WASM32 linear memory has a hard 4 GB ceiling and grows monotonically (no
`memory.shrink`; the GC reclaims objects but never returns pages — see the
runtime-internals skill). JSON-scale encoding of the data-plane is fatal here:
the intermediate strings, the number-to-text expansion, and the parse buffers
ratchet linear memory upward until TinyGo hits `RuntimeError: unreachable` and
the instance is unrecoverable. The binary codec (ADR 0001) exists precisely to
keep those payloads compact and allocation-lean.

The temptation, recurring in every "let's simplify" discussion, is to make the
data-plane JSON too, "so it's all one format." That reintroduces the heap crash.

## Decision

Split the protocol into two tiers, permanently:

- **Data-plane stays BINARY.** Per-field reactive broadcasts are encoded with the
  custom codec and routed **opaquely by key** through the topic hub (Phase 17) —
  the hub moves frames by their key without decoding them. The Phase-16 core and
  the WireVersion byte (Phase 15) sit under this path. The direct-memory
  `dispatchDirect` transport keeps even the JS bridge crossing allocation-free.
- **Control-plane is JSON / plain strings.** register, unregister, ping, online,
  and config are small, infrequent, and encoded as JSON or plain strings for
  debuggability. Their cost is negligible and their human-readability is worth
  more than shaving bytes.

This split is a **standing rule**, recorded here specifically so that a future
"simplify everything to JSON" change cannot quietly convert the data-plane and
reintroduce the WASM32 heap-exhaustion crash. Any proposal to JSON-ify per-field
broadcasts must be rejected at review with a pointer to this ADR.

## Consequences

- The data-plane survives the 5 MB / 150k-item stress cases because payloads stay
  binary and are routed without a decode step; linear memory does not balloon.
- Two encodings coexist. That is deliberate, not accidental debt: each tier uses
  the representation matched to its volume and its debugging needs.
- The topic hub must remain **format-agnostic on the data-plane** — it routes by
  key and never parses field frames. Keeping it opaque is what lets the binary
  rule hold and is a prerequisite for the deferred schema interpreter (ADR 0002)
  to be an *opt-in* overlay rather than a hub responsibility.
- Control-plane messages may evolve freely (JSON is forgiving); data-plane
  evolution goes through the WireVersion byte and the codec tests (ADR 0001).
