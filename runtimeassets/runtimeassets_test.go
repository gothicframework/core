package runtimeassets

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gothicframework/core/corewasm"
	"github.com/gothicframework/core/gothiccore"
	"github.com/gothicframework/core/vendorjs"
	wasmexec "github.com/gothicframework/core/wasmexec"
)

// TestRegistryHasAllAssets guards that every runtime asset the framework serves
// from its embed (the ones that used to be copied into public/, plus the
// self-hosted third-party scripts) is registered.
func TestRegistryHasAllAssets(t *testing.T) {
	want := []string{
		gothiccore.FileName,     // gothic-core.js
		corewasm.WASMFileName,   // gothic-core.wasm
		corewasm.ExecFileName,   // gothic-core-exec.js
		corewasm.BootFileName,   // gothic-core-boot.js
		"wasm_exec.js",          // TinyGo shim
		vendorjs.HtmxFileName,   // htmx.min.js (self-hosted)
		vendorjs.AmzExtFileName, // amz-content-sha256.min.js
	}
	if len(All()) != len(want) {
		t.Errorf("registry has %d assets, want %d", len(All()), len(want))
	}
	for _, name := range want {
		a, ok := Get(name)
		if !ok {
			t.Errorf("asset %q not registered", name)
			continue
		}
		if len(a.Bytes) == 0 {
			t.Errorf("asset %q has no bytes", name)
		}
		if a.ContentType == "" || a.Version == "" {
			t.Errorf("asset %q missing content-type or version", name)
		}
	}
}

// TestAssetBytesMatchSources pins the served bytes to their owning packages so a
// registry wiring mistake (serving the wrong file) is caught.
func TestAssetBytesMatchSources(t *testing.T) {
	cases := map[string]struct {
		want []byte
		ct   string
	}{
		gothiccore.FileName:     {gothiccore.Minified(), contentTypeJS},
		corewasm.WASMFileName:   {corewasm.CoreWASM(), contentTypeWASM},
		corewasm.ExecFileName:   {corewasm.ExecJS(), contentTypeJS},
		corewasm.BootFileName:   {corewasm.BootJS(), contentTypeJS},
		"wasm_exec.js":          {wasmexec.Shim, contentTypeJS},
		vendorjs.HtmxFileName:   {vendorjs.HtmxJS(), contentTypeJS},
		vendorjs.AmzExtFileName: {vendorjs.AmzExtJS(), contentTypeJS},
	}
	for name, c := range cases {
		a, _ := Get(name)
		if string(a.Bytes) != string(c.want) {
			t.Errorf("%s: served bytes do not match source", name)
		}
		if a.ContentType != c.ct {
			t.Errorf("%s: content-type %q, want %q", name, a.ContentType, c.ct)
		}
	}
}

// TestHandlerServesAssetImmutable verifies a versioned request gets the bytes,
// the right Content-Type, and the immutable cache header.
func TestHandlerServesAssetImmutable(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, Prefix+gothiccore.FileName+"?v="+gothiccore.Version(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJS {
		t.Errorf("Content-Type %q, want %q", ct, contentTypeJS)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q, want immutable", cc)
	}
	// No Accept-Encoding on the request → identity bytes (the minified source).
	if enc := rec.Header().Get("Content-Encoding"); enc != "" {
		t.Errorf("Content-Encoding %q, want identity (no Accept-Encoding sent)", enc)
	}
	if rec.Body.String() != string(gothiccore.Minified()) {
		t.Errorf("served body does not match minified gothic-core.js")
	}
}

// TestHandlerNegotiatesCompression verifies the handler serves the brotli/gzip
// variant when the client accepts it (the lever that shrinks the ~1.9 MB core
// wasm over the wire), advertises Vary, and decompresses back to the raw bytes.
func TestHandlerNegotiatesCompression(t *testing.T) {
	h := Handler()

	// brotli preferred over gzip when both are offered.
	req := httptest.NewRequest(http.MethodGet, Prefix+corewasm.WASMFileName+"?v="+corewasm.CoreHash(), nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if enc := rec.Header().Get("Content-Encoding"); enc != "br" {
		t.Errorf("Content-Encoding %q, want br", enc)
	}
	if v := rec.Header().Get("Vary"); v != "Accept-Encoding" {
		t.Errorf("Vary %q, want Accept-Encoding", v)
	}
	if rec.Body.Len() >= len(corewasm.CoreWASM()) {
		t.Errorf("brotli body (%d) not smaller than raw wasm (%d)", rec.Body.Len(), len(corewasm.CoreWASM()))
	}
	br := brotli.NewReader(bytes.NewReader(rec.Body.Bytes()))
	got, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("brotli decode: %v", err)
	}
	if !bytes.Equal(got, corewasm.CoreWASM()) {
		t.Errorf("decompressed brotli body does not match the raw wasm")
	}

	// gzip when br is opted out with q=0.
	req = httptest.NewRequest(http.MethodGet, Prefix+corewasm.WASMFileName, nil)
	req.Header.Set("Accept-Encoding", "br;q=0, gzip")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if enc := rec.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Errorf("Content-Encoding %q, want gzip (br opted out)", enc)
	}
}

// TestHandlerNoVersionNotImmutable verifies a request without ?v= is served but
// NOT marked immutable (mirrors immutableCacheMiddleware).
func TestHandlerNoVersionNotImmutable(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, Prefix+corewasm.WASMFileName, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeWASM {
		t.Errorf("Content-Type %q, want %q", ct, contentTypeWASM)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control should be empty without ?v=, got %q", cc)
	}
}

// TestHandlerUnknownNotFound verifies unknown names and traversal attempts 404.
func TestHandlerUnknownNotFound(t *testing.T) {
	h := Handler()
	for _, path := range []string{Prefix + "nope.js", Prefix + "sub/dir.js", Prefix} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
		}
	}
}
