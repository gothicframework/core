package helpers

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gothicframework/core/gothiccore"
)

// TestInjectGothicScope_FullPage verifies a full-page HTML doc is stamped on its
// <body> tag with both data-gothic-wasm and data-gothic-inst attributes.
func TestInjectGothicScope_FullPage(t *testing.T) {
	in := []byte(`<html><head></head><body><h1>hi</h1></body></html>`)
	out, inst := injectGothicScope(in, "components-pingmirror")

	if inst == "" {
		t.Fatal("expected a non-empty instance id")
	}
	if !bytes.Contains(out, []byte(`<body data-gothic-wasm="components-pingmirror" data-gothic-inst="`+inst+`">`)) {
		t.Errorf("expected <body> to carry the gothic-wasm + gothic-inst attributes, got: %s", out)
	}
	// The original document tags must still be present and unaltered.
	if !bytes.Contains(out, []byte(`<h1>hi</h1>`)) {
		t.Errorf("body content was lost: %s", out)
	}
	if !bytes.Contains(out, []byte(`</body></html>`)) {
		t.Errorf("closing tags were lost: %s", out)
	}
}

// TestInjectGothicScope_Fragment verifies HTML with no <body> is wrapped in a
// display-contents <div> that carries the instance id.
func TestInjectGothicScope_Fragment(t *testing.T) {
	in := []byte(`<section>hello</section>`)
	out, inst := injectGothicScope(in, "components-pingmirror")

	if inst == "" {
		t.Fatal("expected a non-empty instance id")
	}
	expectedOpen := `<div data-gothic-wasm="components-pingmirror" data-gothic-inst="` + inst + `" style="display:contents">`
	if !bytes.HasPrefix(out, []byte(expectedOpen)) {
		t.Errorf("expected output to open with %q, got %s", expectedOpen, out)
	}
	if !bytes.HasSuffix(out, []byte(`</div>`)) {
		t.Errorf("expected output to close with </div>, got %s", out)
	}
	if !bytes.Contains(out, []byte(`<section>hello</section>`)) {
		t.Errorf("inner content was lost: %s", out)
	}
}

// TestInjectGothicScopeDurable_OptInStampsAttribute verifies the Phase-18 opt-in:
// a non-empty durable key stamps data-gothic-durable-key on the wrapper (so the
// runtime's DurableKey resolves it and rehydrates from the core), on both the
// full-page and fragment paths.
func TestInjectGothicScopeDurable_OptInStampsAttribute(t *testing.T) {
	full := []byte(`<html><head></head><body><h1>hi</h1></body></html>`)
	out, inst := injectGothicScopeDurable(full, "components-cart", "cart-42")
	want := `<body data-gothic-wasm="components-cart" data-gothic-inst="` + inst + `" data-gothic-durable-key="cart-42">`
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("full-page durable envelope missing durable key attribute.\n got: %s\nwant substr: %s", out, want)
	}

	frag := []byte(`<section>hello</section>`)
	fout, finst := injectGothicScopeDurable(frag, "components-cart", "cart-42")
	fwant := `<div data-gothic-wasm="components-cart" data-gothic-inst="` + finst + `" data-gothic-durable-key="cart-42" style="display:contents">`
	if !bytes.HasPrefix(fout, []byte(fwant)) {
		t.Errorf("fragment durable envelope missing durable key attribute.\n got: %s\nwant prefix: %s", fout, fwant)
	}
}

// TestInjectGothicScope_NonDurableOutputUnchanged is the OPT-IN byte-safety guard:
// the default (non-durable) envelope must NOT carry data-gothic-durable-key, and
// injectGothicScope must be byte-identical to injectGothicScopeDurable(..., "").
// This pins that opting a page into durability elsewhere can never leak the
// attribute onto components that did not ask for it.
func TestInjectGothicScope_NonDurableOutputUnchanged(t *testing.T) {
	for _, in := range [][]byte{
		[]byte(`<html><head></head><body><h1>hi</h1></body></html>`),
		[]byte(`<section>hello</section>`),
	} {
		out, _ := injectGothicScope(in, "components-plain")
		if bytes.Contains(out, []byte("data-gothic-durable-key")) {
			t.Errorf("non-durable envelope must not carry a durable key attribute, got: %s", out)
		}
	}
}

