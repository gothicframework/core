//go:build js && wasm

package runtime

import (
	"os"
	"strings"
	"sync"
	"syscall/js"
)

var keep []js.Func

// haltChan is the package-level keep-alive sentinel. The generated main()
// selects on GothicHaltChan(); closing this channel makes main() return so the
// keep-alive goroutine ends and the WebAssembly.Instance becomes collectible
// once the bootstrap's per-scope teardown has dropped every JS reference.
// Belt-and-suspenders per the instance-teardown design (dropping the JS
// references should already suffice on its own).
var haltChan = make(chan struct{})
var haltOnce sync.Once

// GothicHaltChan returns the keep-alive sentinel channel. The generated WASM
// main() selects on it so the module returns when the bootstrap's per-scope
// teardown invokes this instance's __gothic_halt callback.
func GothicHaltChan() <-chan struct{} { return haltChan }

// GothicRegisterSchema records a topic/component type's compact schema
// descriptor under its content-hash id at the point the type registers. This is
// the Phase 15 SCHEMA SEAM: a reserved, additive control-plane slot for a future
// generic wire interpreter (Phase 16's core stores it opaquely). NOTHING
// interprets it in v3.0 — it is written once, off the data-plane, and never read
// back by any 3.0 consumer. The descriptor is deposited on window.__gothicSchemas
// (keyed by schemaID) so the core can later pick it up without a wire change.
//
// Generated code only (like the Broadcast*/Listen* helpers); it is never
// hand-written in a ClientSideState block, so it has no user-facing stub-parity
// obligation.
func GothicRegisterSchema(key, schemaID, descriptor string) {
	store := js.Global().Get("__gothicSchemas")
	if store.IsUndefined() || store.IsNull() {
		store = js.Global().Get("Object").New()
		js.Global().Set("__gothicSchemas", store)
	}
	// Opaque record: { id, key, descriptor }. Reserved for the future core;
	// no 3.0 code path reads it.
	rec := js.Global().Get("Object").New()
	rec.Set("id", schemaID)
	rec.Set("key", key)
	rec.Set("descriptor", descriptor)
	store.Set(schemaID, rec)
}

// GothicRegisterWithCore performs the component→core registration RPC against the
// Phase-16 full-Go static core over the `document` control-plane bus. It hands
// the core an OPAQUE schema descriptor keyed by (scopeID, schemaID): the core
// records {scopeId, schemaId, schema} verbatim and acks — it never interprets the
// descriptor (the generic interpreter is DEFERRED).
//
// Ordering mirrors the topic online/ping handshake and handles both startup
// races WITHOUT a goroutine:
//
//   - core already up  → the immediate send is received and acked.
//   - core comes up later → the core announces `gothic:core:online` on boot; this
//     module's online listener re-sends the registration until it is acked.
//
// Asyncify safety is a TWO-SIDED contract. This side (outbound register) wraps
// every dispatch in queueMicrotask (like the topic bus's __gothicDispatchAsync)
// so the register fires from a clean call stack, never from inside this module's
// running scheduler turn. The RETURN side (the core's ack + online announce) is
// symmetric: the full-Go core schedules those on its own microtask (see
// pkg/wasm/core-runtime scheduleDispatch), so the ack that lands here does NOT
// re-enter this component's asyncify turn.
//
// Generated code only (like GothicRegisterSchema / the Broadcast*/Listen*
// helpers); it is never hand-written in a ClientSideState block, so it carries no
// user-facing stub-parity obligation (its host no-op lives in events_stub.go).
func GothicRegisterWithCore(scopeID, schemaID, schema string) {
	doc := js.Global().Get("document")
	acked := false

	ack := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		acked = true
		return nil
	})
	keep = append(keep, ack)
	doc.Call("addEventListener", "gothic:core:ack:"+scopeID, ack)

	// Build the register CustomEvent and its microtask dispatch callback ONCE and
	// reuse them across every (re)fire. The detail {scopeId, schemaId, schema} is
	// constant for this registration. Appending a fresh js.Func to the
	// package-global keep slice (a permanent GC root) on every fire would be a
	// retention leak: fire() re-runs on every gothic:core:online announce while not
	// yet acked, so a per-fire js.Func + CustomEvent would accumulate for the page
	// lifetime. A CustomEvent may be re-dispatched after each dispatch completes, so
	// one cached evt is safe to fire on a clean stack repeatedly via queueMicrotask.
	detail := js.Global().Get("Object").New()
	detail.Set("scopeId", scopeID)
	detail.Set("schemaId", schemaID)
	detail.Set("schema", schema)
	init := js.Global().Get("Object").New()
	init.Set("detail", detail)
	evt := js.Global().Get("CustomEvent").New("gothic:core:register", init)
	cb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		doc.Call("dispatchEvent", evt)
		return nil
	})
	keep = append(keep, cb)

	// fire dispatches gothic:core:register with {scopeId, schemaId, schema} on a
	// clean stack via queueMicrotask.
	fire := func() {
		js.Global().Call("queueMicrotask", cb)
	}

	// Re-send once the core announces readiness (covers component-before-core).
	// acked==true short-circuits, so at most one extra send occurs per page.
	online := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !acked {
			fire()
		}
		return nil
	})
	keep = append(keep, online)
	doc.Call("addEventListener", "gothic:core:online", online)

	// First attempt (covers core-already-up).
	fire()
}

