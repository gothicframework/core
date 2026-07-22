package wasmexec

import (
	_ "embed"
	"os"
)

// Shim is the DEFAULT TinyGo wasm_exec shim served at /_gothic/wasm_exec.js.
// It carries Gothic's manual GC reclamation (_releaseValue, called from
// _makeFuncWrapper) that force-frees dead js.Value ref-table slots, because the
// bundled TinyGo (0.41.1) ships syscall/js WITHOUT runtime finalizers — without
// this reclamation the _values table grows unbounded under repeated callbacks.
//
//go:embed wasm_exec.js
var Shim []byte

// StockShim is the wasm_exec shim WITHOUT the manual GC reclamation — it is
// Shim minus the _releaseValue method and the _makeFuncWrapper result-block, so
// _makeFuncWrapper reverts to plain stock TinyGo form. It MUST be paired with a
// TinyGo toolchain whose syscall/js provides real finalizers (finalizeRef);
// otherwise js.Value slots leak. With finalizers present, the manual reclamation
// in Shim would DOUBLE-manage the _values table and free a slot the finalizer
// still tracks — a use-after-free ("call to released function"). See the
// maintainer doc cli/docs/patched-tinygo-channel.md.
//
//go:embed wasm_exec_stock.js
var StockShim []byte

// stockSelected is read ONCE at process start. When the environment carries
// GOTHIC_WASM_EXEC=stock the server serves the stock shim; otherwise it serves
// the manual-GC default. The value is a build-time capability decision (the CLI
// pairs a patched-TinyGo pin with the stock runtime and sets this env for the
// server it launches / the Lambda it deploys) — NEVER inferred from a version
// number at runtime. Unset (the default) always means the manual-GC shim, so a
// misconfiguration can only fall back to the safe, leak-free-on-0.41.1 runtime.
var stockSelected = os.Getenv("GOTHIC_WASM_EXEC") == "stock"

// StockSelected reports whether the stock (no manual-GC) shim is the active one
// for this process. Read once at start; see stockSelected.
func StockSelected() bool { return stockSelected }

// Selected returns the wasm_exec shim bytes the runtime should serve for this
// process: the stock variant when GOTHIC_WASM_EXEC=stock, else the manual-GC
// default.
func Selected() []byte {
	if stockSelected {
		return StockShim
	}
	return Shim
}
