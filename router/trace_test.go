package helpers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

// mockTraceHook installs a hook that appends every event to a shared slice.
// It restores TraceHook = nil and the slice is read under a mutex (requests
// in these tests are sequential, so ordering is deterministic).
func mockTraceHook(t *testing.T) func() []TraceEvent {
	t.Helper()
	var mu sync.Mutex
	var got []TraceEvent
	TraceHook = func(ev TraceEvent) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, ev)
	}
	t.Cleanup(func() {
		TraceHook = nil
		mu.Lock()
		got = nil
		mu.Unlock()
	})
	return func() []TraceEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]TraceEvent, len(got))
		copy(out, got)
		return out
	}
}

type stubComponent struct{ html string }

func (c stubComponent) Render(_ context.Context, w io.Writer) error {
	_, err := w.Write([]byte(c.html))
	return err
}

// stubPage is generic so it fits RouteConfig[T]'s component factory.
func stubPage[T any](html string) func(T) templ.Component {
	return func(T) templ.Component { return stubComponent{html: html} }
}

// TestTraceHookNilByDefault pins the shipped default: nothing is traced
// unless a tracer installs the hook.
func TestTraceHookNilByDefault(t *testing.T) {
	if TraceHook != nil {
		t.Fatal("TraceHook must default to nil")
	}
}

// TestTraceNilHookIdenticalOutput asserts that with the hook left nil every
// route mode still renders exactly the pre-trace output — the hook adds only
// a nil check.
func TestTraceNilHookIdenticalOutput(t *testing.T) {
	cases := []struct {
		name       string
		cacheType  CacheType
		routeType  ConfigType
		wantFirst  string
		wantSecond string
	}{
		{name: "dynamic", cacheType: CACHE_CONTROL_HEADERS, routeType: DYNAMIC, wantFirst: "<p>d</p>", wantSecond: "<p>d</p>"},
		{name: "static CCH", cacheType: CACHE_CONTROL_HEADERS, routeType: STATIC, wantFirst: "<p>s</p>", wantSecond: "<p>s</p>"},
		{name: "static in-memory", cacheType: IN_MEMORY, routeType: STATIC, wantFirst: "<p>s</p>", wantSecond: "<p>s</p>"},
		{name: "isr in-memory", cacheType: IN_MEMORY, routeType: ISR, wantFirst: "<p>i</p>", wantSecond: "<p>i</p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetGlobalCache()
			defer resetGlobalCache()
			InitCache(tc.cacheType, nil)
			if TraceHook != nil {
				t.Fatal("hook must stay nil for this test")
			}

			cfg := RouteConfig[any]{
				Type:       tc.routeType,
				HttpMethod: GET,
				Middleware: func(w http.ResponseWriter, r *http.Request) any { return nil },
			}
			r := chi.NewRouter()
			cfg.RegisterRoute(r, "/x", stubPage[any]("<p>"+map[ConfigType]string{DYNAMIC: "d", STATIC: "s", ISR: "i"}[tc.routeType]+"</p>"))

			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
			if rec.Body.String() != tc.wantFirst {
				t.Errorf("first response = %q, want %q", rec.Body.String(), tc.wantFirst)
			}
			rec2 := httptest.NewRecorder()
			r.ServeHTTP(rec2, httptest.NewRequest("GET", "/x", nil))
			if rec2.Body.String() != tc.wantSecond {
				t.Errorf("second response = %q, want %q", rec2.Body.String(), tc.wantSecond)
			}
		})
	}
}

// TestTraceHookStageOrderDynamic asserts middleware → render ordering for a
// DYNAMIC route, including the props value and rendered HTML sizes.
func TestTraceHookStageOrderDynamic(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)
	events := mockTraceHook(t)

	cfg := RouteConfig[string]{
		Type:       DYNAMIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string { return "props" },
	}
	r := chi.NewRouter()
	cfg.RegisterRoute(r, "/dyn", stubPage[string]("<p>props</p>"))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dyn", nil))

	got := events()
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Kind != "middleware" || got[0].Name != "DYNAMIC" {
		t.Errorf("event 0 = kind %q name %q, want middleware/DYNAMIC", got[0].Kind, got[0].Name)
	}
	if got[0].Out != "props" {
		t.Errorf("middleware Out = %q, want props", got[0].Out)
	}
	if got[0].In == "" {
		t.Error("middleware In must carry the request line")
	}
	if got[1].Kind != "render" || got[1].Name != "DYNAMIC" {
		t.Errorf("event 1 = kind %q name %q, want render/DYNAMIC", got[1].Kind, got[1].Name)
	}
	if got[1].Out != "<p>props</p>" || got[1].Bytes != len("<p>props</p>") {
		t.Errorf("render Out = %q Bytes = %d, want <p>props</p>/%d", got[1].Out, got[1].Bytes, len("<p>props</p>"))
	}
}

