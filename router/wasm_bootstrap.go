package helpers

import (
	"bytes"
	"fmt"
	"math/rand"
	"strconv"

	"github.com/gothicframework/core/gothiccore"
)

// wasm_bootstrap.go isolates the HTML/JS surface used to bootstrap a WASM module
// for a server-rendered route. It is intentionally kept tiny so it can be tested
// without spinning up the rest of the routes package.
//
// Shared core asset: the idempotent, per-page-once globals
// (__gothic_topic, __gothicDispatchAsync, __gothicFindScope, __gothicInstances,
// __gothicTeardown, the unmount MutationObserver, and the multiplexing structures
// + mux teardown wrapper) NO LONGER live inline in this per-instance script. They
// were extracted into the static gothic-core.js asset (see pkg/helpers/gothiccore),
// which the layout loads once in <head>. The per-instance script now only:
//   1. stamps a unique data-gothic-scope on its wrapper,
//   2. defensively ensures gothic-core.js is present (for the rare HTMX fragment
//      rendered before the layout loaded it), and
//   3. fetches/instantiates/registers its OWN module.
// This shrinks the inline payload from ~4 KB/instance to a few hundred bytes and
// lets the browser cache the shared runtime across renders and fragments.
//
// Duplicate-component disambiguation:
// Two renders of the same component (same wasmName) used to emit the SAME
// data-gothic-wasm attribute. The client-side bootstrap then resolved every
// instance to the FIRST matching wrapper in the DOM. We now stamp each render
// with a unique data-gothic-inst="<hex>" and thread that ID into the bootstrap
// script so the JS selector targets one specific wrapper.

// newInstanceID returns a per-render opaque identifier used to disambiguate
// multiple wrappers that share the same wasmName on the same page.
func newInstanceID() string {
	return strconv.FormatUint(uint64(rand.Uint32()), 16)
}

// injectGothicScope marks the scope boundary for a WASM instance and stamps a
// per-render instance id on the wrapper. The instance id is returned so the
// bootstrap script can be wired to this specific wrapper.
//
// Returns: (modifiedHTML, instanceID).
func injectGothicScope(html []byte, wasmName string) ([]byte, string) {
	return injectGothicScopeDurable(html, wasmName, "")
}

// injectGothicScopeDurable is injectGothicScope with the DURABLE opt-in:
// when durableKey is non-empty it also stamps data-gothic-durable-key on the
// wrapper so a re-mounting component rehydrates its state from the full-Go core
// under that STABLE key (the runtime's DurableKey reads it; DurableObserve keys
// the durable cache by it). durableKey is the caller-declared stable identity —
// e.g. the wasmName for a page singleton, or a Multiplexed row's stable id.
//
// OPT-IN and byte-safe: durableKey=="" (every current caller) produces output
// BYTE-IDENTICAL to before this phase — the attribute is emitted ONLY when a key
// is supplied, so a non-durable component's envelope is unchanged. Users can also
// declare the attribute directly in their own templ markup (the JS resolver
// __gothicDurableKey falls back to a scoped descendant), in which case no server
// stamping is needed at all.
func injectGothicScopeDurable(html []byte, wasmName, durableKey string) ([]byte, string) {
	inst := newInstanceID()
	attr := `data-gothic-wasm="` + wasmName + `" data-gothic-inst="` + inst + `"`
	if durableKey != "" {
		attr += ` data-gothic-durable-key="` + durableKey + `"`
	}
	if bytes.Contains(html, []byte("<body")) {
		return bytes.Replace(html, []byte("<body"), []byte("<body "+attr), 1), inst
	}
	var buf bytes.Buffer
	buf.WriteString(`<div ` + attr + ` style="display:contents">`)
	buf.Write(html)
	buf.WriteString(`</div>`)
	return buf.Bytes(), inst
}

// injectWasmBootstrap injects the WASM loader script. The instance id passed in
// is baked into the JS selector so each render attaches its module to its own
// wrapper, not the first matching wrapper on the page.
func injectWasmBootstrap(html []byte, wasmName string, compression CompressionMethod, compiler WasmCompiler, inst string, multiplexed bool) []byte {
	isFullPage := bytes.Contains(html, []byte("</body>"))

	var findEl string
	if isFullPage {
		findEl = `(document.querySelector('body[data-gothic-wasm="` + wasmName + `"]')||` +
			`(function(){var b=document.body;if(b)b.setAttribute('data-gothic-wasm','` + wasmName + `');return b;})())`
	} else {
		findEl = `(document.currentScript&&document.currentScript.previousElementSibling)` +
			`||document.querySelector('[data-gothic-wasm="` + wasmName + `"][data-gothic-inst="` + inst + `"]')`
	}

	ext := ".wasm.gz"
	if compression == BROTLI {
		ext = ".wasm.br"
	}

	runBody := plainRunBodyFmt
	if multiplexed {
		runBody = muxRunBodyFmt
	}
	script := buildPerInstanceBootstrap(wasmName, findEl, compiler, ext, runBody)

	if isFullPage {
		return bytes.Replace(html, []byte("</body>"), []byte(script+"</body>"), 1)
	}
	return append(html, []byte(script)...)
}

