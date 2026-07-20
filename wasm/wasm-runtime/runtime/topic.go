//go:build js && wasm

package runtime

import (
	"runtime"
	"strconv"
	"syscall/js"
	"time"
	"unsafe"
)

// _gothicKeyRegistry maps topic key names to their TopicKey (stored as any).
// Populated by generated init() calls in the compiled WASM main.

// TopicKey is a typed topic identifier that carries its own codec.
// T encodes the value type — provider and consumer must use the same key.
// Construct via the factory functions (IntKey, StringKey, BinaryKey, etc.),
// not as a struct literal.
type TopicKey[T any] struct {
	Name   string
	encode func(T) string
	decode func(string) T
}

// ── Primitive key factories ──────────────────────────────────────────────────
//
// All 16 primitive TopicKey factories share the same shape: build a
// TopicKey[T] with a strconv-based encode and a strconv-based decode. The
// helper `newPrimitiveKey` removes the boilerplate. Since the existing
// factories already returned generic types, TinyGo monomorphizes each
// instantiation just as it did before (no `interface{}`, no reflection).

// newPrimitiveKey builds a TopicKey[T] for a primitive value using the
// provided strconv-based encode and decode functions.
func newPrimitiveKey[T any](name string, encode func(T) string, decode func(string) T) TopicKey[T] {
	return TopicKey[T]{Name: name, encode: encode, decode: decode}
}

func BoolKey(name string) TopicKey[bool] {
	return newPrimitiveKey(name,
		strconv.FormatBool,
		func(s string) bool { b, _ := strconv.ParseBool(s); return b },
	)
}

func StringKey(name string) TopicKey[string] {
	return newPrimitiveKey(name,
		func(s string) string { return s },
		func(s string) string { return s },
	)
}

func IntKey(name string) TopicKey[int] {
	return newPrimitiveKey(name,
		strconv.Itoa,
		func(s string) int { n, _ := strconv.Atoi(s); return n },
	)
}

func Int8Key(name string) TopicKey[int8] {
	return newPrimitiveKey(name,
		func(v int8) string { return strconv.FormatInt(int64(v), 10) },
		func(s string) int8 { n, _ := strconv.ParseInt(s, 10, 8); return int8(n) },
	)
}

func Int16Key(name string) TopicKey[int16] {
	return newPrimitiveKey(name,
		func(v int16) string { return strconv.FormatInt(int64(v), 10) },
		func(s string) int16 { n, _ := strconv.ParseInt(s, 10, 16); return int16(n) },
	)
}

func Int32Key(name string) TopicKey[int32] {
	return newPrimitiveKey(name,
		func(v int32) string { return strconv.FormatInt(int64(v), 10) },
		func(s string) int32 { n, _ := strconv.ParseInt(s, 10, 32); return int32(n) },
	)
}

func Int64Key(name string) TopicKey[int64] {
	return newPrimitiveKey(name,
		func(v int64) string { return strconv.FormatInt(v, 10) },
		func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n },
	)
}

func UintKey(name string) TopicKey[uint] {
	return newPrimitiveKey(name,
		func(v uint) string { return strconv.FormatUint(uint64(v), 10) },
		func(s string) uint { n, _ := strconv.ParseUint(s, 10, 64); return uint(n) },
	)
}

func Uint8Key(name string) TopicKey[uint8] {
	return newPrimitiveKey(name,
		func(v uint8) string { return strconv.FormatUint(uint64(v), 10) },
		func(s string) uint8 { n, _ := strconv.ParseUint(s, 10, 8); return uint8(n) },
	)
}

func Uint16Key(name string) TopicKey[uint16] {
	return newPrimitiveKey(name,
		func(v uint16) string { return strconv.FormatUint(uint64(v), 10) },
		func(s string) uint16 { n, _ := strconv.ParseUint(s, 10, 16); return uint16(n) },
	)
}

func Uint32Key(name string) TopicKey[uint32] {
	return newPrimitiveKey(name,
		func(v uint32) string { return strconv.FormatUint(uint64(v), 10) },
		func(s string) uint32 { n, _ := strconv.ParseUint(s, 10, 32); return uint32(n) },
	)
}

func Uint64Key(name string) TopicKey[uint64] {
	return newPrimitiveKey(name,
		func(v uint64) string { return strconv.FormatUint(v, 10) },
		func(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n },
	)
}