// TestTraceHookStaticMissAndHit walks a STATIC route twice over the in-memory
// store: first request is middleware + render(miss), second is a single
// render(hit) with the middleware skipped.
func TestTraceHookStaticMissAndHit(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	events := mockTraceHook(t)

	cfg := RouteConfig[string]{
		Type:       STATIC,
		HttpMethod: GET,
		Middleware: func(w http.ResponseWriter, r *http.Request) string { return "s" },
	}
	r := chi.NewRouter()
	cfg.RegisterRoute(r, "/s", stubPage[string]("<p>s</p>"))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/s", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/s", nil))

	got := events()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Kind != "middleware" || got[0].Name != "STATIC" {
		t.Errorf("event 0 = %s/%s, want middleware/STATIC", got[0].Kind, got[0].Name)
	}
	if got[1].Kind != "render" || got[1].Cache != "miss" || got[1].Out != "<p>s</p>" || got[1].Bytes != len("<p>s</p>") {
		t.Errorf("event 1 = %+v, want render miss <p>s</p>", got[1])
	}
	if got[2].Kind != "render" || got[2].Cache != "hit" || got[2].Out != "<p>s</p>" || got[2].Bytes != len("<p>s</p>") {
		t.Errorf("event 2 = %+v, want render hit <p>s</p>", got[2])
	}
}

// TestTraceHookISRMissAndHit does the same walk for an ISR route.
func TestTraceHookISRMissAndHit(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	events := mockTraceHook(t)

	cfg := RouteConfig[string]{
		Type:            ISR,
		HttpMethod:      GET,
		RevalidateInSec: 60,
		Middleware:      func(w http.ResponseWriter, r *http.Request) string { return "i" },
	}
	r := chi.NewRouter()
	cfg.RegisterRoute(r, "/i", stubPage[string]("<p>i</p>"))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/i", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/i", nil))

	got := events()
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[0].Name != "ISR" {
		t.Errorf("event 0 name = %q, want ISR", got[0].Name)
	}
	if got[1].Cache != "miss" || got[2].Cache != "hit" {
		t.Errorf("cache fields = %q then %q, want miss then hit", got[1].Cache, got[2].Cache)
	}
	if got[2].Out != "<p>i</p>" {
		t.Errorf("hit Out = %q, want <p>i</p>", got[2].Out)
	}
}

// TestTraceHookInjection asserts the WASM bootstrap injection stage: kind
// injection, named after the WASM output, with the envelope byte size.
func TestTraceHookInjection(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)
	events := mockTraceHook(t)

	cfg := RouteConfig[any]{
		Type:            DYNAMIC,
		HttpMethod:      GET,
		Middleware:      func(w http.ResponseWriter, r *http.Request) any { return nil },
		ClientSideState: func() {}, // marks the route as WASM-injected
	}
	r := chi.NewRouter()
	cfg.RegisterRoute(r, "/w", stubPage[any]("<p>w</p>"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/w", nil))

	got := events()
	var injection *TraceEvent
	for i := range got {
		if got[i].Kind == "injection" {
			injection = &got[i]
			break
		}
	}
	if injection == nil {
		t.Fatalf("no injection event in %+v", got)
	}
	if injection.Name != WasmOutputName("/w") {
		t.Errorf("injection name = %q, want %q", injection.Name, WasmOutputName("/w"))
	}
	if injection.Bytes <= len("<p>w</p>") {
		t.Errorf("injection Bytes = %d, want > plain render size", injection.Bytes)
	}
	if rec.Body.String() == "<p>w</p>" {
		t.Error("injected output must contain the bootstrap script")
	}
}

// TestTraceHookApiDynamic asserts a DYNAMIC API stage carries the handler's
// output payload and its duration.
func TestTraceHookApiDynamic(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)
	events := mockTraceHook(t)

	api := ApiRouteConfig{Type: DYNAMIC, HttpMethod: GET}
	r := chi.NewRouter()
	api.RegisterRoute(r, "/api/dyn", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/dyn?q=1", nil))

	got := events()
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != "api" || ev.Name != "DYNAMIC" {
		t.Errorf("event = %s/%s, want api/DYNAMIC", ev.Kind, ev.Name)
	}
	if ev.Out != `{"ok":true}` {
		t.Errorf("Out = %q, want {\"ok\":true}", ev.Out)
	}
	if ev.In == "" {
		t.Error("In must carry the request form")
	}
	if ev.MS < 0 {
		t.Errorf("MS = %d, want >= 0", ev.MS)
	}
}

// TestTraceHookApiStaticMissAndHit walks a STATIC API endpoint twice: the
// miss stage carries the response payload, the hit stage replays it from the
// cache store.
func TestTraceHookApiStaticMissAndHit(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(IN_MEMORY, nil)
	events := mockTraceHook(t)

	api := ApiRouteConfig{Type: STATIC, HttpMethod: GET}
	r := chi.NewRouter()
	api.RegisterRoute(r, "/api/s", func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`{"n":1}`))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/s", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/s", nil))

	got := events()
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].Kind != "api" || got[0].Name != "STATIC" || got[0].Cache != "miss" || got[0].Out != `{"n":1}` {
		t.Errorf("event 0 = %+v, want api STATIC miss {\"n\":1}", got[0])
	}
	if got[1].Kind != "api" || got[1].Name != "STATIC" || got[1].Cache != "hit" || got[1].Out != `{"n":1}` || got[1].Bytes != len(`{"n":1}`) {
		t.Errorf("event 1 = %+v, want api STATIC hit {\"n\":1}", got[1])
	}
}

