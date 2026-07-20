// Package runtimeassets is the single registry + HTTP handler for the Gothic
// framework's WASM-runtime assets that used to be COPIED into every project's
// public/ folder at `gothic init` / build time. Instead of shipping bytes into
// the user's tree, the framework now serves them straight from its own embed
// under a dedicated route prefix (/_gothic/), so a framework upgrade updates the
// runtime for every project with no file churn and no stale copies.
//
// The runtime assets served here:
//
//	gothic-core.js              the shared idempotent client runtime (gothiccore, minified)
//	gothic-core.wasm            the prebuilt full-Go static core module (corewasm)
//	gothic-core-exec.js         the standard-Go wasm_exec shim matched to the core (corewasm)
//	gothic-core-boot.js         the generated core boot loader (corewasm)
//	wasm_exec.js                the TinyGo wasm_exec shim (wasmexec)
//	htmx.min.js                 HTMX, self-hosted (vendorjs) — no longer a render-blocking CDN <script>
//
// Every asset is content-negotiated: the handler precompresses each one with
// brotli and gzip at startup and serves the smallest form the client accepts
// (Content-Encoding: br|gzip), falling back to the raw bytes. This is what makes
// the ~1.9 MB gothic-core.wasm transfer at a fraction of its size on routes that
// serve it through this handler (a CDN in front is free to keep doing its own
// compression too).
//
// NOTE the user's own GOROOT-matched wasm_exec_go.js stays in public/ (it is
// version-tied to the user's Go toolchain, copied at build time — not a framework
// artifact) and the per-page user WASM binaries stay under /public/wasm/.
//
// This is a LEAF package over the asset-owning leaf packages (gothiccore,
// corewasm, wasmexec, vendorjs). Those packages must NOT import runtimeassets or a
// cycle forms — runtimeassets sits one level above them.
package runtimeassets

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gothicframework/core/corewasm"
	"github.com/gothicframework/core/gothiccore"
	"github.com/gothicframework/core/vendorjs"
	wasmexec "github.com/gothicframework/core/wasmexec"
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
// prefix), the raw bytes, precompressed brotli/gzip variants (nil when a variant
// is not smaller than the raw bytes), its Content-Type, and the content-hash used
// as the ?v= cache-buster in the URL that references it.
type Asset struct {
	Name        string
	Bytes       []byte // identity (uncompressed)
	Brotli      []byte // brotli-compressed, or nil if not smaller than Bytes
	Gzip        []byte // gzip-compressed, or nil if not smaller than Bytes
	ContentType string
	Version     string
}

// hash16 returns the first 16 hex chars of sha256(b) — the content cache-buster,
// matching the convention used by gothiccore/corewasm.
func hash16(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// gzipBytes returns b gzip-compressed at max level, or nil if compression did not
// shrink it (tiny/already-compressed inputs) so the handler serves raw instead.
func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := zw.Write(b); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	if buf.Len() >= len(b) {
		return nil
	}
	return buf.Bytes()
}

// brotliBytes returns b brotli-compressed at max level, or nil if it did not
// shrink. Brotli typically beats gzip on these text/wasm assets, so it is offered
// first in negotiation.
func brotliBytes(b []byte) []byte {
	var buf bytes.Buffer
	bw := brotli.NewWriterLevel(&buf, brotli.BestCompression)
	if _, err := bw.Write(b); err != nil {
		return nil
	}
	if err := bw.Close(); err != nil {
		return nil
	}
	if buf.Len() >= len(b) {
		return nil
	}
	return buf.Bytes()
}

// newAsset builds an Asset, precomputing its compressed variants once at startup.
func newAsset(name string, b []byte, contentType, version string) Asset {
	return Asset{
		Name:        name,
		Bytes:       b,
		Brotli:      brotliBytes(b),
		Gzip:        gzipBytes(b),
		ContentType: contentType,
		Version:     version,
	}
}

