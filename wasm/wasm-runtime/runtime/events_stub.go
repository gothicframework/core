//go:build !js || !wasm

package runtime

func CreateWasmFunc(name string, fn func())             {}
func CreateWasmStringFunc(name string, fn func(string)) {}
func CreateWasmBoolFunc(name string, fn func(bool))     {}

func CreateWasmFuncWithReturn(name string, fn func(this JSValue, args []JSValue) any) JSValue {
	return JSValue{}
}

// OnUnmount is a no-op on the host build; the real implementation (js && wasm)
// appends the callback to the __gothic_registry[<scope>].__onUnmounts array so
// the bootstrap's per-scope teardown can invoke every registered callback. It
// returns a deregister func (a no-op here) so host builds match the WASM
// signature that drops a callback once it becomes dead weight.
func OnUnmount(fn func()) func() { return func() {} }

// GothicRegisterScope is a no-op on the host build; the real implementation
// (js && wasm) registers the mount scope and publishes a per-instance
// __gothic_register_scope callback for multiplexed pages. Running body directly
// would execute WASM-only runtime calls server-side, so the host stub — like the
// generated main() itself, which is //go:build js && wasm — simply does nothing.
func GothicRegisterScope(body func()) {}

// GothicRegisterSchema is a no-op on the host build. The real implementation
// (js && wasm) deposits the type's compact schema descriptor on
// window.__gothicSchemas under its content-hash id — the Phase 15 schema seam,
// reserved for a future generic interpreter and interpreted by nothing in v3.0.
func GothicRegisterSchema(key, schemaID, descriptor string) {}

// GothicRegisterWithCore is a no-op on the host build. The real implementation
// (js && wasm) performs the component→core registration RPC against the Phase-16
// full-Go static core over the document control-plane bus. Like
// GothicRegisterSchema it is emitted by generated code only, never hand-written
// in a ClientSideState block, so it has no user-facing stub-parity obligation.
func GothicRegisterWithCore(scopeID, schemaID, schema string) {}

// GothicHaltChan returns a nil channel on the host build; the real
// implementation (js && wasm) returns the keep-alive sentinel that main()
// selects on. A nil channel blocks forever, matching the original select{}
// keep-alive semantics for any host-side compile of the generated main().
func GothicHaltChan() <-chan struct{} { return nil }
