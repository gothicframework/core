package helpers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

func TestNormalizeHttpPath(t *testing.T) {
	helper := NewFileBasedRouteHelper()

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "pages index route",
			path:     "src/pages/index_templ.go",
			expected: "/",
		},
		{
			name:     "pages about route",
			path:     "src/pages/about_templ.go",
			expected: "/about",
		},
		{
			name:     "pages nested route",
			path:     "src/pages/blog/post_templ.go",
			expected: "/blog/post",
		},
		{
			name:     "pages nested index route",
			path:     "src/pages/blog/index_templ.go",
			expected: "/blog",
		},
		{
			name:     "pages dynamic route param",
			path:     "src/pages/blog/var_id_templ.go",
			expected: "/blog/{id}",
		},
		{
			name:     "components route",
			path:     "src/components/navbar_templ.go",
			expected: "/components/navbar",
		},
		{
			name:     "api route with .go extension",
			path:     "src/api/users.go",
			expected: "/api/users",
		},
		{
			name:     "api nested route",
			path:     "src/api/v1/health.go",
			expected: "/api/v1/health",
		},
		{
			name:     "api dynamic route param",
			path:     "src/api/users/var_id.go",
			expected: "/api/users/{id}",
		},
		{
			name:     "api nested dynamic route param",
			path:     "src/api/v1/posts/var_postId.go",
			expected: "/api/v1/posts/{postId}",
		},
		{
			name:     "regex anchor: simple var_ prefix",
			path:     "src/pages/var_foo",
			expected: "/{foo}",
		},
		{
			name:     "regex anchor: adjacent var_ tokens treated as one identifier",
			path:     "src/pages/var_foovar_bar",
			expected: "/{foovar_bar}",
		},
		{
			name:     "regex anchor: identifier starting with digit is not a param",
			path:     "src/pages/var_0bad",
			expected: "/var_0bad",
		},
		{
			name:     "regex anchor: no word boundary before var_ means no match",
			path:     "src/pages/_var_hidden",
			expected: "/_var_hidden",
		},
		{
			name:     "deeply nested pages",
			path:     "src/pages/admin/settings/profile_templ.go",
			expected: "/admin/settings/profile",
		},
		{
			name:     "root index only",
			path:     "src/pages/index_templ.go",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := helper.normalizeHttpPath(tt.path)
			if got != tt.expected {
				t.Errorf("normalizeHttpPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestRemoveDuplicates(t *testing.T) {
	helper := NewFileBasedRouteHelper()
	helper.TemplateInfo.Imports = []Imports{
		{Package: "pages", PackagePath: "example.com/src/pages"},
		{Package: "pages", PackagePath: "example.com/src/pages"},
		{Package: "components", PackagePath: "example.com/src/components"},
	}
	helper.TemplateInfo.Routes = []RouteTemplate{
		{ConfigName: "DefaultConfig", PackageName: "pages"},
	}

	helper.RemoveDuplicates()

	if !helper.TemplateInfo.ImportDefault {
		t.Error("expected ImportDefault to be true when DefaultConfig is used")
	}

	if len(helper.TemplateInfo.Imports) != 2 {
		t.Errorf("expected 2 unique imports, got %d", len(helper.TemplateInfo.Imports))
	}
}

func TestInitialize(t *testing.T) {
	helper := NewFileBasedRouteHelper()
	helper.TemplateInfo.Routes = []RouteTemplate{{FunctionName: "old"}}
	helper.TemplateInfo.ApiRoutes = []RouteTemplate{{FunctionName: "old"}}
	helper.TemplateInfo.ImportDefault = true

	helper.Initialize("example.com/mymod")

	if len(helper.TemplateInfo.Routes) != 0 {
		t.Error("expected Routes to be empty after Initialize")
	}
	if len(helper.TemplateInfo.ApiRoutes) != 0 {
		t.Error("expected ApiRoutes to be empty after Initialize")
	}
	if helper.TemplateInfo.GoModName != "example.com/mymod" {
		t.Errorf("expected GoModName to be 'example.com/mymod', got %q", helper.TemplateInfo.GoModName)
	}
	if helper.TemplateInfo.ImportDefault {
		t.Error("expected ImportDefault to be false after Initialize")
	}
}

// mockComponent returns a templ.Component that writes the given HTML string.
func mockComponent(html string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := w.Write([]byte(html))
		return err
	})
}