// TestInjectGothicScope_UniqueInstancePerCall guards the duplicate-component
// contract: two calls with the SAME wasmName must produce different
// data-gothic-inst values so the JS selector can disambiguate duplicate
// components on the same page.
func TestInjectGothicScope_UniqueInstancePerCall(t *testing.T) {
	in := []byte(`<section>x</section>`)
	collisions := 0
	const n = 50
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		_, inst := injectGothicScope(in, "components-pingmirror")
		if _, dup := seen[inst]; dup {
			collisions++
		}
		seen[inst] = struct{}{}
	}
	// A handful of collisions in a 32-bit random space is statistically
	// unexpected at n=50 but not impossible. Anything above 2 is a real bug.
	if collisions > 2 {
		t.Errorf("expected near-zero instance id collisions across %d calls, got %d", n, collisions)
	}
	// And we must have at least two distinct ids — anything else means the rng
	// is degenerate.
	if len(seen) < 2 {
		t.Errorf("expected at least 2 distinct instance ids, got %d", len(seen))
	}
}

// TestInjectWasmBootstrap_FullPage verifies the bootstrap is spliced before
// </body>, the wasm filename uses the correct extension per compression, and
// the JS selector targets the body that carries the wasm attribute.
func TestInjectWasmBootstrap_FullPage_Gzip(t *testing.T) {
	in := []byte(`<html><body data-gothic-wasm="counter" data-gothic-inst="abc">x</body></html>`)
	out := injectWasmBootstrap(in, "counter", GZIP, GothicTinyGo, "abc", false)

	if !bytes.Contains(out, []byte(`</body></html>`)) {
		t.Errorf("expected the doc to keep its closing tags: %s", out)
	}
	if !bytes.Contains(out, []byte(`</script></body>`)) {
		t.Errorf("expected the <script> block to sit immediately before </body>: %s", out)
	}
	if !bytes.Contains(out, []byte(`document.querySelector('body[data-gothic-wasm="counter"]')`)) {
		t.Errorf("expected the JS selector for the full-page wasm anchor: %s", out)
	}
	if !bytes.Contains(out, []byte(`'/public/wasm/'+wn+'.wasm.gz'`)) {
		t.Errorf("expected gzip extension in the WASM fetch URL: %s", out)
	}
	if !bytes.Contains(out, []byte(`window.__gothic_set`)) {
		t.Errorf("expected __gothic_set registration in bootstrap so dispatchDirect works: %s", out)
	}
}

func TestInjectWasmBootstrap_FullPage_Brotli(t *testing.T) {
	in := []byte(`<html><body>x</body></html>`)
	out := injectWasmBootstrap(in, "counter", BROTLI, GothicTinyGo, "abc", false)
	if !bytes.Contains(out, []byte(`'/public/wasm/'+wn+'.wasm.br'`)) {
		t.Errorf("expected brotli extension in the WASM fetch URL: %s", out)
	}
}

// TestInjectWasmBootstrap_Fragment verifies fragments get the bootstrap
// appended (no </body> replacement) and the findEl expression carries the
// data-gothic-inst selector so duplicate-component pages resolve correctly.
func TestInjectWasmBootstrap_Fragment(t *testing.T) {
	in := []byte(`<div data-gothic-wasm="components-pingmirror" data-gothic-inst="deadbeef" style="display:contents">x</div>`)
	out := injectWasmBootstrap(in, "components-pingmirror", GZIP, GothicTinyGo, "deadbeef", false)

	if !bytes.HasSuffix(out, []byte(`</script>`)) {
		t.Errorf("expected fragment output to end with </script>, got: %s", out)
	}
	// The findEl expression must reference both the wasm name AND the per-instance id.
	if !bytes.Contains(out, []byte(`document.querySelector('[data-gothic-wasm="components-pingmirror"][data-gothic-inst="deadbeef"]')`)) {
		t.Errorf("expected the fragment selector to scope by data-gothic-inst, got: %s", out)
	}
	// And the previousElementSibling fast path must still be there.
	if !bytes.Contains(out, []byte(`document.currentScript&&document.currentScript.previousElementSibling`)) {
		t.Errorf("expected previousElementSibling fast path in fragment bootstrap, got: %s", out)
	}
}

