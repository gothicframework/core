// Package gothiccore owns gothic-core.js: the shared, idempotent client runtime
// globals that used to be inlined into every per-instance WASM bootstrap script.
//
// Phase 15 extracts them into a single static asset so the browser fetches and
// parses them ONCE per page (cached across renders and HTMX fragments) instead
// of re-shipping the ~4 KB preamble inside every component's inline <script>.
// The layout references it once in <head>; the per-instance bootstrap only
// fetches/instantiates/registers its own module and defensively ensures the
// core is present.
//
// It is a leaf package (no internal deps) so BOTH the routes bootstrap layer and
// the wasm build layer can import it without a dependency cycle.
package gothiccore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/tdewolff/minify/v2"
	minifyjs "github.com/tdewolff/minify/v2/js"
)

// FileName is the emitted asset's basename under public/.
const FileName = "gothic-core.js"

// JS is the shared idempotent runtime installed once per page. Every block is
// guarded so N components (or repeated HTMX re-mounts) never redeclare a global.
// This is byte-for-byte the preamble that used to live in the per-instance
// bootstrap (topic buffer pool, async dispatch, findScope, instances registry,
// per-scope teardown, the unmount MutationObserver) PLUS the multiplexing
// structures and the once-installed mux teardown wrapper — with one change: the
// MutationObserver install is deferred to DOMContentLoaded when document.body is
// not yet present (this script runs from <head>, before the body exists).
const JS = `// gothic-core.js — shared idempotent Gothic WASM runtime (Phase 15).
// Loaded once per page from the layout <head>; installs window globals used by
// every per-instance WASM bootstrap. All installs are guarded so it is safe to
// load more than once. Interpreted by nothing but the Gothic runtime.
(function(){
    if(!window.__gothic_topic){
        window.__gothic_topic=(function(){
            var _state={};
            var _subs={};
            var _bufs={};
            var _views={};
            return{
                set:function(keyName,ptrI32,byteLen,inst){
                    var offset=ptrI32>>>0;
                    var src=new Uint8Array(inst.exports.memory.buffer,offset,byteLen);
                    var buf=_bufs[keyName];
                    if(!buf||buf.byteLength<byteLen){
                        var cap=byteLen<128?128:byteLen*2;
                        buf=new ArrayBuffer(cap);
                        _bufs[keyName]=buf;
                        _views[keyName]=null;
                    }
                    var view=_views[keyName];
                    if(!view||view.byteLength!==byteLen){
                        view=new Uint8Array(buf,0,byteLen);
                        _views[keyName]=view;
                    }
                    view.set(src);
                    _state[keyName]=view;
                    var handlers=_subs[keyName];
                    if(handlers){handlers.forEach(function(h){queueMicrotask(function(){h(view);});});}
                },
                // setBytes: store an already-materialized Uint8Array under keyName
                // (Phase 17). The full-Go static core uses this to rebroadcast a
                // per-field topic frame VERBATIM — it has no per-instance __gothic_set
                // slot (that is a TinyGo bootstrap detail), so it hands us bytes via
                // CopyBytesToJS instead of a raw linear-memory pointer. Pooled like set().
                setBytes:function(keyName,u8){
                    var byteLen=u8.byteLength;
                    var buf=_bufs[keyName];
                    if(!buf||buf.byteLength<byteLen){
                        var cap=byteLen<128?128:byteLen*2;
                        buf=new ArrayBuffer(cap);
                        _bufs[keyName]=buf;
                        _views[keyName]=null;
                    }
                    var view=_views[keyName];
                    if(!view||view.byteLength!==byteLen){
                        view=new Uint8Array(buf,0,byteLen);
                        _views[keyName]=view;
                    }
                    view.set(u8);
                    _state[keyName]=view;
                },
                subscribe:function(keyName,fn){(_subs[keyName]=_subs[keyName]||[]).push(fn);},
                get:function(keyName){return _state[keyName]||null;},
                // getInto: zero-copy receive twin of set(). Copies the pooled frame
                // for keyName straight into the CONSUMER's WASM linear memory at a
                // Go-supplied scratch pointer and returns ONLY the length (a number →
                // NaN-boxed, no _values slot). Contrast get(), which returns the
                // pooled Uint8Array VIEW; the pool recreates that view whenever
                // byteLength changes, so boxing it adds a never-finalized TinyGo slot
                // per size change. Signals: -1 = no frame; ~n (bitwise NOT, negative)
                // = scratch too small, grow to n and retry. inst is the consumer
                // instance (its exports.memory is where we write).
                getInto:function(keyName,ptrI32,cap,inst){
                    var v=_state[keyName]; if(!v) return -1;
                    var n=v.byteLength; if(n>cap) return ~n;
                    new Uint8Array(inst.exports.memory.buffer, ptrI32>>>0, n).set(v);
                    return n;
                },
                _unsubscribeScope:function(id){}
            };
        })();
    }
    if(!window.__gothicDispatchAsync){
        // Cache ONE bare CustomEvent per name. Go's syscall/js.handleEvent eagerly
        // boxes the dispatched event into the TinyGo _values ref table when the
        // document listener fires; a fresh new CustomEvent(name) per broadcast is
        // a new object identity that never dedups in _ids → +1 permanent slot per
        // broadcast on TinyGo (no finalizers). Re-dispatching the SAME cached event
        // object dedups it in _ids so no new slot is added. Bare signal event (no
        // detail); the payload is read separately from __gothic_topic. The
        // microtask-defer is PRESERVED (asyncify-safety: dispatch must leave a clean
        // call stack, never fire synchronously inside a running asyncify turn).
        var _evs={};
        window.__gothicDispatchAsync=function(name){
            var e=_evs[name]||(_evs[name]=new CustomEvent(name));
            queueMicrotask(function(){document.dispatchEvent(e);});
        };
    }
    if(!window.__gothicFindScope){
        window.__gothicFindScope=function(){
            var e=window.event;
            if(!e||!e.target)return'';
            var t=e.target;
            if(typeof t.closest!=='function')return'';
            var el=t.closest('[data-gothic-scope]');
            return el?(el.dataset.gothicScope||''):'';
        };
    }
    // __gothicInstallProxy(name): install the global click/input proxy window[name]
    // that HTML onclick/oninput/onchange attributes invoke, as INSTANCE-AGNOSTIC
    // pure JS (Phase 24). It resolves the target callback per invocation off the
    // LIVE window.__gothic_registry (scope→name→js.Func), mirroring the Go
    // dispatch() scope-then-first-match logic. Because it is not owned by any one
    // WASM instance, tearing an instance down (which deletes its
    // __gothic_registry[scope] entry) leaves the proxy intact: a sibling instance's
    // click resolves to the sibling's still-registered, LIVE js.Func instead of a
    // halted instance's dead closure. Idempotent via __gothic_proxied so N
    // components (or HTMX re-mounts) install each name exactly once. Forwards
    // arguments verbatim so string/bool-arg callbacks (oninput/onchange) still get
    // their value.
    if(!window.__gothicInstallProxy){
        window.__gothicInstallProxy=function(name){
            if(!window.__gothic_proxied)window.__gothic_proxied={};
            if(window.__gothic_proxied[name])return;
            window.__gothic_proxied[name]=1;
            window[name]=function(){
                var reg=window.__gothic_registry||{};
                var s=window.__gothicFindScope?window.__gothicFindScope():'';
                var m=s&&reg[s]; var fn=m&&m[name];
                if(fn){return fn.apply(null,arguments);}
                for(var k in reg){if(reg[k]&&reg[k][name])return reg[k][name].apply(null,arguments);}
            };
        };
    }
    // __gothicDurableKey(scopeId): resolve the STABLE durable key a placement
    // declared (Phase 18). Scope ids are random per mount, so durable state is
    // keyed by data-gothic-durable-key instead — read it off the scope's wrapper,
    // falling back to a scoped descendant carrying the attribute (user-supplied in
    // their templ). Returns '' (not durable) when absent. Returning a plain string
    // keeps the TinyGo caller (runtime DurableKey) from boxing a DOM element into a
    // never-finalized _values[] slot, same as __gothicFindScope.
    if(!window.__gothicDurableKey){
        window.__gothicDurableKey=function(scopeId){
            if(!scopeId)return'';
            var el=document.querySelector('[data-gothic-scope="'+scopeId+'"]');
            if(!el)return'';
            if(el.dataset.gothicDurableKey)return el.dataset.gothicDurableKey;
            var inner=el.querySelector('[data-gothic-durable-key]');
            return inner?(inner.dataset.gothicDurableKey||''):'';
        };
    }
    if(!window.__gothicInstances){window.__gothicInstances={};}
    if(!window.__gothicTeardown){
        window.__gothicTeardown=function(id){
            if(!id)return;
            if(!window.__gothicTearingDown)window.__gothicTearingDown={};
            if(window.__gothicTearingDown[id])return;
            window.__gothicTearingDown[id]=1;
            var reg=window.__gothic_registry&&window.__gothic_registry[id];
            if(reg){
                if(reg.__onUnmounts){for(var u=0;u<reg.__onUnmounts.length;u++){var F=reg.__onUnmounts[u];if(F){try{F();}catch(e){}}}}
                if(reg.__listeners){
                    for(var i=0;i<reg.__listeners.length;i++){
                        var L=reg.__listeners[i];
                        if(L)try{document.removeEventListener(L.type,L.fn);}catch(e){}
                    }
                }
            }
            if(window.__gothic_registry)delete window.__gothic_registry[id];
            if(window.__gothic_set)delete window.__gothic_set[id];
            if(window.__gothic_topic&&window.__gothic_topic._unsubscribeScope){try{window.__gothic_topic._unsubscribeScope(id);}catch(e){}}
            var inst=window.__gothicInstances&&window.__gothicInstances[id];
            if(inst){
                if(typeof inst.__halt==='function'){try{inst.__halt();}catch(e){}}
                else if(inst.instance&&inst.instance.exports&&typeof inst.instance.exports.__gothic_halt==='function'){try{inst.instance.exports.__gothic_halt();}catch(e){}}
            }
            if(window.__gothicInstances)delete window.__gothicInstances[id];
            delete window.__gothicTearingDown[id];
        };
    }
    // The unmount observer needs document.body. gothic-core.js is loaded from
    // <head>, so defer the install to DOMContentLoaded when the body is not
    // available yet. Guarded so it installs exactly once per page.
    function _installUnmountObserver(){
        if(window.__gothicUnmountObserver||!document.body)return;
        window.__gothicUnmountObserver=new MutationObserver(function(muts){
            for(var m=0;m<muts.length;m++){
                var removed=muts[m].removedNodes;
                for(var i=0;i<removed.length;i++){
                    var node=removed[i];
                    if(!node||node.nodeType!==1)continue;
                    var scopes=[];
                    if(node.hasAttribute&&node.hasAttribute('data-gothic-scope'))scopes.push(node);
                    if(node.querySelectorAll){
                        var inner=node.querySelectorAll('[data-gothic-scope]');
                        for(var k=0;k<inner.length;k++)scopes.push(inner[k]);
                    }
                    for(var s=0;s<scopes.length;s++){
                        (function(id){queueMicrotask(function(){window.__gothicTeardown(id);});})(scopes[s].dataset.gothicScope);
                    }
                }
            }
        });
        window.__gothicUnmountObserver.observe(document.body,{childList:true,subtree:true});
    }
    if(document.body){_installUnmountObserver();}
    else{document.addEventListener('DOMContentLoaded',_installUnmountObserver);}
    // Multiplexing structures + once-installed teardown wrapper. Installing the
    // wrapper unconditionally is harmless on pages with no multiplexed
    // components: it falls through to the base teardown for any scope that is
    // not registered in __gothicScopeMux.
    if(!window.__gothicMux)window.__gothicMux={};
    if(!window.__gothicScopeMux)window.__gothicScopeMux={};
    if(!window.__gothicMuxTeardownInstalled&&window.__gothicTeardown){
        window.__gothicMuxTeardownInstalled=1;
        var _origMuxTeardown=window.__gothicTeardown;
        window.__gothicTeardown=function(tid){
            var mwn=window.__gothicScopeMux&&window.__gothicScopeMux[tid];
            if(!mwn){_origMuxTeardown(tid);return;}
            var mux=window.__gothicMux&&window.__gothicMux[mwn];
            var keyInst=mux?mux.instId:tid;
            var saved=window.__gothicInstances&&window.__gothicInstances[keyInst];
            var had=!!(window.__gothicInstances&&(keyInst in window.__gothicInstances));
            if(window.__gothicInstances)delete window.__gothicInstances[keyInst];
            _origMuxTeardown(tid);
            delete window.__gothicScopeMux[tid];
            if(mux){
                delete mux.scopes[tid];
                if(Object.keys(mux.scopes).length===0){
                    if(saved&&typeof saved.__halt==='function'){try{saved.__halt();}catch(e){}}
                    delete window.__gothicMux[mwn];
                }else if(had){
                    window.__gothicInstances[keyInst]=saved;
                }
            }
        };
    }
})();
`

