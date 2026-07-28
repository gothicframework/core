package wasmexec

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// FileName is the basename this shim is served under, below /_gothic/.
const FileName = "wasm_exec.js"

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

// hash is the first 16 hex chars of sha256(Shim) — the same 16-char content
// digest gothiccore and corewasm use for their own assets.
var hash = func() string {
	sum := sha256.Sum256(Shim)
	return hex.EncodeToString(sum[:])[:16]
}()

// Version returns the shim's content hash: the ?v= cache-buster its URL carries.
//
// This asset is served immutably for a year outside dev, so the ONLY thing that
// lets a toolchain bump reach a returning browser is the URL changing with the
// bytes. A bare /_gothic/wasm_exec.js would pin whatever shim a visitor happened
// to cache, against a core and components that moved on — the shim/runtime
// mismatch this package's doc describes, except unreachable by any redeploy.
func Version() string { return hash }

// AssetPath is the URL the per-instance bootstrap fetches, cache-buster included.
func AssetPath() string { return "/_gothic/" + FileName + "?v=" + hash }
