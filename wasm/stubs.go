// Package wasm provides server-side stubs for the WASM reactive runtime.
// Import this package with a dot import in page files so the state function
// compiles server-side:
//
//	import . "github.com/gothicframework/core/wasm"
//
// At WASM compile time the framework substitutes the real TinyGo implementation
// (signal tracking, DOM manipulation, JS event registration) from the embedded
// wasm-runtime module.  On the server these are all no-ops.
package wasm

import (
	"math"
)

// Observable is a typed reactive state container (server-side no-op).
// Similar to useState in React — holds a value and notifies observers on change.
type Observable[T any] struct{ value T }

// Subscription is a reactive computation (server-side no-op).
type Subscription struct{}

// CreateObservable creates an Observable with the given initial value.
// It is the Gothic equivalent of React's useState hook.
//
// Example:
//
//	count := CreateObservable(0)
//	label := CreateObservable("hello")
//
//	count.Set(count.Get() + 1)  // triggers all Observe callbacks that depend on count
func CreateObservable[T any](initial T) *Observable[T] { return &Observable[T]{value: initial} }

// Get returns the current observable value.
func (s *Observable[T]) Get() T { return s.value }

// Set updates the observable value.
func (s *Observable[T]) Set(v T) { s.value = v }

// Observe runs fn immediately and re-runs it whenever a listed dep changes.
// It is the Gothic equivalent of React's useEffect hook.
// Pass no deps to run fn exactly once with no reactive subscription.
//
// Example:
//
//	count := CreateObservable(0)
//
//	Observe(func() {
//	    SetText("counter", fmt.Sprintf("%d", count.Get()))
//	}, count)
func Observe(fn func(), deps ...any) *Subscription { return &Subscription{} }

// ObserveWithCleanup is like Observe with a cleanup function.
func ObserveWithCleanup(fn func() func(), deps ...any) *Subscription { return &Subscription{} }

// Stop deactivates an effect (no-op server-side).
func (e *Subscription) Stop() {}

// OnUnmount registers a cleanup callback run when the component's
// [data-gothic-scope] element is removed from the DOM (server-side no-op). In
// the WASM runtime it releases things created outside the component's subtree
// (document listeners, timers, topic mounts, in-flight fetch AbortControllers).
// It returns a deregister func (a no-op here) that drops the callback early once
// it becomes dead weight — callers may ignore it.
func OnUnmount(fn func()) func() { return func() {} }

// CaptureScope returns the scope active at the call site (server-side no-op:
// always ""). In the WASM runtime it captures the active [data-gothic-scope] so
// a goroutine can re-establish it with RunInScope when its work runs later —
// a goroutine does not inherit the scope across a suspension point.
//
//	scope := CaptureScope()
//	go func() { RunInScope(scope, func() { SetText("out", result) }) }()
func CaptureScope() string { return "" }

// RunInScope runs fn with the given scope active, restoring the previous scope
// afterwards (server-side no-op: fn is not executed, matching Observe). Pair it
// with CaptureScope to carry a scope into a goroutine or deferred callback.
func RunInScope(id string, fn func()) {}

// DurableKey returns the placement's stable durable key
// (data-gothic-durable-key) or "" when not durable (server-side no-op: always
// ""). In the WASM runtime it reads the attribute off the component wrapper so
// DurableObserve can rehydrate from the full-Go core across a teardown→re-mount.
func DurableKey() string { return "" }

// DurableObserve binds an observable to the core's page-session durable cache
// under `field` so its value SURVIVES the component's teardown→re-mount
// (server-side no-op). OPT-IN: when the placement has no durable key it does
// nothing and the observable behaves exactly as a plain one — so SSR output is
// identical whether or not a component opts into durability. encode/decode are
// the field's string codec (same shape as CustomKey).
//
//	count := CreateObservable(0)
//	DurableObserve("count", count, strconv.Itoa,
//	    func(s string) int { n, _ := strconv.Atoi(s); return n })
func DurableObserve[T any](field string, obs *Observable[T], encode func(T) string, decode func(string) T) {
}

// DOM helpers — all no-ops on the server.

func SetText(id, value string)  {}
func SetHTML(id, html string)   {}
func SetValue(id, value string) {}
func GetValue(id string) string { return "" }

// GetFileBytes reads the contents of the first file selected in a <input type="file"> element.
// Returns nil if the element is not found, no file is selected, or reading fails.
func GetFileBytes(id string) []byte { return nil }

