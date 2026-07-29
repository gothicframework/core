package helpers

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
)

// statusCapture wraps an http.ResponseWriter to capture the first status code
// written to it. Any WriteHeader call or the first Write (which implicitly sets
// 200 OK) records the status, accessible via Status().
//
// Methods Unwrap, Flush, and Hijack are forwarded to the underlying writer
// when supported, so standard library utilities (ResponseController, etc.)
// work transparently.
type statusCapture struct {
	http.ResponseWriter
	status int
}

// WriteHeader captures the first status code, then delegates.
func (s *statusCapture) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write records an implicit 200 OK if no status was set, then delegates.
func (s *statusCapture) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Status returns the captured status code, or 0 if neither WriteHeader nor
// Write has been called.
func (s *statusCapture) Status() int {
	return s.status
}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController
// and similar standard-library utilities.
func (s *statusCapture) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}

// Flush implements http.Flusher when the underlying writer supports it.
func (s *statusCapture) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker when the underlying writer supports it.
func (s *statusCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("statusCapture: underlying ResponseWriter does not implement http.Hijacker")
}

// warnStaticAuthOnce ensures that warnStaticAuthRejected prints at most one
// yellow warning per route path, regardless of how many requests trigger it.
var warnStaticAuthOnce sync.Map

// warnStaticAuthRejected explains, once per route, why a STATIC/ISR route's
// Middleware rejecting a request does not make the route private: its rendered
// output is public cached content. Access control belongs on DYNAMIC routes or
// chi middleware. The ANSI codes mirror the CLI warning palette
// (cli/internal/termcolor.Yellow/Reset) — core cannot import the CLI.
//
// It writes to os.Stderr directly rather than through log: the log package
// captures os.Stderr at init, so a test that swaps os.Stderr cannot observe
// log output, and this warning exists to be asserted.
func warnStaticAuthRejected(path string) {
	if _, loaded := warnStaticAuthOnce.LoadOrStore(path, true); !loaded {
		fmt.Fprintf(os.Stderr, "\033[33m⚠ gothic: %s is STATIC/ISR (public, cached) — its Middleware rejected a request; use DYNAMIC or chi middleware for access-controlled pages\033[0m\n", path)
	}
}
