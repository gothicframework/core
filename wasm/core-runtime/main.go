//go:build js && wasm

// Command core-runtime is the Gothic Framework full-Go STATIC CORE (Phase 16):
// a prebuilt, type-agnostic RPC / registration hub.
//
// # Why full-Go (standard Go GOOS=js GOARCH=wasm), not TinyGo
//
// The core is compiled with the standard Go toolchain so it has the full
// standard library (encoding/json, regexp, crypto, time, locale-aware
// formatting) that TinyGo lacks. Later phases grow the core into a thick
// services hub (durable state cache, generic wire interpreter); those need the
// full stdlib. TinyGo stays the compiler for per-page/per-component modules
// because they must be small and are rebuilt on every hot reload.
//
// # Why static (prebuilt once, committed as an artifact, never rebuilt on save)
//
// A full-Go js/wasm build is slow and large — rebuilding it on every hot-reload
// keystroke would wreck DX. The core is app-INDEPENDENT (framework code, never
// user types), so it is compiled ONCE at framework-release time, content-hashed,
// embedded in the CLI, and merely COPIED into public/ on init and on build. It
// is deliberately NOT part of the GenerateAll per-page rebuild set.
//
// # Coexistence with TinyGo components
//
// The core loads through its own wasm_exec shim slot
// (window.__gothicGoClasses["gothic-core-exec.js"]) so its standard-Go `Go`
// constructor never collides with the TinyGo `Go` used by per-instance
// components on the same page.
//
// # Asyncify safety on the RETURN path
//
// The core must NEVER dispatchEvent synchronously. Its ack and online-announce
// fire BACK into a registering TinyGo component; a synchronous dispatchEvent
// would run that component's listener nested inside the component's own,
// still-unwinding asyncify turn → the documented `RuntimeError: unreachable`
// re-entrancy crash (the same reason topic.go routes broadcasts through
// __gothicDispatchAsync/queueMicrotask, and why the component's own register
// send in events.go is queueMicrotask'd). Every core→component dispatch here is
// therefore scheduled on a fresh microtask (see scheduleDispatch). Only the
// SCHEDULING is deferred — the listeners are still installed synchronously
// before the online announce, so the "listeners installed before online"
// ordering holds.
//
// # What it does in Phase 16 (foundation only)
//
// The core boots, installs a control-plane registration listener on the
// `document` bus, records each registration OPAQUELY (it stores the schema
// descriptor verbatim WITHOUT interpreting it — the generic interpreter is
// DEFERRED to a later phase), and acks. NO topic logic lives here yet (that is
// Phase 17). This is the type-agnostic hub and its handshake, nothing more.
//
// The record→ack DECISION (whether to record, under which key, which scope to
// ack) is factored into the pure, host-tested
// pkg/wasm/core-runtime/protocol package; this file is the thin js.Value adapter
// around it. The full register→ack round-trip over a LIVE WASM instance cannot
// be driven from a host Go test — it is exercised by the Phase-21
// `wasm-core.spec.ts` Playwright e2e on TestGothic.
package main

import (
	"syscall/js"

	"github.com/gothicframework/core/wasm/core-runtime/protocol"
)