// init publishes a per-instance __gothic_halt callback onto this module's slot
// in window.__gothicInstances so the bootstrap teardown can make main() return.
//
// A per-instance js.Func on the slot — rather than a wasm export (//go:wasmexport
// is rejected by the standard Go js/wasm compiler, which the Golang WasmCompiler
// uses) or a single window global (which the last-loaded module would clobber on
// a multi-instance page) — is the only form that is BOTH portable across the
// GothicTinyGo/LocalTinyGo/Golang compilers AND correct per instance.
//
// The bootstrap sets window.__gothicInstances[id] = {go, instance} BEFORE
// go.run, so the slot already exists when this init runs. Idempotent via
// sync.Once so a double-fire from the MutationObserver cannot double-close the
// channel. The js.Func is retained in keep for the instance's lifetime.
//
// This init also arms the Phase-13 scope resolver: it captures the mount scope
// into bootstrapScopeID and points findScopeFn (declared in scope.go) at the
// JS DOM walk. Both must be set before any scope is resolved; scope reads only
// happen once the generated main() runs, which is strictly after every init(),
// so ordering is safe. The halt slot is addressed by bootstrapScope()
// explicitly — at init there is exactly one scope (the mount scope) and the
// slot must never resolve dynamically to some other scope.
func init() {
	bootstrapScopeID = captureBootstrapScope()
	findScopeFn = findScope

	halt := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		haltOnce.Do(func() {
			close(haltChan)
		})
		return nil
	})
	keep = append(keep, halt)
	insts := js.Global().Get("__gothicInstances")
	if insts.IsUndefined() || insts.IsNull() {
		return
	}
	slot := insts.Get(bootstrapScope())
	if slot.IsUndefined() || slot.IsNull() {
		return
	}
	slot.Set("__halt", halt)
}

// captureBootstrapScope reads the mount scope once at init. Two channels:
//
//  1. Go.argv — the deterministic channel. The bootstrap script in
//     pkg/helpers/routes/wasm_bootstrap.go sets
//     go.argv = ['gothic', 'GOTHIC_SCOPE=<id>'] BEFORE go.run, and
//     TinyGo's wasm_exec populates os.Args from argv synchronously, so this
//     read is race-free even when several bootstraps run concurrently.
//
//  2. window.__gothicCurrentModule — legacy global, kept as a fallback for
//     anyone hand-rolling a bootstrap or running the runtime outside the
//     standard envelope. Susceptible to a multi-IIFE race when multiple
//     bootstraps interleave, so the argv channel takes precedence.
//
// If neither channel yields a value it falls back to "_default" and DOM helpers
// behave document-wide. Tests and non-WASM callers rely on this fallback.
func captureBootstrapScope() string {
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "GOTHIC_SCOPE=") {
			return arg[len("GOTHIC_SCOPE="):]
		}
	}
	v := js.Global().Get("__gothicCurrentModule")
	if v.IsUndefined() || v.IsNull() {
		return "_default"
	}
	return v.String()
}

