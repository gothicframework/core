//go:build !js || !wasm

package runtime

import "testing"

// TestHTMXRuntimeStubs_NoOpsDoNotPanic exercises the host twin of the runtime
// HTMX API (htmx_stub.go): the consts, AjaxOpts, and every method must compile
// and be inert on a non-browser build, since the runtime package is host-tested.
func TestHTMXRuntimeStubs_NoOpsDoNotPanic(t *testing.T) {
	if InnerHTML != "innerHTML" || None != "none" || Delete != "delete" {
		t.Error("SwapStrategy consts have unexpected values")
	}
	if EvtBeforeSwap != "htmx:beforeSwap" || EvtXHRLoadend != "htmx:xhr:loadend" {
		t.Error("HtmxEvent consts have unexpected values")
	}

	HTMX.Ajax("GET", "/api")
	HTMX.Ajax("POST", "/api", AjaxOpts{Target: "#o", Swap: InnerHTML})
	HTMX.Swap("#o", "<p>hi</p>", OuterHTML)
	HTMX.Process("#o")
	HTMX.Trigger("#o", EvtAfterSwap, map[string]any{"k": "v"})
	HTMX.On(EvtAfterSwap, func(e Event) { _ = e })
	HTMX.OnGlobal(EvtLoad, func(e Event) { _ = e })
	HTMX.Off(EvtAfterSwap, func(e Event) { _ = e })
	HTMX.AddClass("#o", "c")
	HTMX.RemoveClass("#o", "c")
	HTMX.ToggleClass("#o", "c")
	HTMX.TakeClass("#o", "c")
	HTMX.Remove("#o")

	if HTMX.Find("#x") != (JSValue{}) {
		t.Error("Find stub should return zero JSValue")
	}
	if HTMX.FindAll(".x") != nil {
		t.Error("FindAll stub should return nil")
	}
	if HTMX.Closest("#x", ".y") != (JSValue{}) {
		t.Error("Closest stub should return zero JSValue")
	}
	if len(HTMX.Values("#f")) != 0 {
		t.Error("Values stub should return an empty map")
	}
}