func TestRegisterRouteStaticCACHE_CONTROL_HEADERS(t *testing.T) {
	// CACHE_CONTROL_HEADERS (default): should set Cache-Control header
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			return "hello"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/test", func(props string) templ.Component {
		return mockComponent("<p>" + props + "</p>")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "max-age=31536000" {
		t.Errorf("expected Cache-Control max-age=31536000, got %q", cc)
	}
	if body := rec.Body.String(); body != "<p>hello</p>" {
		t.Errorf("expected '<p>hello</p>', got %q", body)
	}

	resetGlobalCache()
}

func TestRegisterRouteStaticInMemory(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			return "cached"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/mem", func(props string) templ.Component {
		return mockComponent("<div>" + props + "</div>")
	})

	// First request: cache miss
	req := httptest.NewRequest("GET", "/mem", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != "<div>cached</div>" {
		t.Errorf("first request: expected '<div>cached</div>', got %q", body)
	}

	// Second request: cache hit (same content)
	req2 := httptest.NewRequest("GET", "/mem", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if body := rec2.Body.String(); body != "<div>cached</div>" {
		t.Errorf("second request: expected '<div>cached</div>', got %q", body)
	}

	resetGlobalCache()
}

func TestRegisterRouteDynamic(t *testing.T) {
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	callCount := 0
	config := RouteConfig[int]{
		Type:       DYNAMIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) int {
			callCount++
			return callCount
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/dyn", func(props int) templ.Component {
		return mockComponent("<span>dynamic</span>")
	})

	// Each request should call middleware
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/dyn", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	if callCount != 3 {
		t.Errorf("expected middleware called 3 times, got %d", callCount)
	}

	resetGlobalCache()
}

func TestRegisterRouteISRInMemory(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	callCount := 0
	config := RouteConfig[string]{
		Type:            ISR,
		HttpMethod:      GET,
		RevalidateInSec: 60,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			callCount++
			return "isr-data"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/isr", func(props string) templ.Component {
		return mockComponent("<h1>" + props + "</h1>")
	})

	// First request: cache miss
	req := httptest.NewRequest("GET", "/isr", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != "<h1>isr-data</h1>" {
		t.Errorf("expected '<h1>isr-data</h1>', got %q", body)
	}

	// Second request: cache hit (middleware not called again)
	req2 := httptest.NewRequest("GET", "/isr", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected middleware called once (cached), got %d", callCount)
	}
	if body := rec2.Body.String(); body != "<h1>isr-data</h1>" {
		t.Errorf("cache hit: expected '<h1>isr-data</h1>', got %q", body)
	}

	resetGlobalCache()
}

