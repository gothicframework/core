//go:build js && wasm

package runtime

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"syscall/js"
	"unsafe"
)

// withBodyHash resolves the AWS body-hash header for a Fetch, then calls next.
//
// It calls next SYNCHRONOUSLY when no signature is needed — which is every non-AWS
// app and every local dev run — so the common path keeps the exact shape it had
// before signing existed. Only on AWS does it go through a promise.
//
// The hash comes from window.crypto.subtle.digest rather than Go's crypto/sha256:
// this code is compiled into every page and component binary, and linking a Go
// SHA-256 here cost +37 KB brotli in each one. See fetchsign.go for the numbers and
// the alternatives that were rejected.
//
// A failure to hash calls next WITHOUT the header rather than aborting: the request
// then gets a clean 403 from CloudFront, which is visible and traceable, instead of
// vanishing inside the runtime. The reason is logged.
func withBodyHash(url string, cfg *FetchConfig, next func()) {
	if !shouldSignFetch(providerIsAWS(), sameOriginURL(url, pageOrigin())) {
		next()
		return
	}
	// A caller-supplied header wins: they may be reproducing a payload we cannot
	// see, and silently overwriting it would be worse than trusting it.
	if _, ok := cfg.Headers[contentSha256Header]; ok {
		next()
		return
	}

	body := fetchBodyBytes(cfg.Body, cfg.BodyBytes)
	if len(body) == 0 {
		setFetchHeader(cfg, contentSha256Header, emptyBodyHash)
		next()
		return
	}

	subtle := js.Global().Get("crypto")
	if subtle.Truthy() {
		subtle = subtle.Get("subtle")
	}
	if !subtle.Truthy() {
		ConsoleLog("gothic: crypto.subtle unavailable, sending", url, "unsigned")
		next()
		return
	}

	buf := js.Global().Get("Uint8Array").New(len(body))
	js.CopyBytesToJS(buf, body)

	// next ends up calling prepareFetch, which registers an OnUnmount for the
	// per-scope abort. Running that from a promise callback would attach it to
	// whatever scope happens to be active by then, so the caller's scope is
	// captured here and re-established around the continuation — the same
	// discipline FetchAsync uses for its done callback.
	sc := CaptureScope()

	var onOK, onErr js.Func
	release := func() { onOK.Release(); onErr.Release() }
	onOK = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer release()
		if len(args) > 0 {
			setFetchHeader(cfg, contentSha256Header, hexOfDigest(args[0]))
		}
		runInScope(sc, next)
		return nil
	})
	onErr = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer release()
		ConsoleLog("gothic: hashing the body of", url, "failed, sending unsigned")
		runInScope(sc, next)
		return nil
	})
	// then(f, c), never .then(f).catch(c): chained, a throw inside f would invoke
	// the catch handler f has already released. Same rule as wireFetch.
	subtle.Call("digest", "SHA-256", buf).Call("then", onOK, onErr)
}

// setFetchHeader writes into cfg.Headers, allocating the map on first use.
func setFetchHeader(cfg *FetchConfig, key, value string) {
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	cfg.Headers[key] = value
}

// hexOfDigest renders subtle.digest's ArrayBuffer as lowercase hex, the form
// x-amz-content-sha256 takes.
func hexOfDigest(buf js.Value) string {
	arr := js.Global().Get("Uint8Array").New(buf)
	n := arr.Length()
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		b := byte(arr.Index(i).Int())
		out = append(out, hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return string(out)
}

// providerMetaSelector finds the server-rendered marker that says which provider
// the app is deployed on. It is static SSR HTML, present before the WASM boots.
const providerMetaSelector = `meta[name="gothic-provider"]`

// providerIsAWS reports whether the page declares the AWS provider. Resolved once
// per instance: the marker cannot change during the page's life, and this sits on
// the Fetch hot path.
var providerIsAWSOnce struct {
	done bool
	val  bool
}

func providerIsAWS() bool {
	if providerIsAWSOnce.done {
		return providerIsAWSOnce.val
	}
	providerIsAWSOnce.done = true
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return false
	}
	meta := doc.Call("querySelector", providerMetaSelector)
	if !meta.Truthy() {
		return false
	}
	content := meta.Call("getAttribute", "content")
	if content.Type() != js.TypeString {
		return false
	}
	providerIsAWSOnce.val = content.String() == "AWS"
	return providerIsAWSOnce.val
}