// GothicRegisterScope wires a Multiplexed page's ClientSideState body into the
// per-scope registration system. The generated main() of a Multiplexed route
// calls it with the ClientSideState body wrapped in a closure, instead of
// running that body inline. It:
//
//  1. Registers the instance's own mount scope immediately by running body under
//     runInScope(bootstrapScope). This makes the first placement behave exactly
//     like a non-multiplexed instance: body runs under the bootstrap scope, so
//     its observables/callbacks/listeners land in __gothic_registry[bootstrapScope]
//     — byte-identical to the non-multiplexed registration path.
//
//  2. Publishes a per-instance __gothic_register_scope(id) callback onto this
//     module's window.__gothicInstances[<bootstrapScope>] slot (the same slot the
//     Phase-12 __halt lives on). The bootstrap JS invokes it for every SUBSEQUENT
//     placement of the same component type, running the SAME body under
//     runInScope(id). Each invocation re-runs body, creating FRESH observables
//     and callbacks via new closures — this is why Phase 13's scope refactor is
//     the gate for multiplexing: one instance hosts N independent scopes, each
//     with its own state, all resolved per active scope.
//
// Publishing onto the per-instance slot (not a window global) mirrors __halt:
// portable across the GothicTinyGo/LocalTinyGo/Golang compilers AND correct on a
// multi-instance page. main() runs synchronously up to its keep-alive select
// during go.run, so the callback is set before the bootstrap flushes its pending
// queue.
func GothicRegisterScope(body func()) {
	// Register the instance's own mount scope first (the first placement).
	runInScope(bootstrapScope(), body)

	// Per-instance registration entry for subsequent placements.
	reg := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 {
			return nil
		}
		id := args[0].String()
		if id == "" {
			return nil
		}
		runInScope(id, body)
		return nil
	})
	keep = append(keep, reg)

	insts := js.Global().Get("__gothicInstances")
	if insts.IsUndefined() || insts.IsNull() {
		return
	}
	slot := insts.Get(bootstrapScope())
	if slot.IsUndefined() || slot.IsNull() {
		return
	}
	slot.Set("__gothic_register_scope", reg)
}

func ensureRegistry() js.Value {
	reg := js.Global().Get("__gothic_registry")
	if reg.IsUndefined() {
		reg = js.Global().Get("Object").New()
		js.Global().Set("__gothic_registry", reg)
	}
	return reg
}

func moduleRegistry() js.Value {
	reg := ensureRegistry()
	modID := activeScope()
	modReg := reg.Get(modID)
	if modReg.IsUndefined() {
		modReg = js.Global().Get("Object").New()
		reg.Set(modID, modReg)
	}
	return modReg
}

// findScope walks up from window.event.target to find the nearest [data-gothic-scope].
//
// The DOM walk lives in JS (window.__gothicFindScope, installed by the
// bootstrap script) and returns a plain string. We bounce through JS to avoid
// boxing the MouseEvent / target / closest() result through TinyGo's
// js.Value slot table — those refs are never finalized (TinyGo's wasm_exec
// only calls finalizeRef for strings), so any per-click js.Value would
// leak a permanent _values[] entry on every event. Returning a string and
// calling .String() goes through the jsString path, which DOES finalize,
// so this call is allocation-stable across an arbitrary number of clicks.
func findScope() string {
	return js.Global().Call("__gothicFindScope").String()
}

// scopedListener captures the active scope at REGISTRATION time and returns a
// js.Func that re-establishes it (via runInScope) before running body.
//
// Topic broadcasts fire from an async document-event turn (queueMicrotask →
// document.dispatchEvent). During that turn window.event.target is `document`,
// which has no [data-gothic-scope] ancestor, so findScope() yields "" and
// activeScope() would fall back to bootstrapScope. Capturing here pins the
// listener to the scope that created it. For a single-scope instance the
// captured scope IS bootstrapScope, so the body sees exactly the scope the old
// moduleID() would have returned at fire time — byte-identical. For a
// multi-scope instance (Phase 14) each scope's ClientSideState registers under
// runInScope(scopeID), so its listeners capture and re-establish that scope.
func scopedListener(body func()) js.Func {
	scope := activeScope()
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		runInScope(scope, body)
		return nil
	})
}

// registerLocal stores impl in this scope's slot of the global js.Func registry
// (__gothic_registry[activeScope()][name]) and retains it in keep so TinyGo's GC
// won't reclaim it while JS still references it. This is the SOLE dispatch target:
// the pure-JS proxy window[name] (installed by __gothicInstallProxy in
// pkg/helpers/gothiccore/core.go) resolves the clicked element's scope off the
// LIVE __gothic_registry and invokes THIS scope's js.Func, forwarding the DOM
// call's arguments (so string/bool-arg callbacks get their value). It is
// registered once at mount — not per click. The bootstrap teardown (core.go
// __gothicTeardown) drops the JS reference by deleting __gothic_registry[scope],
// which is exactly what makes a torn-down scope's entry simply absent so a sibling
// click resolves to the sibling's live js.Func — no stale registry entry survives
// a remount. The registry entry also lets the duplicate-isolation suite assert
// __gothic_registry[scope][name] exists.
func registerLocal(name string, impl js.Func) {
	keep = append(keep, impl)
	moduleRegistry().Set(name, impl)
}