func TestRegisterRouteStaticLocalFiles(t *testing.T) {
	tmpDir := t.TempDir()
	resetGlobalCache()
	InitCache(LOCAL_FILES, &CacheConfig{CacheFilesPath: tmpDir})

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			return "file-cached"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/files", func(props string) templ.Component {
		return mockComponent("<p>" + props + "</p>")
	})

	req := httptest.NewRequest("GET", "/files", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != "<p>file-cached</p>" {
		t.Errorf("expected '<p>file-cached</p>', got %q", body)
	}

	// Second request hits file cache
	req2 := httptest.NewRequest("GET", "/files", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if body := rec2.Body.String(); body != "<p>file-cached</p>" {
		t.Errorf("cache hit: expected '<p>file-cached</p>', got %q", body)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteDynamic(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	callCount := 0
	config := ApiRouteConfig{
		Type:       DYNAMIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/dyn", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"count":` + fmt.Sprintf("%d", callCount) + `}`))
	})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/dyn", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	if callCount != 3 {
		t.Errorf("expected handler called 3 times for DYNAMIC, got %d", callCount)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteStaticCACHE_CONTROL_HEADERS(t *testing.T) {
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/static-cc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequest("GET", "/api/static-cc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "max-age=31536000" {
		t.Errorf("expected Cache-Control max-age=31536000, got %q", cc)
	}
	if body := rec.Body.String(); body != `{"ok":true}` {
		t.Errorf("expected '{\"ok\":true}', got %q", body)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteStaticInMemory(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	callCount := 0
	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/static-mem", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"cached":"yes"}`))
	})

	// First request: cache miss
	req := httptest.NewRequest("GET", "/api/static-mem", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != `{"cached":"yes"}` {
		t.Errorf("first request: expected '{\"cached\":\"yes\"}', got %q", body)
	}

	// Second request: cache hit (handler not called again)
	req2 := httptest.NewRequest("GET", "/api/static-mem", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected handler called once (cached), got %d", callCount)
	}
	if body := rec2.Body.String(); body != `{"cached":"yes"}` {
		t.Errorf("cache hit: expected '{\"cached\":\"yes\"}', got %q", body)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteISRInMemory(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	callCount := 0
	config := ApiRouteConfig{
		Type:            ISR,
		HttpMethod:      GET,
		RevalidateInSec: 60,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/isr-mem", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"isr":"data"}`))
	})

	// First request: cache miss
	req := httptest.NewRequest("GET", "/api/isr-mem", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != `{"isr":"data"}` {
		t.Errorf("expected '{\"isr\":\"data\"}', got %q", body)
	}

	// Second request: cache hit
	req2 := httptest.NewRequest("GET", "/api/isr-mem", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected handler called once (cached), got %d", callCount)
	}
	if body := rec2.Body.String(); body != `{"isr":"data"}` {
		t.Errorf("cache hit: expected '{\"isr\":\"data\"}', got %q", body)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteISRCACHE_CONTROL_HEADERS(t *testing.T) {
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	config := ApiRouteConfig{
		Type:            ISR,
		HttpMethod:      GET,
		RevalidateInSec: 30,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/isr-cc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequest("GET", "/api/isr-cc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	expected := "max-age=30, stale-while-revalidate=30, stale-if-error=30"
	if cc := rec.Header().Get("Cache-Control"); cc != expected {
		t.Errorf("expected Cache-Control %q, got %q", expected, cc)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteStaticLocalFiles(t *testing.T) {
	tmpDir := t.TempDir()
	resetGlobalCache()
	InitCache(LOCAL_FILES, &CacheConfig{CacheFilesPath: tmpDir})

	callCount := 0
	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/files", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"file":"cached"}`))
	})

	req := httptest.NewRequest("GET", "/api/files", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if body := rec.Body.String(); body != `{"file":"cached"}` {
		t.Errorf("expected '{\"file\":\"cached\"}', got %q", body)
	}

	// Second request: cache hit
	req2 := httptest.NewRequest("GET", "/api/files", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected handler called once (file cached), got %d", callCount)
	}
	if body := rec2.Body.String(); body != `{"file":"cached"}` {
		t.Errorf("cache hit: expected '{\"file\":\"cached\"}', got %q", body)
	}

	resetGlobalCache()
}

func TestApiCachedResponsePreservesStatusCodeAndContentType(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: POST,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/create", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"123"}`))
	})

	// First request: cache miss
	req := httptest.NewRequest("POST", "/api/create", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("first request: expected 201, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("first request: expected Content-Type application/json, got %q", ct)
	}

	// Second request: cache hit — should preserve status code and content type
	req2 := httptest.NewRequest("POST", "/api/create", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Errorf("cache hit: expected 201, got %d", rec2.Code)
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("cache hit: expected Content-Type application/json, got %q", ct)
	}
	if body := rec2.Body.String(); body != `{"id":"123"}` {
		t.Errorf("cache hit: expected '{\"id\":\"123\"}', got %q", body)
	}

	resetGlobalCache()
}

func TestApiRegisterRouteHTTPMethods(t *testing.T) {
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	methods := []struct {
		method     HttpMethod
		httpMethod string
	}{
		{GET, "GET"},
		{POST, "POST"},
		{PUT, "PUT"},
		{PATCH, "PATCH"},
		{DELETE, "DELETE"},
	}

	for _, m := range methods {
		t.Run(m.httpMethod, func(t *testing.T) {
			config := ApiRouteConfig{
				Type:       DYNAMIC,
				HttpMethod: m.method,
			}

			r := chi.NewRouter()
			config.RegisterRoute(r, "/api/method-test", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			})

			req := httptest.NewRequest(m.httpMethod, "/api/method-test", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", m.httpMethod, rec.Code)
			}
		})
	}

	resetGlobalCache()
}

func TestRegisterRouteHTTPMethods(t *testing.T) {
	resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)

	methods := []struct {
		method     HttpMethod
		httpMethod string
	}{
		{GET, "GET"},
		{POST, "POST"},
		{PUT, "PUT"},
		{PATCH, "PATCH"},
		{DELETE, "DELETE"},
	}

	for _, m := range methods {
		t.Run(m.httpMethod, func(t *testing.T) {
			config := RouteConfig[any]{
				Type:       DYNAMIC,
				HttpMethod: m.method,
				Middleware: func(w http.ResponseWriter, r *http.Request) any {
					return nil
				},
			}

			r := chi.NewRouter()
			config.RegisterRoute(r, "/method-test", func(props any) templ.Component {
				return mockComponent("ok")
			})

			req := httptest.NewRequest(m.httpMethod, "/method-test", nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d", m.httpMethod, rec.Code)
			}
		})
	}

	resetGlobalCache()
}

// errorComponent returns a templ.Component that always fails.
func errorComponent() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return fmt.Errorf("simulated render failure")
	})
}

// --- Middleware abort semantics -------------------------------------------------

// TestStaticMiddlewareAbortNotRendered asserts that when the middleware writes
// status >= 300 on a STATIC route, the component is NOT rendered — only the
// middleware's own response reaches the client.
func TestStaticMiddlewareAbortNotRendered(t *testing.T) {
	t.Run("IN_MEMORY", func(t *testing.T) {
		resetGlobalCache()
		InitCache(IN_MEMORY, nil)
		defer resetGlobalCache()

		config := RouteConfig[string]{
			Type:       STATIC,
			HttpMethod: GET,
			Middleware: func(w http.ResponseWriter, r *http.Request) string {
				if r.URL.Query().Get("auth") != "1" {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return ""
				}
				return "authorized"
			},
		}

		r := chi.NewRouter()
		config.RegisterRoute(r, "/test", func(props string) templ.Component {
			return mockComponent("<main>component</main>")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
		// The component's <main> marker must NOT appear — only the MW body.
		if rec.Body.String() != "Forbidden\n" {
			t.Errorf("expected MW body 'Forbidden\\n', got %q", rec.Body.String())
		}
	})

	t.Run("CACHE_CONTROL_HEADERS", func(t *testing.T) {
		resetGlobalCache()
		InitCache(CACHE_CONTROL_HEADERS, nil)
		defer resetGlobalCache()

		config := RouteConfig[string]{
			Type:       STATIC,
			HttpMethod: GET,
			Middleware: func(w http.ResponseWriter, r *http.Request) string {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return ""
			},
		}

		r := chi.NewRouter()
		config.RegisterRoute(r, "/test-cc", func(props string) templ.Component {
			return mockComponent("<main>component</main>")
		})

		req := httptest.NewRequest("GET", "/test-cc", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
		if rec.Body.String() != "Unauthorized\n" {
			t.Errorf("expected MW body 'Unauthorized\\n', got %q", rec.Body.String())
		}
	})
}

// TestStaticMiddlewareSkippedOnCacheHit asserts that the middleware does NOT run
// on a cache hit (STATIC/ISR with store-backed cache). This is THE CONTRACT:
// middleware is a props-loader, not an auth hook.
func TestStaticMiddlewareSkippedOnCacheHit(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0

	// The middleware would reject on >1 call, but after a cache hit it must not run.
	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			callCount++
			if callCount > 1 {
				http.Error(w, "Must not be called", http.StatusInternalServerError)
				return ""
			}
			return "first-call"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/cached", func(props string) templ.Component {
		return mockComponent("<p>" + props + "</p>")
	})

	// First request: miss, MW runs, returns 200
	req := httptest.NewRequest("GET", "/cached", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "<p>first-call</p>" {
		t.Errorf("expected '<p>first-call</p>', got %q", rec.Body.String())
	}

	// Second request: cache hit, MW must NOT run
	req2 := httptest.NewRequest("GET", "/cached", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected MW called once (cached), got %d", callCount)
	}
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 on cache hit, got %d", rec2.Code)
	}
	if rec2.Body.String() != "<p>first-call</p>" {
		t.Errorf("cache hit: expected '<p>first-call</p>', got %q", rec2.Body.String())
	}
}

// TestStaticMiddlewareAbortNotCached asserts that a middleware abort (>= 300)
// is NOT cached. A subsequent request that succeeds IS cached.
func TestStaticMiddlewareAbortNotCached(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			callCount++
			if callCount == 1 {
				http.Error(w, "Rejected", http.StatusForbidden)
				return ""
			}
			return "accepted"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/test", func(props string) templ.Component {
		return mockComponent("<main>" + props + "</main>")
	})

	// Request 1: MW rejects with 403 → NOT cached
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	if rec.Body.String() != "Rejected\n" {
		t.Errorf("expected 'Rejected\\n', got %q", rec.Body.String())
	}

	// Request 2: MW runs again (not cached), returns 200 → cached
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if callCount != 2 {
		t.Errorf("expected MW called 2 times (not cached), got %d", callCount)
	}
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec2.Code)
	}
	if rec2.Body.String() != "<main>accepted</main>" {
		t.Errorf("expected '<main>accepted</main>', got %q", rec2.Body.String())
	}

	// Request 3: cache hit from request 2
	req3 := httptest.NewRequest("GET", "/test", nil)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)

	if callCount != 2 {
		t.Errorf("expected MW still called 2 times (3rd cached), got %d", callCount)
	}
	if rec3.Body.String() != "<main>accepted</main>" {
		t.Errorf("cache hit: expected '<main>accepted</main>', got %q", rec3.Body.String())
	}
}

// TestStaticAbortWarnsYellowOnce asserts that the first >= 400 abort on a STATIC
// route prints a single yellow ANSI warning to stderr, and subsequent aborts
// for the same path are silent.
func TestStaticAbortWarnsYellowOnce(t *testing.T) {
	// Reset the warn map for a clean test.
	warnStaticAuthOnce = sync.Map{}

	// Capture stderr.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writer

	resetGlobalCache()
	InitCache(IN_MEMORY, nil)

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return ""
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/warn-test", func(props string) templ.Component {
		return mockComponent("<main>content</main>")
	})

	// Two 403 requests for the same path.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/warn-test", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}

	writer.Close()
	warnOutput, _ := io.ReadAll(reader)
	os.Stderr = oldStderr

	lines := strings.Split(string(warnOutput), "\n")
	var warnLines int
	for _, line := range lines {
		if strings.Contains(line, "\033[33m") {
			warnLines++
		}
	}
	if warnLines != 1 {
		t.Errorf("expected exactly 1 yellow warn line, got %d; stderr:\n%s", warnLines, string(warnOutput))
	}
	// The warning has to name the route and point at the fix, or it teaches nothing.
	if !strings.Contains(string(warnOutput), "/warn-test") {
		t.Errorf("warning does not name the route path; stderr:\n%s", string(warnOutput))
	}
	if !strings.Contains(string(warnOutput), "DYNAMIC") {
		t.Errorf("warning does not point at DYNAMIC as the fix; stderr:\n%s", string(warnOutput))
	}
}

