//go:build js && wasm

package runtime

import "syscall/js"

// HTMX is an ergonomic Go mirror of the htmx 2.0.3 JavaScript API (window.htmx,
// a classic global installed by the static core WASM at boot). It lets a
// ClientSideState block drive htmx imperatively — swap fragments, fire and
// listen for htmx events, toggle classes, run ajax — without hand-writing
// js.Global().Get("htmx") plumbing. Every method is a safe no-op when htmx has
// not yet loaded (the static core installs it during its boot, so at first paint
// the global may be absent); calls guard js.htmx.IsUndefined() rather than caching
// the ref, re-fetching it lazily on each call.
var HTMX htmxAPI

// htmxAPI is the unexported receiver type behind the HTMX singleton.
type htmxAPI struct{}

// Event is the browser Event passed to On/OnGlobal handlers. It is the same thin
// js.Value wrapper the rest of the runtime uses, so handlers can read
// e.Get("detail"), e.Get("target"), call e.Call("preventDefault"), etc.
type Event = JSValue

// SwapStrategy is a string-backed htmx swap style (the hx-swap vocabulary). It
// is a plain typed string so the eight canonical values give autocomplete while
// a custom style stays reachable via a string cast, e.g.
// SwapStrategy("innerHTML show:top").
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

// HtmxEvent is a string-backed htmx event name. The consts below are the full
// htmx 2.0.3 event catalog (verified against the htmx 2.0.3 source); a custom
// or extension event stays reachable via a string cast, e.g.
// HtmxEvent("htmx:sse:message").
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

// AjaxOpts is the optional context for HTMX.Ajax, mapping onto htmx's ajax()
// context object. Zero fields are omitted, so an empty AjaxOpts behaves like a
// bare htmx.ajax(verb, url).
type AjaxOpts struct {
	Target string            // CSS selector for where to swap the response
	Source string            // CSS selector for the request's source element
	Swap   SwapStrategy      // swap style for the response
	Values map[string]string // extra values sent with the request
}

// htmxJS lazily fetches window.htmx. htmx is injected `defer` in <head> (after
// gothic-core + boot), so it may be undefined at first paint; we never cache the
// ref — we re-fetch each call and every method guards IsUndefined() so an early
// call is a safe no-op instead of a panic.
func htmxJS() js.Value { return js.Global().Get("htmx") }

// doc returns window.document.
func doc() js.Value { return js.Global().Get("document") }

// Ajax issues an htmx AJAX request (htmx.ajax(verb, url, context)). With no opts
// it is a bare htmx.ajax(verb, url); an AjaxOpts becomes htmx's context object
// (target/source/swap/values). The request runs on htmx's own promise — this is
// fire-and-forget from Go.
func (htmxAPI) Ajax(method, url string, opts ...AjaxOpts) {
	hx := htmxJS()
	if hx.IsUndefined() {
		return
	}
	if len(opts) == 0 {
		hx.Call("ajax", method, url)
		return
	}
	o := opts[0]
	ctx := js.Global().Get("Object").New()
	if o.Target != "" {
		ctx.Set("target", o.Target)
	}
	if o.Source != "" {
		ctx.Set("source", o.Source)
	}
	if o.Swap != "" {
		ctx.Set("swap", string(o.Swap))
	}
	if len(o.Values) > 0 {
		vals := js.Global().Get("Object").New()
		for k, v := range o.Values {
			vals.Set(k, v)
		}
		ctx.Set("values", vals)
	}
	hx.Call("ajax", method, url, ctx)
}

// Swap replaces content at the target selector with html using the given swap
// style (default InnerHTML), then runs htmx.process on the target so any nested
// Gothic stateful components / hx-* attributes in the injected HTML boot.
//
// SECURITY: Swap runs with htmx's allowScriptTags behavior — <script> tags in
// html execute. Only ever swap HTML you trust (your own templates / your BFF),
// never untrusted or user-supplied markup, or you open an XSS hole.
func (htmxAPI) Swap(target, html string, s ...SwapStrategy) {
	hx := htmxJS()
	if hx.IsUndefined() {
		return
	}
	style := InnerHTML
	if len(s) > 0 && s[0] != "" {
		style = s[0]
	}
	spec := js.Global().Get("Object").New()
	spec.Set("swapStyle", string(style))
	hx.Call("swap", target, html, spec)
	// Boot nested stateful components / hx-* wiring in the swapped subtree.
	// htmx.process resolves a selector string itself (kt: e=y(e)).
	hx.Call("process", target)
}

// Process runs htmx.process on the target, wiring hx-* attributes on any freshly
// inserted subtree (e.g. after you injected HTML by another means).
func (htmxAPI) Process(target string) {
	hx := htmxJS()
	if hx.IsUndefined() {
		return
	}
	hx.Call("process", target)
}