// addScopedDocListener registers fn as a document event listener for eventType
// AND records {type, fn} into this scope's __gothic_registry[<scope>].__listeners
// array so the bootstrap's per-scope teardown can removeEventListener it when the
// scope's DOM subtree is unmounted. Every ListenTopic* helper routes through this
// single choke point; without it, the document-level closures registered by those
// helpers would keep the instance alive after an HTMX swap (the teardown leak).
//
// The caller is responsible for retaining fn in keep (as the ListenTopic* helpers
// already do) so TinyGo's GC does not reclaim the js.Func while it is still
// referenced by the DOM.
func addScopedDocListener(eventType string, fn js.Func) {
	js.Global().Get("document").Call("addEventListener", eventType, fn)
	reg := moduleRegistry()
	listeners := reg.Get("__listeners")
	if listeners.IsUndefined() {
		listeners = js.Global().Get("Array").New()
		reg.Set("__listeners", listeners)
	}
	entry := js.Global().Get("Object").New()
	entry.Set("type", eventType)
	entry.Set("fn", fn)
	listeners.Call("push", entry)
}

// OnUnmount registers a cleanup callback invoked by the bootstrap's per-scope
// teardown when this component's [data-gothic-scope] element is removed from the
// DOM. Use it to release things created outside the component's own subtree
// (persistent document listeners attached directly, timers, topic mounts). The
// callback is stored at __gothic_registry[<scope>].__onUnmount and retained in
// keep so TinyGo's GC won't reclaim it before teardown fires.
func OnUnmount(fn func()) {
	f := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		fn()
		return nil
	})
	keep = append(keep, f)
	moduleRegistry().Set("__onUnmount", f)
}

// CreateWasmFunc registers a no-arg user callback under name. The Go closure
// becomes a js.Func stored in __gothic_registry[scope][name] (registerLocal),
// keyed by the active scope. That registry js.Func is the SOLE dispatch target:
// the GLOBAL proxy window[name] — the function HTML onclick/onchange invokes — is
// pure, instance-agnostic JS installed once per name by __gothicInstallProxy (see
// pkg/helpers/gothiccore/core.go). On each click that proxy resolves the clicked
// element's scope off the LIVE __gothic_registry and calls this scope's js.Func,
// so tearing down a sibling instance (which deletes its registry entry) can never
// leave the proxy pointing at halted Go code.
func CreateWasmFunc(name string, fn func()) {
	registerLocal(name, js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		fn()
		return nil
	}))
	js.Global().Call("__gothicInstallProxy", name)
}

func CreateWasmStringFunc(name string, fn func(string)) {
	registerLocal(name, js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		val := ""
		if len(args) > 0 {
			val = args[0].String()
		}
		fn(val)
		return nil
	}))
	js.Global().Call("__gothicInstallProxy", name)
}

func CreateWasmBoolFunc(name string, fn func(bool)) {
	registerLocal(name, js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		val := false
		if len(args) > 0 {
			val = args[0].Bool()
		}
		fn(val)
		return nil
	}))
	js.Global().Call("__gothicInstallProxy", name)
}

// CreateWasmFuncWithReturn registers a named global JS function that can return a value back to JS.
// Wraps syscall/js.FuncOf. The returned JSValue holds the js.Func so it can be passed
// directly to JS object properties (chart configs, map renderers, etc.) that require a
// synchronous return value. The js.Func is retained in keep and never released — it must
// stay alive for the lifetime of the page; releasing it would panic on the next invocation.
func CreateWasmFuncWithReturn(name string, fn func(this JSValue, args []JSValue) any) JSValue {
	f := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		jsArgs := make([]JSValue, len(args))
		for i, a := range args {
			jsArgs[i] = JSValue{a}
		}
		return toJSVal(fn(JSValue{this}, jsArgs))
	})
	keep = append(keep, f)
	js.Global().Set(name, f)
	return JSValue{f.Value}
}
