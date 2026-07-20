// Package corewasm owns the Gothic Framework full-Go STATIC CORE artifact:
// the prebuilt, type-agnostic RPC/registration hub compiled with the
// standard Go toolchain (GOOS=js GOARCH=wasm) so it has the full standard library
// TinyGo lacks. See pkg/wasm/core-runtime for the core's source and the rationale
// for full-Go + static.
//
// This package is the emission + versioning seam, mirroring pkg/helpers/gothiccore
// (which owns gothic-core.js). It embeds three artifacts and emits them to the
// project's public/ directory:
//
//	gothic-core.wasm       the prebuilt core module (from core.wasm, committed)
//	gothic-core-exec.js    the standard-Go wasm_exec shim, VERSION-MATCHED to the
//	                       Go toolchain that built core.wasm (committed)
//	gothic-core-boot.js    a tiny loader that instantiates + runs the core once
//	                       per page, generated here so its content-hash tracks the
//	                       core and exec hashes
//
// It is a leaf package (no internal deps) so BOTH the routes bootstrap layer and
// the wasm build layer can import it without a dependency cycle — same shape as
// gothiccore.
//
// # Regenerating the core artifacts (maintainers only)
//
// The committed core.wasm + gothic-core-exec.js are rebuilt ONLY when the core's
// source (wasm/core-runtime) or the pinned Go toolchain changes. Run:
//
//	go generate ./corewasm
//
// which runs the two directives below (build the wasm, copy the matching
// wasm_exec.js). Commit the regenerated files. End users NEVER rebuild these —
// the CLI ships them prebuilt and only COPIES them into public/.
package corewasm

//go:generate sh -c "GOOS=js GOARCH=wasm go build -trimpath -ldflags=\"-s -w\" -o core.wasm github.com/gothicframework/core/wasm/core-runtime"
//go:generate sh -c "cp \"$(go env GOROOT)/lib/wasm/wasm_exec.js\" gothic-core-exec.js"

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"os"
	"path/filepath"
)

// coreWASM is the prebuilt full-Go core module. Committed as an artifact and
// regenerated via `go generate` (see package doc). Emitted verbatim as
// public/gothic-core.wasm.
//
//go:embed core.wasm
var coreWASM []byte

// execJS is the standard-Go wasm_exec shim, version-matched to the toolchain
// that built core.wasm (regenerated alongside it). Emitted as
// public/gothic-core-exec.js. It is loaded under its OWN __gothicGoClasses slot
// so its standard-Go `Go` constructor never collides with the TinyGo one used by
// per-instance components.
//
//go:embed gothic-core-exec.js
var execJS []byte

// Emitted public basenames.
const (
	WASMFileName = "gothic-core.wasm"
	ExecFileName = "gothic-core-exec.js"
	BootFileName = "gothic-core-boot.js"
)