// Trigger dispatches htmx event e on the target element (htmx.trigger). An
// optional detail map becomes the event's detail object; values must be JS-
// encodable primitives (string/number/bool).
func (htmxAPI) Trigger(target string, e HtmxEvent, detail ...map[string]any) {
	hx := htmxJS()
	if hx.IsUndefined() {
		return
	}
	if len(detail) > 0 && detail[0] != nil {
		d := js.Global().Get("Object").New()
		for k, v := range detail[0] {
			d.Set(k, toJSVal(v))
		}
		hx.Call("trigger", target, string(e), d)
		return
	}
	hx.Call("trigger", target, string(e))
}

// On registers a handler for htmx event e that is BOTH lifetime-scoped and
// subtree-scoped: the listener is attached to this component's
// [data-gothic-scope] root (so it only sees events bubbling from the component's
// own subtree) and is automatically detached + its js.Func released on component
// teardown via OnUnmount. The handler runs under the scope captured at
// registration, so scoped DOM writes hit the right subtree. Use OnGlobal for a
// page-wide listener.
func (htmxAPI) On(e HtmxEvent, h func(Event)) { hxRegister(true, e, h) }

// OnGlobal registers a page-wide handler for htmx event e (attached to
// document.body, htmx's own "global" listener target). Like On it is lifetime-
// scoped: automatically detached and released on component teardown.
func (htmxAPI) OnGlobal(e HtmxEvent, h func(Event)) { hxRegister(false, e, h) }

// Off detaches htmx event listeners registered via On/OnGlobal for event e in
// the ACTIVE scope and releases their js.Funcs. NOTE: Go function values are not
// comparable, so Off cannot single out one specific handler — the h argument is
// accepted for API symmetry but ignored, and Off removes every On/OnGlobal
// registration for event e in the current scope. In practice you rarely need it:
// On/OnGlobal already auto-detach on teardown; Off is the early-removal escape
// hatch.
func (htmxAPI) Off(e HtmxEvent, h func(Event)) {
	hx := htmxJS()
	if hx.IsUndefined() {
		return
	}
	scope := activeScope()
	for _, r := range hxRegs {
		if r.scope == scope && r.event == string(e) {
			r.release(hx)
		}
	}
}

// AddClass adds class to the target element (htmx.addClass).
func (htmxAPI) AddClass(target, class string) {
	if hx := htmxJS(); !hx.IsUndefined() {
		hx.Call("addClass", target, class)
	}
}

// RemoveClass removes class from the target element (htmx.removeClass).
func (htmxAPI) RemoveClass(target, class string) {
	if hx := htmxJS(); !hx.IsUndefined() {
		hx.Call("removeClass", target, class)
	}
}

// ToggleClass toggles class on the target element (htmx.toggleClass).
func (htmxAPI) ToggleClass(target, class string) {
	if hx := htmxJS(); !hx.IsUndefined() {
		hx.Call("toggleClass", target, class)
	}
}

// TakeClass adds class to the target and removes it from all sibling elements
// (htmx.takeClass) — useful for single-active-item states like tabs.
func (htmxAPI) TakeClass(target, class string) {
	if hx := htmxJS(); !hx.IsUndefined() {
		hx.Call("takeClass", target, class)
	}
}

// Find returns the first element matching sel (htmx.find → document.querySelector),
// or a null JSValue when htmx is absent or nothing matches.
func (htmxAPI) Find(sel string) JSValue {
	hx := htmxJS()
	if hx.IsUndefined() {
		return JSValue{}
	}
	return JSValue{hx.Call("find", sel)}
}

// FindAll returns all elements matching sel (htmx.findAll), or nil when htmx is
// absent.
func (htmxAPI) FindAll(sel string) []JSValue {
	hx := htmxJS()
	if hx.IsUndefined() {
		return nil
	}
	list := hx.Call("findAll", sel)
	if list.IsNull() || list.IsUndefined() {
		return nil
	}
	n := list.Length()
	out := make([]JSValue, n)
	for i := 0; i < n; i++ {
		out[i] = JSValue{list.Index(i)}
	}
	return out
}

// Closest returns the nearest ancestor of target (inclusive) matching sel
// (htmx.closest), or a null JSValue when htmx is absent or nothing matches.
func (htmxAPI) Closest(target, sel string) JSValue {
	hx := htmxJS()
	if hx.IsUndefined() {
		return JSValue{}
	}
	return JSValue{hx.Call("closest", target, sel)}
}