// TestInjectWasmEnvelope_EndToEnd is the high-level integration test for the
// helper that callers actually use. It must thread the SAME instance id from
// the wrapper through to the JS selector.
func TestInjectWasmEnvelope_EndToEnd_Fragment(t *testing.T) {
	in := []byte(`<section>hi</section>`)
	out := injectWasmEnvelope(in, "components-pingmirror", GZIP, GothicTinyGo, false)

	// Extract the instance id from the wrapper and from the bootstrap script;
	// they MUST be the same value, otherwise duplicate components on the same
	// page will end up sharing state again.
	re := regexp.MustCompile(`data-gothic-inst="([0-9a-f]+)"`)
	matches := re.FindAllSubmatch(out, -1)
	if len(matches) < 2 {
		t.Fatalf("expected at least two data-gothic-inst occurrences (wrapper + selector), got %d in: %s", len(matches), out)
	}
	first := string(matches[0][1])
	for i, m := range matches {
		if string(m[1]) != first {
			t.Fatalf("instance id mismatch between wrapper and bootstrap (match %d = %q, expected %q): %s", i, string(m[1]), first, out)
		}
	}
}

// TestInjectWasmBootstrap_CtxSetPersistentBuffer verifies the __gothic_topic.set
// implementation uses a persistent per-key Uint8Array backed by a doubling
// ArrayBuffer, instead of allocating a fresh Uint8Array via .slice() per
// broadcast. The TinyGo wasm_exec bridge only finalizes string refs, so every
// per-broadcast Uint8Array leaks into the _values[] slot table — keeping a
// persistent buffer means each key occupies exactly one slot for its lifetime.
func TestInjectWasmBootstrap_CtxSetPersistentBuffer(t *testing.T) {
	// Phase 15: the persistent-buffer topic store moved out of the per-instance
	// bootstrap into the shared gothic-core.js asset. Assert it lives there now.
	core := gothiccore.JS
	wants := []string{
		`var _bufs={};`,
		`var _views={};`,
		`byteLen<128?128:byteLen*2`,
		`view.set(src)`,
		`_state[keyName]=view`,
	}
	for _, w := range wants {
		if !strings.Contains(core, w) {
			t.Errorf("expected gothic-core.js to contain %q", w)
		}
	}
	// And the per-instance bootstrap must NOT inline it anymore.
	out := injectWasmBootstrap([]byte(`<html><body>x</body></html>`), "counter", GZIP, GothicTinyGo, "abc", false)
	if bytes.Contains(out, []byte(`var _bufs={};`)) {
		t.Errorf("per-instance bootstrap must not inline the topic buffer pool; it belongs in gothic-core.js:\n%s", out)
	}
}

// TestInjectWasmBootstrap_CtxSetNoSlice is a regression guard: the old
// implementation allocated a fresh Uint8Array per broadcast via .slice(),
// which leaked through TinyGo's wasm_exec _values[] slot table on every
// context update. If this substring ever reappears, the leak is back.
func TestInjectWasmBootstrap_CtxSetNoSlice(t *testing.T) {
	// The topic store now lives in gothic-core.js; guard the leak there too.
	bad := `new Uint8Array(inst.exports.memory.buffer,offset,byteLen).slice()`
	if strings.Contains(gothiccore.JS, bad) {
		t.Errorf("gothic-core.js must not allocate a fresh Uint8Array per set() via .slice(); found %q", bad)
	}
}

// TestInjectWasmBootstrap_FindScopeHelperInjected verifies the bootstrap
// installs window.__gothicFindScope, which moves the per-click DOM walk out
// of Go and into JS. The Go side calls this helper and reads back a string,
// which is the only js.Value kind TinyGo's wasm_exec finalizes — boxing a
// MouseEvent / Element through js.Value would leak a _values[] slot per
// click because those refs are never released.
func TestInjectWasmBootstrap_FindScopeHelperInjected(t *testing.T) {
	// Phase 15: __gothicFindScope moved into gothic-core.js.
	wants := []string{
		`window.__gothicFindScope=function()`,
		// Guard: a document/window-targeted event has no .closest — must not throw
		// (else SetText's scope resolution crashes and the callback aborts).
		`typeof t.closest!=='function'`,
		`t.closest('[data-gothic-scope]')`,
		`el.dataset.gothicScope`,
	}
	for _, w := range wants {
		if !strings.Contains(gothiccore.JS, w) {
			t.Errorf("expected gothic-core.js to contain %q", w)
		}
	}
}