// wasmExecShimHash content-hashes the TinyGo wasm_exec shim locally: unlike the
// other assets, wasmexec exposes only the bytes (Shim), not a Version(), so we
// compute its cache-buster here.
var wasmExecShimHash = hash16(wasmexec.Shim)

// registry is the name→Asset lookup. Bytes and hashes are pulled from the
// asset-owning leaf packages so there is a single source of truth per artifact.
var registry = func() map[string]Asset {
	list := []Asset{
		newAsset(gothiccore.FileName, gothiccore.Minified(), contentTypeJS, gothiccore.Version()),       // gothic-core.js (minified)
		newAsset(corewasm.WASMFileName, corewasm.CoreWASM(), contentTypeWASM, corewasm.CoreHash()),      // gothic-core.wasm
		newAsset(corewasm.ExecFileName, corewasm.ExecJS(), contentTypeJS, corewasm.ExecHash()),          // gothic-core-exec.js
		newAsset(corewasm.BootFileName, corewasm.BootJS(), contentTypeJS, corewasm.Version()),           // gothic-core-boot.js
		newAsset("wasm_exec.js", wasmexec.Shim, contentTypeJS, wasmExecShimHash),                        // TinyGo shim
		newAsset(vendorjs.HtmxFileName, vendorjs.HtmxJS(), contentTypeJS, vendorjs.HtmxVersion()), // htmx.min.js (self-hosted)
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

// negotiateEncoding picks the best Content-Encoding the client accepts from the
// two we precompute: brotli first (smaller on our assets), then gzip. It honors
// an explicit q=0 (a client opting an encoding OUT) and the "*" wildcard, and
// returns "" (serve identity) when neither is acceptable.
func negotiateEncoding(accept string) string {
	if accept == "" {
		return ""
	}
	var br, gz bool
	for _, part := range strings.Split(accept, ",") {
		tok := strings.TrimSpace(part)
		if tok == "" {
			continue
		}
		name := tok
		if i := strings.IndexByte(tok, ';'); i >= 0 {
			name = strings.TrimSpace(tok[:i])
			rest := tok[i+1:]
			if j := strings.Index(strings.ToLower(rest), "q="); j >= 0 {
				if q, err := strconv.ParseFloat(strings.TrimSpace(rest[j+2:]), 64); err == nil && q == 0 {
					continue // this encoding explicitly disallowed
				}
			}
		}
		switch strings.ToLower(name) {
		case "br":
			br = true
		case "gzip":
			gz = true
		case "*":
			br, gz = true, true
		}
	}
	if br {
		return "br"
	}
	if gz {
		return "gzip"
	}
	return ""
}

// Handler serves the runtime assets under Prefix. It strips the prefix, looks the
// file up in the registry, sets the right Content-Type, marks it immutable when a
// ?v= cache-buster is present (mirroring immutableCacheMiddleware), negotiates a
// compressed encoding from Accept-Encoding, and writes the bytes. Unknown names
// 404. Only GET/HEAD are meaningful; other methods 405.
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
		h := w.Header()
		h.Set("Content-Type", asset.ContentType)
		// Response varies by Accept-Encoding (we serve br/gzip/identity of the
		// same URL), so caches must key on it.
		h.Set("Vary", "Accept-Encoding")
		// A ?v=<hash> cache-buster means the URL is content-addressed: cache it
		// immutably for a year. A framework upgrade changes the hash (hence the
		// URL), so the cache is busted automatically.
		if r.URL.Query().Get("v") != "" {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		body := asset.Bytes
		switch negotiateEncoding(r.Header.Get("Accept-Encoding")) {
		case "br":
			if asset.Brotli != nil {
				h.Set("Content-Encoding", "br")
				body = asset.Brotli
			}
		case "gzip":
			if asset.Gzip != nil {
				h.Set("Content-Encoding", "gzip")
				body = asset.Gzip
			}
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		w.Write(body)
	})
}