// TestStaticRedirectAbort asserts that a 302 redirect from middleware stops
// rendering (no component marker), preserves the Location header, and does NOT
// trigger the static-auth warning (302 < 400).
func TestStaticRedirectAbort(t *testing.T) {
	warnStaticAuthOnce = sync.Map{}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writer

	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer func() {
		resetGlobalCache()
		writer.Close()
		os.Stderr = oldStderr
	}()

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			http.Redirect(w, r, "/login", http.StatusFound)
			return ""
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/protected", func(props string) templ.Component {
		return mockComponent("<main>secret</main>")
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("expected Location: /login, got %q", loc)
	}
	// The component marker must NOT appear in the body.
	if strings.Contains(rec.Body.String(), "<main>secret</main>") {
		t.Errorf("component must not render after redirect, body: %q", rec.Body.String())
	}

	// Collect stderr — must contain no yellow warning (302 < 400).
	writer.Close()
	warnOutput, _ := io.ReadAll(reader)
	os.Stderr = oldStderr

	if strings.Contains(string(warnOutput), "\033[33m") {
		t.Errorf("redirect abort must not produce a yellow warning, got: %s", string(warnOutput))
	}
}

// TestDynamicMiddlewareAbort asserts that a DYNAMIC route with middleware
// writing >= 300 aborts without rendering and without a yellow warning.
func TestDynamicMiddlewareAbort(t *testing.T) {
	warnStaticAuthOnce = sync.Map{}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writer

	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer func() {
		resetGlobalCache()
		writer.Close()
		os.Stderr = oldStderr
	}()

	config := RouteConfig[string]{
		Type:       DYNAMIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return ""
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/dyn-protected", func(props string) templ.Component {
		return mockComponent("<dynamic>content</dynamic>")
	})

	req := httptest.NewRequest("GET", "/dyn-protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	if rec.Body.String() != "Forbidden\n" {
		t.Errorf("expected MW body 'Forbidden\\n', got %q", rec.Body.String())
	}

	// No yellow warning for DYNAMIC routes.
	writer.Close()
	warnOutput, _ := io.ReadAll(reader)
	os.Stderr = oldStderr

	if strings.Contains(string(warnOutput), "\033[33m") {
		t.Errorf("DYNAMIC abort must not produce a yellow warning, got: %s", string(warnOutput))
	}
}