func AddClass(id, className string)       {}
func RemoveClass(id, className string)    {}
func ToggleClass(id, className string)    {}
func SetAttr(id, attr, value string)      {}
func SetStyle(id, property, value string) {}

// FetchConfig configures an HTTP request made via Fetch.
type FetchConfig struct {
	Method    string            // "GET", "POST", "PUT", "DELETE" — default: "GET"
	Headers   map[string]string // request headers
	Body      string            // request body (for POST/PUT) — text body
	BodyBytes []byte            // binary body — used when Body is empty
	Query     map[string]string // query parameters appended to the URL
}

// Response is the result of a Fetch call (server-side no-op mirror of the WASM
// runtime type). Body holds the raw response bytes; use Text()/Bytes()/OK().
type Response struct {
	Status  int               // HTTP status code
	Headers map[string]string // response headers
	Body    []byte            // raw response body bytes
}

// Text returns the response body decoded as a UTF-8 string.
func (r Response) Text() string { return string(r.Body) }

// Bytes returns the raw response body bytes.
func (r Response) Bytes() []byte { return r.Body }

// OK reports whether the response status is in the 2xx (200..299) range.
func (r Response) OK() bool { return r.Status >= 200 && r.Status < 300 }

// MapAny parses the response body as a JSON object into a map[string]any
// (server-side no-op mirror: always nil, nil). In the WASM runtime it uses a
// reflection-free, TinyGo-safe parser that never panics on malformed input —
// invalid JSON returns a non-nil error. The top-level value must be a JSON
// object; nested values are coerced to map[string]any / []any / string / float64
// (int64 > 2^53 loses precision) / bool / nil.
//
// Example:
//
//	resp, err := Fetch("/api/user/1")
//	if err == nil && resp.OK() {
//	    m, err := resp.MapAny()
//	    if err == nil { name, _ := m["name"].(string) }
//	}
func (r Response) MapAny() (map[string]any, error) { return nil, nil }

// Fetch makes an HTTP request using the browser's fetch API and blocks until complete.
// Config is optional — omit for a simple GET request. The body is read as raw bytes;
// use Response.Text() for a string view, Response.Bytes() for binary, Response.OK()
// for a 2xx check.
// Must be called from inside a goroutine or CreateWasmFunc handler.
//
// Example:
//
//	resp, err := Fetch("https://api.example.com/todos/1")
//	if err == nil && resp.OK() {
//	    body := resp.Text()
//	}
//
//	resp, err := Fetch("https://api.example.com/todos", FetchConfig{
//	    Method:  "POST",
//	    Headers: map[string]string{"Content-Type": "application/json"},
//	    Body:    `{"title":"foo"}`,
//	})
func Fetch(url string, config ...FetchConfig) (Response, error) { return Response{}, nil }

// Decode parses r's JSON body into a value of struct type T and returns it
// (server-side no-op: returns the zero value and nil). At WASM build time the
// gothic CLI REPLACES each Decode[T](resp) call with a generated, reflection-free
// _jsonDecode_T(resp): the CLI reads T's fields (and json tags) via go/types and
// emits code that reads them out of the runtime's reflection-free JSON parse —
// so NEITHER reflect NOR encoding/json is pulled into the TinyGo binary. This
// server-side stub exists only so a ClientSideState block calling Decode[T]
// type-checks during SSR / ScanPages.
//
// T must be a struct type. Fields are matched by json tag (falling back to the
// Go field name); JSON numbers coerce to numeric fields (int64 magnitudes above
// 2^53 lose precision), and a missing key or JSON null yields the field's zero
// value. Nested structs, slices, pointers, and string-keyed maps are supported;
// unsupported field types decode as their zero value.
//
// Example:
//
//	resp, err := Fetch("/api/user/1")
//	if err == nil && resp.OK() {
//	    user, err := Decode[User](resp)
//	    if err == nil { SetText("name", user.Name) }
//	}
func Decode[T any](r Response) (T, error) {
	var zero T
	return zero, nil
}

