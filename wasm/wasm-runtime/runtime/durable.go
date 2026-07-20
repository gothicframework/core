//go:build js && wasm

package runtime

import "syscall/js"

// durable.go is the TinyGo runtime side of the DURABLE STATE CACHE. It
// lets a component OPT IN to page-session persistence: its reactive state SURVIVES
// its own teardown → re-mount (an HTMX swap-away/back, or a Multiplexed
// list re-render), rehydrating from the always-loaded full-Go static core instead
// of refetching from the server.
//
// # Opt-in, default OFF
//
// Durability is keyed by a STABLE durable key the caller declares on the
// component wrapper as data-gothic-durable-key (user-supplied in their templ, or
// auto-derived — a page singleton can use its wasmName, a Multiplexed row supplies
// its stable row id). Scope ids are RANDOM per mount and so CANNOT key state
// across re-mounts; only the caller knows a placement's stable identity. When a
// placement has NO durable key, DurableObserve is a NO-OP and the observable
// behaves EXACTLY as today (no core traffic, no cache) — durability never changes
// the default path.
//
// # Two-tier protocol (mirrors topics; see core-runtime/protocol/durable.go)
//
//   - CONTROL-PLANE: registerDurableWithCore hands the core {key, fields} so it
//     subscribes to each per-field WRITE event and can replay stored state; the
//     per-key durable-online ack (listenDurableCoreOnline) tells this component
//     replay has drained and it may start persisting.
//   - DATA-PLANE (binary): requestDurableSetField writes a per-field frame to the
//     core; listenDurableField applies a replayed frame. Both ride the existing
//     __gothic_topic buffer plumbing (dispatchDirect / the scoped doc listeners),
//     so no new JS transport is needed and the core stays opaque.
//
// # Rehydrate-before-the-component-goes-live ordering
//
// On mount DurableObserve installs the field's replay listener + a persistence
// effect (gated OFF until online), then registers with the core. The core replays
// each stored frame (microtask) BEFORE announcing durable-online, so the replayed
// value is applied to the observable — and any Observe the user wired for the DOM
// re-runs — before the online gate opens. The gate is why the component never
// writes its DEFAULT value over the stored one on re-mount: persistence is
// suppressed until after replay drains.
//
// This is "before the hydrated component goes live", NOT literally before the
// first paint: the WASM instance itself boots asynchronously (instantiateStreaming),
// so the SSR-rendered default can be briefly visible before replay lands — the
// same short hydration flash every Gothic WASM component has. There is no clobber
// (the online gate guarantees the restored value wins); at worst the user sees the
// default for a frame before it snaps to the durable value.
//
// # Lifecycle boundary
//
// Durable state persists for the PAGE SESSION only. The core is a fresh instance
// per page load, so durable entries survive unmount/re-mount within a page but do
// NOT survive a full reload and are NOT server-persisted. On last-scope-out
// teardown of a Multiplexed type the core's durable KV entries persist (they live
// in the core, not the torn-down instance), so a later re-mount restores them.

// Durable control-plane event names. MUST stay byte-identical to
// pkg/wasm/core-runtime/protocol/durable.go (that package can't be imported here —
// the runtime is a separate TinyGo module extracted at build time — so the two are
// kept in lockstep by hand, same as the topic names in topic.go).
const (
	evDurableRegister     = "gothic:core:durable-register"
	evDurableReqPrefix    = "gothic:durable-req:"
	evDurableBcastPrefix  = "gothic:durable:"
	evDurableOnlinePrefix = "gothic:core:durable-online:"
)

// DurableKey returns the STABLE durable key declared on the active scope's wrapper
// (data-gothic-durable-key), or "" when the placement is not durable. The DOM read
// lives in JS (window.__gothicDurableKey in gothic-core.js) and returns a plain
// string so no per-mount MouseEvent/element js.Value is boxed into TinyGo's slot
// table (the same _values[]-leak avoidance as findScope). Empty string is the
// default and means "durability off" — callers treat it as opt-out.
func DurableKey() string {
	scope := activeScope()
	if scope == "" || scope == "_default" {
		// No addressable wrapper to read the attribute from; treat as not durable.
		return ""
	}
	v := js.Global().Call("__gothicDurableKey", js.ValueOf(scope))
	if v.IsUndefined() || v.IsNull() {
		return ""
	}
	return v.String()
}

// DurableObserve binds obs to the core's durable KV as `field` under this
// placement's durable key. It rehydrates obs from the core BEFORE the component
// goes live, then persists every subsequent change for the page session. When the
// placement has NO durable key it is a NO-OP and obs behaves exactly as today
// (OPT-IN; default off). encode/decode are the field's string codec (same shape
// as CustomKey — reuse strconv for primitives, or a binary Encoder/Decoder for a
// struct field); they exist in both build worlds so the same ClientSideState
// block compiles server-side (no-op) and under TinyGo.
//
//	count := CreateObservable(0)
//	DurableObserve("count", count, strconv.Itoa,
//	    func(s string) int { n, _ := strconv.Atoi(s); return n })
//	Observe(func() { SetText("out", strconv.Itoa(count.Get())) }, count)
func DurableObserve[T any](field string, obs *Observable[T], encode func(T) string, decode func(string) T) {
	dk := DurableKey()
	if dk == "" {
		return // opt-in: not a durable placement → obs behaves exactly as today.
	}
	online := false

	// Hydration: apply each replayed per-field frame. obs.Set notifies the user's
	// DOM effects so the UI reflects the restored value; because `online` is still
	// false during replay, the persistence effect below does NOT write it back.
	listenDurableField(dk, field, func(detail string) {
		obs.Set(decode(detail))
	})

	// Persistence: on every change, once online, write the field to the core.
	// Observe runs once synchronously now (online==false → no write), establishing
	// the dep; each later change re-runs it. The initial DEFAULT value is therefore
	// never written before the core has (re)hydrated us.
	Observe(func() {
		v := obs.Get()
		if !online {
			return
		}
		requestDurableSetField(dk, field, encode(v))
	}, obs)

	// Online ack: the core announces this after replay drains, so opening the gate
	// here means every stored frame has already been applied above.
	listenDurableCoreOnline(dk, func() { online = true })

	// Register this field with the core (incremental — one field per call).
	registerDurableWithCore(dk, []string{field})
}

