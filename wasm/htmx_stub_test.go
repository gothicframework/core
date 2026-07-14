package wasm_test

import (
	"testing"

	wasm "github.com/gothicframework/core/wasm"
)

// TestHTMXStubs_NoOpsDoNotPanic exercises every HTMX server-side stub plus the
// SwapStrategy/HtmxEvent consts and AjaxOpts, confirming they are inert (no
// panics) and return documented zero values so a ClientSideState block using the
// HTMX API compiles and runs harmlessly for SSR.
func TestHTMXStubs_NoOpsDoNotPanic(t *testing.T) {
	// consts type-check and carry the htmx wire strings.
	if wasm.InnerHTML != "innerHTML" || wasm.OuterHTML != "outerHTML" ||
		wasm.BeforeBegin != "beforebegin" || wasm.AfterBegin != "afterbegin" ||
		wasm.BeforeEnd != "beforeend" || wasm.AfterEnd != "afterend" ||
		wasm.Delete != "delete" || wasm.None != "none" {
		t.Error("SwapStrategy consts have unexpected values")
	}
	if wasm.EvtAfterSwap != "htmx:afterSwap" || wasm.EvtBeforeRequest != "htmx:beforeRequest" ||
		wasm.EvtResponseError != "htmx:responseError" || wasm.EvtXHRProgress != "htmx:xhr:progress" ||
		wasm.EvtValidationValidate != "htmx:validation:validate" {
		t.Error("HtmxEvent consts have unexpected values")
	}

	// Custom-event escape hatch via string cast still type-checks.
	custom := wasm.HtmxEvent("htmx:sse:message")
	_ = custom

	// Every method is a safe no-op.
	wasm.HTMX.Ajax("GET", "/api")
	wasm.HTMX.Ajax("POST", "/api", wasm.AjaxOpts{
		Target: "#out", Source: "#form", Swap: wasm.OuterHTML,
		Values: map[string]string{"a": "b"},
	})
	wasm.HTMX.Swap("#out", "<p>hi</p>")
	wasm.HTMX.Swap("#out", "<p>hi</p>", wasm.BeforeEnd)
	wasm.HTMX.Process("#out")
	wasm.HTMX.Trigger("#out", wasm.EvtAfterSwap)
	wasm.HTMX.Trigger("#out", wasm.EvtTrigger, map[string]any{"n": 1})
	wasm.HTMX.On(wasm.EvtAfterSwap, func(e wasm.Event) { _ = e })
	wasm.HTMX.OnGlobal(wasm.EvtLoad, func(e wasm.Event) { _ = e })
	wasm.HTMX.Off(wasm.EvtAfterSwap, func(e wasm.Event) { _ = e })
	wasm.HTMX.AddClass("#out", "on")
	wasm.HTMX.RemoveClass("#out", "on")
	wasm.HTMX.ToggleClass("#out", "on")
	wasm.HTMX.TakeClass("#out", "on")
	wasm.HTMX.Remove("#out")

	if got := wasm.HTMX.Find("#x"); got != (wasm.JSValue{}) {
		t.Error("Find stub should return zero JSValue")
	}
	if got := wasm.HTMX.FindAll(".x"); got != nil {
		t.Error("FindAll stub should return nil")
	}
	if got := wasm.HTMX.Closest("#x", ".y"); got != (wasm.JSValue{}) {
		t.Error("Closest stub should return zero JSValue")
	}
	if got := wasm.HTMX.Values("#form"); len(got) != 0 {
		t.Error("Values stub should return an empty map")
	}
}
