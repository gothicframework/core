# ADR 0005 — Typed Fetch + HTMX tooling in the TinyGo WASM component

- Status: Accepted
- Date: 2026-07-13
- Scope: Gothic v3.0.0 WASM client tooling — Typed Fetch + HTMX (Phase 8–11)
- Related: [0001](0001-custom-codec-not-protobuf.md), [0002](0002-schema-seam-generic-interpreter-deferred.md), [0004](0004-static-full-go-core.md)

## Context

A Gothic page hosts thin TinyGo WASM components whose interactivity is written in
plain Go inside a route's `ClientSideState`. Two gaps limited what that Go could
do against the network:

- **`Fetch` returned only a raw string.** A component could issue a request but
  got back `(string, error)` — no status code, no response headers, no typed
  body. Anything structured (status-code branching, content-type checks, JSON)
  had to be reconstructed by hand from the string, and there was a separate
  `FetchBytes` for the binary case.
- **TinyGo cannot give you typed JSON the normal way.** `encoding/json` +
  `reflect` are exactly the reflection-heavy machinery TinyGo either omits or
  discourages (the same reason the topic codec is codegen'd, not reflected — see
  ADR 0001). So a component could not simply `json.Unmarshal` a response into a
  struct: on the client that path is partial, bloats the binary, and is fragile.

Meanwhile Gothic is an **htmx-first framework**: the server renders HTML, the
browser swaps fragments. Components frequently need to *drive* htmx from Go —
issue an ajax request, swap trusted HTML, add a class, read a form's values,
listen for `htmx:afterSwap` — but there was no Go surface for htmx from inside
`ClientSideState`; you dropped to raw `js.Value` calls.

The unifying question: how does a TinyGo component get **structured** results
(status, headers, typed structs) from the network, and **drive htmx** — without
dragging reflection or `net/http` into the WASM binary, and without a core round
trip?

## Decision

Ship **two complementary client tooling surfaces** in `core/wasm`, dot-imported
so `ClientSideState` calls them unqualified.

**1. A structured Fetch / Response API.**

- `Fetch(url, cfg ...FetchConfig) (Response, error)` — blocking to the component
  (asyncify), non-blocking to the browser. `Response` carries `Status`,
  `Headers`, and raw `Body`, with `.Text()`, `.Bytes()`, `.OK()` (200–299) and
  `.MapAny()` (a reflection-free untyped JSON read, object roots only).
- Async variants over the same request: `FetchAsync(url, cfg, done)` (callback,
  no goroutine) and `FetchChan(url, cfg...) <-chan FetchResult` (channel /
  fan-out).
- **Per-scope cancellation is automatic.** Each component scope owns an
  `AbortController`; on component teardown its in-flight requests are aborted.
  No user action is required — a component that unmounts mid-request does not
  leak a pending fetch.

**2. Typed JSON by CLI codegen, not reflection.**

- `Decode[T any](r Response) (T, error)` and `Encode[T any](v T) []byte`. The
  gothic CLI reads `T` via `go/types` at WASM build time and emits a
  reflection-free `_jsonDecode_T` / `_jsonEncode_T` — straight-line code that
  matches fields by `json:` tag. This is the same "the struct is the source of
  truth, generate the codec from it" strategy as the topic codec (ADR 0001),
  applied to JSON.
- Because the codegen rewrite is **syntactic**, the type argument must be
  **explicit** (`Decode[shared.EchoStruct](resp)`, never inferred) and `T` must
  be a struct.

**3. The own-BFF-JSON + shared-struct pattern.** Your API route emits JSON; the
WASM client decodes it into the **same** struct the server marshalled from — one
DTO, no drift. The DTO must live in a **`net/http`-free leaf package** (the
reference app uses `src/shared`), imported by *both* the server handler and the
WASM page — see Consequences for why this is load-bearing.

**4. A Go mirror of htmx 2.0.3.** A dot-imported `var HTMX` exposes the htmx
surface to Go: `Ajax`, `Swap` (runs scripts + `htmx.process` so nested Gothic
stateful components boot after the swap), `Process`, `Trigger`, class helpers,
`Find`/`FindAll`/`Closest`, `Values`, `Remove`, and event listeners `On`
(subtree- and lifetime-scoped, leak-safe) / `OnGlobal` / `Off`. Swap strategies
are bare consts (`InnerHTML`, `OuterHTML`, …); the full htmx event catalog ships
as `Evt`-prefixed `HtmxEvent` consts, with `HtmxEvent("htmx:whatever")` for
custom events.

**5. Switch the framework WASM GC to `-gc=precise`.** See Consequences.

