// Package corewasm owns the Gothic Framework STATIC CORE artifact: the prebuilt,
// type-agnostic RPC/registration hub compiled with the framework's pinned TinyGo
// fork. See wasm/core-runtime for the core's source and the rationale for a
// single static hub shared by every component on the page.
//
// This package is the emission + versioning seam, mirroring gothiccore
// (which owns gothic-core.js). It embeds two artifacts and emits them to the
// project's public/ directory:
//
//	gothic-core.wasm       the prebuilt core module (from core.wasm, committed)
//	gothic-core-boot.js    a tiny loader that instantiates + runs the core once
//	                       per page, generated here so its content-hash tracks
//	                       the core wasm and the shared exec shim
//
// The exec shim is NOT duplicated here — it is served from wasmexec (the same
// wasm_exec.js per-instance components use), avoiding the earlier 17,732 B
// duplicate download.
//
// It is a leaf package (no internal deps) so BOTH the routes bootstrap layer and
// the wasm build layer can import it without a dependency cycle — same shape as
// gothiccore.
//
// # Regenerating the core artifacts (maintainers only)
//
// The committed core.wasm is rebuilt ONLY when the core's source
// (wasm/core-runtime), one of its dependencies, or the pinned TinyGo version
// changes. Run:
//
//	go generate ./corewasm
//
// The build needs the framework's pinned TinyGo fork on PATH as `tinygo`; set
// GOTHIC_TINYGO to point at a specific binary instead (the CLI caches one under
// ~/.cache/gothic-cli/tinygo/tinygo-<version>/<platform>/tinygo/bin/tinygo). The
// pinned version is the one cli/internal/build.tinyGoVersion names — building
// with a different one produces a different binary and therefore a different
// cache-buster.
//
// The flags match what the CLI compiles per-instance components with, and they
// make the output byte-reproducible: two runs of the same source with the same
// toolchain emit an identical core.wasm. Everything downstream depends on that,
// because the ?v= cache-buster is a content hash of these bytes.
//
// Commit the regenerated files. End users NEVER rebuild these — the CLI ships
// them prebuilt and only COPIES them into public/.
package corewasm

//go:generate sh -c "${DOLLAR}{GOTHIC_TINYGO:-tinygo} build -no-debug -opt=z -target wasm -gc precise -o core.wasm github.com/gothicframework/core/wasm/core-runtime"

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/gothicframework/core/wasmexec"
)

// coreWASM is the prebuilt full-Go core module. Committed as an artifact and
// regenerated via `go generate` (see package doc). Emitted verbatim as
// public/gothic-core.wasm.
//
//go:embed core.wasm
var coreWASM []byte

// The wasm_exec shim the core instantiates through is served from the shared
// /_gothic/wasm_exec.js — the same file per-instance components load. Both use
// the same __gothicGoClasses slot because the slot holds the Go constructor
// function (not an instance), and each party creates its own Go instance via
// new GoCls().

// Emitted public basenames.
const (
	WASMFileName = "gothic-core.wasm"
	BootFileName = "gothic-core-boot.js"
)