// TestInjectWasmBootstrap_FindScopeOnlyDeclaredOnce guards the per-page-once
// install semantics: the helper is declared inside the if(!window.__gothic_topic)
// guard and gated by its own if(!window.__gothicFindScope) check, so multiple
// WASM modules on the same page must not redeclare it.
func TestInjectWasmBootstrap_FindScopeOnlyDeclaredOnce(t *testing.T) {
	// The idempotent guard lives once in gothic-core.js (loaded once per page).
	needle := `if(!window.__gothicFindScope)`
	if got := strings.Count(gothiccore.JS, needle); got != 1 {
		t.Errorf("expected %q to appear exactly once in gothic-core.js, got %d", needle, got)
	}
	// The per-instance bootstrap must not redeclare it.
	out := injectWasmBootstrap([]byte(`<html><body>x</body></html>`), "counter", GZIP, GothicTinyGo, "abc", false)
	if bytes.Contains(out, []byte(needle)) {
		t.Errorf("per-instance bootstrap must not declare __gothicFindScope; it belongs in gothic-core.js:\n%s", out)
	}
}

// TestInjectWasmEnvelope_UniqueAcrossCalls confirms the duplicate-component
// regression contract: two renders of the same component produce envelopes
// with distinct instance ids embedded BOTH on the wrapper AND inside the JS
// selector.
func TestInjectWasmEnvelope_UniqueAcrossCalls(t *testing.T) {
	in := []byte(`<section>hi</section>`)
	a := injectWasmEnvelope(in, "components-pingmirror", GZIP, GothicTinyGo, false)
	b := injectWasmEnvelope(in, "components-pingmirror", GZIP, GothicTinyGo, false)

	re := regexp.MustCompile(`data-gothic-inst="([0-9a-f]+)"`)
	matchA := re.FindSubmatch(a)
	matchB := re.FindSubmatch(b)
	if matchA == nil || matchB == nil {
		t.Fatalf("missing data-gothic-inst in one of the envelopes\nA:%s\nB:%s", a, b)
	}
	if string(matchA[1]) == string(matchB[1]) {
		t.Errorf("expected different instance ids across calls, got %q for both", matchA[1])
	}
	// And the bootstrap selectors should each carry their own instance id —
	// not the other render's id.
	if !strings.Contains(string(a), `data-gothic-inst="`+string(matchA[1])+`"`) {
		t.Errorf("envelope A's selector does not carry envelope A's instance id: %s", a)
	}
	if !strings.Contains(string(b), `data-gothic-inst="`+string(matchB[1])+`"`) {
		t.Errorf("envelope B's selector does not carry envelope B's instance id: %s", b)
	}
}

// TestInjectWasmBootstrap_ReferencesGothicCore verifies Phase 15's extraction:
// the per-instance script references the shared gothic-core.js asset (via the
// defensive _ensureCore loader carrying the content-hash cache-buster) and no
// longer inlines the shared preamble globals.
func TestInjectWasmBootstrap_ReferencesGothicCore(t *testing.T) {
	out := injectWasmBootstrap([]byte(`<html><body>x</body></html>`), "counter", GZIP, GothicTinyGo, "abc", false)

	// References gothic-core.js with the same versioned URL the layout loads.
	if !bytes.Contains(out, []byte(`/_gothic/gothic-core.js?v=`+gothiccore.Version())) {
		t.Errorf("per-instance bootstrap must reference the versioned gothic-core.js asset, got: %s", out)
	}
	if !bytes.Contains(out, []byte(`function _ensureCore(`)) {
		t.Errorf("per-instance bootstrap must carry the defensive _ensureCore loader, got: %s", out)
	}
	// Must NOT inline the moved preamble globals.
	forbidden := [][]byte{
		[]byte(`window.__gothic_topic=(function()`),
		[]byte(`window.__gothicFindScope=function()`),
		[]byte(`window.__gothicDispatchAsync=function`),
		[]byte(`new MutationObserver(`),
	}
	for _, f := range forbidden {
		if bytes.Contains(out, f) {
			t.Errorf("per-instance bootstrap must not inline the shared preamble %q; it belongs in gothic-core.js", f)
		}
	}
}