// Control-plane event names. Every one is dispatched on `document` and carries
// its payload in the CustomEvent `detail` as plain JS values (control-plane =
// ergonomics over bytes; the binary data-plane is topic-only and does not exist
// here yet). The `gothic:core:*` namespace never overlaps the topic bus
// (`gothic:topic:*` / `gothic:topic-*:*`), so the two coexist cleanly. The
// per-scope ack event name (protocol.AckPrefix + scopeId) is owned by the
// protocol package so the decision logic and this adapter cannot drift.
const (
	// evRegister: component → core. detail = {scopeId, schemaId, schema}. The
	// core records {scopeId, schemaId, schema} under schemaId, verbatim.
	evRegister = "gothic:core:register"
	// evPing: component → core. "Are you online?" The core answers by
	// (re-)announcing evOnline. Mirrors the topic-ping/topic-online shape.
	evPing = "gothic:core:ping"
	// evOnline: core → all. Announced once when the core becomes ready (boot) and
	// again on every ping, so a component that mounted BEFORE the core came up
	// learns the core is ready and re-sends its registration.
	evOnline = "gothic:core:online"
	// evTopicRegister: component → core. detail = {key, fields:[...]}. Phase 17
	// topic handshake (CONTROL-PLANE JSON): the core learns a topic's wire key and
	// its ordered field-name list, subscribes to each per-field set-request event,
	// replays the topic's current per-field state to the (re)joining consumer, and
	// announces the per-key online ack. Listing field NAMES is routing metadata,
	// NOT payload interpretation — the core never decodes a topic frame (the
	// generic wire interpreter is DEFERRED). The name lives in the gothic:core:*
	// control namespace and never overlaps the gothic:topic:* binary data-plane.
	evTopicRegister = "gothic:core:topic-register"
	// evDurableRegister: component → core. detail = {key, fields:[...]}. Phase 18
	// DURABLE STATE CACHE handshake (CONTROL-PLANE JSON): a component that wants its
	// state to SURVIVE its own teardown→re-mount registers under a caller-supplied
	// STABLE durable key (scope ids are random per mount, so they cannot key state
	// across re-mounts). The core subscribes to each per-field WRITE event, replays
	// the durable key's current per-field state to the (re)mounting consumer, and
	// announces the per-key durable-online ack. Fields register INCREMENTALLY (one
	// DurableObserve call each), so the core subscribes the delta. Like the topic
	// register this lists field NAMES for routing only — the core never decodes a
	// durable frame (the generic wire interpreter is DEFERRED).
	evDurableRegister = "gothic:core:durable-register"
)

// Window globals the core owns.
const (
	// glReady is a boolean flag set true once the core has installed its
	// listeners and is ready to serve registrations. Also guards against a second
	// core instance booting on the same page.
	glReady = "__gothicCoreReady"
	// glRegistered is the reserved OPAQUE schema store: an object keyed by
	// schemaId whose values are {scopeId, schemaId, schema}. The core writes it
	// and never reads `schema` back — it is deposited for a future generic
	// interpreter (deferred). Distinct from the Phase-15 window.__gothicSchemas
	// seam, which components populate at registration time; this one is the
	// core's own authoritative record of what it has acked.
	glRegistered = "__gothicCoreRegistered"
)

