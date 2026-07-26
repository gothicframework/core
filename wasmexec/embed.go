package wasmexec

import (
	_ "embed"
)

// Shim is the TinyGo wasm_exec shim served at /_gothic/wasm_exec.js. Slot
// reclamation in the _values ref table belongs entirely to the TinyGo runtime,
// whose syscall/js registers real finalizers (finalizeRef).
//
// There is deliberately only ONE shim. An earlier design embedded a second copy
// carrying manual reclamation (_releaseValue) for toolchains without finalizers
// and chose between them at request time from an environment variable the CLI
// set on the server it launched. Any server started outside that path, a plain
// `go run` against the app or the EMBEDDED single binary, silently got the
// manual copy: two independent reclaimers then managed the same table and
// force-freed slots the runtime still tracked. A single shim removes the pairing
// and with it the possibility of getting the pairing wrong.
//
//go:embed wasm_exec_stock.js
var Shim []byte

// Selected returns the wasm_exec shim bytes to serve.
func Selected() []byte { return Shim }