## Rationale

**Why NOT route fetch/JSON through the core (the rejected "core-proxy").** The
tempting alternative was to send the response to the static full-Go core (ADR
0004), parse the JSON there with the full standard library, and codec the result
back to the component — reusing the core's stdlib instead of solving JSON on the
client. Rejected because it would require (a) network-fetch-in-core, (b) a new
RPC channel dedicated to fetch results, and (c) effectively the deferred generic
interpreter (ADR 0002) to turn arbitrary parsed JSON back into a typed value.
It breaks the core's opacity (the core routes data-plane frames by key; it does
not parse arbitrary user JSON), adds a round trip to every fetch, and — the
decisive point — **does not solve large-data-in-TinyGo**: a big payload still has
to land in the component's linear memory to be used. The chosen design keeps
parsing *in the component* via CLI-generated straight-line code — a smaller
surface, zero core changes, and it handles large data because the bytes never
leave the component.

**Why codegen over reflection.** Same reasoning as ADR 0001: TinyGo reflection is
partial and inflates the binary. Generated `_jsonDecode_T`/`_jsonEncode_T` is
branch-free append/read with no reflection and no schema registry in the WASM —
binary size stays a product feature.

**The render-vs-compute discriminator (how a user chooses).**

- *Rendering HTML* (the server/BFF already produced the markup) → use **HTMX**
  (`HTMX.Swap` / `HTMX.Ajax`). Let htmx put trusted HTML in the DOM and boot any
  nested components.
- *Computing on structured data* (branch on fields, sum values, build a request
  body) → use **`Fetch` + `Decode[T]`** (typed) or **`MapAny`** (untyped).
- Third-party APIs that need a secret key or lack CORS → proxy through a
  server-side **BFF** route, exactly as a React app would; the component then
  fetches your own route.

**XSS caveat.** `HTMX.Swap` runs `<script>` tags and `htmx.process` on the swapped
markup (that is precisely how nested stateful components boot). Therefore only
swap **trusted** HTML — your own templates or your own BFF — never arbitrary
third-party HTML.

## Consequences

- **Breaking: `Fetch` signature changed and `FetchBytes` was removed.** `Fetch`
  now returns `(Response, error)` instead of `(string, error)`; callers read
  `resp.Text()` for the old string and `resp.Bytes()` for the old
  `FetchBytes` result. Any pre-Phase-8 code using `Fetch`'s string return or
  `FetchBytes` must migrate.
- **DTOs used with `Decode[T]`/`Encode[T]` must live in a `net/http`-free
  package.** A cross-package `T` forces the CLI to `import T's package` into the
  generated WASM `main`, so TinyGo compiles that whole package. If it (transitively)
  imports `net/http` — e.g. an `api` package whose handlers take
  `http.ResponseWriter` — the TinyGo build **fails** (TinyGo 0.41.1 cannot compile
  `net/http/roundtrip_js.go`). Shared DTOs therefore live in a leaf package
  (`src/shared`) that is TinyGo-safe: struct decls only, no `net/http`, no
  `reflect`, no `encoding/json`.
- **Explicit type arguments are mandatory** for `Decode`/`Encode`; inferred calls
  are a build error because the CLI rewrite is syntactic.
- **Numeric precision:** JSON numbers decode through float64, so an `int64`
  greater than 2^53 loses precision (float64 mantissa limit). MapAny numbers are
  `float64` for the same reason.
- **`omitempty` is a v1 gap:** the `json:",omitempty"` option is parsed for the
  key name but omitempty *semantics* are ignored — the field is always emitted.
  nil slices/pointers/maps encode as `null`.
- **`-gc=precise` is now framework-wide.** A late stress finding: repeated
  large-payload decodes leaked memory under TinyGo `-gc=conservative` — the
  conservative collector false-pins transient `map[string]any` garbage (integer
  bit-patterns that look like pointers keep dead allocations alive), so RSS grew
  monotonically under load. Switching the framework's WASM GC mode to
  `-gc=precise` fixed the leak with **zero regressions** and higher stress
  throughput. This applies to all Gothic WASM builds, not just fetch-heavy pages.

---

This ADR extends the Part II codegen philosophy (ADR 0001 — generate the codec
from the user's Go struct; do not reflect on the client) from the topic wire
format to HTTP+JSON, and it deliberately declines the core-proxy shortcut that
would have leaned on the deferred generic interpreter (ADR 0002) and the full-Go
core (ADR 0004). Runnable, tested examples of every surface live in
[`../typed-fetch-and-htmx.md`](../typed-fetch-and-htmx.md).