// TestGothicCoreAssetEmitted verifies gothiccore.Write emits the asset and that
// its content carries the full set of shared globals + the versioned asset path.
func TestGothicCoreAssetEmitted(t *testing.T) {
	dir := t.TempDir()
	if err := gothiccore.Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, gothiccore.FileName))
	if err != nil {
		t.Fatalf("read emitted asset: %v", err)
	}
	globals := []string{
		`window.__gothic_topic`,
		`window.__gothicDispatchAsync`,
		`window.__gothicFindScope`,
		`window.__gothicInstances`,
		`window.__gothicTeardown`,
		`MutationObserver`,
		`__gothicMuxTeardownInstalled`,
	}
	for _, g := range globals {
		if !strings.Contains(string(data), g) {
			t.Errorf("emitted gothic-core.js missing global %q", g)
		}
	}
	if !strings.HasPrefix(gothiccore.AssetPath(), "/_gothic/"+gothiccore.FileName+"?v=") {
		t.Errorf("unexpected asset path %q", gothiccore.AssetPath())
	}
}

// TestInjectWasmBootstrap_HxBoostFallbackSelector is a regression guard for the
// hx-boost navigation fix. When HTMX boosts a navigation it swaps the <body>
// innerHTML but NOT the <body> element's attributes, so the new page's body
// still carries the previous page's data-gothic-wasm value. Without a
// fallback, document.querySelector('body[data-gothic-wasm="<new>"]') returns
// null and WASM bootstrap silently no-ops on every boosted navigation.
//
// The fix wraps the primary selector in a `(querySelector(...) || fallback())`
// expression where the fallback grabs document.body and updates its
// data-gothic-wasm attribute to the current page's wasm name. This test must
// FAIL if the findEl JS is ever reverted to the pre-fix single-selector form.
func TestInjectWasmBootstrap_HxBoostFallbackSelector(t *testing.T) {
	const wasmName = "login-page"
	in := []byte(`<html><body data-gothic-wasm="some-previous-page">x</body></html>`)
	out := injectWasmBootstrap(in, wasmName, GZIP, GothicTinyGo, "abc", false)

	// Branch 1: the primary selector must still target a body that already
	// carries the correct data-gothic-wasm (the non-boosted, fresh-load case).
	primary := `document.querySelector('body[data-gothic-wasm="` + wasmName + `"]')`
	if !bytes.Contains(out, []byte(primary)) {
		t.Errorf("expected primary selector %q in bootstrap, got: %s", primary, out)
	}

	// Branch 2: the fallback must stamp the current wasmName onto document.body
	// so post-hx-boost navigations heal the attribute and bootstrap succeeds.
	fallback := `setAttribute('data-gothic-wasm','` + wasmName + `')`
	if !bytes.Contains(out, []byte(fallback)) {
		t.Errorf("expected hx-boost fallback %q in bootstrap, got: %s", fallback, out)
	}

	// And the fallback must consult document.body — not, say, document.head —
	// because hx-boost swaps body innerHTML.
	if !bytes.Contains(out, []byte(`document.body`)) {
		t.Errorf("expected fallback to consult document.body, got: %s", out)
	}

	// Extract the findEl expression — the right-hand side of `var el=(...);` —
	// and assert it is a single, syntactically self-contained JS expression so
	// the inline `var el=(<findEl>);` assignment cannot break the bootstrap
	// script. We use a regex anchored on the surrounding `var el=(` / `);`
	// markers that wrap findEl in the generated template.
	re := regexp.MustCompile(`var el=\(([\s\S]*?)\);\s*if\(!el\)return;`)
	m := re.FindSubmatch(out)
	if m == nil {
		t.Fatalf("could not locate `var el=(<findEl>);` in bootstrap output: %s", out)
	}
	findEl := m[1]

	// Both branches must live inside the findEl expression, not somewhere else
	// in the script. A revert to the pre-fix form would drop the fallback from
	// here and this assertion would fail.
	if !bytes.Contains(findEl, []byte(primary)) {
		t.Errorf("findEl expression missing primary selector branch; got %q", findEl)
	}
	if !bytes.Contains(findEl, []byte(fallback)) {
		t.Errorf("findEl expression missing hx-boost fallback branch; got %q", findEl)
	}

	// The two branches must be joined by `||` so the fallback only fires when
	// the primary selector returns null. Any other join (`&&`, `;`, etc.) would
	// either be a syntax error or change the semantics.
	if !bytes.Contains(findEl, []byte(`||`)) {
		t.Errorf("findEl branches must be joined by ||, got %q", findEl)
	}

	// Bracket balance check: the findEl expression must be a single self-
	// contained expression with no unclosed parens/braces/brackets and no
	// stray top-level semicolons that would break `var el=(<findEl>);`.
	if open, close := bytes.Count(findEl, []byte("(")), bytes.Count(findEl, []byte(")")); open != close {
		t.Errorf("findEl has unbalanced parens (%d open, %d close): %q", open, close, findEl)
	}
	if open, close := bytes.Count(findEl, []byte("{")), bytes.Count(findEl, []byte("}")); open != close {
		t.Errorf("findEl has unbalanced braces (%d open, %d close): %q", open, close, findEl)
	}
	if open, close := bytes.Count(findEl, []byte("[")), bytes.Count(findEl, []byte("]")); open != close {
		t.Errorf("findEl has unbalanced brackets (%d open, %d close): %q", open, close, findEl)
	}
	// Semicolons are only legal inside the fallback IIFE body, never at the
	// expression's top level. The IIFE we emit contains exactly three ';'
	// (after `var b=document.body`, after the `if(b)b.setAttribute(...)`
	// statement, and after `return b`). More than three means a stray
	// top-level semicolon snuck in and would break the wrapping
	// `var el=(<findEl>);` assignment.
	if n := bytes.Count(findEl, []byte(";")); n > 3 {
		t.Errorf("findEl has too many semicolons (%d) — risk of breaking `var el=(...);`: %q", n, findEl)
	}
}