func Float32Key(name string) TopicKey[float32] {
	return newPrimitiveKey(name,
		func(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) },
		func(s string) float32 { f, _ := strconv.ParseFloat(s, 32); return float32(f) },
	)
}

func Float64Key(name string) TopicKey[float64] {
	return newPrimitiveKey(name,
		func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) },
		func(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f },
	)
}

// RuneKey is IntKey for rune (= int32).
func RuneKey(name string) TopicKey[rune] {
	return newPrimitiveKey(name,
		func(v rune) string { return strconv.FormatInt(int64(v), 10) },
		func(s string) rune { n, _ := strconv.ParseInt(s, 10, 32); return rune(n) },
	)
}

// ByteKey is UintKey for byte (= uint8).
func ByteKey(name string) TopicKey[byte] {
	return newPrimitiveKey(name,
		func(v byte) string { return strconv.FormatUint(uint64(v), 10) },
		func(s string) byte { n, _ := strconv.ParseUint(s, 10, 8); return byte(n) },
	)
}

// ── JS topic store ───────────────────────────────────────────────────────────

func ensureTopicStore() js.Value {
	store := js.Global().Get("__gothic_topic_store")
	if store.IsUndefined() {
		store = js.Global().Get("Object").New()
		js.Global().Set("__gothic_topic_store", store)
	}
	return store
}

// ── Direct-memory payload dispatch ──────────────────────────────────────────

// dispatchHold keeps each key's payload slice alive until the next dispatch on
// the same key overwrites it. The queueMicrotask callback in Gothic's bootstrap
// JS reads directly from WASM linear memory via the raw pointer; the slice must
// not be GC'd before that microtask fires.
var dispatchHold = map[string][]byte{}

// dispatchDirect writes encoded into a Go-owned buffer, passes the raw WASM
// memory offset to __gothic_topic.set (Bootstrap JS reads the bytes directly
// from instance.exports.memory.buffer — no js.CopyBytesToJS, no Uint8Array
// allocation, no _values[] entry for the payload), then fires an async event
// so document listeners still receive it.
func dispatchDirect(keyName, eventPrefix string, encoded []byte) {
	buf := make([]byte, len(encoded))
	copy(buf, encoded)
	dispatchHold[eventPrefix+keyName] = buf

	ptr := int32(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))
	// __gothic_set is keyed by the instance's BOOTSTRAP mount scope (the
	// bootstrap sets __gothic_set[id] with id == GOTHIC_SCOPE), same as the
	// halt slot, __gothic_registry, and __gothicInstances. It selects which
	// WASM instance's linear memory the JS reads from — NOT which
	// [data-gothic-scope] the event came from. In a multi-scope instance
	// activeScope() may be a non-mount scope with no __gothic_set entry, so
	// keying by it would resolve undefined and Invoke would throw. Always key
	// by bootstrapScope(). (Single-scope: bootstrapScope() == activeScope().)
	js.Global().Get("__gothic_set").Get(bootstrapScope()).Invoke(
		js.ValueOf(eventPrefix+keyName),
		js.ValueOf(ptr),
		js.ValueOf(len(buf)),
	)
	js.Global().Call("__gothicDispatchAsync", js.ValueOf(eventPrefix+keyName))
}

// ── SharedTopicObservable ───────────────────────────────────────────────────────

// SharedTopicObservable is a reactive Observable bound to a shared topic key.
// Get/Set work like a regular Observable, but Set also broadcasts the new value
// to every other WASM module sharing the same key.
// Used internally by the auto-generated topic constructors (e.g. PageTopic()).
type SharedTopicObservable[T any] struct {
	inner *Observable[T]
	key   TopicKey[T]
}

func (s *SharedTopicObservable[T]) Get() T { return s.inner.Get() }

func (s *SharedTopicObservable[T]) Set(v T) {
	s.inner.value = v
	s.inner.notifyAll()
	encoded := s.key.encode(v)
	ensureTopicStore().Set(s.key.Name, encoded)
	dispatchDirect(s.key.Name, "gothic:topic:", []byte(encoded))
}

func (s *SharedTopicObservable[T]) addEffect(e *Subscription)    { s.inner.addEffect(e) }
func (s *SharedTopicObservable[T]) removeEffect(e *Subscription) { s.inner.removeEffect(e) }