// Encode marshals a value of struct type T to a JSON []byte, for use as a request
// body (server-side no-op: returns nil). It is the write-direction mirror of
// Decode. At WASM build time the gothic CLI REPLACES each Encode[T](v) call with
// a generated, reflection-free _jsonEncode_T(v): the CLI reads T's fields (and
// json tags) via go/types and emits code that appends their JSON by hand — so
// NEITHER reflect NOR encoding/json is pulled into the TinyGo binary. This
// server-side stub exists only so a ClientSideState block calling Encode[T]
// type-checks during SSR / ScanPages.
//
// T must be a struct type, and the call MUST carry an explicit type argument
// (Encode[T](v)) — the build-time rewrite is syntactic and cannot recover an
// inferred T. Fields are emitted in struct order, keyed by json tag (falling back
// to the field name); `json:"-"` is skipped; `,omitempty` is ignored (the field
// is always written); nil slices/pointers/maps become JSON null.
//
// Example:
//
//	body := Encode[CreateUser](CreateUser{Name: name})
//	Fetch("/api/users", FetchConfig{Method: "POST", BodyBytes: body})
func Encode[T any](v T) []byte {
	return nil
}

// FetchResult pairs a Response with its error for delivery over a channel
// (server-side no-op mirror of the WASM runtime type). It is what FetchChan
// sends: exactly one FetchResult per request.
type FetchResult struct {
	Response Response
	Err      error
}

// FetchAsync makes an HTTP request and invokes done(Response, error) when it
// completes, without blocking (server-side no-op). done runs inside the scope
// active at the call site so DOM writes hit the right subtree, and the request
// is aborted on component teardown. Use it when a blocking Fetch would stall a
// handler.
//
// Example:
//
//	FetchAsync("/api/todos", FetchConfig{}, func(resp Response, err error) {
//	    if err == nil && resp.OK() {
//	        SetText("out", resp.Text())
//	    }
//	})
func FetchAsync(url string, cfg FetchConfig, done func(Response, error)) {}

// FetchChan makes an HTTP request and returns a receive-only channel that yields
// exactly one FetchResult when it completes (server-side no-op mirror). It does
// not block — select on the returned channel. Use it to await one of several
// requests or to combine a fetch with a timeout in a select.
//
// Example:
//
//	select {
//	case r := <-FetchChan("/api/todos"):
//	    if r.Err == nil && r.Response.OK() { SetText("out", r.Response.Text()) }
//	}
func FetchChan(url string, cfg ...FetchConfig) <-chan FetchResult {
	ch := make(chan FetchResult, 1)
	ch <- FetchResult{}
	return ch
}

// JSValue is a server-side stub for syscall/js.Value.
// All methods are no-ops; the real implementation lives in the WASM runtime.
type JSValue struct{}

func JS() JSValue       { return JSValue{} }
func Window() JSValue   { return JSValue{} }
func Document() JSValue { return JSValue{} }

func ConsoleLog(args ...any) {}

func GetElementById(id string) JSValue    { return JSValue{} }
func CreateElement(tag string) JSValue    { return JSValue{} }
func QuerySelector(sel string) JSValue    { return JSValue{} }
func QuerySelectorAll(sel string) JSValue { return JSValue{} }

func (v JSValue) Get(key string) JSValue                  { return JSValue{} }
func (v JSValue) Set(key string, val any)                 {}
func (v JSValue) Call(method string, args ...any) JSValue { return JSValue{} }
func (v JSValue) New(args ...any) JSValue                 { return JSValue{} }
func (v JSValue) String() string                          { return "" }
func (v JSValue) Int() int                                { return 0 }
func (v JSValue) Float() float64                          { return 0 }
func (v JSValue) Bool() bool                              { return false }
func (v JSValue) IsNull() bool                            { return true }
func (v JSValue) IsUndefined() bool                       { return true }
func (v JSValue) Truthy() bool                            { return false }
func (v JSValue) Index(i int) JSValue                     { return JSValue{} }
func (v JSValue) SetIndex(i int, val any)                 {}
func (v JSValue) Length() int                             { return 0 }

func CopyBytesToJS(dst JSValue, src []byte) int { return 0 }
func CopyBytesToGo(dst []byte, src JSValue) int { return 0 }

// TriggerDownload prompts the browser to download `data` as a file named `filename` with the given MIME type.
// Server-side no-op.
func TriggerDownload(filename string, data []byte, mimeType string) {}

