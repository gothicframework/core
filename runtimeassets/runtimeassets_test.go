package runtimeassets

import (
	"net/http"
	"net/http/httptest"
	"testing"

	wasmexec "github.com/gothicframework/core/wasmexec"
	"github.com/gothicframework/core/corewasm"
	"github.com/gothicframework/core/gothiccore"
)

// TestRegistryHasAllFiveAssets guards that every runtime asset that used to be
// copied into public/ is registered and served from the framework embed.
func TestRegistryHasAllFiveAssets(t *testing.T) {
	want := []string{
		gothiccore.FileName,   // gothic-core.js
		corewasm.WASMFileName, // gothic-core.wasm
		corewasm.ExecFileName, // gothic-core-exec.js
		corewasm.BootFileName, // gothic-core-boot.js
		"wasm_exec.js",        // TinyGo shim
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
		gothiccore.FileName:   {[]byte(gothiccore.JS), contentTypeJS},
		corewasm.WASMFileName: {corewasm.CoreWASM(), contentTypeWASM},
		corewasm.ExecFileName: {corewasm.ExecJS(), contentTypeJS},
		corewasm.BootFileName: {corewasm.BootJS(), contentTypeJS},
		"wasm_exec.js":        {wasmexec.Shim, contentTypeJS},
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
	if rec.Body.String() != gothiccore.JS {
		t.Errorf("served body does not match gothic-core.js")
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
