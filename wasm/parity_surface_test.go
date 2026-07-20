//go:build !js || !wasm

package wasm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestUserFacingStubParity guards the exact gap that let OnUnmount slip through:
// a user-facing exported function that exists in the WASM runtime
// (pkg/wasm/wasm-runtime/runtime) but is MISSING from this package's server-side
// stubs (stubs.go). Users dot-import this package so their ClientSideState block
// compiles for SSR — every symbol a user may hand-write in that block MUST have
// a no-op here or the server build breaks.
//
// This is a curated list on purpose: only symbols a human writes in
// ClientSideState belong here — NOT the Broadcast*/Listen*/Request* topic
// helpers (emitted by generated WASM code, never compiled server-side) nor
// runtime internals. When you add a NEW user-facing exported func to the
// runtime, add its name below AND add a matching no-op to stubs.go; this test
// fails until you do.
func TestUserFacingStubParity(t *testing.T) {
	mustProvide := []string{
		// reactive state & effects
		"CreateObservable", "Observe", "ObserveWithCleanup", "NewObservableField",
		// durable state cache
		"DurableObserve", "DurableKey",
		// lifecycle
		"OnUnmount",
		// scope carrier
		"CaptureScope", "RunInScope",
		// named DOM helpers
		"SetText", "SetHTML", "SetValue", "GetValue", "GetFileBytes",
		"AddClass", "RemoveClass", "ToggleClass", "SetAttr", "SetStyle",
		// event registration
		"CreateWasmFunc", "CreateWasmStringFunc", "CreateWasmBoolFunc",
		"CreateWasmFuncWithReturn", "AddEventListener", "AddEventListenerWithEvent",
		// HTTP
		"Fetch", "FetchAsync", "FetchChan",
		// typed JSON decode/encode — rewritten to generated
		// _jsonDecode_T / _jsonEncode_T in WASM
		"Decode", "Encode",
		// storage / cookies
		"LocalStorageSet", "LocalStorageGet", "LocalStorageRemove",
		"SessionStorageSet", "SessionStorageGet", "SessionStorageRemove",
		"CookieSet", "CookieGet", "CookieDelete",
		// low-level JS / DOM tree / navigation
		"JS", "Window", "Document", "ConsoleLog", "ExecJS",
		"GetElementById", "CreateElement", "QuerySelector", "QuerySelectorAll",
		"AppendChild", "RemoveElement", "ClickElement", "WriteClipboard",
		"TriggerDownload", "CopyBytesToJS", "CopyBytesToGo",
		"Navigate", "Reload", "PushState", "GoBack",
		// topics (user-declared)
		"CreateTopic", "BinaryKey", "AutoKey",
	}

	provided := exportedFuncs(t, "stubs.go")
	for _, name := range mustProvide {
		if !provided[name] {
			t.Errorf("stubs.go is missing user-facing stub %q — a ClientSideState block using it would fail to compile server-side. Add a no-op func %s(...) to stubs.go.", name, name)
		}
	}
}

// exportedFuncs parses a Go source file and returns the set of its exported,
// top-level (non-method) function names.
func exportedFuncs(t *testing.T, path string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	set := map[string]bool{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil { // skip methods
			continue
		}
		if fn.Name.IsExported() {
			set[fn.Name.Name] = true
		}
	}
	return set
}