// AddEventListenerWithEvent attaches a persistent event listener to el for the given event name.
// fn receives the browser Event object as a JSValue, giving access to event properties and methods.
// Use this when you need to inspect or interact with the event itself — call preventDefault,
// read event.target, event.key, event.clientX, event.detail, etc.
// The listener stays alive for the lifetime of the page — it is never removed automatically.
//
// Example:
//
//	AddEventListenerWithEvent(form, "submit", func(e JSValue) {
//	    e.Call("preventDefault")               // stop the default form submission
//	    val := e.Get("target").Get("value").String()
//	})
//
//	AddEventListenerWithEvent(Document(), "keydown", func(e JSValue) {
//	    if e.Get("key").String() == "Escape" {
//	        // close modal, etc.
//	    }
//	})
func AddEventListenerWithEvent(el JSValue, event string, fn func(JSValue)) {}

// AddEventListener attaches a persistent event listener to el for the given event name.
// fn is called with no arguments each time the event fires.
// The listener stays alive for the lifetime of the page — it is never removed automatically.
//
// Common use cases: reacting to browser events (click, input, toggle) or framework
// events (htmx:afterSwap, htmx:beforeSwap) on any JSValue element including Document()
// and Window().
//
// Example:
//
//	body := Document().Get("body")
//	AddEventListener(body, "htmx:afterSwap", func() {
//	    // re-sync DOM after HTMX swaps content
//	})
//
//	details := QuerySelector("details#menu")
//	AddEventListener(details, "toggle", func() {
//	    // react to open/close state changes
//	})
func AddEventListener(el JSValue, event string, fn func()) {}

// Element tree helpers — server-side no-ops.
func AppendChild(parent, child JSValue) {}
func RemoveElement(el JSValue)          {}
func ClickElement(el JSValue)           {}

// WriteClipboard writes text to the system clipboard. Server-side no-op.
func WriteClipboard(text string) {}

// ExecJS executes a JavaScript snippet in the browser. Server-side no-op.
func ExecJS(script string) {}

// Navigation helpers — server-side no-ops.
func Navigate(url string)         {}
func Reload()                     {}
func PushState(url, title string) {}
func GoBack()                     {}

// Event registration — no-ops on the server.

func CreateWasmFunc(name string, fn func())             {}
func CreateWasmStringFunc(name string, fn func(string)) {}
func CreateWasmBoolFunc(name string, fn func(bool))     {}

// CreateWasmFuncWithReturn registers a named global JS function that can return a value back to JS.
// Wraps syscall/js.FuncOf. Use when a JS library expects a callback that returns a value
// synchronously (e.g. option objects, formatters, renderers). The function persists for the
// lifetime of the page. Returns a JSValue so it can be passed directly to JS object properties.
//
// Example:
//
//	// Register a callback and pass it as a property on a JS config object:
//	cb := CreateWasmFuncWithReturn("myCallback", func(this JSValue, args []JSValue) any {
//	    return args[0].String() + "_suffix"
//	})
//	config.Set("formatter", cb)
func CreateWasmFuncWithReturn(name string, fn func(this JSValue, args []JSValue) any) JSValue {
	return JSValue{}
}

// ── Topic infrastructure (generated code only — not part of the user API) ────

// TopicKey is a typed key used by the auto-generated topic system.
// Users never construct these directly — the CLI generates them from src/topics/*.go.
type TopicKey[T any] struct {
	Name   string
	encode func(T) string
	decode func(string) T
}

// BinaryKey is used exclusively by CLI-generated code in src/topics/topic_gen.go.
func BinaryKey[T any](name string, encode func(T, *Encoder), decode func(*Decoder) T) TopicKey[T] {
	return TopicKey[T]{
		Name: name,
		encode: func(v T) string {
			e := NewEncoder(64)
			encode(v, e)
			return hexEncode(e.Buf)
		},
		decode: func(s string) T {
			d := NewDecoder(hexDecode(s))
			return decode(d)
		},
	}
}

// AutoKey is rewritten to BinaryKey by the CLI before TinyGo compiles.
// Server-side this is a no-op stub so the code compiles.
func AutoKey[T any](name string) TopicKey[T] { return TopicKey[T]{Name: name} }

// Compression is the compression algorithm used for a topic's WASM payload.
type Compression int

const (
	GZIP   Compression = iota // default
	BROTLI Compression = iota
)

// WasmCompiler selects the WASM build toolchain for a topic.
type WasmCompiler int

const (
	GothicTinyGo WasmCompiler = iota // default: embedded TinyGo binary
	LocalTinyGo                      // system tinygo binary in PATH
	Golang                           // GOOS=js GOARCH=wasm standard Go compiler
)

