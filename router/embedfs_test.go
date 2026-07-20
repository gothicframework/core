package helpers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"
)

// newEmbedTestFS builds an in-memory FS rooted at the public dir (its top-level
// entries are the files that would live inside public/), matching what the
// generated fs.Sub'd embed.FS provides to SetEmbeddedPublicFS.
func newEmbedTestFS() fstest.MapFS {
	return fstest.MapFS{
		"app.css":             {Data: []byte("body{color:red}")},
		"gothic-main.wasm.gz": {Data: []byte("\x1f\x8bfake-gzip-wasm")},
	}
}

func TestSetup_Embedded_ServesFromEmbedFS(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "") // non-dev
	SetEmbeddedPublicFS(newEmbedTestFS())
	defer SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	req := httptest.NewRequest("GET", "/public/app.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 serving embedded /public/app.css, got %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), []byte("body{color:red}")) {
		t.Errorf("embedded body mismatch: got %q", rec.Body.String())
	}
	resetGlobalCache()
}

func TestSetup_Embedded_WasmEncodingPreserved(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")
	SetEmbeddedPublicFS(newEmbedTestFS())
	defer SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	req := httptest.NewRequest("GET", "/public/gothic-main.wasm.gz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for embedded .wasm.gz, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("expected Content-Type application/wasm, got %q", ct)
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Errorf("expected Content-Encoding gzip, got %q", ce)
	}
	resetGlobalCache()
}

func TestSetup_Embedded_ImmutableCacheOnVersioned(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")
	SetEmbeddedPublicFS(newEmbedTestFS())
	defer SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	req := httptest.NewRequest("GET", "/public/app.css?v=abc123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for versioned embedded asset, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected immutable prod Cache-Control, got %q", cc)
	}
	resetGlobalCache()
}

func TestSetup_Embedded_PathTraversalSafe(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")
	SetEmbeddedPublicFS(newEmbedTestFS())
	defer SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	// Attempt to escape the embed root. io/fs rejects paths containing "..",
	// so this must never return the contents of a source file.
	req := httptest.NewRequest("GET", "/public/../setup.go", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("path traversal should not return 200, got 200 with body %q", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("package helpers")) {
		t.Error("path traversal leaked source file contents")
	}
	resetGlobalCache()
}

func TestSetup_Embedded_NilFS_DoesNotPanic(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "")
	// Simulate the setter never having run.
	SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	// Must not panic even though ServeStaticFiles==EMBEDDED and no FS registered.
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	req := httptest.NewRequest("GET", "/public/app.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("with nil embed FS no /public/* handler should serve 200, got %d", rec.Code)
	}
	resetGlobalCache()
}

func TestSetup_Embedded_DevStillServesDisk(t *testing.T) {
	resetGlobalCache()
	t.Setenv("GOTHIC_MODE", "dev")
	// Register an embed FS whose file only exists in the embed, not on disk.
	SetEmbeddedPublicFS(newEmbedTestFS())
	defer SetEmbeddedPublicFS(nil)

	r := chi.NewRouter()
	Setup(r, AppConfig{ServeStaticFiles: EMBEDDED}, func(r chi.Router) {})

	// Dev serves from disk (./public/), which has no app.css in this test env,
	// so the embed content must NOT be served — proving dev ignores the embed.
	req := httptest.NewRequest("GET", "/public/app.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && bytes.Equal(rec.Body.Bytes(), []byte("body{color:red}")) {
		t.Error("dev mode must serve from disk, not the embedded FS")
	}
	resetGlobalCache()
}
