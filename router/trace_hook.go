package helpers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
)

// trace_hook.go carries the request-tracing seam for file-based routes and API
// endpoints. It is deliberately tiny: a plain event struct, one package-level
// hook variable defaulting to nil, and a one-branch notifier. When no hook is
// installed, request handling executes the exact same code path as before (the
// timing variables stay zero and nothing is formatted, captured or copied).

// TraceEvent is a single observed stage of a request: the per-route
// middleware, a render, the WASM bootstrap injection, or an API handler run.
// It carries only plain strings/ints so consumers in other packages stay
// decoupled from routing internals.
type TraceEvent struct {
	// Kind is the stage family: "middleware" | "render" | "injection" | "api".
	Kind string
	// Name identifies the stage: the route type (DYNAMIC/STATIC/ISR) for
	// middleware and render stages, the WASM output name for injection, and
	// the API HTTP path for api stages.
	Name string
	// MS is the stage duration in whole milliseconds.
	MS int64
	// Bytes is the size of the stage output when known: rendered HTML,
	// injected payload, or API response body.
	Bytes int
	// Cache is "hit" | "miss" for stages that pass through a cache store,
	// empty otherwise.
	Cache string
	// In is a truncated string form of the stage input.
	In string
	// Out is a truncated string form of the stage output (props value,
	// rendered HTML, or API response body).
	Out string

	// ctx ties the event to its originating request so a request-scoped
	// collector can attribute it. It is not exported, so it never appears in
	// any serialized form; access it via the exported Ctx method.
	ctx context.Context
}

// Ctx returns the request context the event was produced under, or nil when
// the event originated outside a traced request (direct TraceHook observers).
func (e TraceEvent) Ctx() context.Context { return e.ctx }

// TraceHook, when non-nil, receives every stage event the router observes.
// Default nil: each call site performs a single nil check and nothing else,
// so requests take the identical path as before tracing existed and allocate
// nothing. The dev-mode tracer in the middlewares package installs the real
// implementation at construction; tests may install their own observer.
var TraceHook func(TraceEvent)

// traceMaxPayload bounds In/Out strings so a large render or request body
// cannot flood the trace buffer. Overflow is reported by a trailing byte count.
const traceMaxPayload = 2048

// traceTrunc clamps s at traceMaxPayload bytes, appending the dropped byte
// count when anything was cut.
func traceTrunc(s string) string {
	if len(s) <= traceMaxPayload {
		return s
	}
	return fmt.Sprintf("%s…(+%d bytes)", s[:traceMaxPayload], len(s)-traceMaxPayload)
}

// traceStage reports one stage event to the installed TraceHook with the
// request context attached for attribution. Cost when TraceHook is nil: one
// branch, no allocation.
func traceStage(ctx context.Context, ev TraceEvent) {
	if TraceHook == nil {
		return
	}
	if ctx != nil {
		ev.ctx = ctx
	}
	TraceHook(ev)
}

// configTypeName renders a ConfigType as its route-type name for trace events.
func configTypeName(t ConfigType) string {
	switch t {
	case STATIC:
		return "STATIC"
	case DYNAMIC:
		return "DYNAMIC"
	default:
		return "ISR"
	}
}

// traceWriter counts the bytes a handler writes through it and keeps a capped
// copy of the stream for the trace. It is constructed only when TraceHook is
// non-nil. Header and WriteHeader pass through the embedded ResponseWriter;
// Flush, Hijack and Unwrap are forwarded explicitly so streaming (SSE) and
// connection-taking dev handlers keep working while tracing is installed —
// struct embedding alone only forwards the three methods both sides share.
type traceWriter struct {
	http.ResponseWriter
	written int
	capped  bytes.Buffer
}

// Flush forwards Flusher support so a streaming handler's writes still reach
// the client during a traced request.
func (tw *traceWriter) Flush() {
	if f, ok := tw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards Hijacker support (websockets, dev SSE handshake upgrades).
func (tw *traceWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := tw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap exposes the underlying writer for callers using
// http.ResponseController/status-aware tooling.
func (tw *traceWriter) Unwrap() http.ResponseWriter { return tw.ResponseWriter }

func (tw *traceWriter) Write(b []byte) (int, error) {
	n, err := tw.ResponseWriter.Write(b)
	tw.written += n
	if room := traceMaxPayload - tw.capped.Len(); room > 0 {
		if room > len(b) {
			room = len(b)
		}
		tw.capped.Write(b[:room])
	}
	return n, err
}

// out returns the capped copy of everything written through the writer.
func (tw *traceWriter) out() string { return tw.capped.String() }

// traceCaptureRequest builds a truncated string form of the request input
// (path, query, and body) so an API stage can record its input payload. It
// reads the request body and restores it afterwards, so the wrapped handler
// still sees the full request. Callers only invoke it when TraceHook is
// non-nil: reading and rewrapping the body allocates.
func traceCaptureRequest(r *http.Request) string {
	in := r.URL.Path
	if q := r.URL.RawQuery; q != "" {
		in += "?" + q
	}
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err == nil && len(body) > 0 {
			in += " body=" + string(body)
		}
	}
	return traceTrunc(in)
}