func main() {
	global := js.Global()
	doc := global.Get("document")

	// If a core already owns this page (defensive: the layout should load exactly
	// one), do not install a second set of listeners. Re-announce online so any
	// component still waiting gets a readiness signal, then idle for the page
	// lifetime without competing for the control plane.
	if global.Get(glReady).Truthy() {
		announceOnline(global, doc)
		select {}
	}

	// Reserved opaque record of acked registrations: schemaId -> {scopeId,
	// schemaId, schema}. Created empty; never interpreted.
	registered := global.Get("Object").New()
	global.Set(glRegistered, registered)

	// Registration RPC: record opaquely, then ack the registering scope. The
	// decision (record? which key? which ack event?) is the pure protocol helper;
	// the schema value never enters that decision, which is exactly what keeps it
	// OPAQUE — schema is carried straight from the inbound detail to the record.
	regFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		detail := args[0].Get("detail")
		if !detail.Truthy() {
			return nil
		}
		schema := detail.Get("schema") // OPAQUE: stored verbatim, never parsed.

		dec := protocol.DecideRegister(
			stringOf(detail.Get("scopeId")),
			stringOf(detail.Get("schemaId")),
		)
		if !dec.Record {
			return nil
		}

		rec := global.Get("Object").New()
		rec.Set("scopeId", dec.AckScopeID)
		rec.Set("schemaId", dec.AckSchemaID)
		rec.Set("schema", schema)
		registered.Set(dec.RecordKey, rec)

		// Ack the specific scope that registered — deferred to a clean stack so it
		// never re-enters the registering component's asyncify turn.
		ackDetail := global.Get("Object").New()
		ackDetail.Set("scopeId", dec.AckScopeID)
		ackDetail.Set("schemaId", dec.AckSchemaID)
		scheduleDispatch(global, doc, dec.AckEvent, ackDetail)
		return nil
	})
	doc.Call("addEventListener", evRegister, regFn)

	// Ping listener: re-announce readiness so a component that came up before the
	// core (and missed the boot announce) learns the core is now serving.
	pingFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		announceOnline(global, doc)
		return nil
	})
	doc.Call("addEventListener", evPing, pingFn)

	// ── Topic hub (Phase 17) ────────────────────────────────────────────────
	//
	// The generic, OPAQUE, per-key/per-field store-and-forward that replaces the
	// N per-topic MANAGER WASM instances. Two Go-side maps hold the hub state for
	// the page lifetime:
	//
	//   coreTopics: key -> registered field-name list (routing metadata only).
	//   coreState:  key -> field -> latest per-field frame bytes, stored VERBATIM.
	//               These bytes are never decoded — the whole point of the two-tier
	//               protocol is that the data-plane stays binary and the core routes
	//               it by key+field string alone (generic interpreter DEFERRED).
	//
	// Large payloads (the 5 MB image / 150k-item stress case) now land in THIS
	// full-Go heap instead of a TinyGo manager's — full-Go's GC reclaims them far
	// better than TinyGo's conservative GC on the WASM32 no-shrink heap — but the
	// per-field wire STAYS BINARY here; nothing is ever JSON-ified onto the
	// data-plane (that would regress the heap ceiling the binary codec exists for).
	coreTopics := map[string][]string{}
	coreState := map[string]map[string][]byte{}

	topicRegFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		detail := args[0].Get("detail")
		if !detail.Truthy() {
			return nil
		}
		key := stringOf(detail.Get("key"))
		incoming := stringSliceOf(detail.Get("fields"))

		_, alreadyKnown := coreTopics[key]
		dec := protocol.DecideTopicRegister(key, alreadyKnown, incoming)
		if dec.OnlineEvent == "" {
			return nil // empty key — ignored.
		}
		if dec.Subscribe {
			coreTopics[key] = dec.Fields
			if coreState[key] == nil {
				coreState[key] = map[string][]byte{}
			}
			for _, field := range dec.Fields {
				subscribeTopicField(global, doc, coreState, key, field)
			}
		}
		// Online hydration = per-field REPLAY. Re-forward every stored per-field
		// frame for this key (data-plane, verbatim) so the (re)joining consumer
		// rebuilds its state field-by-field, THEN announce the per-key online ack
		// (control-plane). Both are microtask-scheduled; the replays enqueue first,
		// so the consumer applies all fields before it flips _online. The core does
		// NOT build a whole-struct frame here — it cannot without slicing, and the
		// generic interpreter is deferred.
		for _, field := range coreTopics[key] {
			if frame := coreState[key][field]; len(frame) > 0 {
				forwardTopicField(global, key, field, frame)
			}
		}
		scheduleDispatch(global, doc, dec.OnlineEvent, js.Undefined())
		return nil
	})
	retainedTopicFns = append(retainedTopicFns, topicRegFn)
	doc.Call("addEventListener", evTopicRegister, topicRegFn)

	// ── Durable state cache (Phase 18) ──────────────────────────────────────
	//
	// The opaque, page-session KV that lets a component's state SURVIVE its own
	// teardown→re-mount. It is INDEPENDENT of any per-instance heap: the bytes
	// live in THIS always-loaded full-Go core, so when Phase-12 teardown drops a
	// component's per-scope registry/state the durable frames stay put here and a
	// later re-mount rehydrates from them — no server round-trip. Two Go-side maps
	// hold it for the page lifetime:
	//
	//   coreDurable:    key -> field -> latest per-field frame bytes, stored
	//                   VERBATIM. Never decoded — routing by key+field string only.
	//   durableSubbed:  key -> set of fields whose per-field WRITE listener is
	//                   already installed (fields register incrementally, so this
	//                   dedupes the delta — DecideDurableRegister owns the decision).
	//
	// Unlike the topic hub a durable WRITE is store-ONLY: there is no live
	// cross-consumer fan-out (a durable component is private to one placement), so
	// frames leave the core exclusively on register-time REPLAY. This is a fresh
	// core instance per page load, so durable state is PAGE-SESSION scoped — it
	// survives unmount/re-mount within a page but NOT a full reload (documented on
	// the runtime side too).
	coreDurable := map[string]map[string][]byte{}
	durableSubbed := map[string]map[string]bool{}

	durableRegFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		detail := args[0].Get("detail")
		if !detail.Truthy() {
			return nil
		}
		key := stringOf(detail.Get("key"))
		incoming := stringSliceOf(detail.Get("fields"))

		dec := protocol.DecideDurableRegister(key, durableSubbed[key], incoming)
		if dec.OnlineEvent == "" {
			return nil // empty key — ignored.
		}
		if coreDurable[key] == nil {
			coreDurable[key] = map[string][]byte{}
		}
		if durableSubbed[key] == nil {
			durableSubbed[key] = map[string]bool{}
		}
		for _, field := range dec.NewFields {
			durableSubbed[key][field] = true
			subscribeDurableField(global, doc, coreDurable, key, field)
		}
		// Online hydration = per-field REPLAY. Re-forward every stored per-field
		// frame for this key (data-plane, verbatim) so the (re)mounting consumer
		// rebuilds its state field-by-field, THEN announce the per-key durable-online
		// ack (control-plane). Both are microtask-scheduled; the replays enqueue
		// first, so the consumer applies all fields before it flips its local online
		// gate and begins persisting future changes (see the runtime's DurableObserve
		// online gate — it must NOT write its default value over the replayed one).
		//
		// Every field PRESENT in the map is replayed, INCLUDING one whose stored
		// frame is empty: empty is a legitimate durable value (a cleared field), and
		// the default the component mounts with may differ from empty, so the empty
		// frame must override it. Presence — not length — gates the replay.
		for field, frame := range coreDurable[key] {
			forwardDurableField(global, key, field, frame)
		}
		scheduleDispatch(global, doc, dec.OnlineEvent, js.Undefined())
		return nil
	})
	retainedDurableFns = append(retainedDurableFns, durableRegFn)
	doc.Call("addEventListener", evDurableRegister, durableRegFn)

	// Mark ready and announce. Ordering mirrors the topic online/ping handshake:
	// the register and ping listeners are installed BEFORE the online announce, so
	// no incoming registration can race past a not-yet-listening core. Only the
	// SCHEDULING of the announce is deferred (queueMicrotask); the listeners are
	// already installed synchronously above.
	global.Set(glReady, js.ValueOf(true))
	announceOnline(global, doc)

	// regFn and pingFn are retained for the page lifetime because main never
	// returns (select {} blocks forever), keeping the scheduler and every
	// js.Func alive.
	select {}
}