// wasmExecFile returns the correct wasm_exec.js filename for the given compiler.
// This is used only as the __gothicGoClasses SLOT KEY (an identity string), not
// as the fetch URL — see wasmExecPath for that.
func wasmExecFile(compiler WasmCompiler) string {
	if compiler == Golang {
		return "wasm_exec_go.js"
	}
	return "wasm_exec.js"
}

// wasmExecPath returns the URL the bootstrap fetches the wasm_exec shim from.
// The two shims live in DIFFERENT places:
//   - TinyGo's wasm_exec.js is a framework artifact served from the embed via
//     the /_gothic/ route (no longer copied into public/).
//   - The standard-Go wasm_exec_go.js is copied from the USER's GOROOT at build
//     time (version-tied to their toolchain), so it stays in /public/.
func wasmExecPath(compiler WasmCompiler) string {
	if compiler == Golang {
		return "/public/wasm_exec_go.js"
	}
	return "/_gothic/wasm_exec.js"
}

// injectWasmEnvelope is a convenience helper that owns the instance id for one
// render: it stamps the wrapper, then bakes the same id into the bootstrap.
func injectWasmEnvelope(html []byte, wasmName string, compression CompressionMethod, compiler WasmCompiler, multiplexed bool) []byte {
	scoped, inst := injectGothicScope(html, wasmName)
	return injectWasmBootstrap(scoped, wasmName, compression, compiler, inst, multiplexed)
}

// perInstanceHeadFmt opens the per-instance bootstrap: it stamps the scope id and
// defensively ensures gothic-core.js is loaded before running the module body.
// The shared globals live in gothic-core.js (loaded once by the layout); this
// _ensureCore guard only fetches it when a fragment is somehow rendered before
// the layout installed it. Verbs: wn, findEl, gothic-core cache-buster hash.
const perInstanceHeadFmt = `<script>
(function(){
    var wn='%s';
    var el=(%s);
    if(!el)return;
    var id=wn+'-'+(Math.random()*0xFFFFFFFF>>>0).toString(16).padStart(8,'0');
    el.setAttribute('data-gothic-scope',id);
    function _ensureCore(cb){
        if(window.__gothic_topic){cb();return;}
        var ex=document.querySelector('script[data-gothic-core]');
        if(ex){ex.addEventListener('load',cb);ex.addEventListener('error',cb);return;}
        var s=document.createElement('script');
        s.src='/_gothic/gothic-core.js?v=%s';
        s.setAttribute('data-gothic-core','1');
        s.onload=cb;s.onerror=cb;
        document.head.appendChild(s);
    }
    _ensureCore(function(){
`

// perInstanceTailFmt closes the _ensureCore callback and the outer IIFE.
const perInstanceTailFmt = `    });
})();
</script>`

// plainRunBodyFmt is the non-multiplexed run body: one WASM instance per
// placement. Verbs: slot (wasm_exec file name, the __gothicGoClasses key),
// wasm_exec script src (full path, /_gothic/ for TinyGo or /public/ for Go), ext.
const plainRunBodyFmt = `        (async function(){
            var slot='%s';
            if(!window.__gothicGoClasses)window.__gothicGoClasses={};
            if(!window.__gothicGoClasses[slot]){
                var prevGo=(typeof Go!=='undefined')?Go:undefined;
                await new Promise(function(res,rej){
                    var s=document.createElement('script');
                    s.src='%s';
                    s.onload=res;s.onerror=rej;
                    document.head.appendChild(s);
                });
                window.__gothicGoClasses[slot]=Go;
                if(prevGo!==undefined){try{window.Go=prevGo;}catch(_){}}
            }
            var GoCls=window.__gothicGoClasses[slot];
            var go=new GoCls();
            window.__gothicGo=go;
            go.argv=['gothic','GOTHIC_SCOPE='+id];
            var r=await WebAssembly.instantiateStreaming(
                fetch('/public/wasm/'+wn+'%s'),go.importObject
            );
            window.__gothicCurrentModule=id;
            window.__gothic_set=window.__gothic_set||{};
            window.__gothic_set[id]=function(k,p,n){window.__gothic_topic.set(k,p,n,r.instance);};
            if(!window.__gothicTD)window.__gothicTD=new TextDecoder();
            // Full-page (body-rooted) teardown: hx-boost swaps the <body> content
            // but keeps the <body> NODE, so the outgoing full-page scope root is
            // never REMOVED from the DOM and the unmount MutationObserver never
            // fires for it (it only tears down removed [data-gothic-scope] nodes).
            // Track the current full-page scope and tear down the PREVIOUS one when
            // a new full-page instance mounts, so hx-boost navigation stops leaking
            // one live WASM instance per navigation. Only the body case is handled;
            // fragments/components (el is a wrapper div) still tear down via node
            // removal. Guards: no prior on first load; _prev!==id never tears down
            // the new instance; runs before this instance is registered.
            if(el===document.body){
                var _prev=window.__gothicPageScope;
                if(_prev&&_prev!==id&&window.__gothicInstances&&window.__gothicInstances[_prev]){window.__gothicTeardown(_prev);}
                window.__gothicPageScope=id;
            }
            window.__gothicInstances=window.__gothicInstances||{};
            window.__gothicInstances[id]={go:go,instance:r.instance,__setText:function(el,p,n){if(el)el.textContent=window.__gothicTD.decode(new Uint8Array(r.instance.exports.memory.buffer,p>>>0,n));},__getInto:function(key,p,cap){return window.__gothic_topic.getInto(key,p,cap,r.instance);}};
            go.run(r.instance);
        })();
`

