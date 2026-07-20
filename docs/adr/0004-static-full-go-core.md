# ADR 0004 — Static full-Go core, NOT a shared runtime other modules link against

- Status: Accepted
- Date: 2026-07-08
- Scope: Gothic v3.0.0 WASM re-architecture, Part II
- Related: [0001](0001-custom-codec-not-protobuf.md), [0002](0002-schema-seam-generic-interpreter-deferred.md), [0003](0003-two-tier-protocol.md)

## Context

Gothic pages can host several WASM components. A natural instinct is to factor
the shared services (the topic hub, the durable cache, stdlib-heavy helpers) into
a **shared runtime** that every component module dynamically links against — one
copy of the code, linked once, called by many. That is how native shared
libraries work, and it would in principle dedup the runtime bytes across
components.

On the WASM/TinyGo target this is infeasible:

- **Each instance owns its own linear memory.** Two WASM instances do not share
  an address space. A pointer produced in instance A is meaningless in instance
  B. There is no shared heap for a common runtime to operate over.
- **No dynamic-linking / shared-table ABI.** The toolchain offers no stable
  cross-module dynamic-linking ABI (shared function tables, shared globals) that
  we could target to make many components call one linked-in runtime.
- **Cross-module interaction is scalar calls + shared-JS-memory reads only.**
  What instances *can* do is call each other through JS with scalar arguments and
  read bytes out of another instance's `exports.memory.buffer` from JS. That is
  exactly the mechanism the topic bus already uses (`dispatchDirect` +
  `__gothic_topic.set`, per the runtime-internals skill). It is a message bus,
  not a linker.
- **The only runtime-byte dedup available is bundling** all components into one
  module — which defeats per-component code-splitting and lazy loading, the very
  thing `StatefulComponentOf` buys us.

## Decision

Do **not** build a shared runtime that component modules link against. Instead:

- The **core is a separate, full-Go module** (compiled with the standard
  `GOOS=js GOARCH=wasm` Go compiler, not TinyGo) that hosts the stdlib-heavy
  services: the **topic hub**, the **durable page-session cache**, and other
  services that want the full standard library.
- **Components stay thin TinyGo modules** and **RPC into** the core over the
  JS/scalar/shared-memory bus. They do not link the core; they message it.
- Runtime-byte "dedup" is achieved by *centralizing services in the one core
  instance*, not by sharing linked code across component instances.

## Consequences

- Components stay small (TinyGo) and independently code-split / lazy-loaded; the
  heavy stdlib cost is paid once, in the single full-Go core, not per component.
- The core can use the full Go standard library (maps, time, crypto, encoding,
  goroutines) without TinyGo's stdlib gaps — appropriate for a hub/cache that is
  not on the tight per-component size budget.
- The interaction surface between components and core is the **message bus**, so
  it inherits the two-tier protocol (ADR 0003): binary per-field frames on the
  data-plane, JSON/string on the control-plane. The core routes data-plane frames
  opaquely by key.
- We accept that identical helper code may be duplicated between a component and
  the core (no shared linkage). This is the unavoidable cost of the WASM
  isolation model; trying to eliminate it via bundling would cost us
  code-splitting, which is the worse trade.
- The durable cache lives in the core precisely because it must **outlive** any
  single component's teardown→re-mount — a shared linked library could not
  provide that lifetime guarantee across independent instances, but a distinct
  long-lived core instance can.

---

These four ADRs underpin the Part II WASM re-architecture: the **WireVersion
byte** (ADR 0001/0003), the **schema seam** (ADR 0002), the **topic hub
consolidation** (ADR 0003/0004), and the **durable page-session cache** (ADR
0004). The codec's reliability — the reason we can own the wire format instead
of adopting a serializer — is locked by the round-trip and fuzz tests
(`codec_roundtrip_test.go`, `codec_fuzz_test.go`), per ADR 0001.