// retainedTopicFns keeps the topic hub's js.Func callbacks alive for the page
// lifetime. main() never returns (select {} blocks forever), but the register
// handler and each per-field req listener are created AFTER boot inside callback
// turns, so they must be anchored somewhere the GC can see — a package-level
// slice, appended to on the single JS event-loop goroutine (no data race).
var retainedTopicFns []js.Func

// retainedDurableFns keeps the durable hub's js.Func callbacks (the register
// handler and each per-field WRITE listener) alive for the page lifetime, for
// the same reason as retainedTopicFns: they are created AFTER boot inside
// callback turns, so they must be anchored where the GC can see them. Appended
// only on the single JS event-loop goroutine (no data race).
var retainedDurableFns []js.Func

// stringOf coerces a js.Value to string, treating undefined/null as "".
func stringOf(v js.Value) string {
	if !v.Truthy() {
		return ""
	}
	return v.String()
}

// stringSliceOf reads a JS array of strings into a Go slice, tolerating a
// missing/undefined value (returns nil). Used to lift the control-plane
// {fields:[...]} list off the register detail — these are field NAMES for
// routing, never payload bytes, so reading them keeps the core opaque.
func stringSliceOf(v js.Value) []string {
	if !v.Truthy() {
		return nil
	}
	n := v.Length()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, v.Index(i).String())
	}
	return out
}