// TestStaticRenderErrorReturns500 asserts that when the component render fails
// on a cache miss, the response is 500 and the cache is NOT populated.
func TestStaticRenderErrorReturns500(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	config := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string {
			return "props"
		},
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/broken", func(props string) templ.Component {
		return errorComponent()
	})

	// First request: render fails → 500, nothing cached
	req := httptest.NewRequest("GET", "/broken", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on render error, got %d", rec.Code)
	}

	// Second request: nothing cached, so we hit the handler again.
	req2 := httptest.NewRequest("GET", "/broken", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on second render error, got %d", rec2.Code)
	}
}

// --- API store.Set guard (5xx not cached, 4xx stays cacheable) ------------------

// TestApiStaticCaches2xx asserts that a 200 API response is cached (handler
// called only once).
func TestApiStaticCaches2xx(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0
	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/ok", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	// Two requests → handler runs once (second is cache hit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/ok", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	if callCount != 1 {
		t.Errorf("expected handler called 1 time (cached), got %d", callCount)
	}
}

// TestApiStaticCaches4xx asserts that a 404 API response IS cached (4xx stays
// cacheable per spec).
func TestApiStaticCaches4xx(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0
	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/notfound", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/notfound", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("request %d: expected 404, got %d", i+1, rec.Code)
		}
	}

	if callCount != 1 {
		t.Errorf("expected handler called 1 time (4xx cached), got %d", callCount)
	}
}

