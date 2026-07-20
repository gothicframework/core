//go:build !js || !wasm

package runtime

// Host-build no-ops for the durable state cache. The real
// implementations (js && wasm, durable.go) perform the core register/replay
// handshake and per-field persistence. Server-side a durable component compiles
// and renders exactly like a non-durable one — DurableObserve does nothing and
// DurableKey resolves empty — so SSR output is byte-identical whether or not a
// component opts into durability.

// DurableKey is empty on the host build (no DOM to read data-gothic-durable-key
// from), so DurableObserve treats every placement as non-durable server-side.
func DurableKey() string { return "" }

// DurableObserve is a no-op server-side: obs behaves as a plain observable and no
// durable wiring is emitted into the SSR render.
func DurableObserve[T any](field string, obs *Observable[T], encode func(T) string, decode func(string) T) {
}