// muxRunBodyFmt is the multiplexed run body: ONE shared WASM instance per
// wasmName. The first placement instantiates the binary and, once main() has
// published its __gothic_register_scope callback, flushes any queued scopes.
// Subsequent placements register their scope on the live instance (or queue it
// until ready). The once-installed teardown wrapper that halts the shared
// instance when its LAST scope unregisters now lives in gothic-core.js.
// Verbs: slot (wasm_exec file name, the __gothicGoClasses key), wasm_exec script
// src (full path, /_gothic/ for TinyGo or /public/ for Go), ext.
const muxRunBodyFmt = `        if(!window.__gothicMux)window.__gothicMux={};
        if(!window.__gothicScopeMux)window.__gothicScopeMux={};
        var mux=window.__gothicMux[wn];
        if(mux){
            window.__gothicScopeMux[id]=wn;
            mux.scopes[id]=1;
            if(mux.ready){
                var _slot=window.__gothicInstances[mux.instId];
                var _rs=_slot&&_slot.__gothic_register_scope;
                if(typeof _rs==='function')_rs(id);
            }else{
                mux.pending.push(id);
            }
            return;
        }
        mux=window.__gothicMux[wn]={ready:false,instId:id,pending:[],scopes:{}};
        mux.scopes[id]=1;
        window.__gothicScopeMux[id]=wn;
        (async function(){
            var slot='%s';
            if(!window.__gothicGoClasses)window.__gothicGoClasses={};
            if(!window.__gothicGoClasses[slot]){
                var prevGo=(typeof Go!=='undefined')?Go:undefined;
                await new Promise(function(res,rej){
                    var s=document.createElement('script');
                    s.src='%s';
                    s.onload=res;s.onerror=rej;
                    document.head.appendChild(s);
                });
                window.__gothicGoClasses[slot]=Go;
                if(prevGo!==undefined){try{window.Go=prevGo;}catch(_){}}
            }
            var GoCls=window.__gothicGoClasses[slot];
            var go=new GoCls();
            window.__gothicGo=go;
            go.argv=['gothic','GOTHIC_SCOPE='+id];
            var r=await WebAssembly.instantiateStreaming(
                fetch('/public/wasm/'+wn+'%s'),go.importObject
            );
            window.__gothicCurrentModule=id;
            window.__gothic_set=window.__gothic_set||{};
            window.__gothic_set[id]=function(k,p,n){window.__gothic_topic.set(k,p,n,r.instance);};
            if(!window.__gothicTD)window.__gothicTD=new TextDecoder();
            window.__gothicInstances=window.__gothicInstances||{};
            window.__gothicInstances[id]={go:go,instance:r.instance,__setText:function(el,p,n){if(el)el.textContent=window.__gothicTD.decode(new Uint8Array(r.instance.exports.memory.buffer,p>>>0,n));},__getInto:function(key,p,cap){return window.__gothic_topic.getInto(key,p,cap,r.instance);}};
            go.run(r.instance);
            if(window.__gothicMux[wn]!==mux){
                window.__gothicTeardown(id);
                return;
            }
            mux.ready=true;
            var _slot=window.__gothicInstances[id];
            var _rs=_slot&&_slot.__gothic_register_scope;
            if(typeof _rs==='function'){
                for(var _i=0;_i<mux.pending.length;_i++){_rs(mux.pending[_i]);}
            }
            mux.pending=[];
        })();
`

// buildPerInstanceBootstrap assembles the per-instance <script>: the shared head
// (scope stamp + gothic-core.js ensure) + the given run body + the closing tail.
// Both the plain and multiplexed paths share the head and tail; only the run
// body differs. The gothic-core.js cache-buster hash is threaded into the head so
// the defensive loader fetches the same immutable URL the layout references.
func buildPerInstanceBootstrap(wasmName, findEl string, compiler WasmCompiler, ext, runBodyFmt string) string {
	slotFile := wasmExecFile(compiler) // __gothicGoClasses key (filename identity)
	slotSrc := wasmExecPath(compiler)  // fetch URL (/_gothic/ for TinyGo, /public/ for Go)
	head := fmt.Sprintf(perInstanceHeadFmt, wasmName, findEl, gothiccore.Version())
	body := fmt.Sprintf(runBodyFmt, slotFile, slotSrc, ext)
	return head + body + perInstanceTailFmt
}
