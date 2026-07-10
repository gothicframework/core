# Inspirations & Prior Art

The Gothic Framework v3.0.0 re-architecture — the OpenTofu migration (Part I) and the WASM runtime re-work (Part II) — did not appear from nowhere. Several established systems shaped its design. This document credits each one and, just as importantly, records **what we deliberately did *not* take**. Borrowing a *model* is not the same as adopting a *dependency*; several of these influenced our design precisely because we chose to reimplement a small, purpose-built slice of them rather than pull in the whole runtime.

The load-bearing choices are captured as Architecture Decision Records under [`docs/adr/`](./adr/):

- [ADR 0001 — Custom codec, not Protocol Buffers](./adr/0001-custom-codec-not-protobuf.md)
- [ADR 0002 — Schema seam, generic interpreter deferred](./adr/0002-schema-seam-generic-interpreter-deferred.md)
- [ADR 0003 — Two-tier protocol](./adr/0003-two-tier-protocol.md)
- [ADR 0004 — Static full-Go core](./adr/0004-static-full-go-core.md)

---

## Apache Avro

**What we borrowed:** the core Avro insight that the *data* on the wire can be **tagless** — carrying no field names or type tags — as long as a **separate schema** describes how to read it. Gothic's per-field binary codec is exactly this: a little-endian frame with no self-description, paired with a canonical wire descriptor derived from the struct at build time. That descriptor + its content-hash `schemaId` is the **schema seam** (`GothicRegisterSchema`, deposited on `window.__gothicSchemas`), reserved for a future generic interpreter.

**What we deliberately did *not* take:** the Avro *runtime* — its object container files, its JSON schema language, its resolution rules, and the reference libraries. We took the tagless-data + external-schema *model*, not the machinery. In 3.0 the descriptor is INERT (written once, never read back); we adopted the idea of separating data from schema without importing Avro's implementation of it.

## Protocol Buffers

**What we borrowed:** Protobuf is our reference for what a **descriptor-driven dynamic decoder** looks like — varint/zigzag integer encoding and a descriptor that lets a generic reader decode a message it wasn't compiled against. It is the model we study for the **deferred generic wire interpreter** the schema seam reserves space for.

**What we deliberately did *not* take:** Protobuf as a **dependency**. We did not adopt `protoc`, `.proto` files, generated Go message types, or the varint format itself — Gothic keeps its own fixed-width little-endian codec, which is simpler to generate from the AST and cheaper to encode in TinyGo. The generic interpreter is deferred (YAGNI in 3.0), and if it ships it will be a Gothic-native descriptor reader, not a Protobuf runtime. See [ADR 0001](./adr/0001-custom-codec-not-protobuf.md) for the full "custom codec, not Protobuf" decision.

## Schema fingerprinting (Avro fingerprints / content hashing)

**What we borrowed:** the practice of **content-hashing a schema to a stable fingerprint** so two endpoints can cheaply detect whether they agree on a wire shape. Gothic's `schemaId` is a crc32 (IEEE) content hash of the canonical descriptor — stable for an unchanged type, and it changes the instant any field name, order, wire type, or `gothic:`-tag width changes. The same content-hashing discipline drives the `?v=` cache-busters on the shared public assets (`gothic-core.js` and the static-core artifacts).

**What we deliberately did *not* take:** Avro's specific fingerprint algorithm (Rabin/CRC-64-AVRO) and its schema-registry infrastructure. We use a plain crc32 over our own descriptor format and keep the fingerprint local — its only 3.0 job is hot-reload **skew safety** (an old cached component meeting a new core can be detected), not distributed schema governance.

## Hexagonal Architecture (Ports & Adapters)

**What we borrowed:** the ports-and-adapters discipline of isolating a stable core behind swappable seams. In **Part I**, the deploy pipeline is structured around interfaces — `DeploymentEngine`, `CDNEngine`, `BinaryManager` — so OpenTofu, CloudFront, and the tofu-binary lifecycle are adapters behind ports rather than hard-wired call sites. In **Part II**, the runtime splits along the same line: **thin TinyGo clients** (per-component, small, hot-reloaded) talk to a **full-Go services hub** (the static core) across a well-defined register/ack boundary — the client is the adapter, the core is the port-backed service.

**What we deliberately did *not* take:** dogmatic layering everywhere. We applied ports at the boundaries that actually swap or need isolation (infra engines, the client/core seam) and left the rest direct — no interface-per-struct ceremony. See [ADR 0004](./adr/0004-static-full-go-core.md) for why the core is a single static full-Go service rather than many per-topic binaries.

## The web platform — `MutationObserver`

**What we borrowed:** the browser's own `MutationObserver` is the backbone of the **per-scope teardown lifecycle**. Rather than invent a framework-specific unmount signal, we lean on the platform: one global observer on `document.body` watches for removed `[data-gothic-scope]` nodes and drives `__gothicTeardown`, which fires `OnUnmount`, strips listeners, and halts the instance. The lifecycle *is* the DOM's own removal event.

**What we deliberately did *not* take:** a virtual DOM or a bespoke component-lifecycle framework to know when something unmounts. There is no diffing layer and no synthetic lifecycle — the real DOM removal is the trigger, which is what keeps the teardown honest for HTMX swaps, `hx-boost` navigation, and multiplexed row re-renders alike.

## The pre-existing "manager WASM as single source of truth" topic model

**What we borrowed:** Gothic v2 already had the right *shape* for cross-component state — a manager WASM that owned the canonical encoded state for a topic key, diffed incoming writes byte-for-byte, and broadcast only changed fields to consumers. Phase 17 **generalized** that proven model: instead of one manager binary *per topic*, the single static core became the one generic, opaque store-and-forward hub for **all** keys, with the same per-field diff, per-field broadcast, replay-on-join, and online-ack behavior.

**What we deliberately did *not* take:** the per-topic binary itself. Consolidating N manager binaries into one core removed N WASM loads per page and collapsed the N-instance leak surface — while keeping the exact user-facing topic API (`CreateTopic` / `PageTopic()` / `AddPageTopic()`) unchanged. The model was already correct; we kept the behavior and retired the duplication.

---

*Credit where due: these systems informed Gothic's design. Any oversimplification of them here is ours, not theirs — consult each project's own documentation for an authoritative description.*
