# ADR 0008 — Operational Security Hardening

- Status: Accepted
- Date: 2026-07-28
- Scope: Gothic v1.6.0-beta.12 — operational decisions from the same security audit as ADR-0007
- Related: [0007](0007-route-middleware-abort-semantics-and-static-public-contract.md)

## Context

The server-side security audit that produced ADR-0007 also identified multiple operational hardening gaps. Each has its own shape, but they share a theme: the framework was permissive by default in areas where a security-conscious default or a fail-closed posture costs nothing on the happy path. These decisions bundle into one ADR because they are from the same audit, were debated in the same review, and ship in the same release.

## Decisions

### 1. `CacheConfig.MaxEntries` is OPT-IN (0 = unbounded)

- **Decision:** The new `MaxEntries int` field defaults to 0, which preserves the current unbounded behaviour. A cap is only applied when `MaxEntries > 0`.
- **Why not default-on:** A default cap would evict hot pages on high-cardinality ISR sites (parameterized profile-style pages — the same pattern this framework shares with Next.js). Users who reach the unbounded limit tend to have high-cardinality pages; an LRU cap would be the wrong solution for them. The documented recommendation for bounded production caches is the REDIS backend with `maxmemory` + `allkeys-lru`, because eviction under pressure is a solved problem there.
- **Why not change the key shape:** The cache key keeps including `r.URL.RequestURI()` (the query string). Changing the key to strip the query was considered and rejected because pages legitimately vary by query (`?lang=`, `?page=`, `?q=`). A stripped key would serve wrong content.
- **Why not warn at runtime:** A warning when the store size grows large would be a periodic log line that most production deployments would not monitor.

### 2. Toolchain downloads fail closed on checksum verification

- **Decision:** TinyGo (already had checksums) now hard-errors on mismatch instead of warning-and-proceeding. Tailwind (had no integrity check) gained checksum verification: `sha256sums.txt` is fetched from the pinned version's release assets, or embedded pre-computed hashes are used when the endpoint does not serve the pinned version. A cached binary (previously verified) is trusted without re-verification.
- **Why fail-closed:** the framework builds WASM binaries with the downloaded TinyGo and processes CSS with the downloaded Tailwind, so a corrupted or altered toolchain yields corrupted or altered output. A flaky network causing a build failure is cheaper than a bad binary reaching users unnoticed.
- **What this does and does not protect against — state it plainly:** the checksum file is fetched from the same release as the binary it describes, so an actor who can alter one can alter both. This verifies **integrity of the download** — truncation, corruption, a wrong asset, an altered response from anything between the release and the machine that TLS did not already stop. It is **not** protection against a compromised upstream release. Real supply-chain assurance needs hashes pinned in this repository or signature verification against a key we control, and neither exists yet. Do not describe this as supply-chain hardening in user-facing material.
- **Why cached binaries are trusted:** re-verifying on every build adds latency and network requests for nothing. The cache is local and was populated by a verified download; anyone able to write to it already controls the machine doing the build.

### 3. `.env` and cache artifacts excluded from Docker build context

- **Decision:** `.env`, `.gothic-cache`, and `gothic_outputs.json` are added to the `buildContextSkipDirs` set in the Docker engine.
- **Why:** The Docker build context is a tar sent to the Docker daemon, and the daemon may run on a remote host. Secrets in `.env` would be visible to anyone with access to the daemon or the build logs. `.gothic-cache` is large and irrelevant to the container image.
- **Why not a more general exclude mechanism:** A declarative exclude list (e.g., `.dockerignore` in the project) would be user-configurable and could be forgotten. A hard-coded skip list ensures these files are never shipped regardless of project configuration.

### 4. OpenTofu state bucket gets PublicAccessBlock at creation

- **Decision:** `PutPublicAccessBlock` with all four flags (`BlockPublicAcls`, `IgnorePublicAcls`, `BlockPublicPolicy`, `RestrictPublicBuckets`) is applied when the state S3 bucket is created. Existing buckets are not modified.
- **Why:** State files can carry plaintext secrets (environment variable values, database connection strings). An S3 bucket with public access is a common misconfiguration; the block is a defence-in-depth measure that costs nothing on the happy path.
- **Why not retroactively apply to existing buckets:** Existing deployments may have legitimate reasons for alternative access policies (e.g., a shared state bucket across toolchains). A retroactive change could break an existing workflow without warning. The block is a creation-time safety net, not a policy enforcement.

### 5. Codegen quotes every developer-controlled string and validates identifiers

- **Decision:** `strconv.Quote` is applied at data-build time to topic `KeyName`, route `HttpPath`, and any other developer-controlled string interpolated into generated Go. `SubscriberFnName` is validated as a valid Go identifier with a hard error before any file is written.
- **Why:** Raw string interpolation into generated Go produces broken code (compilation errors) when the string contains quotes, backslashes, or special characters. A compilation error from generated code is confusing — the developer sees a syntax error in a file they did not write. Quoting at data-build time ensures the generated Go is always syntactically valid. Identifier validation ensures a clear error at generation time rather than a cryptic compiler error.
- **Why not just document the constraint:** A documented constraint ("topic key names must be valid Go string literals") would be missed, forgotten, or misunderstood. Hard enforcement at generation time prevents the class of bug.

### 6. OptimizedImage alt parameter URL-encoded

- **Decision:** `url.QueryEscape(componentProps.Alt)` is used in `optimizeImages.templ` when building the `hx-get="...?alt="` URL.
- **Why:** Without encoding, an `Alt` value containing `&` splits the query string, producing a malformed URL. The browser interprets everything after the first `&` as a separate query parameter. `url.QueryEscape` is the standard Go solution and handles all URL-significant characters.

## Scope of this release

The audit produced more findings than this release implements. Each one that is
not here was reviewed and given a decision — some closed as not-a-finding, some
approved as work for a later release — rather than forgotten.

Those decisions, and the reasoning behind each, are recorded in the maintainers'
private register rather than in this document. Listing open items in a public
decision record tells everyone which parts of the currently released code to look
at, which is not information this file exists to publish.

## Consequences

- Default behaviour is unchanged for all existing projects (MaxEntries=0, unbounded).
- A builder with a flaky network will hit a new hard error instead of a warning. This is intentional — the cost of a false negative (compromised binary) is higher.
- Existing deployments are unaffected by the PAB change (creation-time only).
- Developers with special characters in topic keys or route paths will get correct generated code instead of broken compilation.
- The findings this release does not implement are tracked privately, each with a decision, and are not forgotten.