// hash16 returns the first 16 hex chars of sha256(b) — the content cache-buster.
func hash16(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// coreHash / execHash content-hash the two binary artifacts; they become the
// ?v= cache-busters the boot loader uses to fetch them, so a framework upgrade
// that changes either binary invalidates the browser cache automatically while
// an unchanged core stays immutably cached.
var coreHash = hash16(coreWASM)
var execHash = hash16(execJS)

// bootJS is the loader referenced once by the layout <head>. It loads the
// version-matched exec shim into its own __gothicGoClasses slot (coexisting with
// TinyGo components), then instantiates and runs the core exactly once per page.
//
// It fetches the wasm as an ArrayBuffer and uses WebAssembly.instantiate (rather
// than instantiateStreaming) so a static host that mis-serves the .wasm
// Content-Type cannot break the boot. The core.wasm + exec URLs carry their own
// content hashes, so this loader's OWN content — and therefore its hash — changes
// whenever either binary changes.
var bootJS = "// gothic-core-boot.js — boots the Gothic full-Go static core once per page.\n" +
	"// Loaded once from the layout <head>. Instantiates public/gothic-core.wasm through\n" +
	"// its own wasm_exec slot so the standard-Go `Go` constructor coexists with TinyGo\n" +
	"// components. A __gothicCoreBooting latch guards against a double boot; it is CLEARED\n" +
	"// on exec-load or instantiate failure so a later attempt (e.g. an HTMX fragment) can\n" +
	"// retry rather than wedge the page. Interpreted by nothing but the browser.\n" +
	"(function(){\n" +
	"    if(window.__gothicCoreBooting)return;\n" +
	"    window.__gothicCoreBooting=1;\n" +
	"    var EXEC='/_gothic/" + ExecFileName + "?v=" + execHash + "';\n" +
	"    var CORE='/_gothic/" + WASMFileName + "?v=" + coreHash + "';\n" +
	"    var SLOT='" + ExecFileName + "';\n" +
	"    function boot(){\n" +
	"        if(!window.__gothicGoClasses)window.__gothicGoClasses={};\n" +
	"        var GoCls=window.__gothicGoClasses[SLOT];\n" +
	"        if(!GoCls){window.__gothicCoreBooting=0;try{console.error('gothic: core exec class missing');}catch(_){}return;}\n" +
	"        var go=new GoCls();\n" +
	"        fetch(CORE).then(function(resp){return resp.arrayBuffer();})\n" +
	"            .then(function(buf){return WebAssembly.instantiate(buf,go.importObject);})\n" +
	"            .then(function(res){go.run(res.instance);})\n" +
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

// bootHash content-hashes the boot loader. Because bootJS embeds coreHash and
// execHash, bootHash changes whenever the core or the exec shim changes, so the
// single ?v= the layout carries transitively cache-busts all three artifacts.
var bootHash = hash16([]byte(bootJS))

// Version returns the boot loader's content hash — the ?v= cache-buster the
// layout <head> references. One hash covers all three artifacts (see bootHash).
func Version() string { return bootHash }

// CoreWASM returns the prebuilt full-Go core module bytes (gothic-core.wasm).
// Exposed so the runtime-asset registry can serve it from the framework embed
// (via the /_gothic/ route) instead of copying it into the project's public/.
func CoreWASM() []byte { return coreWASM }

// ExecJS returns the standard-Go wasm_exec shim bytes (gothic-core-exec.js),
// version-matched to the toolchain that built core.wasm. Served from /_gothic/.
func ExecJS() []byte { return execJS }

// BootJS returns the generated boot loader bytes (gothic-core-boot.js). Served
// from /_gothic/.
func BootJS() []byte { return []byte(bootJS) }

// CoreHash / ExecHash return the content hashes of the two binary artifacts —
// the ?v= cache-busters the boot loader embeds when fetching them.
func CoreHash() string { return coreHash }
func ExecHash() string { return execHash }

// BootAssetPath is the URL the layout references, including the content-hash
// cache-buster: /_gothic/gothic-core-boot.js?v=<bootHash>. Served from the
// framework embed via the /_gothic/ route (no longer copied into public/).
func BootAssetPath() string { return "/_gothic/" + BootFileName + "?v=" + bootHash }

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

// Write emits the three static-core artifacts into publicDir (creating it if
// needed). Called at `gothic init` (to seed the files so the layout reference
// resolves on the first render) and on every build (so existing projects pick up
// a new core when the CLI is upgraded).
//
// Emission is IDEMPOTENT and mtime-stable: each file is rewritten only when its
// content changed. The core is NEVER recompiled here — it is copied from the
// embedded, prebuilt artifact — so it is not part of the GenerateAll per-page
// rebuild set and a hot-reload cycle leaves all three files' mtimes unchanged.
func Write(publicDir string) error {
	if err := os.MkdirAll(publicDir, 0755); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(publicDir, WASMFileName), coreWASM); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(publicDir, ExecFileName), execJS); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(publicDir, BootFileName), []byte(bootJS)); err != nil {
		return err
	}
	return nil
}