// hash16 returns the first 16 hex chars of sha256(b) — the content cache-buster.
func hash16(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// coreHash content-hashes the wasm binary; it becomes the ?v= cache-buster the
// boot loader uses to fetch the module, so a framework upgrade that changes the
// core invalidates the browser cache automatically while an unchanged core stays
// immutably cached. The exec shim is served from wasmexec.Shim and its content
// hash comes from wasmexec.Version().
var coreHash = hash16(coreWASM)

// bootJS is the loader referenced once by the layout <head>. It loads the
// version-matched exec shim into its own __gothicGoClasses slot (coexisting with
// TinyGo components), then instantiates and runs the core exactly once per page.
//
// The wasm download + compile start immediately and run CONCURRENTLY with the exec
// shim's own download, because compiling needs no import object — only the final
// instantiate depends on the shim. Chaining them instead costs a full extra round
// trip before the core can come online.
//
// It fetches the wasm as an ArrayBuffer and uses WebAssembly.compile (rather than
// compileStreaming) so a static host that mis-serves the .wasm Content-Type cannot
// break the boot. The core.wasm + exec URLs carry their own content hashes, so this
// loader's OWN content — and therefore its hash — changes whenever either binary
// changes.
var bootJS = "// gothic-core-boot.js — boots the Gothic full-Go static core once per page.\n" +
	"// Loaded once from the layout <head>. Instantiates gothic-core.wasm through the\n" +
	"// same wasm_exec.js shim that per-instance TinyGo components use — both load the\n" +
	"// Go constructor from the same __gothicGoClasses slot and create independent\n" +
	"// instances via new GoCls(). A __gothicCoreBooting latch guards against a double\n" +
	"// boot; it is CLEARED on exec-load or instantiate failure so a later attempt\n" +
	"// (e.g. an HTMX fragment) can retry rather than wedge the page.\n" +
	"(function(){\n" +
	"    if(window.__gothicCoreBooting)return;\n" +
	"    window.__gothicCoreBooting=1;\n" +
	"    var EXEC='/_gothic/wasm_exec.js?v=" + wasmexec.Version() + "';\n" +
	"    var CORE='/_gothic/" + WASMFileName + "?v=" + coreHash + "';\n" +
	"    var SLOT='wasm_exec.js';\n" +
	"    // Download and compile the module NOW, in parallel with the exec shim below,\n" +
	"    // so the two round trips overlap instead of chaining. Compilation needs no\n" +
	"    // import object, so only the final instantiate has to wait for the shim.\n" +
	"    var modP=fetch(CORE).then(function(resp){return resp.arrayBuffer();})\n" +
	"        .then(function(buf){return WebAssembly.compile(buf);});\n" +
	"    // The real failure path lives in boot(); this only keeps a rejection that\n" +
	"    // lands before boot() attaches from surfacing as an unhandled rejection.\n" +
	"    modP.catch(function(){});\n" +
	"    function boot(){\n" +
	"        if(!window.__gothicGoClasses)window.__gothicGoClasses={};\n" +
	"        var GoCls=window.__gothicGoClasses[SLOT];\n" +
	"        if(!GoCls){window.__gothicCoreBooting=0;try{console.error('gothic: core exec class missing');}catch(_){}return;}\n" +
	"        var go=new GoCls();\n" +
	"        modP.then(function(mod){return WebAssembly.instantiate(mod,go.importObject);})\n" +
	"            .then(function(inst){go.run(inst);})\n" +
	"            .catch(function(e){window.__gothicCoreBooting=0;try{console.error('gothic: core boot failed',e);}catch(_){}});\n" +
	"    }\n" +
	"    if(window.__gothicGoClasses&&window.__gothicGoClasses[SLOT]){boot();return;}\n" +
	"    var prevGo=(typeof Go!=='undefined')?Go:undefined;\n" +
	"    var s=document.createElement('script');\n" +
	"    s.src=EXEC;\n" +
	"    s.onload=function(){\n" +
	"        if(!window.__gothicGoClasses)window.__gothicGoClasses={};\n" +
	"        if(!window.__gothicGoClasses[SLOT])window.__gothicGoClasses[SLOT]=Go;\n" +
	"        if(prevGo!==undefined){try{window.Go=prevGo;}catch(_){}}\n" +
	"        boot();\n" +
	"    };\n" +
	"    s.onerror=function(e){window.__gothicCoreBooting=0;try{console.error('gothic: core exec load failed',e);}catch(_){}};\n" +
	"    document.head.appendChild(s);\n" +
	"})();\n"

// bootHash content-hashes the boot loader. Because bootJS embeds the core wasm
// hash and the exec shim version, bootHash changes whenever the core wasm OR
// the exec shim changes, so the single ?v= the layout carries transitively
// cache-busts all artifacts.
var bootHash = hash16([]byte(bootJS))

// Version returns the boot loader's content hash — the ?v= cache-buster the
// layout <head> references. One hash covers all served artifacts.
func Version() string { return bootHash }

// CoreWASM returns the prebuilt full-Go core module bytes (gothic-core.wasm).
// Exposed so the runtime-asset registry can serve it from the framework embed
// (via the /_gothic/ route) instead of copying it into the project's public/.
func CoreWASM() []byte { return coreWASM }

// BootJS returns the generated boot loader bytes (gothic-core-boot.js). Served
// from /_gothic/.
func BootJS() []byte { return []byte(bootJS) }

// CoreHash returns the content hash of the core wasm binary — the ?v=
// cache-buster the boot loader embeds when fetching it.
func CoreHash() string { return coreHash }

// BootAssetPath is the URL the layout references, including the content-hash
// cache-buster: /_gothic/gothic-core-boot.js?v=<bootHash>. Served from the
// framework embed via the /_gothic/ route (no longer copied into public/).
func BootAssetPath() string { return "/_gothic/" + BootFileName + "?v=" + bootHash }

// WASMAssetPath is the core module URL, byte-identical to the one the boot loader
// fetches. The layout preloads it so the download starts while the HTML is still
// parsing instead of waiting for the boot loader itself to arrive; a preload whose
// URL diverged from the loader's would download the module twice, so both are
// built from the same coreHash.
func WASMAssetPath() string { return "/_gothic/" + WASMFileName + "?v=" + coreHash }

// writeIfChanged writes data to path only when the file is absent or its current
// content differs. This keeps the file's MODIFICATION TIME STABLE across repeated
// emissions with identical content — the property that lets the static core sit
// in the hot-reload emission path without its mtime churning every save cycle.
func writeIfChanged(path string, data []byte) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil // content identical — leave the file (and its mtime) untouched.
	}
	return os.WriteFile(path, data, 0644)
}

// Write emits the two static-core artifacts into publicDir (creating it if
// needed). Called at `gothic init` (to seed the files so the layout reference
// resolves on the first render) and on every build (so existing projects pick up
// a new core when the CLI is upgraded).
//
// Emission is IDEMPOTENT and mtime-stable: each file is rewritten only when its
// content changed. The core is NEVER recompiled here — it is copied from the
// embedded, prebuilt artifact — so it is not part of the GenerateAll per-page
// rebuild set and a hot-reload cycle leaves both files' mtimes unchanged. The
// exec shim (wasm_exec.js) is served from the /_gothic/ embed and is no longer
// emitted here.
func Write(publicDir string) error {
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(publicDir, WASMFileName), coreWASM); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(publicDir, BootFileName), []byte(bootJS)); err != nil {
		return err
	}
	return nil
}