// minified is the SERVED form of gothic-core.js: JS run through a real JS
// minifier once at package init. The JS const above stays the readable,
// maintained source of truth; browsers only ever receive the minified bytes, so
// the layout's "Minify JavaScript" audit is clean and the download is smaller
// (on top of the gzip/brotli the /_gothic/ handler applies over the wire). If
// minification ever fails, we fall back to the readable source rather than
// serving nothing.
var minified = func() []byte {
	m := minify.New()
	m.AddFunc("text/javascript", minifyjs.Minify)
	out, err := m.Bytes("text/javascript", []byte(JS))
	if err != nil || len(out) == 0 {
		return []byte(JS)
	}
	return out
}()

// Minified returns the minified gothic-core.js bytes actually served under
// /_gothic/ (and emitted by Write). Its content hash is Version().
func Minified() []byte { return minified }

// hash is the hex sha256 of the MINIFIED bytes, truncated to 16 chars. It is the
// cache-buster query value: it changes whenever the served content changes, so a
// new CLI version invalidates any browser-cached copy while an unchanged core
// stays immutably cached. (Hashing the served bytes — not the readable source —
// keeps the ?v= in lockstep with what the browser actually downloads.)
var hash = func() string {
	sum := sha256.Sum256(minified)
	return hex.EncodeToString(sum[:])[:16]
}()

// Version returns the content hash used as the ?v= cache-buster.
func Version() string { return hash }

// AssetPath is the URL the layout and the per-instance bootstrap load, including
// the content-hash cache-buster: /_gothic/gothic-core.js?v=<hash>. Served from
// the framework embed via the /_gothic/ route (no longer copied into public/).
func AssetPath() string { return "/_gothic/" + FileName + "?v=" + hash }

// Write emits the (minified) gothic-core.js into publicDir (creating it if
// needed). Called at init (to seed the file) and on every build (so existing
// projects pick up a new core when the CLI is upgraded). Idempotent: overwrites
// in place. It writes the same bytes the /_gothic/ handler serves.
func Write(publicDir string) error {
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(publicDir, FileName), minified, 0644)
}
