<img alt="Gothic Framework" src="Doc/Assets/gothic-hero.png" width="100%"/>

[![CI](https://github.com/gothicframework/core/actions/workflows/ci.yml/badge.svg)](https://github.com/gothicframework/core/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/gothicframework/core/branch/main/graph/badge.svg)](https://codecov.io/gh/gothicframework/core)

# Gothic Framework — Core

**Gothic Framework** is a developer-first toolset for building fast, scalable, modern web apps in Go with the **GOTTH stack**: **Go**, **TailwindCSS**, **Templ**, and **HTMX**. Inspired by Next.js, it brings full-stack ergonomics — file-based routing, edge-ready static caching, ISR, image optimization, link prefetching, hot reloading, and one-command cloud deploys — to Go developers.

This module (`github.com/gothicframework/core`) is the heart of the framework. It ships:

- the **`gothic` CLI** — scaffolding, dev server, build pipeline, WASM/CSS/image tooling, and deploy; and
- the **runtime library** your app imports — public packages `config`, `router`, `wasm`, `runtimeassets`, `gothiccore`, `corewasm`, `wasmexec`, and `render`.

The reusable UI components (`RuntimeScripts`, `Styles`, `StatefulComponentOf`, `OptimizedImage`) and the one-line chi runtime middleware live in the companion module **[`github.com/gothicframework/components`](https://github.com/gothicframework/components)**.

---

## Installation

Install the `gothic` binary with the Go toolchain:

```bash
go install github.com/gothicframework/cli/v3/cmd/gothic@latest
```

This puts a `gothic` executable on your `PATH` (in `$(go env GOPATH)/bin`). Verify it:

```bash
gothic version
```

### Scaffold a new project

```bash
gothic init github.com/you/my-app
```

Pass your Go module path and `init` runs fully non-interactively: the module is used as-is and the project name is **derived from it** — the last path segment, minus any `/vN` major-version suffix (so `github.com/you/my-app/v3` → `my-app`). Omit the argument to be prompted for the module. The derived `ProjectName` is written into `gothic.config.go` and can be edited afterward.

---

## Prerequisites

- **Go 1.25+** — the module targets `go 1.25.0`. Used to build your app and run deploy lifecycle hooks.
- **Docker daemon running** *(deploy-time only)* — Gothic builds your Lambda container image via the Docker SDK and pushes it to ECR. The daemon must be reachable at deploy time.
- **AWS credentials configured** *(deploy-time only)* — via the shared config file (`~/.aws/config` / `~/.aws/credentials`) or environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`). The `Profile` field in `gothic.config.go` selects the shared profile.
- **OpenTofu** — **no manual install required.** The CLI downloads a pinned OpenTofu release to `.gothicCli/bin/tofu` on the first deploy and reuses it afterward. Point `TofuBinaryPath` at a pre-installed binary to skip the download.

> Local development (`gothic build`, `gothic hot-reload`, `gothic wasm`, `gothic css`) needs only Go — Docker and AWS credentials are deploy-time concerns.

---

## The `gothic` command

| Command | What it does |
|---|---|
| `gothic init [module-path]` | Scaffold a new project. Module path optional (prompted if omitted); project name derived from it. |
| `gothic hot-reload` | Dev server. Watches `.templ`, `.go`, and CSS; rebuilds templ, emits runtime assets, recompiles WASM, and live-reloads. |
| `gothic build` | Compile `.templ` files to their `_templ.go` equivalents. **Does not** emit runtime WASM assets or CSS. |
| `gothic wasm` | Compile per-component `ClientSideState` to TinyGo WASM and emit the shared runtime/core assets. |
| `gothic css` | Generate the Tailwind stylesheet from classes found in `.templ` files. |
| `gothic optimize-images` | Produce low-res blurred placeholders for lazy-loaded images referenced in templates. |
| `gothic deploy` | Build + push the container image and apply infrastructure with OpenTofu (see below). |
| `gothic migrate-v2` | Migrate a v1 project to v2 conventions. |
| `gothic migrate-v3` | Migrate a v2 project to v3 (config → Go, imports, SAM cleanup, topic mounts). |
| `gothic version` | Print the installed CLI version. |

---

## Configuration (`gothic.config.go`)

Gothic projects are configured with a **type-safe Go source file**, `gothic.config.go`, at the project root. It is parsed by the CLI via Go's AST (no type-checker, no runtime JSON), so you get IDE completion and compile-time validation. `gothic init` scaffolds it for you.

```go
package main

import (
	gothic "github.com/gothicframework/core/config"
)

var Config = gothic.Config{
	ProjectName: "my-app", // used to derive deterministic AWS resource names
	// Your Go module name is read automatically from go.mod — it is NOT a field here.

	// Optional binary overrides — leave empty to use the CLI-managed defaults:
	TofuBinaryPath: "", // absolute path to a pre-installed OpenTofu binary (skips auto-download)
	DockerfilePath: "", // absolute path to a custom deploy Dockerfile (overrides the embedded one)
	WasmBinary:     "", // absolute path to a tinygo binary override
	TailwindBinary: "", // absolute path to a tailwind binary override

	OptimizeImages: gothic.OptimizeImagesConfig{
		LowResolutionRate: 20, // low-res placeholder quality for lazy-loaded images
	},

	// Runtime router config: cache backend + static-file serving. The zero value
	// equals the defaults below, so this block can be omitted entirely.
	Runtime: gothic.RuntimeConfig{
		CacheStrategy:         gothic.CACHE_CONTROL_HEADERS,
		LocalDevelopmentCache: gothic.IN_MEMORY,
		ServeStaticFiles:      gothic.HOT_RELOAD_ONLY,
	},

	Deploy: &gothic.DeployConfig{
		ServerMemory:  512,         // Lambda memory (MB)
		ServerTimeout: 30,          // Lambda timeout (seconds)
		Region:        "us-east-1", // AWS region to deploy into
		Profile:       "default",   // shared-config profile for credentials

		Stages: map[string]gothic.Stage{
			"dev": {
				ENV: map[string]gothic.EnvValue{
					"PORT":    gothic.Env("8080"),                        // plain string value
					"DB_URL":  gothic.SSMParam("/my-app/dev/db-url"),     // resolved from SSM Parameter Store
					"API_KEY": gothic.SecretsManager("/my-app/dev/api-key"), // resolved from Secrets Manager
				},
			},
		},
	},
}
```

### ENV value builders

Each entry in a stage's `ENV` map is produced by one of three builders. Secrets resolved from SSM or Secrets Manager are pulled by OpenTofu data sources at apply time and **never land in plain text in your config**:

| Builder | Source | Use for |
|---|---|---|
| `gothic.Env("8080")` | Plain string | Non-sensitive config values |
| `gothic.SSMParam("/path")` | AWS SSM Parameter Store | Config / secrets stored in SSM |
| `gothic.SecretsManager("/path")` | AWS Secrets Manager | Secrets stored in Secrets Manager |

Any other function in an `ENV` value is rejected by the parser with an actionable error.

### Lifecycle hooks

Declare top-level `BeforeDeploy` and/or `AfterDeploy` functions in `gothic.config.go` to run custom Go code around a deploy. Both are optional.

```go
func BeforeDeploy(ctx context.Context, gctx *gothic.GothicContext) error
func AfterDeploy(ctx context.Context, gctx *gothic.GothicContext) error
```

- `BeforeDeploy` runs **synchronously before** the image build and `tofu apply`. A non-nil error **aborts the deploy** — nothing is pushed or changed.
- `AfterDeploy` runs **after** `tofu apply` and the S3 asset sync. By then `gctx.Outputs` carries stack outputs (`cloudfront_distribution_id`, `cloudfront_domain_name`, `s3_bucket_arn`, `lambda_function_arn`). The `GothicContext` also carries `Stage`, `ProjectName`, `Suffix`, `Region`, and `Env`.

---

## Wiring the runtime (`main.go`)

Gothic's whole server runtime — cache backend, `/public/*` static serving, the `/optimizedImage/*` endpoint, and the framework's WASM-runtime assets under `/_gothic/*` — is applied as a **single chi middleware** from the components module, driven by the `Runtime` block above. You never edit `main.go` when the framework adds a built-in route feature.

```go
package main

import (
	"log"
	"net/http"
	"os"

	gothicServer "github.com/gothicframework/middlewares"
	"github.com/you/my-app/src/routes"

	"github.com/go-chi/chi/v5"
)

func main() {
	router := chi.NewMux()
	router.Use(gothicServer.Middleware(Config.Runtime)) // the entire Gothic runtime
	routes.RegisterFileBasedRoutes(router)              // your file-based pages
	log.Fatal(http.ListenAndServe(os.Getenv("HTTP_LISTEN_ADDR"), router))
}
```

In your layout `<head>`, pull in the framework's runtime scripts and styles from the components module — the content-hash cache-busters stay in sync with the framework version automatically:

```go
templ PageLayout() {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			@gothicComponents.Styles()
			@gothicComponents.RuntimeScripts()
		</head>
		<body>{ children... }</body>
	</html>
}
```

---

## Deploying

```bash
gothic deploy --stage dev                 # build, push, apply, sync assets, invalidate CDN
gothic deploy --stage dev --action delete # tear the stack down
```

Infrastructure is managed as code from embedded OpenTofu `.tf.json` stack files — no `template.yaml`, no SAM CLI. On the first deploy the CLI:

1. Runs `BeforeDeploy` (if declared).
2. Downloads and caches the OpenTofu binary to `.gothicCli/bin/tofu` (reused afterward).
3. Builds your Lambda container image via the Docker SDK and pushes it to **ECR**.
4. Bootstraps the OpenTofu S3 state backend + DynamoDB lock table (skipped if they exist).
5. Generates the `.tf.json` stack into `.gothicCli/tofu/<stage>/` and runs `tofu init` + `tofu apply` (Lambda + API Gateway + CloudFront + S3).
6. Syncs the `public/` folder to S3 and invalidates the CloudFront distribution.
7. Runs `AfterDeploy` (if declared) with stack outputs populated.

`--action delete` removes the stack, then prompts before deleting the remote state bucket and lock table (answer `N` to preserve them).

---

## Migrating from v2

If you have an existing v2 project (with `gothic-config.json` and SAM templates), convert it to v3 with one command:

```bash
gothic migrate-v3           # migrate the current directory
gothic migrate-v3 --dry-run # preview the changes without writing
```

It backs up `gothic-config.json`, generates `gothic.config.go` from your existing values, removes SAM artifacts, rewrites `/v2 → /v3` imports, cleans up the removed topic-mount API, updates `go.mod`, and runs `go mod tidy`. It is idempotent. See [`RELEASE_NOTES_v3.md`](RELEASE_NOTES_v3.md) for the full breaking-change list and the manual import playbook for projects with live deployments.

> The v3 CLI has no runtime JSON fallback. If `GetConfig()` finds a `gothic-config.json`, it directs you to run `gothic migrate-v3`.

---

## Design docs

- [`docs/DESIGN-INSPIRATIONS.md`](docs/DESIGN-INSPIRATIONS.md) — prior art that shaped the v3 design, and what we deliberately did *not* adopt.
- [`docs/adr/`](docs/adr/) — Architecture Decision Records (custom codec, schema seam, two-tier protocol, static full-Go core).
- [`RELEASE_NOTES_v3.md`](RELEASE_NOTES_v3.md) — the v3.0.0 breaking-change and feature notes.