// AutoKey is rewritten to BinaryKey by the CLI before TinyGo compiles.
// This stub exists so server-side code compiles; WASM code never calls it directly.
func AutoKey[T any](name string) TopicKey[T] { return TopicKey[T]{Name: name} }

// ── Per-field topic signals ───────────────────────────────────────────────────

// ObservableField is a reactive Observable bound to one field of a shared topic struct.
// It behaves like *Observable[T] but Set also broadcasts the full topic to other modules.
// Pass *ObservableField as a dep in Observe to react to individual property changes.
type ObservableField[T any] struct {
	sig       *Observable[T]
	broadcast func()
}

// NewObservableField creates an ObservableField with the given initial value.
func NewObservableField[T any](initial T) *ObservableField[T] {
	return &ObservableField[T]{sig: &Observable[T]{value: initial}}
}

// SetBroadcast wires the broadcast callback called whenever Set updates this field.
func (f *ObservableField[T]) SetBroadcast(fn func()) { f.broadcast = fn }

// Get returns the current value, auto-registering as a dep of any running effect.
func (f *ObservableField[T]) Get() T { return f.sig.Get() }

// Peek returns the current value without registering as an effect dependency.
// Used internally by broadcast closures to read sibling field values safely.
func (f *ObservableField[T]) Peek() T { return f.sig.value }

// Set sends a set-request to the topic manager WASM.
// The local value is silently updated so Peek() returns the correct value during
// encoding, but subscribers are NOT notified until the manager broadcasts back.
func (f *ObservableField[T]) Set(v T) {
	f.sig.value = v
	f.broadcast()
}

// ApplyExternal updates value and notifies subscribers without triggering broadcast.
// Used by generated topic listeners and Set-all methods to avoid redundant events.
func (f *ObservableField[T]) ApplyExternal(v T) {
	f.sig.value = v
	f.sig.notifyAll()
}

func (f *ObservableField[T]) addEffect(e *Subscription)    { f.sig.addEffect(e) }
func (f *ObservableField[T]) removeEffect(e *Subscription) { f.sig.removeEffect(e) }

// ── Cross-module topic helpers ────────────────────────────────────────────────

// ReadTopicStore reads the encoded topic value from the shared JS store.
func ReadTopicStore(keyName string) (string, bool) {
	v := ensureTopicStore().Get(keyName)
	if v.IsUndefined() || v.IsNull() {
		return "", false
	}
	return v.String(), true
}

// BroadcastTopicEncoded writes encoded to the JS store and dispatches a CustomEvent.
func BroadcastTopicEncoded(keyName, encoded string) {
	ensureTopicStore().Set(keyName, encoded)
	dispatchDirect(keyName, "gothic:topic:", []byte(encoded))
}

// topicViewInto copies the current pooled JS view for fullKey into *scratch — a
// PER-LISTENER buffer reused across receives — and returns the filled slice.
//
// This is the receive-side twin of the JS bootstrap's _bufs/_views pool: for a
// stable-size payload *scratch is allocated ONCE and reused on every subsequent
// broadcast, so a repeated toggle produces NO new []byte per receive. On TinyGo's
// conservative, no-shrink GC each fresh make([]byte,n) of a multi-MB frame was
// ratcheting linear memory up (the documented 720 MB heap growth); the scratch
// buffer removes that per-receive allocation entirely, and threading the returned
// slice straight into the decoder removes the extra string(dst)+[]byte(detail)
// round-trip copies the old path made.
//
// ok is false for an absent/empty view (topic listeners treat n==0 as a no-op).
// The returned slice ALIASES *scratch and MUST be consumed synchronously inside
// the callback: the next broadcast on the same listener overwrites it. The
// generated decoder only reads through the frame and copies out its own String()
// fields, so it never retains the buffer — the alias is safe. *scratch grows only
// (never shrinks), so a later smaller frame reuses the existing capacity.
// The zero-copy path uses the per-instance __getInto closure (installed on this
// module's window.__gothicInstances[<mountScope>] slot next to __setText), the
// receive-side twin of the SetText fix. __getInto copies the pooled frame bytes
// straight into this module's own linear memory at *scratch's pointer and returns
// only the length as a NUMBER (NaN-boxed, never a _values slot) — so unlike the
// boxed get() path it never boxes the pool's Uint8Array view (which the pool
// recreates on every byteLength change, adding a never-finalized TinyGo slot per
// size change). fullKey is a CONSTANT string per listener, so js.ValueOf(fullKey)
// dedups in _ids and adds no slot either. Net on the hot receive path: zero new
// js.Value boxed.
func topicViewInto(fullKey string, scratch *[]byte) ([]byte, bool) {
	if fn := topicGetInto(); !fn.IsUndefined() && !fn.IsNull() {
		return topicViewIntoZeroCopy(fn, fullKey, scratch)
	}
	return topicViewIntoBoxed(fullKey, scratch)
}