// Values returns the input values htmx would submit from the target element /
// form (htmx.values) as a Go map. Scalar fields come back as string/number/bool;
// multi-value fields as a []any. Returns an empty map when htmx is absent or the
// target is not found.
func (htmxAPI) Values(target string) map[string]any {
	hx := htmxJS()
	if hx.IsUndefined() {
		return map[string]any{}
	}
	// htmx.values reads htmx-internal-data off the element, so it needs a real
	// element, not a selector string — resolve it first.
	el := doc().Call("querySelector", target)
	if el.IsNull() || el.IsUndefined() {
		return map[string]any{}
	}
	return jsObjectToMap(hx.Call("values", el))
}

// Remove removes the target element from the DOM (htmx.remove).
func (htmxAPI) Remove(target string) {
	if hx := htmxJS(); !hx.IsUndefined() {
		hx.Call("remove", target)
	}
}

// ── internal listener plumbing ───────────────────────────────────────────────

// hxReg records one On/OnGlobal listener so Off and teardown can detach + release
// it exactly once. sel == "" means the listener is document-wide (2-arg htmx.on).
//
// bound tracks whether htmx.on has actually run yet: On/OnGlobal may be called
// from a component's ClientSideState BEFORE the static core WASM has installed
// window.htmx (the core boots asynchronously and can land after a component), in
// which case the real htmx.on is deferred until the core announces readiness (see
// hxRegister / deferHxBind). release() must therefore only htmx.off a listener it
// actually attached.
type hxReg struct {
	scope    string
	event    string
	sel      string
	f        js.Func
	bound    bool
	released bool
}

// hxRegs is the append-only registry of live htmx listeners created via
// On/OnGlobal. Entries are marked released (never spliced) so Off and OnUnmount
// can both run idempotently. WASM is single-threaded, so no locking is needed.
var hxRegs []*hxReg

// release detaches the listener (htmx.off) and releases its js.Func, once.
//
// The off() call is wrapped in a recover: on the normal teardown path the scope
// root [data-gothic-scope="X"] has ALREADY been removed from the DOM (the
// MutationObserver fires __gothicTeardown on removedNodes), so htmx.off →
// document.querySelector(sel) → null → null.removeEventListener throws a JS
// TypeError, which surfaces as a Go panic. That is harmless — the browser
// already dropped the detached element's listeners — but it must not (a) skip the
// Release below (the js.Func's _values bridge slot is otherwise leaked, one per
// On per mount → monotonic growth on a repeatedly-swapped component) nor (b)
// escape this js.Func and trap the TinyGo instance mid-teardown. So we swallow
// the off() throw and ALWAYS Release. For the Off()-while-mounted path the root
// still exists, off() succeeds, and Release runs exactly once (released guard).
func (r *hxReg) release(hx js.Value) {
	if r.released {
		return
	}
	r.released = true
	// Only detach a listener we actually attached. A deferred registration whose
	// core never came online (or that tore down before it did) is unbound — there
	// is nothing to htmx.off, and hx may even be undefined here.
	if r.bound && !hx.IsUndefined() {
		func() {
			defer func() { _ = recover() }()
			hxOnOff(hx, "off", r.sel, r.event, r.f)
		}()
	}
	r.f.Release() // always — the only GC path for the listener js.Func
}

// bind performs the deferred htmx.on exactly once, and only while the
// registration is still live (a teardown before the core came online marks it
// released, so a late gothic:core:online must not resurrect it).
func (r *hxReg) bind(hx js.Value) {
	if r.bound || r.released {
		return
	}
	r.bound = true
	hxOnOff(hx, "on", r.sel, r.event, r.f)
}

// hxRegister wires an On (subtree=true) or OnGlobal (subtree=false) listener:
// it builds a scope-capturing, event-forwarding js.Func, attaches it via htmx.on
// (targeting this scope's [data-gothic-scope] root for subtree listeners, or
// document.body for global ones), records it for Off, and schedules automatic
// detach + Release on component teardown through OnUnmount.
func hxRegister(subtree bool, e HtmxEvent, h func(Event)) {
	// Capture scope + selector SYNCHRONOUSLY: activeScope() is only meaningful in
	// this call turn, and the scope root element is in the DOM now (the page is
	// rendered before ClientSideState runs). The captured selector string stays
	// valid for the deferred bind because the component is still mounted then.
	scope := activeScope()
	sel := ""
	if subtree {
		sel = scopeSelector(scope)
	}
	f := scopedEventListener(scope, h)
	// hxRegs stores the *hxReg (which holds f), rooting the js.Func from Go until
	// reg.release() Releases it — so no separate permanent `keep` root is needed
	// (that would leave one stale post-Release entry per On for the page life).
	reg := &hxReg{scope: scope, event: string(e), sel: sel, f: f}
	hxRegs = append(hxRegs, reg)
	// Teardown ALWAYS releases f (and htmx.off's it if it was bound), whether or
	// not the deferred bind ever ran.
	OnUnmount(func() { reg.release(htmxJS()) })

	if hx := htmxJS(); !hx.IsUndefined() {
		reg.bind(hx)
		return
	}
	// window.htmx is not installed yet. The static core WASM installs it during its
	// own boot, which is asynchronous and can land AFTER this component's
	// ClientSideState runs (exactly the race GothicRegisterWithCore handles for the
	// register RPC). Early-returning here would silently drop the listener forever —
	// instead defer the htmx.on until the core announces gothic:core:online.
	deferHxBind(reg)
}

