# Changelog

## v1.6.0-beta.12 (2026-07-28) — Security patch: abort semantics, cache hardening, deploy hardening

**Security fix — upgrade promptly if any route uses `Middleware` to reject a request.** On earlier versions a rejection did not stop the page from rendering. Upgrading is the fix; there is no configuration workaround.

**This release requires no public API or config changes.** Byte-identical responses on the happy path. If you have routes with a `Middleware` that writes a status, review them once after upgrading — rejections and redirects are now final.

### Middleware rejections now end the request

A `Middleware` that writes status >= 300 stops the handler: no render, nothing cached, its response is the whole response. Earlier versions did not stop the render. Affects anyone whose Middleware writes an error or a redirect.

### STATIC/ISR are public by contract

The Middleware is a props loader; it runs on cache miss, never on a hit. Access control belongs on DYNAMIC routes or in chi middleware. A rejection on a STATIC/ISR route now logs a one-time yellow warning naming the route.

### Error responses are never cached

4xx/5xx pages and API 5xx no longer become cache entries. API 4xx and 2xx still cache (deterministic — the handler would produce them again).

### Render failures return a clean 500

Instead of partial HTML, a render error returns `500 Internal Server Error` and caches nothing. A subsequent request re-renders normally.

### `Runtime.MaxEntries`

New, optional. Bounds the IN_MEMORY store with LRU eviction. **Default is unbounded: existing behaviour is unchanged.** Point high-cardinality sites at REDIS with `maxmemory` + `allkeys-lru`.

### Builds fail on bad or missing toolchain checksums

Instead of warning and continuing, a mismatched or unreachable checksum for TinyGo or Tailwind is a hard error. This verifies that the download arrived intact; the checksum file comes from the same release as the binary, so it is not a defence against a compromised upstream release. Invisible when downloads verify normally.

### `.env` no longer enters the Docker build context

`.env`, `.gothic-cache`, and `gothic_outputs.json` are excluded from the Docker build tar. Only affects a custom Dockerfile that reads `.env` at build time.

### State bucket gets public-access block

Newly created OpenTofu state S3 buckets get `PutPublicAccessBlock` with all four flags. Existing buckets are not modified.

### Codegen quoting + validation

Topic identifiers with quotes or special characters now produce a correct Go string literal instead of broken generated code. An invalid `SubscriberFnName` produces a clear error at generation time.

### OptimizedImage alt URL-encoded

Alt text containing `&`, `=`, or other URL-significant characters no longer produces a malformed `hx-get` URL.

### What does NOT change

- No public API, signature or required config changes.
- Byte-identical responses on the happy path.
- Existing state buckets are unaffected.
- The cache-key shape is unchanged (includes query string).
- Default MaxEntries is 0 (unbounded) — existing behaviour unchanged unless opted in.

### Review your Middleware

**Action item:** If you have routes with a `Middleware` that writes a status, review them once after upgrading — rejections and redirects are now final. The Middleware signature does not change, but its output is now authoritative.

---

### Release body (copy-paste for GitHub release)

```markdown
## v1.6.0-beta.12 — Security patch

This release ships the findings of a server-side security audit of `core`. No public API changes required.

**Middleware rejections now end the request.** A Middleware that writes status >= 300 stops the handler — no render, no cache, its response is final.

**STATIC/ISR are public by contract.** The Middleware is a props loader, runs on cache miss only, never on hit. Authz belongs on DYNAMIC routes or chi middleware.

**Errors never cached.** 4xx/5xx pages and API 5xx are never written to the cache. Render failures return 500 and cache nothing.

**`Runtime.MaxEntries` — optional LRU bound.** Default 0 = unbounded (unchanged).

**Toolchain downloads fail closed** on checksum mismatch. `.env` excluded from Docker build context. State bucket gets PublicAccessBlock on creation. Codegen quotes developer strings. OptimizedImage alt URL-encoded.

Full details in `core/docs/adr/0007` and `0008`.

**Action item:** Review any route whose Middleware writes a status — rejections are now final.
```