// TestApiStaticDoesNotCache5xx asserts that a 500 API response is NOT cached
// (handler runs on every request).
func TestApiStaticDoesNotCache5xx(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0
	config := ApiRouteConfig{
		Type:       STATIC,
		HttpMethod: GET,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/error", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/error", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("request %d: expected 500, got %d", i+1, rec.Code)
		}
	}

	if callCount != 2 {
		t.Errorf("expected handler called 2 times (5xx not cached), got %d", callCount)
	}
}

// TestApiISRDoesNotCache5xx asserts that the same 5xx guard works for ISR API
// routes.
func TestApiISRDoesNotCache5xx(t *testing.T) {
	resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	defer resetGlobalCache()

	callCount := 0
	config := ApiRouteConfig{
		Type:            ISR,
		HttpMethod:      GET,
		RevalidateInSec: 60,
	}

	r := chi.NewRouter()
	config.RegisterRoute(r, "/api/isr-error", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"unavailable"}`))
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/isr-error", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("request %d: expected 503, got %d", i+1, rec.Code)
		}
	}

	if callCount != 2 {
		t.Errorf("expected handler called 2 times (5xx not cached on ISR), got %d", callCount)
	}
}

// --- statusCapture passthrough tests -------------------------------------------

// TestStatusCapturePassthrough verifies that statusCapture properly delegates
// Flusher and Unwrap (for ResponseController).
func TestStatusCapturePassthrough(t *testing.T) {
	// httptest.ResponseRecorder implements http.Flusher.
	rec := httptest.NewRecorder()
	sc := &statusCapture{ResponseWriter: rec}

	// Verify Flusher is implemented (compile-time check via interface assertion).
	var flusher http.Flusher = sc
	flusher.Flush() // must not panic

	// ResponseController reaches the recorder through Unwrap. httptest's recorder
	// implements Flusher, so this must succeed — an error means the chain broke.
	rc := http.NewResponseController(sc)
	if err := rc.Flush(); err != nil {
		t.Errorf("ResponseController.Flush through the wrapper: %v", err)
	}

	// WriteHeader captures first status.
	sc.WriteHeader(http.StatusCreated)
	if sc.Status() != http.StatusCreated {
		t.Errorf("expected status 201, got %d", sc.Status())
	}

	// Write does not overwrite captured status.
	sc.Write([]byte("hello"))
	if sc.Status() != http.StatusCreated {
		t.Errorf("Write must not change captured status, got %d", sc.Status())
	}

	// A fresh statusCapture with Write only gets 200.
	sc2 := &statusCapture{ResponseWriter: httptest.NewRecorder()}
	sc2.Write([]byte("data"))
	if sc2.Status() != http.StatusOK {
		t.Errorf("expected 200 after Write only, got %d", sc2.Status())
	}

	// Unwrap returns the underlying writer.
	if sc.Unwrap() != rec {
		t.Error("Unwrap should return the original ResponseWriter")
	}
}

// hijackableRecorder is an httptest.ResponseRecorder that also implements
// http.Hijacker, so the delegation path can be observed.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// TestStatusCaptureHijack covers the passthrough that protects WebSocket and
// other connection-upgrade middleware: a wrapper that swallows Hijack turns
// those into a silent failure at runtime.
func TestStatusCaptureHijack(t *testing.T) {
	t.Run("delegates when the underlying writer supports it", func(t *testing.T) {
		rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
		sc := &statusCapture{ResponseWriter: rec}

		var hijacker http.Hijacker = sc
		if _, _, err := hijacker.Hijack(); err != nil {
			t.Errorf("Hijack should delegate without error, got %v", err)
		}
		if !rec.hijacked {
			t.Error("Hijack did not reach the underlying writer")
		}
	})

	t.Run("reports an error when the underlying writer does not", func(t *testing.T) {
		// httptest.ResponseRecorder is not an http.Hijacker.
		sc := &statusCapture{ResponseWriter: httptest.NewRecorder()}
		if _, _, err := sc.Hijack(); err == nil {
			t.Error("Hijack must return an error when the underlying writer cannot hijack")
		}
	})
}