// requestDurableSetField writes a per-field durable frame to the core (data-plane
// binary), reusing the topic dispatchDirect transport under the durable-req
// prefix. Event: gothic:durable-req:<key>:<field>.
func requestDurableSetField(key, field, encoded string) {
	dispatchDirect(key+":"+field, evDurableReqPrefix, []byte(encoded))
}

// listenDurableField subscribes to the core's per-field durable REPLAY
// (gothic:durable:<key>:<field>). Like the topic listeners it re-establishes the
// registering scope (scopedListener) because the replay fires from an async
// document-event turn where findScope() cannot see the [data-gothic-scope], and
// routes through addScopedDocListener so the per-scope teardown removes it on
// unmount.
func listenDurableField(key, field string, fn func(string)) {
	fullKey := evDurableBcastPrefix + key + ":" + field
	// Per-listener scratch buffer reused across replays: a stable-size durable
	// frame allocates it ONCE instead of a fresh make([]byte,n) per receive,
	// keeping the durable replay path off the no-shrink-heap ratchet the topic
	// consumer path had (see topicViewInto). Durable replay is per-remount rather
	// than per-toggle, so it is far cooler than the topic data-plane, but the
	// reuse costs nothing and keeps the two paths consistent.
	var scratch []byte
	listener := scopedListener(func() {
		data := js.Global().Get("__gothic_topic").Call("get", js.ValueOf(fullKey))
		// A null/undefined view means NO frame was ever stored (absent) — skip.
		// A present view of length 0 is a legitimate CLEARED value: fn("") applies
		// the empty so the observable restores the clear, not its mount default.
		// (This differs from the topic listeners, which skip n==0 as a no-op.)
		if data.IsNull() || data.IsUndefined() {
			return
		}
		n := data.Get("byteLength").Int()
		if n == 0 {
			fn("")
			return
		}
		if cap(scratch) < n {
			scratch = make([]byte, n)
		}
		scratch = scratch[:n]
		js.CopyBytesToGo(scratch, data)
		fn(string(scratch))
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// registerDurableWithCore performs the durable → core control-plane registration.
// The CustomEvent detail is a plain JS object {key, fields:[...]} — control-plane
// values, never binary. It mirrors RegisterTopicWithCore exactly (including the
// consumer-before-core re-send on gothic:core:online and the queueMicrotask
// asyncify hop) so the two handshakes cannot drift, differing only in the event
// name and that durable state is private (store-only, replay-on-register).
func registerDurableWithCore(key string, fields []string) {
	doc := js.Global().Get("document")
	acked := false

	ack := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		acked = true
		return nil
	})
	keep = append(keep, ack)
	doc.Call("addEventListener", evDurableOnlinePrefix+key, ack)

	// Build the register CustomEvent and its microtask dispatch callback ONCE and
	// reuse them across every (re)fire. Mirrors RegisterTopicWithCore: the detail
	// {key, fields} is constant, and appending a fresh js.Func to the package-global
	// keep slice (a permanent GC root) on every fire would accumulate for the page
	// lifetime because fire() re-runs on every gothic:core:online announce while not
	// yet acked. A CustomEvent may be re-dispatched after each dispatch completes.
	detail := js.Global().Get("Object").New()
	detail.Set("key", key)
	arr := js.Global().Get("Array").New()
	for _, f := range fields {
		arr.Call("push", f)
	}
	detail.Set("fields", arr)
	init := js.Global().Get("Object").New()
	init.Set("detail", detail)
	evt := js.Global().Get("CustomEvent").New(evDurableRegister, init)
	cb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		doc.Call("dispatchEvent", evt)
		return nil
	})
	keep = append(keep, cb)

	fire := func() {
		js.Global().Call("queueMicrotask", cb)
	}

	// Re-send once the core announces readiness (covers component-before-core).
	online := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if !acked {
			fire()
		}
		return nil
	})
	keep = append(keep, online)
	doc.Call("addEventListener", evCoreOnline, online)

	// First attempt (covers core-already-up).
	fire()
}

// listenDurableCoreOnline registers a handler for the core's per-key durable
// online ack (gothic:core:durable-online:<key>). The core dispatches it AFTER
// replaying the durable key's per-field state to this component, so by the time
// the handler runs the listenDurableField handlers have already applied the
// replayed values. Scoped + teardown-tracked like the other durable listeners.
func listenDurableCoreOnline(key string, fn func()) {
	listener := scopedListener(fn)
	keep = append(keep, listener)
	addScopedDocListener(evDurableOnlinePrefix+key, listener)
}
