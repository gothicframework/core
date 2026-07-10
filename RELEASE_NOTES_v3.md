# Gothic Framework v3.0.0

First release of the standalone **`gothic` CLI at `v3.0.0`** (`gothic version` → `v3.0.0`), and the first release to **split the framework into separate modules** — the runtime split into [`github.com/gothicframework/core`](https://github.com/gothicframework/core) (runtime), [`github.com/gothicframework/components`](https://github.com/gothicframework/components) (UI components), and [`github.com/gothicframework/middlewares`](https://github.com/gothicframework/middlewares) (runtime middleware), all published at **`v1.0.0`** (no `/v3` suffix), while the `gothic` tool ships as [`github.com/gothicframework/cli/v3`](https://github.com/gothicframework/cli) at **`v3.0.0`** (plus an internal `e2e-tests` module). v3.0.0 is a **hard cutover** that bundles two epics into one breaking release:

- **Part I — OpenTofu migration** (deploy/infra + Go config)
- **Part II — WASM runtime re-architecture** (instance lifecycle, multiplexing, static core, durable state)

> [!WARNING]
> **This is a breaking release.** The module path, config format, deploy backend, and topic API all change, with no legacy fallback. Read [Breaking Changes](#-breaking-changes) and [Migration](#-migration) before upgrading — `gothic migrate-v3` automates most of it.

## Highlights

- 🧱 **OpenTofu replaces AWS SAM** — infra as embedded `.tf.json`; the pinned `tofu` binary auto-downloads; Docker SDK builds + pushes the image.
- 📝 **Go config** — `gothic-config.json` → type-safe `gothic.config.go` with IDE completion and `BeforeDeploy` / `AfterDeploy` hooks.
- 🧹 **WASM re-mount memory leak fixed** — instances now tear down on DOM removal instead of stacking up until the tab OOMs.
- ⚡ **Component multiplexing** (`Multiplexed`, opt-in) — N placements of a component type share **one** WASM instance.
- 🧠 **Static full-Go core** — one prebuilt `gothic-core.wasm` is the shared topic hub + a **durable state cache** that survives re-mounts.
- 🔌 **Topics simplified** — no more mount: declare in `src/topics/`, use the accessor, done.

---

## 🚨 Breaking Changes

### Part I — Infra & config

- **Module path & split:** the single legacy `gothicframework/v2` module is split apart. The runtime becomes three modules published at **`v1.0.0`** (no `/v3` suffix) — `github.com/gothicframework/core` (runtime), `github.com/gothicframework/components` (UI components), and `github.com/gothicframework/middlewares` (runtime middleware) — and the `gothic` tool is its own module `github.com/gothicframework/cli/v3` at **`v3.0.0`**. Install the CLI with `go install github.com/gothicframework/cli/v3/cmd/gothic@latest`; the binary is now `gothic` (`gothic version` reports `v3.0.0`).
- **Config format:** `gothic-config.json` → `gothic.config.go` (Go source, parsed via AST). No runtime JSON fallback.
- **Deploy backend:** AWS SAM CLI is gone; **OpenTofu** manages infrastructure and the **Docker daemon is required** at deploy time.
- **Resource naming:** `.gothicCli/app-id.txt` is no longer generated. Names are now deterministic — derived from the Go module name + project name (CRC32 hex suffix). Existing live stacks change names; see the manual import playbook in the README.

### Part II — WASM runtime

- **Topic mount removed.** There is **no** `@AddXxxTopic()` mount, **no** `TopicConfig.ComponentFnName` field (setting it is now a compile error), and the internal `TopicManagerComponent(...)` helper is gone. The new convention is **accessor-only**: declare `CreateTopic(...)` in `src/topics/` (a required folder) and use its generated accessor (named by the topic's var, or by `SubscriberFnName`) in `ClientSideState` — that alone auto-registers the topic with the always-loaded core. `gothic migrate-v3` **auto-strips** both the `ComponentFnName:` lines and the `@AddXxxTopic()` calls so migrated projects build without manual edits.
- **New required layout runtime.** Every project now references the framework's client runtime in its layout `<head>` via the components module (`@gothicComponents.RuntimeScripts()` + `@gothicComponents.Styles()`), and mounts the runtime middleware (`server.Middleware(Config.Runtime)`). The shared runtime globals moved out of the per-instance inline bootstrap into a cached `gothic-core.js`, and the WASM-runtime assets are served from the framework embed under `/_gothic/*` rather than copied into each project's `public/`. Multiplexing and the durable cache themselves are **opt-in** — a page that adopts neither behaves exactly as before.

---

## ✨ New Features

### Part I — OpenTofu & Go config

- **`gothic.config.go`** — type-safe Go config with IDE completion and compile-time validation.
- **OpenTofu deploys** — infrastructure as embedded `.tf.json` files; no `template.yaml`. The pinned OpenTofu binary auto-downloads and caches to `.gothicCli/bin/tofu` on first deploy.
- **Docker SDK image build + ECR push** — the Lambda container image is built via the Docker SDK and pushed to ECR (Docker daemon required at deploy).
- **Lifecycle hooks** — `BeforeDeploy` / `AfterDeploy` functions in `gothic.config.go` run custom Go code around each deploy.
- **`gothic migrate-v3`** — one-command v2 → v3 migration (config, imports, SAM cleanup, `go.mod`, topic mounts). Flags: `--path`, `--dry-run`. Idempotent.
- **Deterministic resource naming** — same suffix on every machine; survives `git clone`.
- **`TofuBinaryPath` override** — point to a pre-installed OpenTofu binary to skip the auto-download.
- **`DockerfilePath` override** — use a custom Dockerfile instead of the embedded one.

### Part II — WASM runtime

- **`OnUnmount(func())`** — new hook to release resources (timers, subscriptions, abort controllers) when an instance is torn down.
- **`RouteConfig.Multiplexed bool`** *(opt-in, default `false`)* — every placement of a component *type* shares **one** WASM instance via scope register/unregister, collapsing the N-per-row case to a single instance.
- **Static full-Go core.** A prebuilt, embedded `gothic-core.wasm` (`GOOS=js GOARCH=wasm`, ~1.9 MB) is the single generic **opaque** topic hub — store-and-forward per `(key, field)`, byte-diff, per-field replay + per-key online ack. **No `topic-<key>.wasm` is emitted or fetched** anymore; consumers self-register their key + fields at runtime.
- **`DurableObserve[T](field, obs, encode, decode)` + `data-gothic-durable-key`** *(opt-in)* — a component's state survives an unmount → re-mount within the page session, rehydrated from the core with no server round-trip. Empty is a first-class value (a cleared field restores as cleared). **Page-session lifetime** — not reload-durable, not server-persisted.
- **Shared runtime assets.** `gothic init`, `gothic wasm`, `gothic hot-reload`, and `gothic deploy` emit `gothic-core.js` (shared runtime globals), `gothic-core.wasm` (the static core), `gothic-core-exec.js` (version-matched `wasm_exec` shim), and `gothic-core-boot.js` (once-per-page loader). They are served from the framework embed under `/_gothic/*` by `server.Middleware` and referenced through `@gothicComponents.RuntimeScripts()`. Content-hashed (`?v=`) from the framework version, so an upgrade cache-busts automatically. Plain `gothic build` compiles templ only and does **not** emit them.
- **Wire-version byte.** Every top-level codec frame carries a format-version byte at position 0 (`WireVersion = 1`), written by `NewEncoder` and validated by `NewDecoder` (mismatch → sticky error, no panic). Makes future codec changes safe across hot-reload skew.
- **Two-tier protocol.** State broadcasts ride an opaque **binary data-plane** (`window.__gothic_topic`); register/online/ping/config metadata rides a **JSON/string control-plane**. JSON stays off the hot path.
- **Schema seam** *(reserved, inert in 3.0)* — the CLI derives a canonical wire descriptor + content-hash `schemaId` per topic struct and threads them into registration via `GothicRegisterSchema`. Nothing interprets it yet; it's a reserved slot for a future generic interpreter and a hot-reload skew fingerprint.

---

## 🐛 Fixes

- **WASM instance leak on HTMX re-mount.** Gothic mounted WASM components but never unmounted them — an HTMX swap-away/back stacked a second instance on top of the first, and since WASM linear memory only grows, committed memory ratcheted until `WebAssembly.instantiate()` failed with `Out of memory`. A single global `MutationObserver` on `document.body` now tears an instance down when its `[data-gothic-scope]` is removed: it runs the component's `OnUnmount`, strips its event listeners, drops its JS references, and halts its keep-alive goroutine (`__gothic_halt` / `GothicHaltChan`). Fragment-heavy UIs (per-row WASM, filter-refetched charts) stay memory-bounded across re-mounts.

---

## 📦 Dependencies

- **Added:** `github.com/opentofu/tofudl`, `github.com/hashicorp/terraform-exec/tfexec`, `github.com/docker/docker/client`, `github.com/aws/aws-sdk-go-v2/...`
- **Removed:** the AWS SAM CLI dependency (was a shell exec; replaced by SDK calls).

---

## 📖 Migration

### From v2 → v3

Run `gothic migrate-v3` in your v2 project root (add `--dry-run` to preview):

```bash
gothic migrate-v3 --path . --dry-run   # preview — writes nothing
gothic migrate-v3 --path .             # apply
```

It rewrites `gothic-config.json` → `gothic.config.go`, rewrites the legacy `gothicframework/v2` imports to the new split module paths (`core` / `components` / `middlewares`, all `v1.0.0`) across `.go` / `.templ`, removes SAM artifacts, **cleans up the removed topic mount** (AST-scoped strip of `ComponentFnName:` fields from `src/topics/*.go` `TopicConfig` literals + removal of `@AddXxxTopic()` calls from `.templ` files), updates `go.mod`, and runs `go mod tidy`. It's idempotent. See the README for the manual import playbook for projects with existing live deployments (resource names change from the old random app-id to a deterministic module-derived suffix).

> [!IMPORTANT]
> **The layout runtime is not rewritten by `migrate-v3`.** After migrating, make sure your app wires the v3 runtime:
>
> 1. In `main.go`, apply the runtime middleware: `router.Use(gothicServer.Middleware(Config.Runtime))` (import `gothicServer "github.com/gothicframework/middlewares"`).
> 2. In your layout `<head>`, reference the framework runtime and styles:
>
>    ```go
>    @gothicComponents.Styles()
>    @gothicComponents.RuntimeScripts()
>    ```
>
> `RuntimeScripts()` serves `gothic-core.js` / `gothic-core-boot.js` (and HTMX) from the framework embed with version-matched `?v=` cache-busters — no manual `<script>` tags or `public/` asset copies. Without it, v3 WASM components have no shared runtime or core to register against and will not hydrate.

### Upgrading within v3 (CLI patch upgrade)

When you upgrade the Gothic CLI within v3 (e.g. a patch that changes `gothic-core.js` or the static core), the runtime assets are served from the framework embed and their `?v=` cache-busters are derived from the framework version — so `@gothicComponents.RuntimeScripts()` picks up the new hashes automatically. Just rebuild (`gothic hot-reload` / `gothic wasm`); no layout edits are needed.

Your `ClientSideState`, topic definitions, and `StatefulComponentOf` calls need no source changes across a v3 patch upgrade. `Multiplexed` and `DurableObserve` are opt-in and can be adopted incrementally.

---

## ⚠️ Known Limitations & gotchas

<details>
<summary>Six behaviors worth knowing (click to expand)</summary>

- **(a) Un-scoped return functions.** `CreateWasmFuncWithReturn` sets a bare `window[name]` global (not scope-routed). Do **not** reuse the same return-function name across multiplexed placements or multiple instances of a component — last registration wins.
- **(b) Offline per-field sets are dropped.** A per-field `topic.Field.Set` *before the topic is online* is dropped. Only a whole-struct `topic.Set(struct)` buffers as pending and flushes on online.
- **(c) First-registration-wins field list.** Consumers of a topic must agree on its fields; a later consumer can't extend or redefine them.
- **(d) Hex-ASCII on the bus.** The per-field data-plane payload is transported hex-ASCII (`BinaryKey` `HexEncode`) over the `__gothic_topic` bus. Still opaque/binary-semantic (never parsed as JSON), but the on-bus representation is hex text in devtools.
- **(e) Durable cache is page-session only.** Not reload-durable, not server-persisted.
- **(f) Brief SSR-default flash.** A momentary default paint before WASM hydration is normal (the instance boots asynchronously). The online gate guarantees the restored/correct value wins.

</details>

---

**Docs:** [`docs/DESIGN-INSPIRATIONS.md`](docs/DESIGN-INSPIRATIONS.md) and [`docs/adr/`](docs/adr/) for the design rationale · the [core README](README.md) for the full CLI and configuration reference.