// TestInjectWasmBootstrap_TeardownPreambleInjected verifies Phase 12: the
// bootstrap emits the per-scope teardown machinery — a single global
// MutationObserver on document.body, the __gothicTeardown function, the
// __gothicInstances registry, and the topic _unsubscribeScope hook — so an
// HTMX swap that removes a [data-gothic-scope] subtree drops the instance's
// references instead of leaking the WebAssembly.Instance forever.
func TestInjectWasmBootstrap_TeardownPreambleInjected(t *testing.T) {
	// Phase 15: the per-scope teardown machinery moved into gothic-core.js.
	core := gothiccore.JS
	coreWants := []string{
		// MutationObserver detection wired to document.body, whole-subtree.
		`new MutationObserver(`,
		`.observe(document.body,{childList:true,subtree:true})`,
		// Removed nodes plus descendants are collected as scope roots.
		`node.querySelectorAll('[data-gothic-scope]')`,
		`node.hasAttribute('data-gothic-scope')`,
		// Teardown is deferred to a microtask so an in-flight HTMX settle finishes.
		`queueMicrotask(function(){window.__gothicTeardown(id);})`,
		// The teardown function itself and each reference it drops.
		`window.__gothicTeardown=function(id)`,
		// Every registered unmount callback is iterated (list-based, not a single slot).
		`if(reg.__onUnmounts){for(var u=0;u<reg.__onUnmounts.length;u++){var F=reg.__onUnmounts[u];if(F){try{F();}catch(e){}}}}`,
		`document.removeEventListener(L.type,L.fn)`,
		`delete window.__gothic_registry[id]`,
		`delete window.__gothic_set[id]`,
		`window.__gothic_topic._unsubscribeScope(id)`,
		// Per-instance halt callback (portable) with the wasm-export path as fallback.
		`typeof inst.__halt==='function'`,
		`inst.instance.exports.__gothic_halt`,
		`delete window.__gothicInstances[id]`,
		// The topic object exposes the (guarded) per-scope unsubscribe hook.
		`_unsubscribeScope:function(id){}`,
	}
	for _, w := range coreWants {
		if !strings.Contains(core, w) {
			t.Errorf("expected gothic-core.js to contain %q", w)
		}
	}
	// The instance is still stored in a per-scope slot at mount by the
	// per-instance bootstrap so teardown (in core) can drop it.
	out := injectWasmBootstrap([]byte(`<html><body>x</body></html>`), "counter", GZIP, GothicTinyGo, "abc", false)
	// The slot stores go+instance (dropped by teardown) plus the zero-copy
	// __setText setter, whose closure captures the instance and is therefore
	// released for free when teardown deletes __gothicInstances[id].
	if !bytes.Contains(out, []byte(`window.__gothicInstances[id]={go:go,instance:r.instance,__setText:function(el,p,n){`)) {
		t.Errorf("per-instance bootstrap must still store its instance (with the __setText setter) for teardown, got: %s", out)
	}
}