// TestTraceHookApiDynamicRequestBody asserts the input payload (a JSON body)
// is captured and the handler still receives the full body afterwards.
func TestTraceHookApiDynamicRequestBody(t *testing.T) {
	resetGlobalCache()
	defer resetGlobalCache()
	InitCache(CACHE_CONTROL_HEADERS, nil)
	events := mockTraceHook(t)

	api := ApiRouteConfig{Type: DYNAMIC, HttpMethod: POST}
	r := chi.NewRouter()
	var handlerSaw string
	api.RegisterRoute(r, "/api/echo", func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		handlerSaw = string(b)
		w.Write(b)
	})
	body := `{"message":"hello"}`
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/echo", stringReader(body)))

	got := events()
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].Out != body {
		t.Errorf("Out = %q, want echoed body", got[0].Out)
	}
	if !contains(got[0].In, body) {
		t.Errorf("In = %q, want it to contain the body %q", got[0].In, body)
	}
	if handlerSaw != body {
		t.Errorf("handler saw %q, want %q — captured body must be restored", handlerSaw, body)
	}
}

// TestTraceTrunc asserts the payload cap: long strings are cut at 2 KB with
// the dropped byte count appended; short strings pass through untouched.
func TestTraceTrunc(t *testing.T) {
	short := "hello"
	if got := traceTrunc(short); got != short {
		t.Errorf("short string changed: %q", got)
	}
	long := make([]byte, traceMaxPayload+10)
	for i := range long {
		long[i] = 'x'
	}
	got := traceTrunc(string(long))
	if len(got) >= len(long)+32 || len(got) <= traceMaxPayload {
		t.Errorf("unexpected truncated length %d", len(got))
	}
	if !contains(got, "…(+10 bytes)") {
		t.Errorf("truncation suffix missing: %q", got[len(got)-24:])
	}
}

// TestTraceSanitizeHookEventsIsMiddlewaresSide documents that the router
// layer does NOT redact: raw values flow to the hook, redaction belongs to
// the tracer (middlewares/trace.go).
func TestTraceNoRedactionAtRouterLayer(t *testing.T) {
	if contains(traceTrunc(`password:"abc"`), "[REDACTED]") {
		t.Error("router layer must not redact; the tracer owns redaction")
	}
}

// TestTraceCaptureRequestRestoresBody verifies the input-capture helper reads
// the body and leaves the request readable again.
func TestTraceCaptureRequestRestoresBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/x?k=v", stringReader(`{"a":1}`))
	in := traceCaptureRequest(req)
	if !contains(in, "k=v") || !contains(in, `{"a":1}`) {
		t.Errorf("in = %q, want query and body", in)
	}
	rest, err := io.ReadAll(req.Body)
	if err != nil || string(rest) != `{"a":1}` {
		t.Errorf("restored body = %q err %v, want original", rest, err)
	}
}

// stringReader avoids importing strings just for NewReader.
func stringReader(s string) io.Reader { return io.Reader(newSliceReader(s)) }

type sliceReader struct {
	s string
	i int
}

func newSliceReader(s string) *sliceReader { return &sliceReader{s: s} }

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

// contains keeps the test assertions dependency-light.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// silence unused-import guards if json/os end up unneeded later
var _ = json.Marshal
var _ = os.Getenv
