# ADR 0007 — Route Middleware abort semantics and STATIC/ISR public contract

- Status: Accepted
- Date: 2026-07-28
- Scope: Gothic v1.6.0-beta.12 — route Middleware behaviour under error/redirect and the STATIC/ISR cache contract
- Related: [0001](0001-custom-codec-not-protobuf.md), [0004](0004-static-full-go-core.md)

## Context

A server-side security audit found two related gaps in how the route `Middleware`
interacts with rendering and caching:

1. **A `Middleware` had no way to end a request.** Writing a status did not stop
   the handler, so a rejection or a redirect could not be made final.

2. **Response status did not affect cacheability.** Error responses could become
   cache entries, and on API STATIC/ISR routes so could a 5xx.

Underneath both sits a design question the framework had never answered in
writing. The signature `(w http.ResponseWriter, r *http.Request) T` receives the
ResponseWriter, so it reads as a place for authorization — but on STATIC and ISR
routes its output is cached and served to every caller. The intent, that those
routes are public content in the Next.js SSG sense, existed in nobody's head but
the author's: it was neither documented nor enforced.

Operational detail of what the gaps allowed is recorded in the private knowledge
base rather than here. Releases before this one are still running, and this
document is public.

## Decision

### Doctrinal anchor

The `Middleware` is a **props loader** — the analogue of Next.js `getStaticProps` / `getServerSideProps` — not an auth hook. Next.js gives `getStaticProps` NO request object, making SSG public-by-definition and keeping auth out of it. Their middleware runs before the cache, and their own guidance is that middleware must not be the sole protection for routes. CVE-2025-29927 was fixed by validating a forgeable internal header — without touching cache semantics.

Gothic follows the same model after this patch:

| Gothic route type | Middleware runs when | Cacheable? | Authz here? |
|---|---|---|---|
| STATIC | on cache MISS (props loader) | yes (public content) | NO — public by contract |
| ISR | on cache MISS / revalidation | yes, with TTL (public) | NO — public by contract |
| DYNAMIC | EVERY request | never | YES — abort works end-to-end |
| chi `router.Use` | every request, before routing | — | YES (recommended home) |

### Abort semantics

1. Any status >= 300 written by the `Middleware` ends the request: no render, no cache write, the middleware's own response (status line, headers, body) is preserved as the complete HTTP response.
2. Redirects (3xx) abort the render silently — no warning.
3. On STATIC/ISR routes, a rejection >= 400 emits a one-shot yellow warning naming the route and directing the developer toward DYNAMIC or chi middleware.
4. DYNAMIC routes abort silently (no warning), as they are the documented authz home.

### Cache hardening

5. A render error on the miss branch returns `http.Error(w, "Internal Server Error", 500)` and caches nothing.
6. API STATIC/ISR handlers cache only responses whose `StatusCode < 500`. 5xx errors are never written to the cache. 4xx stays cacheable (deterministic).
7. The cache-hit path does NOT re-run the Middleware — that is the documented contract, pinned by tests.

### Implementation approach

- A new `statusCapture` wrapper that intercepts `WriteHeader()` and exposes `Status()` and the standard `Unwrap()`/`Flush()`/`Hijack()` forwarders.
- Internal-only handler rewrites in `staticHandler`, `isrHandler`, `dynamicHandler`, `apiStaticHandler`, `apiISRHandler`. No exported signature changes.
- No change to cache keys, TTLs, `Cache-Control` values, or the `RouteConfig`/`Middleware` public API.

## Rejected alternatives

**a) Run Middleware on every request including cache hits.** This was the originally proposed "full fix." Rejected in design review because it would re-execute the data queries the cache exists to skip — the cache would save only the render and lose its purpose.

**b) Change the Middleware signature to return an error.** A hard breaking change to a load-bearing public API (`func(w http.ResponseWriter, r *http.Request) T`). The abort semantics achieve the same with no signature change.

**c) Documentation only.** Leaves the defect live on every route while telling users to work around it. Rejected: this is a behaviour gap in the framework, not a gap in its documentation.

**d) Validate the cache-hit path separately (chi middleware pre-check).** Would require running the same logic twice (before the cache and on miss), creating a consistency hazard. The single miss-path check is simpler and correct.

## Consequences

- STATIC/ISR are now explicitly public by contract, identical to Next.js SSG.
- Authz belongs on DYNAMIC routes (Middleware runs every request and can abort properly) or in chi middleware.
- Behaviour deltas are confined to paths that were already broken. The happy path — no rejection, no error — produces byte-identical responses.
- Users must review any route whose `Middleware` writes a status — those rejections and redirects are now final.