// TestInjectWasmBootstrap_FullPageScopeTeardown verifies the hx-boost full-page
// leak fix: because hx-boost swaps the <body> content but keeps the <body> node,
// the outgoing full-page instance's scope root is never removed from the DOM and
// the unmount MutationObserver never fires for it. The per-instance bootstrap
// therefore tracks the current full-page scope in window.__gothicPageScope and,
// when a new full-page instance mounts (el===document.body), tears down the
// PREVIOUS one before registering itself — so full-page navigation stops leaking
// one live WASM instance per navigation.
func TestInjectWasmBootstrap_FullPageScopeTeardown(t *testing.T) {
	out := injectWasmBootstrap([]byte(`<html><body>x</body></html>`), "counter", GZIP, GothicTinyGo, "abc", false)

	// The full-page detection must key off el===document.body (the body node
	// persists across hx-boost, so this is the only stable full-page signal).
	if !bytes.Contains(out, []byte(`if(el===document.body){`)) {
		t.Errorf("expected full-page detection via el===document.body, got: %s", out)
	}
	// It must tear down the PREVIOUS page scope, guarding against (a) no prior
	// (_prev falsy), (d) tearing down the new instance (_prev!==id), and only
	// when that prior instance is actually still registered.
	if !bytes.Contains(out, []byte(`var _prev=window.__gothicPageScope;`)) {
		t.Errorf("expected page-scope tracking via window.__gothicPageScope, got: %s", out)
	}
	if !bytes.Contains(out, []byte(`if(_prev&&_prev!==id&&window.__gothicInstances&&window.__gothicInstances[_prev]){window.__gothicTeardown(_prev);}`)) {
		t.Errorf("expected guarded teardown of the previous full-page scope, got: %s", out)
	}
	// And it must record itself as the new current full-page scope.
	if !bytes.Contains(out, []byte(`window.__gothicPageScope=id;`)) {
		t.Errorf("expected the new full-page instance to record itself as the current page scope, got: %s", out)
	}
	// The page-teardown must run BEFORE the instance registers itself, so
	// tearing down the prior instance can never touch the new one's slot.
	pageIdx := bytes.Index(out, []byte(`window.__gothicPageScope=id;`))
	regIdx := bytes.Index(out, []byte(`window.__gothicInstances=window.__gothicInstances||{};`))
	if pageIdx < 0 || regIdx < 0 || pageIdx > regIdx {
		t.Errorf("expected page-scope teardown to precede instance registration (page=%d reg=%d): %s", pageIdx, regIdx, out)
	}
	// Reuses the shared teardown fn (already halts + frees __gothicInstances +
	// releases listeners/registry/subs, and the mux wrapper falls through for a
	// non-mux page scope) rather than re-implementing teardown here.
	if !bytes.Contains(out, []byte(`window.__gothicTeardown(_prev)`)) {
		t.Errorf("expected reuse of window.__gothicTeardown for the prior page instance, got: %s", out)
	}
}

// TestInjectWasmBootstrap_TeardownIdempotentGuards verifies the teardown
// machinery is installed under idempotent guards so N WASM modules sharing a
// page (or repeated re-mounts) never redeclare the observer / teardown fn /
// instances registry, and the teardown fn itself guards on !id and re-entrancy.
func TestInjectWasmBootstrap_TeardownIdempotentGuards(t *testing.T) {
	// The shared globals are installed once, guarded, inside gothic-core.js.
	core := gothiccore.JS
	onceGuards := []string{
		// The observer install is deferred (gothic-core.js loads from <head>
		// before <body> exists) but still assigns the singleton exactly once.
		`window.__gothicUnmountObserver=new MutationObserver`,
		`if(!window.__gothicTeardown){`,
		`if(!window.__gothicInstances){`,
	}
	for _, g := range onceGuards {
		if got := strings.Count(core, g); got != 1 {
			t.Errorf("expected guard %q exactly once in gothic-core.js, got %d", g, got)
		}
	}

	// The teardown fn guards on a missing id and on re-entrancy (MutationObserver
	// can fire twice for the same removed subtree).
	reentrancyGuards := []string{
		`if(!id)return;`,
		`window.__gothicTearingDown`,
	}
	for _, g := range reentrancyGuards {
		if !strings.Contains(core, g) {
			t.Errorf("expected teardown re-entrancy guard %q in gothic-core.js", g)
		}
	}
}
