// Package runtimeassets is the single registry + HTTP handler for the Gothic
// framework's WASM-runtime assets that used to be COPIED into every project's
// public/ folder at `gothic init` / build time. Instead of shipping bytes into
// the user's tree, the framework now serves them straight from its own embed
// under a dedicated route prefix (/_gothic/), so a framework upgrade updates the
// runtime for every project with no file churn and no stale copies.
//
// The five runtime assets served here:
//
//	gothic-core.js       the shared idempotent client runtime (pkg/helpers/gothiccore)
//	gothic-core.wasm     the prebuilt full-Go static core module (pkg/helpers/corewasm)
//	gothic-core-exec.js  the standard-Go wasm_exec shim matched to the core (corewasm)
//	gothic-core-boot.js  the generated core boot loader (corewasm)
//	wasm_exec.js         the TinyGo wasm_exec shim (pkg/data/wasm_exec)
//
// NOTE the user's own GOROOT-matched wasm_exec_go.js stays in public/ (it is
// version-tied to the user's Go toolchain, copied at build time — not a framework
// artifact) and the per-page user WASM binaries stay under /public/wasm/.
//
// This is a LEAF package over the three asset-owning leaf packages (gothiccore,
// corewasm, wasmexec). Those packages must NOT import runtimeassets or a cycle
// forms — runtimeassets sits one level above them.
package runtimeassets

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	wasmexec "github.com/gothicframework/core/wasmexec"
	"github.com/gothicframework/core/corewasm"
	"github.com/gothicframework/core/gothiccore"
)

// Prefix is the route namespace the framework serves its runtime assets under.
// It is deliberately distinct from /public/ so it never collides with a user's
// static files and can carry its own edge-cache behavior at the CDN.
const Prefix = "/_gothic/"

// Content types. The .js assets are served as JavaScript; the core module as the
// spec Content-Type so streaming/instantiate paths work on strict hosts.
const (
	contentTypeJS   = "text/javascript; charset=utf-8"
	contentTypeWASM = "application/wasm"
)

// Asset is one served runtime file: its basename (the path segment after the
// prefix), the bytes to write, its Content-Type, and the content-hash used as
// the ?v= cache-buster in the URL that references it.
type Asset struct {
	Name        string
	Bytes       []byte
	ContentType string
	Version     string
}

// hash16 returns the first 16 hex chars of sha256(b) — the content cache-buster,
// matching the convention used by gothiccore/corewasm.
func hash16(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// wasmExecShimHash content-hashes the TinyGo wasm_exec shim locally: unlike the
// other four assets, pkg/data/wasm_exec exposes only the bytes (Shim), not a
// Version(), so we compute its cache-buster here.
var wasmExecShimHash = hash16(wasmexec.Shim)

// registry is the name→Asset lookup. Bytes and hashes are pulled from the
// asset-owning leaf packages so there is a single source of truth per artifact.
var registry = func() map[string]Asset {
	list := []Asset{
		{
			Name:        gothiccore.FileName, // gothic-core.js
			Bytes:       []byte(gothiccore.JS),
			ContentType: contentTypeJS,
			Version:     gothiccore.Version(),
		},
		{
			Name:        corewasm.WASMFileName, // gothic-core.wasm
			Bytes:       corewasm.CoreWASM(),
			ContentType: contentTypeWASM,
			Version:     corewasm.CoreHash(),
		},
		{
			Name:        corewasm.ExecFileName, // gothic-core-exec.js
			Bytes:       corewasm.ExecJS(),
			ContentType: contentTypeJS,
			Version:     corewasm.ExecHash(),
		},
		{
			Name:        corewasm.BootFileName, // gothic-core-boot.js
			Bytes:       corewasm.BootJS(),
			ContentType: contentTypeJS,
			Version:     corewasm.Version(),
		},
		{
			Name:        "wasm_exec.js", // TinyGo shim
			Bytes:       wasmexec.Shim,
			ContentType: contentTypeJS,
			Version:     wasmExecShimHash,
		},
	}
	m := make(map[string]Asset, len(list))
	for _, a := range list {
		m[a.Name] = a
	}
	return m
}()

// Get returns the asset registered under name (the path segment after Prefix),
// and whether it exists.
func Get(name string) (Asset, bool) {
	a, ok := registry[name]
	return a, ok
}

// All returns every registered asset (unordered).
func All() []Asset {
	out := make([]Asset, 0, len(registry))
	for _, a := range registry {
		out = append(out, a)
	}
	return out
}

// Path returns the referencing URL for an asset name: Prefix+name+"?v="+version.
func Path(name, version string) string {
	return Prefix + name + "?v=" + version
}

// Handler serves the runtime assets under Prefix. It strips the prefix, looks the
// file up in the registry, sets the right Content-Type, marks it immutable when a
// ?v= cache-buster is present (mirroring immutableCacheMiddleware), and writes the
// bytes. Unknown names 404. Only GET/HEAD are meaningful; other methods 405.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		// Reject nested paths / traversal — assets are a flat namespace.
		if name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		asset, ok := registry[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.ContentType)
		// A ?v=<hash> cache-buster means the URL is content-addressed: cache it
		// immutably for a year. A framework upgrade changes the hash (hence the
		// URL), so the cache is busted automatically.
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if r.Method == http.MethodHead {
			return
		}
		w.Write(asset.Bytes)
	})
}