// topicGetInto resolves this module's __getInto closure, addressed by
// bootstrapScope() exactly like __setText and the __gothic_set / __halt slots
// (it selects which instance's linear MEMORY the copy targets). Returns an
// undefined js.Value when absent (non-bootstrap / test / hand-rolled envelopes),
// which routes callers to the boxed fallback.
func topicGetInto() js.Value {
	insts := js.Global().Get("__gothicInstances")
	if insts.IsUndefined() || insts.IsNull() {
		return js.Undefined()
	}
	slot := insts.Get(bootstrapScope())
	if slot.IsUndefined() || slot.IsNull() {
		return js.Undefined()
	}
	return slot.Get("__getInto")
}

// topicViewIntoZeroCopy invokes __getInto(fullKey, ptr, cap), passing *scratch's
// linear-memory pointer and capacity, and grows+retries once on the "scratch too
// small" signal. Return codes from getInto: -1 = no frame; ~n (negative) = frame
// is n bytes but scratch cap is smaller (grow to n and retry); 0 = empty frame
// (no-op); n>0 = n bytes were copied into *scratch.
func topicViewIntoZeroCopy(fn js.Value, fullKey string, scratch *[]byte) ([]byte, bool) {
	if cap(*scratch) == 0 {
		*scratch = make([]byte, 0, 128)
	}
	for {
		buf := (*scratch)[:cap(*scratch)]
		ptr := int32(uintptr(unsafe.Pointer(unsafe.SliceData(buf))))
		n := fn.Invoke(js.ValueOf(fullKey), js.ValueOf(ptr), js.ValueOf(cap(buf))).Int()
		// The Invoke is synchronous: __getInto writes the bytes and returns before
		// we regain control, so the backing array cannot be collected mid-copy.
		runtime.KeepAlive(*scratch)
		if n == -1 { // no frame — same as the old !data.Truthy() no-op
			return nil, false
		}
		if n < 0 { // scratch too small: n == ~size, so size == ^n. Grow and retry.
			*scratch = make([]byte, ^n)
			continue
		}
		if n == 0 { // present but empty — no-op, matching the old n==0 branch
			return nil, false
		}
		*scratch = buf[:n]
		return *scratch, true
	}
}

// topicViewIntoBoxed is the pre-zero-copy fallback: box the pooled Uint8Array
// view and CopyBytesToGo it into *scratch. Used only when __getInto is absent
// (non-bootstrap / test contexts) so unit tests and hand-rolled envelopes behave
// exactly as before.
func topicViewIntoBoxed(fullKey string, scratch *[]byte) ([]byte, bool) {
	data := js.Global().Get("__gothic_topic").Call("get", js.ValueOf(fullKey))
	if data.IsNull() || data.IsUndefined() {
		return nil, false
	}
	n := data.Get("byteLength").Int()
	if n == 0 {
		return nil, false
	}
	if cap(*scratch) < n {
		*scratch = make([]byte, n)
	}
	buf := (*scratch)[:n]
	js.CopyBytesToGo(buf, data)
	*scratch = buf
	return buf, true
}