// subscribeTopicField installs a DATA-PLANE listener for one topic field's
// set-requests (gothic:topic-req:<key>:<field>). On each request it reads the raw
// frame out of the shared window.__gothic_topic buffer (never decoding it),
// byte-compares against the last stored frame (protocol.DecideForward — the
// no-op-suppressing diff), and on a change stores + rebroadcasts the frame
// VERBATIM to every consumer. The core touches bytes only to compare and copy —
// it never interprets them (generic interpreter DEFERRED). The js.Func is
// retained for the page lifetime (never Release'd).
func subscribeTopicField(global, doc js.Value, coreState map[string]map[string][]byte, key, field string) {
	reqEvent := protocol.TopicReqEvent(key, field)
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		topic := global.Get("__gothic_topic")
		if !topic.Truthy() {
			return nil
		}
		view := topic.Call("get", js.ValueOf(reqEvent))
		if !view.Truthy() {
			return nil
		}
		n := view.Get("byteLength").Int()
		if n == 0 {
			return nil
		}
		buf := make([]byte, n)
		js.CopyBytesToGo(buf, view)
		if !protocol.DecideForward(coreState[key][field], buf) {
			return nil
		}
		coreState[key][field] = buf
		forwardTopicField(global, key, field, buf)
		return nil
	})
	retainedTopicFns = append(retainedTopicFns, fn)
	doc.Call("addEventListener", reqEvent, fn)
}

// forwardTopicField rebroadcasts a per-field frame VERBATIM on the data-plane
// (gothic:topic:<key>:<field>). It copies the opaque bytes into a fresh
// Uint8Array, deposits them in the shared __gothic_topic buffer under the
// broadcast key, then dispatches the bare CustomEvent through
// __gothicDispatchAsync — which queueMicrotask-defers the dispatch so it fires on
// a CLEAN call stack, never nested inside the registering/requesting TinyGo
// component's asyncify turn (the documented `unreachable` re-entrancy crash).
// Every core→component topic dispatch goes through this microtask hop.
// forwardBufs / forwardBufLens pool one Uint8Array per broadcast key. The
// full-Go core registers every js.Value from Uint8Array.New() in the wasm_exec
// bridge table until a Go GC cycle runs finalizers — which tiny per-click frames
// never trigger — so a fresh New() per broadcast accumulates unbounded across a
// session (heap-snapshot-leak). setBytes copies the bytes into its OWN per-key
// _views pool (view.set), so the pooled arr can be safely overwritten on every
// broadcast. Reallocated only when the frame length changes (setBytes reads
// u8.byteLength, so the view must be exactly len(buf)). Mutated only on the
// single JS event-loop goroutine — same safety basis as retainedTopicFns.
var forwardBufs = map[string]js.Value{}
var forwardBufLens = map[string]int{}

func forwardBufFor(global js.Value, bcast string, n int) js.Value {
	if forwardBufLens[bcast] == n {
		if arr, ok := forwardBufs[bcast]; ok {
			return arr
		}
	}
	arr := global.Get("Uint8Array").New(n)
	forwardBufs[bcast] = arr
	forwardBufLens[bcast] = n
	return arr
}

func forwardTopicField(global js.Value, key, field string, buf []byte) {
	topic := global.Get("__gothic_topic")
	if !topic.Truthy() {
		return
	}
	bcast := protocol.TopicBroadcastEvent(key, field)
	arr := forwardBufFor(global, bcast, len(buf))
	js.CopyBytesToJS(arr, buf)
	topic.Call("setBytes", js.ValueOf(bcast), arr)
	global.Call("__gothicDispatchAsync", js.ValueOf(bcast))
}