// deferHxBind arms a one-shot gothic:core:online listener that runs reg.bind once
// the static core has installed window.htmx. It mirrors GothicRegisterWithCore's
// core-comes-up-later handshake: the deferred path is only taken while htmx is
// undefined (i.e. the core has NOT yet announced online), so the online announce
// is guaranteed to be in the future and this listener will catch it. The listener
// js.Func is released either when it fires-and-binds or on component teardown,
// whichever comes first, so no js.Func leaks if the core never comes online.
func deferHxBind(reg *hxReg) {
	d := doc()
	var onlineFn js.Func
	done := false
	cleanup := func() {
		if done {
			return
		}
		done = true
		d.Call("removeEventListener", evCoreOnline, onlineFn)
		onlineFn.Release()
	}
	onlineFn = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		hx := htmxJS()
		if hx.IsUndefined() {
			return nil // a later announce will carry an installed htmx
		}
		reg.bind(hx) // no-op if the component already tore down
		cleanup()
		return nil
	})
	d.Call("addEventListener", evCoreOnline, onlineFn)
	OnUnmount(cleanup)
}

// hxOnOff calls htmx.on / htmx.off, picking the 2-arg document form (sel == "")
// or the 3/4-arg selector-targeted form.
func hxOnOff(hx js.Value, method, sel, event string, f js.Func) {
	if sel == "" {
		hx.Call(method, event, f.Value)
		return
	}
	hx.Call(method, sel, event, f.Value)
}

// scopedEventListener builds a js.Func that re-establishes the captured scope
// (runInScope) and forwards the DOM Event to fn — the event-carrying twin of
// events.go's no-arg scopedListener. Capturing the scope pins the handler to the
// component that registered it, so scoped DOM writes land in the right subtree
// even when the event fires outside a synchronous user-event turn.
func scopedEventListener(scope string, fn func(JSValue)) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		var ev JSValue
		if len(args) > 0 {
			ev = JSValue{args[0]}
		}
		runInScope(scope, func() { fn(ev) })
		return nil
	})
}

// scopeSelector returns the CSS selector for a scope's root element, or "" (bind
// document-wide) for the "_default"/host scope or when no such element exists —
// which keeps htmx.on from throwing on a null target. A selector string (not a
// captured element js.Value) is used deliberately: TinyGo's wasm_exec only
// finalizes string refs, so holding an element ref for the page lifetime would
// leak a _values slot, whereas a string is GC-clean.
func scopeSelector(scope string) string {
	if scope == "" || scope == "_default" {
		return ""
	}
	sel := `[data-gothic-scope="` + scope + `"]`
	el := doc().Call("querySelector", sel)
	if el.IsNull() || el.IsUndefined() {
		return ""
	}
	return sel
}

// jsObjectToMap converts a plain JS object to a Go map[string]any.
func jsObjectToMap(obj js.Value) map[string]any {
	out := map[string]any{}
	if obj.IsNull() || obj.IsUndefined() {
		return out
	}
	keys := js.Global().Get("Object").Call("keys", obj)
	n := keys.Length()
	for i := 0; i < n; i++ {
		k := keys.Index(i).String()
		out[k] = jsToGo(obj.Get(k))
	}
	return out
}

// jsToGo converts a JS value to a Go any: string/bool/number scalars, arrays to
// []any, and nested objects to map[string]any.
func jsToGo(v js.Value) any {
	switch v.Type() {
	case js.TypeString:
		return v.String()
	case js.TypeBoolean:
		return v.Bool()
	case js.TypeNumber:
		return v.Float()
	case js.TypeObject:
		if v.InstanceOf(js.Global().Get("Array")) {
			n := v.Length()
			arr := make([]any, n)
			for i := 0; i < n; i++ {
				arr[i] = jsToGo(v.Index(i))
			}
			return arr
		}
		return jsObjectToMap(v)
	default:
		return nil
	}
}