// ListenTopicEvent registers a cross-module listener for topic updates.
//
// The listener re-establishes the scope that registered it (see scopedListener)
// because the topic CustomEvent fires from an async turn where findScope()
// cannot see the component's [data-gothic-scope]; fn typically drives scoped
// DOM helpers via ApplyExternal → Observe, which must resolve to this scope.
func ListenTopicEvent(keyName string, fn func(string)) {
	fullKey := "gothic:topic:" + keyName
	var scratch []byte
	listener := scopedListener(func() {
		if buf, ok := topicViewInto(fullKey, &scratch); ok {
			fn(string(buf))
		}
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// RequestTopicSet dispatches a set-request to the topic manager WASM for this key.
// The manager is the sole writer: it applies the update and broadcasts back.
func RequestTopicSet(keyName, encoded string) {
	dispatchDirect(keyName, "gothic:topic-req:", []byte(encoded))
}

// ListenTopicSetReq registers a handler for incoming set-requests on a topic manager WASM.
func ListenTopicSetReq(keyName string, fn func(string)) {
	fullKey := "gothic:topic-req:" + keyName
	var scratch []byte
	listener := scopedListener(func() {
		if buf, ok := topicViewInto(fullKey, &scratch); ok {
			fn(string(buf))
		}
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// BroadcastTopicEncodedField broadcasts an already-encoded single field value.
// Event name: "gothic:topic:<keyName>:<fieldName>"
func BroadcastTopicEncodedField(keyName, fieldName, encoded string) {
	dispatchDirect(keyName+":"+fieldName, "gothic:topic:", []byte(encoded))
}

// RequestTopicSetField sends a per-field set-request to the manager.
// Event name: "gothic:topic-req:<keyName>:<fieldName>"
func RequestTopicSetField(keyName, fieldName, encoded string) {
	dispatchDirect(keyName+":"+fieldName, "gothic:topic-req:", []byte(encoded))
}

// RequestTopicSetFieldBytes is RequestTopicSetField for callers that already
// hold the encoded frame as []byte (the generated consumer's _broadcastAll). It
// hands the bytes straight to dispatchDirect — which copies them into
// dispatchHold — skipping the string(encoded)→[]byte(encoded) round-trip the
// string form forces on every per-field send. Event name is identical:
// "gothic:topic-req:<keyName>:<fieldName>".
func RequestTopicSetFieldBytes(keyName, fieldName string, b []byte) {
	dispatchDirect(keyName+":"+fieldName, "gothic:topic-req:", b)
}

// ListenTopicEventField subscribes to per-field broadcasts from the core hub.
//
// fn receives the raw frame as []byte (not string): this is the HOT consumer
// data-plane path (one call per broadcast per subscribed field). Passing []byte
// lets the generated consumer feed the pooled scratch buffer straight into
// NewDecoder, eliminating the string(dst)→[]byte(detail) double copy the old
// func(string) signature forced. Combined with the per-listener scratch reuse in
// topicViewInto, a stable-payload rebroadcast now allocates ZERO transient frame
// bytes on the receive side — the fix for the multi-MB linear-memory ratchet on
// TinyGo's no-shrink conservative heap. The []byte is only valid for the duration
// of fn (it aliases the reused scratch); the decoder copies out its own values.
func ListenTopicEventField(keyName, fieldName string, fn func([]byte)) {
	fullKey := "gothic:topic:" + keyName + ":" + fieldName
	var scratch []byte
	listener := scopedListener(func() {
		if buf, ok := topicViewInto(fullKey, &scratch); ok {
			fn(buf)
		}
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// ListenTopicSetReqField subscribes to per-field set-requests (used by the manager).
func ListenTopicSetReqField(keyName, fieldName string, fn func(string)) {
	fullKey := "gothic:topic-req:" + keyName + ":" + fieldName
	var scratch []byte
	listener := scopedListener(func() {
		if buf, ok := topicViewInto(fullKey, &scratch); ok {
			fn(string(buf))
		}
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// pingEvents caches one CustomEvent JS object per keyName so we don't allocate
// a new JS value (and a permanent TinyGo bridge slot) on every ping.
var pingEvents = map[string]js.Value{}

// PingTopicManager dispatches a ping to the topic manager asking for an online ack.
func PingTopicManager(keyName string) {
	evt, ok := pingEvents[keyName]
	if !ok {
		evt = js.Global().Get("CustomEvent").New("gothic:topic-ping:" + keyName)
		pingEvents[keyName] = evt
	}
	js.Global().Get("document").Call("dispatchEvent", evt)
}

// ListenTopicOnline registers a handler that receives the manager's online ack with current state.
// Fires once on manager startup and on every ping response.
func ListenTopicOnline(keyName string, fn func(string)) {
	fullKey := "gothic:topic-online:" + keyName
	var scratch []byte
	listener := scopedListener(func() {
		if buf, ok := topicViewInto(fullKey, &scratch); ok {
			fn(string(buf))
		}
	})
	keep = append(keep, listener)
	addScopedDocListener(fullKey, listener)
}

// ListenTopicPing registers a handler for incoming pings on the topic manager WASM.
func ListenTopicPing(keyName string, fn func()) {
	listener := scopedListener(fn)
	keep = append(keep, listener)
	addScopedDocListener("gothic:topic-ping:"+keyName, listener)
}

// BroadcastTopicOnline dispatches the online ack to all consumer WASMs for this key.
func BroadcastTopicOnline(keyName, encoded string) {
	ensureTopicStore().Set(keyName, encoded)
	dispatchDirect(keyName, "gothic:topic-online:", []byte(encoded))
}

// UpdateTopicOnlineStore updates the JS-side topic store so that late-joining
// consumers see fresh data via ReadTopicStore, WITHOUT dispatching the
// gothic:topic-online event. Use this from ListenTopicSetReq to fix the startup
// race (T5) without triggering ListenTopicOnline scans in already-running consumers.
func UpdateTopicOnlineStore(keyName string, encoded []byte) {
	ensureTopicStore().Set(keyName, string(encoded))
}

// PingUntilOnline retries PingTopicManager every 50 ms until isOnline returns true.
// Runs in its own goroutine so it doesn't block the caller.
//
// A goroutine does not inherit the scope carrier across suspension points, so we
// CaptureScope() at spawn and RunInScope() each iteration to re-establish the
// caller's scope (isOnline may read scoped state). For a single-scope instance
// the captured scope is bootstrapScope, so behaviour is unchanged.
func PingUntilOnline(keyName string, isOnline func() bool) {
	scope := CaptureScope()
	go func() {
		for {
			done := false
			RunInScope(scope, func() {
				if isOnline() {
					done = true
					return
				}
				PingTopicManager(keyName)
			})
			if done {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

// ── Topic ↔ full-Go core control-plane ───────────────────────────────────────
//
// The per-topic MANAGER WASM is retired: the always-loaded full-Go static core
// is now the single, generic topic hub. The DATA-PLANE is unchanged —
// per-field binary frames on gothic:topic-req:<key>:<field> (RequestTopicSetField,
// sender→hub) and gothic:topic:<key>:<field> (ListenTopicEventField, hub→consumers).
// The core store-and-forwards those frames OPAQUELY by key+field string, never
// decoding a byte. What changes is the CONTROL-PLANE handshake below: a JSON-shaped
// register message that tells the core a topic's key + field names (so it can
// subscribe + replay) and a per-key online ack the consumer waits on. This is the
// two-tier split — binary on the data-plane, JSON/string on the control-plane.
//
// The event names MUST stay byte-identical to
// pkg/wasm/core-runtime/protocol/topic.go. That package can't be imported here —
// the runtime is a separate TinyGo module extracted at build time — so the two
// are kept in lockstep by hand (same situation as the gothic:topic-* names above).
const (
	evCoreOnline        = "gothic:core:online"
	evTopicRegister     = "gothic:core:topic-register"
	evTopicOnlinePrefix = "gothic:core:topic-online:"
)

// RegisterTopicWithCore performs the topic → core control-plane registration.
// The CustomEvent detail is a plain JS object {key, fields:[...]} — control-plane
// JSON/values, NEVER binary. It hands the core the topic's wire key and the
// ORDERED field-name list so the core can subscribe to each
// gothic:topic-req:<key>:<field> and replay the current per-field state back. The
// field names are ROUTING metadata; the core still never interprets a payload
// byte (the generic interpreter is DEFERRED), so this keeps the core opaque.
//
// Startup races are handled the same way as GothicRegisterWithCore, with no
// goroutine:
//
//   - core already up  → the register is received; the core replays this topic's
//     stored per-field frames and announces the per-key online ack.
//   - core comes up later → it announces gothic:core:online on boot; this re-fires
//     the register until the per-key online ack lands (acked short-circuits, so at
//     most one extra send per core boot).
//
// Every dispatch is wrapped in queueMicrotask so it leaves a clean call stack and
// never fires from inside this module's running asyncify turn (the RETURN side —
// the core's replay + ack — is symmetric: the full-Go core microtask-schedules
// those, so nothing re-enters this component's scheduler).
func RegisterTopicWithCore(key string, fields []string) {
	doc := js.Global().Get("document")
	acked := false

	ack := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		acked = true
		return nil
	})
	keep = append(keep, ack)
	doc.Call("addEventListener", evTopicOnlinePrefix+key, ack)

	// Build the register CustomEvent and its microtask dispatch callback ONCE and
	// reuse them across every (re)fire. The detail {key, fields} is constant for
	// this registration, so a fresh object graph per fire would be pure waste — and,
	// critically, appending a new js.Func to the package-global keep slice (a
	// permanent GC root) on every fire is a genuine retention leak: fire() re-runs
	// on every gothic:core:online announce (multi-component pages, pings), so a
	// per-fire js.Func + CustomEvent would accumulate for the page lifetime. A
	// CustomEvent may be re-dispatched after each dispatch completes, so one cached
	// evt is safe to fire repeatedly.
	detail := js.Global().Get("Object").New()
	detail.Set("key", key)
	arr := js.Global().Get("Array").New()
	for _, f := range fields {
		arr.Call("push", f)
	}
	detail.Set("fields", arr)
	init := js.Global().Get("Object").New()
	init.Set("detail", detail)
	evt := js.Global().Get("CustomEvent").New(evTopicRegister, init)
	cb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		doc.Call("dispatchEvent", evt)
		return nil
	})
	keep = append(keep, cb)

	fire := func() {
		js.Global().Call("queueMicrotask", cb)
	}

	// Re-send once the core announces readiness (covers consumer-before-core).
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

// ListenTopicCoreOnline registers a handler for the core's per-key online ack
// (gothic:core:topic-online:<key>). The core dispatches it AFTER replaying the
// topic's current per-field state to this consumer, so by the time the handler
// runs the per-field ListenTopicEventField handlers have already applied the
// replayed values. The generated consumer uses it to flip _online and flush any
// pending whole-struct Set (fanned out per-field).
//
// Like the other topic listeners it re-establishes the registering scope
// (scopedListener) because the ack fires from an async document-event turn where
// findScope() cannot see the component's [data-gothic-scope], and routes through
// addScopedDocListener so the per-scope teardown can remove it on unmount.
func ListenTopicCoreOnline(key string, fn func()) {
	listener := scopedListener(fn)
	keep = append(keep, listener)
	addScopedDocListener(evTopicOnlinePrefix+key, listener)
}

// CustomKey returns a TopicKey with user-supplied encode/decode functions.
func CustomKey[T any](name string, encode func(T) string, decode func(string) T) TopicKey[T] {
	return TopicKey[T]{Name: name, encode: encode, decode: decode}
}

// BinaryKey returns a TopicKey that serializes T using a compact little-endian binary
// codec. No reflection, no encoding/json — just typed Encoder/Decoder calls.
// The encode function writes fields onto e; the decode function reads them back and returns T.
// Field order must match between encode and decode.
//
// Example:
//
//	BinaryKey[Page]("page",
//	    func(v Page, e *Encoder) {
//	        e.I64(int64(v.Pings))
//	        e.String(v.Label)
//	        e.String(v.Theme)
//	    },
//	    func(d *Decoder) PageCtx {
//	        return PageCtx{Pings: int(d.I32()), Label: d.String(), Theme: d.String()}
//	    },
//	)
func BinaryKey[T any](name string, encode func(T, *Encoder), decode func(*Decoder) T) TopicKey[T] {
	return TopicKey[T]{
		Name: name,
		encode: func(v T) string {
			e := NewEncoder(64)
			encode(v, e)
			return HexEncode(e.Buf)
		},
		decode: func(s string) T {
			d := NewDecoder(HexDecode(s))
			return decode(d)
		},
	}
}

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

// TopicConfig holds per-topic configuration.
type TopicConfig struct {
	Name             string
	Compression      Compression  // GZIP (default) or BROTLI
	Compiler         WasmCompiler // GothicTinyGo (default), LocalTinyGo, or Golang
	SubscriberFnName string       // overrides generated accessor func name (default: <StructName>Topic)
}

// CreateTopic declares a topic. The CLI AST scanner detects this call and
// generates the concrete typed accessor. At runtime this returns a no-op.
func CreateTopic[T any](zero T, cfg TopicConfig) func() interface{} {
	return func() interface{} { return nil }
}