// TopicConfig holds per-topic configuration. The CLI AST scanner reads the
// Name and Compression fields from CreateTopic call sites to drive code
// generation.
type TopicConfig struct {
	Name             string
	Compression      Compression  // GZIP (default) or BROTLI
	Compiler         WasmCompiler // GothicTinyGo (default), LocalTinyGo, or Golang
	SubscriberFnName string       // overrides generated accessor func name (default: <StructName>Topic)
}

// CreateTopic declares a topic. The CLI AST scanner detects this call and
// generates the concrete typed accessor. Server-side this is a no-op stub.
func CreateTopic[T any](zero T, cfg TopicConfig) func() interface{} {
	return func() interface{} { return nil }
}

// WireVersion is the codec's frame-format version (server-side stub — mirrors
// runtime.WireVersion). Written as byte 0 of every top-level frame by NewEncoder
// and validated by NewDecoder.
const WireVersion byte = 1

// Encoder writes a little-endian binary stream (server-side stub — mirrors runtime.Encoder).
type Encoder struct{ Buf []byte }

// NewEncoder opens a new frame whose buffer already carries the WireVersion
// header byte at position 0 (mirrors runtime.NewEncoder).
func NewEncoder(cap int) *Encoder {
	if cap < 1 {
		cap = 1
	}
	return &Encoder{Buf: append(make([]byte, 0, cap), WireVersion)}
}
func (e *Encoder) U8(v uint8)   { e.Buf = append(e.Buf, v) }
func (e *Encoder) U16(v uint16) { e.Buf = append(e.Buf, byte(v), byte(v>>8)) }
func (e *Encoder) U32(v uint32) {
	e.Buf = append(e.Buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
func (e *Encoder) U64(v uint64) {
	e.Buf = append(e.Buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}
func (e *Encoder) I32(v int32)   { e.U32(uint32(v)) }
func (e *Encoder) I64(v int64)   { e.U64(uint64(v)) }
func (e *Encoder) F32(v float32) { e.U32(math.Float32bits(v)) }
func (e *Encoder) F64(v float64) { e.U64(math.Float64bits(v)) }
func (e *Encoder) Bool(v bool) {
	b := byte(0)
	if v {
		b = 1
	}
	e.Buf = append(e.Buf, b)
}
func (e *Encoder) Bytes(v []byte)  { e.U32(uint32(len(v))); e.Buf = append(e.Buf, v...) }
func (e *Encoder) String(v string) { e.U32(uint32(len(v))); e.Buf = append(e.Buf, v...) }

// Decoder reads a little-endian binary stream (server-side stub — mirrors runtime.Decoder).
type Decoder struct {
	Buf []byte
	Pos int
	Err error
}

func (d *Decoder) need(n int) bool {
	if d.Err != nil {
		return false
	}
	if d.Pos+n > len(d.Buf) {
		d.Err = decErr("codec: buffer underflow")
		return false
	}
	return true
}

type decErr string

func (e decErr) Error() string { return string(e) }

// NewDecoder opens a frame produced by NewEncoder: it validates the WireVersion
// header byte and positions Pos after it, or sets Err on an empty/wrong-version
// buffer without panicking (mirrors runtime.NewDecoder).
func NewDecoder(buf []byte) *Decoder {
	d := &Decoder{Buf: buf}
	if len(buf) == 0 || buf[0] != WireVersion {
		d.Err = decErr("gothic codec: unsupported wire version")
		return d
	}
	d.Pos = 1
	return d
}

func (d *Decoder) U8() uint8 {
	if !d.need(1) {
		return 0
	}
	v := d.Buf[d.Pos]
	d.Pos++
	return v
}
func (d *Decoder) U16() uint16 {
	if !d.need(2) {
		return 0
	}
	v := uint16(d.Buf[d.Pos]) | uint16(d.Buf[d.Pos+1])<<8
	d.Pos += 2
	return v
}
func (d *Decoder) U32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := uint32(d.Buf[d.Pos]) | uint32(d.Buf[d.Pos+1])<<8 |
		uint32(d.Buf[d.Pos+2])<<16 | uint32(d.Buf[d.Pos+3])<<24
	d.Pos += 4
	return v
}
func (d *Decoder) U64() uint64 {
	if !d.need(8) {
		return 0
	}
	v := uint64(d.Buf[d.Pos]) | uint64(d.Buf[d.Pos+1])<<8 |
		uint64(d.Buf[d.Pos+2])<<16 | uint64(d.Buf[d.Pos+3])<<24 |
		uint64(d.Buf[d.Pos+4])<<32 | uint64(d.Buf[d.Pos+5])<<40 |
		uint64(d.Buf[d.Pos+6])<<48 | uint64(d.Buf[d.Pos+7])<<56
	d.Pos += 8
	return v
}
func (d *Decoder) I32() int32   { return int32(d.U32()) }
func (d *Decoder) I64() int64   { return int64(d.U64()) }
func (d *Decoder) F32() float32 { return math.Float32frombits(d.U32()) }
func (d *Decoder) F64() float64 { return math.Float64frombits(d.U64()) }
func (d *Decoder) Bool() bool   { return d.U8() != 0 }
func (d *Decoder) Bytes() []byte {
	n := d.U32()
	if !d.need(int(n)) {
		return nil
	}
	v := d.Buf[d.Pos : d.Pos+int(n)]
	d.Pos += int(n)
	return v
}
func (d *Decoder) String() string {
	n := d.U32()
	if !d.need(int(n)) {
		return ""
	}
	v := string(d.Buf[d.Pos : d.Pos+int(n)])
	d.Pos += int(n)
	return v
}

const hextable = "0123456789abcdef"

func hexEncode(src []byte) string {
	dst := make([]byte, len(src)*2)
	for i, b := range src {
		dst[i*2] = hextable[b>>4]
		dst[i*2+1] = hextable[b&0xf]
	}
	return string(dst)
}

func hexDecode(s string) []byte {
	if len(s)%2 != 0 {
		return nil
	}
	dst := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		dst[i/2] = unhex(s[i])<<4 | unhex(s[i+1])
	}
	return dst
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// SharedTopicObservable is the internal type backing auto-generated topic constructors.
// Users access shared topic state via the generated accessor e.g. PageTopic() — not directly.
type SharedTopicObservable[T any] struct{ value T }

func (s *SharedTopicObservable[T]) Get() T  { return s.value }
func (s *SharedTopicObservable[T]) Set(v T) { s.value = v }

// ObservableField is a per-field reactive observable for a generated topic struct.
// Server-side stub — no broadcast, no effect tracking.
type ObservableField[T any] struct{ sig *Observable[T] }

// NewObservableField creates an ObservableField with the given initial value.
func NewObservableField[T any](initial T) *ObservableField[T] {
	return &ObservableField[T]{sig: &Observable[T]{value: initial}}
}
func (f *ObservableField[T]) SetBroadcast(fn func()) {}
func (f *ObservableField[T]) Get() T                 { return f.sig.Get() }
func (f *ObservableField[T]) Peek() T                { return f.sig.value }
func (f *ObservableField[T]) Set(v T)                { f.sig.value = v }
func (f *ObservableField[T]) ApplyExternal(v T)      { f.sig.Set(v) }

// LocalStorage helpers — server-side no-ops.
func LocalStorageSet(key, value string) {}
func LocalStorageGet(key string) string { return "" }
func LocalStorageRemove(key string)     {}

// SessionStorage helpers — server-side no-ops.
func SessionStorageSet(key, value string) {}
func SessionStorageGet(key string) string { return "" }
func SessionStorageRemove(key string)     {}

// CookieOptions configures CookieSet behaviour.
type CookieOptions struct {
	MaxAge   int    // seconds; 0 = session cookie
	Path     string // defaults to "/"
	SameSite string // "Strict", "Lax", or "None"
	Secure   bool
}

// Cookie helpers — server-side no-ops.
func CookieSet(key, value string, opts ...CookieOptions) {}
func CookieGet(key string) string                        { return "" }
func CookieDelete(key string)                            {}

// ── HTMX: ergonomic mirror of the vendored htmx 2.0.3 JS API ─────────────────
//
// Server-side no-ops. At WASM compile time the framework substitutes the real
// TinyGo implementation (runtime/htmx.go) that drives window.htmx. Users call
// these unqualified via the dot import, e.g. HTMX.Swap("#out", html).

// HTMX is the htmx API singleton (server-side no-op).
var HTMX htmxAPI

// htmxAPI is the unexported receiver type behind the HTMX singleton.
type htmxAPI struct{}

// Event is the browser Event passed to On/OnGlobal handlers (server-side: JSValue
// stub). In the WASM runtime it exposes the DOM Event — call e.Get("detail"),
// e.Get("target"), e.Call("preventDefault"), etc.
type Event = JSValue

// SwapStrategy is a string-backed htmx swap style (the hx-swap vocabulary). The
// eight canonical values give autocomplete; a custom style is reachable via a
// string cast, e.g. SwapStrategy("innerHTML show:top").
type SwapStrategy string

const (
	InnerHTML   SwapStrategy = "innerHTML"   // replace the target's inner HTML (default)
	OuterHTML   SwapStrategy = "outerHTML"   // replace the entire target element
	BeforeBegin SwapStrategy = "beforebegin" // insert before the target element
	AfterBegin  SwapStrategy = "afterbegin"  // insert as the target's first child
	BeforeEnd   SwapStrategy = "beforeend"   // insert as the target's last child
	AfterEnd    SwapStrategy = "afterend"    // insert after the target element
	Delete      SwapStrategy = "delete"      // delete the target regardless of response
	None        SwapStrategy = "none"        // do not append the response content
)

// HtmxEvent is a string-backed htmx event name. The consts are the full htmx
// 2.0.3 event catalog; a custom/extension event is reachable via a string cast,
// e.g. HtmxEvent("htmx:sse:message").
type HtmxEvent string

const (
	EvtAbort                     HtmxEvent = "htmx:abort"
	EvtAfterOnLoad               HtmxEvent = "htmx:afterOnLoad"
	EvtAfterProcessNode          HtmxEvent = "htmx:afterProcessNode"
	EvtAfterRequest              HtmxEvent = "htmx:afterRequest"
	EvtAfterSettle               HtmxEvent = "htmx:afterSettle"
	EvtAfterSwap                 HtmxEvent = "htmx:afterSwap"
	EvtBadResponseURL            HtmxEvent = "htmx:badResponseUrl"
	EvtBeforeCleanupElement      HtmxEvent = "htmx:beforeCleanupElement"
	EvtBeforeHistorySave         HtmxEvent = "htmx:beforeHistorySave"
	EvtBeforeHistoryUpdate       HtmxEvent = "htmx:beforeHistoryUpdate"
	EvtBeforeOnLoad              HtmxEvent = "htmx:beforeOnLoad"
	EvtBeforeProcessNode         HtmxEvent = "htmx:beforeProcessNode"
	EvtBeforeRequest             HtmxEvent = "htmx:beforeRequest"
	EvtBeforeSend                HtmxEvent = "htmx:beforeSend"
	EvtBeforeSwap                HtmxEvent = "htmx:beforeSwap"
	EvtBeforeTransition          HtmxEvent = "htmx:beforeTransition"
	EvtConfigRequest             HtmxEvent = "htmx:configRequest"
	EvtConfirm                   HtmxEvent = "htmx:confirm"
	EvtError                     HtmxEvent = "htmx:error"
	EvtEvalDisallowedError       HtmxEvent = "htmx:evalDisallowedError"
	EvtEventFilterError          HtmxEvent = "htmx:eventFilter:error"
	EvtHistoryCacheError         HtmxEvent = "htmx:historyCacheError"
	EvtHistoryCacheMiss          HtmxEvent = "htmx:historyCacheMiss"
	EvtHistoryCacheMissLoad      HtmxEvent = "htmx:historyCacheMissLoad"
	EvtHistoryCacheMissLoadError HtmxEvent = "htmx:historyCacheMissLoadError"
	EvtHistoryItemCreated        HtmxEvent = "htmx:historyItemCreated"
	EvtHistoryRestore            HtmxEvent = "htmx:historyRestore"
	EvtInvalidPath               HtmxEvent = "htmx:invalidPath"
	EvtLoad                      HtmxEvent = "htmx:load"
	EvtOnLoadError               HtmxEvent = "htmx:onLoadError"
	EvtOobAfterSwap              HtmxEvent = "htmx:oobAfterSwap"
	EvtOobBeforeSwap             HtmxEvent = "htmx:oobBeforeSwap"
	EvtOobErrorNoTarget          HtmxEvent = "htmx:oobErrorNoTarget"
	EvtPrompt                    HtmxEvent = "htmx:prompt"
	EvtPushedIntoHistory         HtmxEvent = "htmx:pushedIntoHistory"
	EvtReplacedInHistory         HtmxEvent = "htmx:replacedInHistory"
	EvtResponseError             HtmxEvent = "htmx:responseError"
	EvtRestored                  HtmxEvent = "htmx:restored"
	EvtSendAbort                 HtmxEvent = "htmx:sendAbort"
	EvtSendError                 HtmxEvent = "htmx:sendError"
	EvtSwapError                 HtmxEvent = "htmx:swapError"
	EvtSyntaxError               HtmxEvent = "htmx:syntax:error"
	EvtTargetError               HtmxEvent = "htmx:targetError"
	EvtTimeout                   HtmxEvent = "htmx:timeout"
	EvtTrigger                   HtmxEvent = "htmx:trigger"
	EvtValidateURL               HtmxEvent = "htmx:validateUrl"
	EvtValidationValidate        HtmxEvent = "htmx:validation:validate"
	EvtValidationFailed          HtmxEvent = "htmx:validation:failed"
	EvtValidationHalted          HtmxEvent = "htmx:validation:halted"
	EvtXHRAbort                  HtmxEvent = "htmx:xhr:abort"
	EvtXHRLoadstart              HtmxEvent = "htmx:xhr:loadstart"
	EvtXHRLoadend                HtmxEvent = "htmx:xhr:loadend"
	EvtXHRProgress               HtmxEvent = "htmx:xhr:progress"
)

// AjaxOpts is the optional context for HTMX.Ajax (maps onto htmx's ajax()
// context object). Zero fields are omitted.
type AjaxOpts struct {
	Target string            // CSS selector for where to swap the response
	Source string            // CSS selector for the request's source element
	Swap   SwapStrategy      // swap style for the response
	Values map[string]string // extra values sent with the request
}

// Ajax issues an htmx AJAX request (server-side no-op).
func (htmxAPI) Ajax(method, url string, opts ...AjaxOpts) {}

// Swap replaces content at the target selector with html and re-processes it so
// nested Gothic stateful components boot (server-side no-op). SECURITY: in the
// WASM runtime Swap runs <script> tags in html — only swap trusted HTML.
func (htmxAPI) Swap(target, html string, s ...SwapStrategy) {}

// Process wires hx-* attributes on a freshly inserted subtree (server-side no-op).
func (htmxAPI) Process(target string) {}

// Trigger dispatches htmx event e on the target element (server-side no-op).
func (htmxAPI) Trigger(target string, e HtmxEvent, detail ...map[string]any) {}

// On registers a subtree- and lifetime-scoped handler for htmx event e; it auto-
// detaches on component teardown (server-side no-op).
func (htmxAPI) On(e HtmxEvent, h func(Event)) {}

// OnGlobal registers a page-wide, lifetime-scoped handler for htmx event e
// (server-side no-op).
func (htmxAPI) OnGlobal(e HtmxEvent, h func(Event)) {}

// Off detaches On/OnGlobal listeners for event e in the active scope (server-side
// no-op). The h argument is accepted for symmetry but ignored (Go funcs are not
// comparable).
func (htmxAPI) Off(e HtmxEvent, h func(Event)) {}

// AddClass adds class to the target element (server-side no-op).
func (htmxAPI) AddClass(target, class string) {}

// RemoveClass removes class from the target element (server-side no-op).
func (htmxAPI) RemoveClass(target, class string) {}

// ToggleClass toggles class on the target element (server-side no-op).
func (htmxAPI) ToggleClass(target, class string) {}

// TakeClass adds class to the target and removes it from siblings (server-side no-op).
func (htmxAPI) TakeClass(target, class string) {}

// Find returns the first element matching sel (server-side no-op: zero JSValue).
func (htmxAPI) Find(sel string) JSValue { return JSValue{} }

// FindAll returns all elements matching sel (server-side no-op: nil).
func (htmxAPI) FindAll(sel string) []JSValue { return nil }

// Closest returns the nearest ancestor of target matching sel (server-side no-op).
func (htmxAPI) Closest(target, sel string) JSValue { return JSValue{} }

// Values returns the input values htmx would submit from target (server-side no-op).
func (htmxAPI) Values(target string) map[string]any { return map[string]any{} }

// Remove removes the target element from the DOM (server-side no-op).
func (htmxAPI) Remove(target string) {}
