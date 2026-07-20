package runtime

// Per-invocation active-scope resolution.
//
// This replaces the old package-global single scope (cachedModuleID /
// moduleID) with a resolver that lets ONE WASM instance own more than one
// [data-gothic-scope] container. The pure-Go part of the machinery lives here,
// with NO build tag, so it compiles into both build worlds:
//
//   - js && wasm  — events.go points findScopeFn at the __gothicFindScope DOM
//     walk and sets bootstrapScopeID from GOTHIC_SCOPE at init.
//   - !js || !wasm (host build/tests) — findScopeFn stays the "" stub and
//     bootstrapScopeID stays "_default", so DOM helpers behave document-wide and
//     the resolver is unit-testable without syscall/js.
//
// Backward-compatibility bar: a single-scope instance must resolve exactly the
// scope moduleID() used to return, on every path. See activeScope for the proof.

// currentScope / currentScopeActive form the explicit active-scope carrier for
// programmatic and async paths where there is no live user event (topic
// broadcasts, timers, goroutines). currentScopeActive distinguishes "no scope
// set" from an explicit empty/"_default" scope, since "" is a legal fallback.
// They are mutated only through runInScope, which saves and restores them.
var (
	currentScope       string
	currentScopeActive bool
)

// bootstrapScopeID is the default/fallback scope — the mount scope captured
// once at init on js&&wasm (the old cachedModuleID). It is the scope the
// instance's bootstrap keyed __gothic_set / __gothic_registry / __gothicInstances
// under, so the halt slot and the dispatch shim must resolve to it.
// Defaults to "_default" so host builds and un-bootstrapped runtimes behave
// document-wide, unchanged from before.
var bootstrapScopeID = "_default"

// findScopeFn resolves the scope from the current synchronous user event by
// walking window.event.target up to the nearest [data-gothic-scope]. On host
// builds it is the "" stub; events.go rebinds it to findScope (the JS DOM walk)
// at init. It is a var so unit tests can inject a fake resolver.
var findScopeFn = func() string { return "" }

// bootstrapScope returns the mount/default scope. Callers that must address the
// instance itself (the halt slot) use this rather than activeScope so they never
// resolve dynamically to a non-bootstrap scope.
func bootstrapScope() string { return bootstrapScopeID }

// runInScope sets currentScope to id for the duration of fn, then restores the
// previous carrier state. Save/restore (not clear) makes it nest correctly.
//
// IMPORTANT: the carrier is a single package global and runInScope restores it
// only when fn RETURNS, never at a suspension point. fn must therefore NOT block
// on a channel receive (or any async yield) while relying on the carrier: a
// parked goroutine that still holds a scope leaks it onto whatever goroutine
// runs while it is parked, and cannot trust the global on resume (another
// goroutine may have set its own scope and parked in turn). Blocking runtime
// helpers (Fetch/GetFileBytes) bracket their receive with parkScope
// so the carrier is never held across a suspension. Keep blocking calls OUTSIDE
// runInScope/RunInScope, mirroring PingUntilOnline's time.Sleep.
func runInScope(id string, fn func()) {
	prevScope, prevActive := currentScope, currentScopeActive
	currentScope, currentScopeActive = id, true
	defer func() {
		currentScope, currentScopeActive = prevScope, prevActive
	}()
	fn()
}

// parkScope snapshots and CLEARS the active-scope carrier immediately before a
// goroutine suspends on a blocking channel receive, returning a restore func to
// call immediately after it resumes:
//
//	restore := parkScope()
//	r := <-ch
//	restore()
//
// The carrier is a single package global; a scope must only ever be set while a
// goroutine is actively on the call stack, never while it is parked. Clearing
// before <-ch stops this goroutine from leaking its scope onto whoever runs
// while it is parked; restoring from the local (not the global) after resume
// stops it from trusting a value another goroutine may have clobbered.
func parkScope() func() {
	savedScope, savedActive := currentScope, currentScopeActive
	currentScope, currentScopeActive = "", false
	return func() {
		currentScope, currentScopeActive = savedScope, savedActive
	}
}

// activeScope resolves the scope for the current invocation:
//
//  1. An explicit scope set by runInScope (async/programmatic paths,
//     multiplexing) wins unconditionally.
//  2. Otherwise, if we are inside a synchronous user-event dispatch,
//     findScopeFn() returns the event's [data-gothic-scope] and that wins.
//  3. Otherwise fall back to the mount/bootstrap scope.
//
// Single-scope proof: an instance owns exactly one [data-gothic-scope], whose id
// equals bootstrapScopeID. During any of its user events findScopeFn() returns
// that id (case 2 == bootstrapScopeID); in async/no-event turns findScopeFn()
// returns "" and case 3 returns bootstrapScopeID. Either way the result is the
// single scope — byte-identical to the old moduleID() field read.
func activeScope() string {
	if currentScopeActive {
		return currentScope
	}
	if s := findScopeFn(); s != "" {
		return s
	}
	return bootstrapScopeID
}

// CaptureScope returns the scope active at the call site. Capture it before
// spawning a goroutine or scheduling async work whose body touches scoped DOM
// helpers, then re-establish it with RunInScope when that work runs — a
// goroutine does not inherit the carrier across a suspension point.
//
// Keep any blocking call (a fetch, a channel receive) OUTSIDE RunInScope and
// wrap only the scoped, non-blocking work — mirroring how PingUntilOnline keeps
// time.Sleep outside RunInScope:
//
//	scope := CaptureScope()
//	go func() {
//	    body, _ := Fetch(url)                       // blocks — outside RunInScope
//	    RunInScope(scope, func() { SetText("out", body) })  // scoped, non-blocking
//	}()
func CaptureScope() string { return activeScope() }

// RunInScope runs fn with the given scope active, restoring the previous scope
// afterwards. Pair it with CaptureScope to carry a scope into a goroutine or
// deferred callback. Nesting is supported.
func RunInScope(id string, fn func()) { runInScope(id, fn) }