// pageOrigin returns location.origin ("https://host[:port]"), or "" when it is
// unavailable — in which case nothing is treated as same-origin and no request is
// signed, which is the safe direction to fail.
func pageOrigin() string {
	loc := js.Global().Get("location")
	if !loc.Truthy() {
		return ""
	}
	o := loc.Get("origin")
	if o.Type() != js.TypeString {
		return ""
	}
	return o.String()
}

// Scope-aware DOM helpers.
//
// The naive implementation calls document.getElementById(id), which returns
// the FIRST element with that id in document order. When the same component
// is rendered twice on a page (e.g. two PingMirror instances), both modules
// call SetText("pm-local-count", ...) and both writes land on instance #1's
// text node — instance #2's local counter appears frozen.
//
// Instead we route every per-id lookup through the calling module's own
// [data-gothic-scope="<id>"] container, resolved per call via activeScope()
// (see scope.go): the explicit scope for programmatic/async paths, else the
// current user event's scope, else the mount scope. The wrapper is the element
// the bootstrap script stamps with data-gothic-scope, so a scoped querySelector
// cannot stray into a sibling component.
//
// Non-WASM callers and the rare full-page case where no bootstrap ran
// continue to work because scopeRoot() falls back to `document` when the
// resolved scope is "_default".

var document = js.Global().Get("document")

// scopeRoot returns the DOM container Go should query inside. For a module
// loaded through the WASM bootstrap, this is the [data-gothic-scope="<id>"]
// element. For pure-Go / non-bootstrap contexts (activeScope() == "_default")
// we fall back to the full document so behaviour outside the WASM path is
// unchanged.
//
// This is intentionally NOT memoised: the user may re-render the wrapper
// (HTMX swap, programmatic DOM replacement), so we resolve the root on
// every call. The cost is a single querySelector — negligible compared to
// the JS<->WASM bridge overhead the call already pays.
func scopeRoot() js.Value {
	id := activeScope()
	if id == "" || id == "_default" {
		return document
	}
	sel := `[data-gothic-scope="` + escapeAttr(id) + `"]`
	root := document.Call("querySelector", sel)
	if root.IsNull() || root.IsUndefined() {
		// Defensive fallback: if the scope wrapper was removed (e.g. an
		// HTMX swap killed the container before the module saw the event)
		// we degrade to document rather than silently no-op'ing every
		// helper. Calls land on the first id match — which is the best we
		// can do once our own wrapper has gone.
		return document
	}
	return root
}

// queryByIdInScope returns the first element with the given id INSIDE the
// calling module's scope. We use [id="..."] (attribute selector) instead of
// the `#id` form so ids containing colons or other CSS-special characters
// continue to work.
func queryByIdInScope(id string) js.Value {
	root := scopeRoot()
	if root.IsNull() || root.IsUndefined() {
		return js.Null()
	}
	sel := `[id="` + escapeAttr(id) + `"]`
	return root.Call("querySelector", sel)
}