// subscribeDurableField installs a DATA-PLANE listener for one durable field's
// WRITE requests (gothic:durable-req:<key>:<field>). On each write it reads the
// raw frame out of the shared window.__gothic_topic buffer (never decoding it) and
// asks protocol.DecideDurableStore whether to store it, passing the CURRENTLY
// stored frame AND whether the field has ever been written (presence). Unlike the
// topic path an EMPTY frame is NOT skipped: empty is a legitimate durable value (a
// cleared field), so a zero-length write must overwrite a prior non-empty value.
// Presence is read from the map itself (`_, present := coreDurable[key][field]`)
// and an empty write is stored as a non-nil zero-length slice so the field reads
// back as present next time. Unlike the topic path it does NOT rebroadcast: a
// durable component is private to one placement, so stored frames leave the core
// only on register-time replay (forwardDurableField). The core touches bytes only
// to compare and copy — it never interprets them (generic interpreter DEFERRED).
// The js.Func is retained for the page lifetime (never Release'd).
func subscribeDurableField(global, doc js.Value, coreDurable map[string]map[string][]byte, key, field string) {
	reqEvent := protocol.DurableReqEvent(key, field)
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		topic := global.Get("__gothic_topic")
		if !topic.Truthy() {
			return nil
		}
		view := topic.Call("get", js.ValueOf(reqEvent))
		if !view.Truthy() {
			return nil // no frame deposited at all — nothing to store.
		}
		n := view.Get("byteLength").Int()
		// n may be 0 (a cleared value). make([]byte, 0) is non-nil, so a stored
		// empty frame reads back as present. Only copy when there are bytes.
		buf := make([]byte, n)
		if n > 0 {
			js.CopyBytesToGo(buf, view)
		}
		stored, present := coreDurable[key][field]
		if !protocol.DecideDurableStore(stored, present, buf) {
			return nil
		}
		coreDurable[key][field] = buf
		return nil
	})
	retainedDurableFns = append(retainedDurableFns, fn)
	doc.Call("addEventListener", reqEvent, fn)
}

// forwardDurableField re-forwards a stored per-field frame VERBATIM on the
// data-plane (gothic:durable:<key>:<field>) during register-time REPLAY. It
// copies the opaque bytes into a fresh Uint8Array (zero-length for a cleared
// field — empty is a legitimate durable value and must be replayed so the
// consumer restores the clear over its mount default), deposits them in the
// shared __gothic_topic buffer under the broadcast key, then dispatches the bare
// CustomEvent through __gothicDispatchAsync — which queueMicrotask-defers the
// dispatch so it fires on a CLEAN call stack, never nested inside the registering
// TinyGo component's asyncify turn (the documented `unreachable` re-entrancy
// crash). Mirrors forwardTopicField, minus the empty-skip.
func forwardDurableField(global js.Value, key, field string, buf []byte) {
	topic := global.Get("__gothic_topic")
	if !topic.Truthy() {
		return
	}
	bcast := protocol.DurableBroadcastEvent(key, field)
	arr := forwardBufFor(global, bcast, len(buf))
	if len(buf) > 0 {
		js.CopyBytesToJS(arr, buf)
	}
	topic.Call("setBytes", js.ValueOf(bcast), arr)
	global.Call("__gothicDispatchAsync", js.ValueOf(bcast))
}

// announceOnline schedules the readiness announcement (no detail) on a clean
// stack. See the package doc's asyncify note: a synchronous announce would
// re-enter a registering TinyGo component's asyncify turn.
func announceOnline(global, doc js.Value) {
	scheduleDispatch(global, doc, evOnline, js.Undefined())
}

// scheduleDispatch builds and fires a CustomEvent on a fresh microtask so the
// listener always runs on a clean call stack — never nested inside the
// scheduler turn that triggered it (asyncify re-entrancy safety), mirroring the
// topic bus's __gothicDispatchAsync. Pass js.Undefined() for detail to emit a
// detail-less event. The one-shot js.Func releases itself after firing so the
// core does not leak a bridge slot per ack/announce.
func scheduleDispatch(global, doc js.Value, name string, detail js.Value) {
	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) any {
		var evt js.Value
		if detail.Truthy() {
			init := global.Get("Object").New()
			init.Set("detail", detail)
			evt = global.Get("CustomEvent").New(name, init)
		} else {
			evt = global.Get("CustomEvent").New(name)
		}
		doc.Call("dispatchEvent", evt)
		cb.Release()
		return nil
	})
	global.Call("queueMicrotask", cb)
}