// escapeAttr backslash-escapes characters that would break out of a
// double-quoted CSS attribute selector value. Component ids in the
// generated templates are alnum + `-` so this is defence-in-depth, but it
// keeps the helpers safe if a user-supplied id ever flows in.
func escapeAttr(s string) string {
	if !strings.ContainsAny(s, `"\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

func SetText(id, value string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	setTextZeroCopy(el, value)
}

// setTextZeroCopy assigns el.textContent WITHOUT boxing a fresh Go string into
// the TinyGo _values[] ref table on every call. `el.Set("textContent", value)`
// went through syscall/js.stringVal → boxValue, adding a _values slot per call
// that TinyGo (no finalizers) never reclaims: on a per-click SetText that is a
// permanent per-click leak.
//
// Instead we mirror the topic data-plane's zero-copy transport (dispatchDirect →
// __gothic_set → __gothic_topic.set): hand JS the string's linear-memory
// (ptr,len) as plain numbers and let a per-instance bootstrap closure (__setText,
// installed on this module's window.__gothicInstances[<mountScope>] slot next to
// go/instance) TextDecoder the bytes straight out of the module's own
// instance.exports.memory.buffer and assign el.textContent. Numbers are NaN-boxed
// (never stored in _values); `el` is the querySelector-deduped DOM node already in
// _ids (reused slot, not a new one); the setter function, its slot object and the
// __gothicInstances container are likewise deduped. Net: zero new _values slots
// per SetText.
//
// The setter hangs off the instance slot keyed by bootstrapScope() (the mount
// scope) — the SAME slot as __halt and the go/instance handles — so it is torn
// down for free: __gothicTeardown deletes __gothicInstances[id], releasing the
// closure and its captured instance with no separate cleanup and no change to the
// shared gothic-core.js asset. Reading from the mount-scope instance is always
// correct: it selects which instance's linear MEMORY to read, not the event's
// [data-gothic-scope]; a multiplexed instance's hosted scopes share one memory,
// and `el` (resolved via the event scope) is passed in explicitly.
//
// If the setter is absent (pure-Go / non-bootstrap contexts, e.g. tests or a
// hand-rolled envelope) we fall back to the plain boxed Set so behaviour is
// unchanged outside the standard WASM envelope.
func setTextZeroCopy(el js.Value, s string) {
	insts := js.Global().Get("__gothicInstances")
	if insts.IsUndefined() || insts.IsNull() {
		el.Set("textContent", s)
		return
	}
	slot := insts.Get(bootstrapScope())
	if slot.IsUndefined() || slot.IsNull() {
		el.Set("textContent", s)
		return
	}
	fn := slot.Get("__setText")
	if fn.IsUndefined() || fn.IsNull() {
		el.Set("textContent", s)
		return
	}
	// unsafe.StringData on an empty string may return an invalid pointer; keep
	// ptr==0 so the JS side constructs a zero-length Uint8Array at a valid offset.
	var ptr int32
	if len(s) > 0 {
		ptr = int32(uintptr(unsafe.Pointer(unsafe.StringData(s))))
	}
	fn.Invoke(el, js.ValueOf(ptr), js.ValueOf(len(s)))
	// Keep s alive until JS has finished reading its bytes. The Invoke is
	// synchronous (the closure decodes + assigns before returning), so the
	// backing array cannot be collected mid-read.
	runtime.KeepAlive(s)
}

func SetHTML(id, html string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Set("innerHTML", html)
}

func SetValue(id, value string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Set("value", value)
}

func GetValue(id string) string {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return ""
	}
	return el.Get("value").String()
}

// GetFileBytes reads the contents of the first selected file from a <input type="file"> element.
// Blocks until the FileReader completes. Returns nil on error or if no file is selected.
func GetFileBytes(id string) []byte {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return nil
	}
	files := el.Get("files")
	if files.IsNull() || files.IsUndefined() || files.Get("length").Int() == 0 {
		return nil
	}
	file := files.Index(0)

	type result struct {
		data []byte
		ok   bool
	}
	ch := make(chan result, 1)

	reader := js.Global().Get("FileReader").New()

	var onLoad, onError js.Func
	onLoad = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		onError.Release()
		arrayBuffer := reader.Get("result")
		uint8Array := js.Global().Get("Uint8Array").New(arrayBuffer)
		data := make([]byte, uint8Array.Get("length").Int())
		js.CopyBytesToGo(data, uint8Array)
		ch <- result{data: data, ok: true}
		return nil
	})
	onError = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		onLoad.Release()
		onError.Release()
		ch <- result{ok: false}
		return nil
	})

	reader.Set("onload", onLoad)
	reader.Set("onerror", onError)
	reader.Call("readAsArrayBuffer", file)

	restore := parkScope()
	r := <-ch
	restore()
	if !r.ok {
		return nil
	}
	return r.data
}

func AddClass(id, className string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Get("classList").Call("add", className)
}

func RemoveClass(id, className string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Get("classList").Call("remove", className)
}

func ToggleClass(id, className string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Get("classList").Call("toggle", className)
}

func SetAttr(id, attr, value string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Call("setAttribute", attr, value)
}

func SetStyle(id, property, value string) {
	el := queryByIdInScope(id)
	if el.IsNull() || el.IsUndefined() {
		return
	}
	el.Get("style").Set(property, value)
}

// FetchConfig configures an HTTP request made via Fetch.
type FetchConfig struct {
	Method    string
	Headers   map[string]string
	Body      string
	BodyBytes []byte
	Query     map[string]string
}

// Response is the result of a Fetch call. Body always holds the raw response
// bytes (read via arrayBuffer, so binary payloads are not UTF-8-corrupted);
// use Text()/Bytes()/OK() to consume it.
type Response struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// Text returns the response body decoded as a UTF-8 string.
func (r Response) Text() string { return string(r.Body) }

// Bytes returns the raw response body bytes.
func (r Response) Bytes() []byte { return r.Body }

// OK reports whether the response status is in the 2xx (200..299) range.
func (r Response) OK() bool { return r.Status >= 200 && r.Status < 300 }

// MapAny parses the response body as a JSON OBJECT and returns it as a
// map[string]any. It uses the runtime's reflection-free parser (jsonparse.go, no
// encoding/json / reflect — TinyGo-safe) so it never panics on malformed input:
// invalid JSON yields a non-nil error. Values are coerced per D5 — nested objects
// become map[string]any, arrays []any, strings string, numbers float64 (int64
// magnitudes above 2^53 lose precision), booleans bool and null nil.
//
// The top-level JSON value MUST be an object; a body whose root is an array,
// string, number, bool or null returns an error (use parseJSON-shaped helpers for
// those). Typical use:
//
//	resp, err := Fetch("/api/user/1")
//	if err == nil && resp.OK() {
//	    m, err := resp.MapAny()
//	    if err == nil { name, _ := m["name"].(string) }
//	}
func (r Response) MapAny() (map[string]any, error) {
	v, err := parseJSON(r.Body)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("gothic json: response body is not a JSON object")
	}
	return m, nil
}

// FetchResult pairs a Response with its error for delivery over a channel. It is
// what FetchChan sends: exactly one FetchResult per request, so a caller can
// `r := <-FetchChan(url)` and read r.Response / r.Err.
type FetchResult struct {
	Response Response
	Err      error
}

// errAborted is the sentinel returned by the blocking Fetch when the instance
// halt channel (GothicHaltChan) fires before the request completes — the
// per-scope AbortController is fired and the in-flight request is cancelled.
var errAborted = errors.New("gothic: fetch aborted")

// prepareFetch resolves the request URL (appending query params) and builds the
// JS `init` object (method/headers/body). It also creates a per-scope
// AbortController, wires its signal into `init`, and registers an OnUnmount hook
// so the request is aborted when the calling component's [data-gothic-scope] is
// torn down (D4 cancellation). It MUST be called synchronously while the scope
// is still active (before any parkScope / channel suspension). The returned
// AbortController lets the blocking Fetch additionally abort on instance halt;
// the returned deregister func drops the abort hook once the request settles so
// a repeatedly-fetching instance does not accumulate a js.Func + AbortController
// per request (called exactly once from wireFetch's settle path).
func prepareFetch(url string, cfg FetchConfig) (string, js.Value, js.Value, fetchLifetime) {
	if cfg.Method == "" {
		cfg.Method = "GET"
	}

	// Build URL with query parameters
	if len(cfg.Query) > 0 {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		for k, v := range cfg.Query {
			url += sep + js.Global().Get("encodeURIComponent").Invoke(k).String() +
				"=" + js.Global().Get("encodeURIComponent").Invoke(v).String()
			sep = "&"
		}
	}

	// Build fetch init object
	init := js.Global().Get("Object").New()
	init.Set("method", cfg.Method)

	if len(cfg.Headers) > 0 {
		headers := js.Global().Get("Object").New()
		for k, v := range cfg.Headers {
			headers.Set(k, v)
		}
		init.Set("headers", headers)
	}

	if cfg.Body != "" {
		init.Set("body", cfg.Body)
	} else if len(cfg.BodyBytes) > 0 {
		uint8Array := js.Global().Get("Uint8Array").New(len(cfg.BodyBytes))
		js.CopyBytesToJS(uint8Array, cfg.BodyBytes)
		init.Set("body", uint8Array)
	}

	// Per-scope cancellation (D4): attach an AbortSignal and fire it from the
	// component's teardown so an in-flight request is cancelled on unmount.
	ac := js.Global().Get("AbortController").New()
	init.Set("signal", ac.Get("signal"))

	// torndown lets wireFetch drop a settlement that lands after teardown: the abort below rejects the promise on a LATER turn, when the scope is already gone.
	torndown := new(bool)
	dropAbort := OnUnmount(func() {
		*torndown = true
		ac.Call("abort")
	})

	return url, init, ac, fetchLifetime{torndown: torndown, dropAbort: dropAbort}
}

// fetchLifetime tells wireFetch whether the request outlived its component, and how to drop the abort hook once it settles.
type fetchLifetime struct {
	torndown  *bool
	dropAbort func()
}

// The nil guards keep a zero-value lifetime from killing the instance: a nil deref is fatal on wasm32, with no recover.
func (l fetchLifetime) dead() bool { return l.torndown != nil && *l.torndown }

func (l fetchLifetime) release() {
	if l.dropAbort != nil {
		l.dropAbort()
	}
}

// wireFetch attaches then/catch (plus the nested arrayBuffer then/catch) to a
// fetch promise and delivers the assembled Response — or an error — exactly once
// via `deliver`. The body is read via arrayBuffer (never text()) so binary
// payloads are preserved byte-for-byte; use Response.Text() for a UTF-8 view.
//
// js.Func lifecycle: thenFn/catchFn (and bufThen/bufCatch) are RETAINED until
// they fire — they are NOT appended to `keep` (that would leak, they are
// one-shot) and NOT released before their callback runs. Each pair releases
// both of itself on entry, so no js.Func outlives the single delivery. `deliver`
// must not block (all call sites send on a cap-1 channel or run a user callback).
//
// `deregister` drops the per-scope abort hook created by prepareFetch. It runs
// via `settle` — which wraps every terminal path (bufThen success, bufCatch,
// outer catchFn) — so it fires EXACTLY ONCE the moment the request completes,
// BEFORE the result is delivered. This bounds the leak: once a request settles
// its AbortController + abort js.Func are reclaimed, so a polling/concurrent
// instance holds at most one per IN-FLIGHT request, not one per request ever.
func wireFetch(promise js.Value, life fetchLifetime, deliver func(Response, error)) {
	// settle drops the (now-dead) abort registration, then delivers. deregister
	// is idempotent; deliver is invoked once per request across all paths.
	//
	// A settlement arriving after teardown is dropped: it is that teardown's own abort coming back, and delivering it would run a user callback in a dead scope.
	settle := func(resp Response, err error) {
		if life.dead() {
			return
		}
		life.release()
		deliver(resp, err)
	}

	var thenFn, catchFn js.Func
	thenFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		thenFn.Release()
		catchFn.Release()
		// Nothing downstream would be read, so skip reading the body too.
		if life.dead() {
			return nil
		}
		resp := args[0]

		// Capture status and headers synchronously off the Response object
		// while it is in hand — these are discarded once we await the body.
		status := resp.Get("status").Int()
		headers := map[string]string{}
		hdrs := resp.Get("headers")
		if !hdrs.IsUndefined() && !hdrs.IsNull() {
			// Headers.forEach invokes its callback as (value, key, object),
			// synchronously — safe to Release the one-shot Func right after.
			var forEachCb js.Func
			forEachCb = js.FuncOf(func(_ js.Value, a []js.Value) interface{} {
				if len(a) >= 2 {
					headers[a[1].String()] = a[0].String()
				}
				return nil
			})
			hdrs.Call("forEach", forEachCb)
			forEachCb.Release()
		}

		// Read the body via arrayBuffer() (never text()) so binary payloads are
		// not UTF-8-corrupted; Response.Text() decodes to a string on demand.
		var bufThen, bufCatch js.Func
		bufThen = js.FuncOf(func(_ js.Value, a []js.Value) interface{} {
			bufThen.Release()
			bufCatch.Release()
			if life.dead() {
				return nil
			}
			uint8Array := js.Global().Get("Uint8Array").New(a[0])
			data := make([]byte, uint8Array.Get("length").Int())
			js.CopyBytesToGo(data, uint8Array)
			settle(Response{Status: status, Headers: headers, Body: data}, nil)
			return nil
		})
		bufCatch = js.FuncOf(func(_ js.Value, a []js.Value) interface{} {
			bufThen.Release()
			bufCatch.Release()
			settle(Response{}, errors.New(a[0].String()))
			return nil
		})
		// then(f, c), never .then(f).catch(c): chained, the reject handler sits on the promise then RETURNS, so a throw inside bufThen would invoke the bufCatch it just Released.
		resp.Call("arrayBuffer").Call("then", bufThen, bufCatch)
		return nil
	})
	catchFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		thenFn.Release()
		catchFn.Release()
		settle(Response{}, errors.New(args[0].String()))
		return nil
	})

	// Both handlers on the same promise, as above, so exactly one runs and the mutual Release is safe.
	promise.Call("then", thenFn, catchFn)
}

// Fetch makes an HTTP request using the browser's fetch API and blocks until
// complete. The body is read via arrayBuffer (never text()) so binary responses
// are preserved byte-for-byte; use Response.Text() for a UTF-8 string view.
//
// Cancellation (D4): the request carries a per-scope AbortController fired on
// component teardown (OnUnmount). As a backstop, the blocking receive also
// selects on GothicHaltChan — if the whole instance is halted before the
// response arrives, the request is aborted and errAborted is returned.
func Fetch(url string, config ...FetchConfig) (Response, error) {
	var cfg FetchConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	ch := make(chan FetchResult, 1)
	// ac is only known once the request is actually dispatched, which on the AWS
	// signing path happens after the body hash resolves. A halt arriving before
	// that has nothing to abort — the fetch has not started — so the zero Value is
	// the correct state, not a missing one.
	var ac js.Value
	withBodyHash(url, &cfg, func() {
		u, init, a, life := prepareFetch(url, cfg)
		ac = a
		wireFetch(js.Global().Call("fetch", u, init), life, func(resp Response, err error) {
			ch <- FetchResult{Response: resp, Err: err}
		})
	})

	restore := parkScope()
	defer restore()
	select {
	case r := <-ch:
		return r.Response, r.Err
	case <-GothicHaltChan():
		if ac.Truthy() {
			ac.Call("abort")
		}
		return Response{}, errAborted
	}
}

// FetchAsync makes an HTTP request and invokes done(Response, error) when it
// completes — it does NOT block and starts no goroutine (the browser drives the
// promise). done runs inside the scope active at the call site (captured here
// and re-established via runInScope) so DOM writes hit the right subtree. The
// request is aborted on component teardown via a per-scope AbortController.
func FetchAsync(url string, cfg FetchConfig, done func(Response, error)) {
	sc := CaptureScope()
	// withBodyHash is synchronous unless the request is being signed for AWS, so
	// the no-goroutine, no-block contract above holds either way: on the signing
	// path the dispatch simply moves into the digest's promise callback, which is
	// still the browser driving it.
	withBodyHash(url, &cfg, func() {
		u, init, _, life := prepareFetch(url, cfg)
		wireFetch(js.Global().Call("fetch", u, init), life, func(resp Response, err error) {
			runInScope(sc, func() { done(resp, err) })
		})
	})
}

// FetchChan makes an HTTP request and returns a receive-only channel that yields
// exactly one FetchResult when the request completes. It does NOT block — the
// caller selects on the returned channel. The channel is buffered (cap 1) so the
// promise callback never blocks even if the caller stops receiving. The request
// is aborted on component teardown via a per-scope AbortController.
func FetchChan(url string, cfg ...FetchConfig) <-chan FetchResult {
	var c FetchConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	ch := make(chan FetchResult, 1)
	// The channel is returned immediately either way; on the signing path it is
	// simply filled one promise turn later.
	withBodyHash(url, &c, func() {
		u, init, _, life := prepareFetch(url, c)
		wireFetch(js.Global().Call("fetch", u, init), life, func(resp Response, err error) {
			ch <- FetchResult{Response: resp, Err: err}
		})
	})
	return ch
}

// JSValue wraps js.Value. Never create as a literal; use JS(), Document(), etc.
type JSValue struct{ v js.Value }

func JS() JSValue       { return JSValue{js.Global()} }
func Window() JSValue   { return JS() }
func Document() JSValue { return JSValue{js.Global().Get("document")} }

func ConsoleLog(args ...any) { js.Global().Get("console").Call("log", toJSArgs(args)...) }

func GetElementById(id string) JSValue {
	return JSValue{js.Global().Get("document").Call("getElementById", id)}
}
func CreateElement(tag string) JSValue {
	return JSValue{js.Global().Get("document").Call("createElement", tag)}
}
func QuerySelector(sel string) JSValue {
	return JSValue{js.Global().Get("document").Call("querySelector", sel)}
}
func QuerySelectorAll(sel string) JSValue {
	return JSValue{js.Global().Get("document").Call("querySelectorAll", sel)}
}

func (v JSValue) Get(key string) JSValue  { return JSValue{v.v.Get(key)} }
func (v JSValue) Set(key string, val any) { v.v.Set(key, toJSVal(val)) }
func (v JSValue) Call(method string, args ...any) JSValue {
	return JSValue{v.v.Call(method, toJSArgs(args)...)}
}
func (v JSValue) New(args ...any) JSValue { return JSValue{v.v.New(toJSArgs(args)...)} }
func (v JSValue) String() string          { return v.v.String() }
func (v JSValue) Int() int                { return v.v.Int() }
func (v JSValue) Float() float64          { return v.v.Float() }
func (v JSValue) Bool() bool              { return v.v.Bool() }
func (v JSValue) IsNull() bool            { return v.v.IsNull() }
func (v JSValue) IsUndefined() bool       { return v.v.IsUndefined() }
func (v JSValue) Truthy() bool            { return v.v.Truthy() }
func (v JSValue) Index(i int) JSValue     { return JSValue{v.v.Index(i)} }
func (v JSValue) SetIndex(i int, val any) { v.v.SetIndex(i, toJSVal(val)) }
func (v JSValue) Length() int             { return v.v.Length() }

func CopyBytesToJS(dst JSValue, src []byte) int { return js.CopyBytesToJS(dst.v, src) }
func CopyBytesToGo(dst []byte, src JSValue) int { return js.CopyBytesToGo(dst, src.v) }

func toJSVal(v any) any {
	switch x := v.(type) {
	case JSValue:
		return x.v
	default:
		return js.ValueOf(x)
	}
}

func toJSArgs(args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = toJSVal(a)
	}
	return out
}

// TriggerDownload prompts the browser to download `data` as a file named `filename` with the given MIME type.
func TriggerDownload(filename string, data []byte, mimeType string) {
	uint8Array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(uint8Array, data)
	blobParts := js.Global().Get("Array").New(uint8Array)
	opts := js.Global().Get("Object").New()
	opts.Set("type", mimeType)
	blob := js.Global().Get("Blob").New(blobParts, opts)
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	a := js.Global().Get("document").Call("createElement", "a")
	a.Set("href", url)
	a.Set("download", filename)
	js.Global().Get("document").Get("body").Call("appendChild", a)
	a.Call("click")
	js.Global().Get("document").Get("body").Call("removeChild", a)
	js.Global().Get("URL").Call("revokeObjectURL", url)
}

// AddEventListenerWithEvent attaches a persistent event listener to el for the given event name.
// fn receives the browser Event object as a JSValue, giving access to event properties and methods
// such as preventDefault(), stopPropagation(), event.target, event.key, event.detail, etc.
// The js.Func is retained in keep and never released — persistent listeners must stay
// alive for the lifetime of the page; releasing them would panic on the next event.
func AddEventListenerWithEvent(el JSValue, event string, fn func(JSValue)) {
	f := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		var ev JSValue
		if len(args) > 0 {
			ev = JSValue{args[0]}
		}
		fn(ev)
		return nil
	})
	keep = append(keep, f)
	el.v.Call("addEventListener", event, f)
}

// AddEventListener attaches a persistent event listener to el for the given event name.
// fn is called with no arguments each time the event fires.
// The js.Func is retained in keep and never released — persistent listeners must stay
// alive for the lifetime of the page; releasing them would panic on the next event.
func AddEventListener(el JSValue, event string, fn func()) {
	f := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		fn()
		return nil
	})
	keep = append(keep, f)
	el.v.Call("addEventListener", event, f)
}

// Element tree helpers.
func AppendChild(parent, child JSValue) { parent.v.Call("appendChild", child.v) }
func RemoveElement(el JSValue)          { el.v.Call("remove") }
func ClickElement(el JSValue)           { el.v.Call("click") }

// WriteClipboard writes the given text to the system clipboard via navigator.clipboard.writeText.
func WriteClipboard(text string) {
	js.Global().Get("navigator").Get("clipboard").Call("writeText", text)
}

// ExecJS executes the given script string in the global scope.
func ExecJS(script string) {
	js.Global().Call("eval", script)
}

// Navigation helpers.
func Navigate(url string) { js.Global().Get("location").Set("href", url) }
func Reload()             { js.Global().Get("location").Call("reload") }
func PushState(url, title string) {
	js.Global().Get("history").Call("pushState", js.Null(), title, url)
}
func GoBack() { js.Global().Get("history").Call("back") }

// LocalStorage helpers
func LocalStorageSet(key, value string) {
	js.Global().Get("localStorage").Call("setItem", key, value)
}

func LocalStorageGet(key string) string {
	v := js.Global().Get("localStorage").Call("getItem", key)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

func LocalStorageRemove(key string) {
	js.Global().Get("localStorage").Call("removeItem", key)
}

// SessionStorage helpers
func SessionStorageSet(key, value string) {
	js.Global().Get("sessionStorage").Call("setItem", key, value)
}

func SessionStorageGet(key string) string {
	v := js.Global().Get("sessionStorage").Call("getItem", key)
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	return v.String()
}

func SessionStorageRemove(key string) {
	js.Global().Get("sessionStorage").Call("removeItem", key)
}

// CookieOptions configures CookieSet behaviour.
type CookieOptions struct {
	MaxAge   int    // seconds; 0 = session cookie
	Path     string // defaults to "/"
	SameSite string // "Strict", "Lax", or "None"
	Secure   bool
}

// CookieSet writes a cookie to document.cookie.
func CookieSet(key, value string, opts ...CookieOptions) {
	path := "/"
	maxAge := 0
	sameSite := ""
	secure := false
	if len(opts) > 0 {
		o := opts[0]
		if o.Path != "" {
			path = o.Path
		}
		maxAge = o.MaxAge
		sameSite = o.SameSite
		secure = o.Secure
	}
	cookie := key + "=" + value + "; Path=" + path
	if maxAge != 0 {
		cookie += "; Max-Age=" + strconv.Itoa(maxAge)
	}
	if sameSite != "" {
		cookie += "; SameSite=" + sameSite
	}
	if secure {
		cookie += "; Secure"
	}
	js.Global().Get("document").Set("cookie", cookie)
}

// CookieGet reads a cookie value from document.cookie.
// Returns "" for missing or HttpOnly cookies.
func CookieGet(key string) string {
	raw := js.Global().Get("document").Get("cookie").String()
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, key+"=") {
			return part[len(key)+1:]
		}
	}
	return ""
}

// CookieDelete expires a cookie immediately.
func CookieDelete(key string) {
	CookieSet(key, "", CookieOptions{MaxAge: -1})
}
