//go:build !js || !wasm

package runtime

import "testing"

// scope.go carries no build tag, so its resolver compiles here on the host and
// these tests exercise the pure-Go parts directly. The two-scope dispatch-routing
// assertion (one WASM instance owning two [data-gothic-scope] containers, events
// routed to the correct scope) is only observable under js && wasm in a browser
// and is authored as a Playwright spec on TestGothic.

// saveScopeGlobals snapshots the package-level scope carrier so a test can mutate
// it freely and restore it, keeping tests independent.
func saveScopeGlobals(t *testing.T) {
	t.Helper()
	s, a, f, b := currentScope, currentScopeActive, findScopeFn, bootstrapScopeID
	t.Cleanup(func() {
		currentScope, currentScopeActive, findScopeFn, bootstrapScopeID = s, a, f, b
	})
}

func TestRunInScopeSaveRestore(t *testing.T) {
	saveScopeGlobals(t)
	currentScope, currentScopeActive = "", false

	ran := false
	runInScope("A", func() {
		ran = true
		if !currentScopeActive || currentScope != "A" {
			t.Errorf("inside runInScope: active=%v scope=%q, want true/%q", currentScopeActive, currentScope, "A")
		}
	})
	if !ran {
		t.Fatal("runInScope did not run fn")
	}
	if currentScopeActive || currentScope != "" {
		t.Errorf("after runInScope: active=%v scope=%q, want false/empty", currentScopeActive, currentScope)
	}
}

// TestRunInScopeRestoresPrevExplicit proves save/restore restores an ALREADY
// active outer scope (not just the empty state) — the nesting contract
// multiplexing relies on.
func TestRunInScopeRestoresPrevExplicit(t *testing.T) {
	saveScopeGlobals(t)
	currentScope, currentScopeActive = "", false

	runInScope("outer", func() {
		if currentScope != "outer" || !currentScopeActive {
			t.Fatalf("outer: scope=%q active=%v", currentScope, currentScopeActive)
		}
		runInScope("inner", func() {
			if currentScope != "inner" {
				t.Errorf("inner: scope=%q, want inner", currentScope)
			}
		})
		if currentScope != "outer" || !currentScopeActive {
			t.Errorf("after inner restore: scope=%q active=%v, want outer/true", currentScope, currentScopeActive)
		}
	})
	if currentScopeActive {
		t.Errorf("after outer: active=%v, want false", currentScopeActive)
	}
}

func TestActiveScopeResolutionOrder(t *testing.T) {
	saveScopeGlobals(t)
	bootstrapScopeID = "boot"

	// 1. explicit scope (runInScope) wins even when an event is present.
	findScopeFn = func() string { return "event" }
	runInScope("explicit", func() {
		if got := activeScope(); got != "explicit" {
			t.Errorf("explicit precedence: activeScope()=%q, want explicit", got)
		}
	})

	// 2. no explicit scope → the current event's scope wins over bootstrap.
	currentScope, currentScopeActive = "", false
	findScopeFn = func() string { return "event" }
	if got := activeScope(); got != "event" {
		t.Errorf("event precedence: activeScope()=%q, want event", got)
	}

	// 3. no explicit scope, no event → bootstrap fallback.
	findScopeFn = func() string { return "" }
	if got := activeScope(); got != "boot" {
		t.Errorf("bootstrap fallback: activeScope()=%q, want boot", got)
	}
}

// TestActiveScopeSingleScopeIdentical pins the backward-compat bar: for an
// instance that owns exactly one scope, activeScope() yields that one scope on
// BOTH the event path and the async path — exactly what moduleID() returned.
func TestActiveScopeSingleScopeIdentical(t *testing.T) {
	saveScopeGlobals(t)
	currentScope, currentScopeActive = "", false
	bootstrapScopeID = "only"

	// During one of the instance's events, findScope() sees its sole scope.
	findScopeFn = func() string { return "only" }
	if got := activeScope(); got != "only" {
		t.Errorf("event path: activeScope()=%q, want only", got)
	}
	// In an async/no-event turn, findScope()=="" and bootstrap == same scope.
	findScopeFn = func() string { return "" }
	if got := activeScope(); got != "only" {
		t.Errorf("async path: activeScope()=%q, want only", got)
	}
}

// TestCaptureAndRunInScope models the goroutine hand-off: capture the scope at
// spawn, lose it in an async turn, re-establish it with the public helpers.
func TestCaptureAndRunInScope(t *testing.T) {
	saveScopeGlobals(t)
	currentScope, currentScopeActive = "", false
	bootstrapScopeID = "_default"
	findScopeFn = func() string { return "" }

	var captured string
	runInScope("spawner", func() { captured = CaptureScope() })
	if captured != "spawner" {
		t.Fatalf("CaptureScope inside runInScope = %q, want spawner", captured)
	}

	// Async turn with no event and no explicit scope: without re-establishing
	// we would fall back to _default.
	if got := activeScope(); got != "_default" {
		t.Fatalf("baseline activeScope = %q, want _default", got)
	}

	var reEstablished string
	RunInScope(captured, func() { reEstablished = activeScope() })
	if reEstablished != "spawner" {
		t.Errorf("RunInScope re-established scope = %q, want spawner", reEstablished)
	}
}

// TestParkScopeClearsThenRestores proves the carrier is CLEARED while "parked"
// (so a suspended goroutine leaks nothing onto whoever runs meanwhile) and is
// restored from the local snapshot on resume (so it does not trust a global a
// concurrent goroutine may have clobbered).
func TestParkScopeClearsThenRestores(t *testing.T) {
	saveScopeGlobals(t)
	currentScope, currentScopeActive = "A", true

	restore := parkScope()
	// Parked: carrier is cleared, so anything that runs now sees no scope.
	if currentScopeActive || currentScope != "" {
		t.Errorf("while parked: scope=%q active=%v, want empty/false", currentScope, currentScopeActive)
	}
	// Simulate another goroutine clobbering the global while we are parked.
	currentScope, currentScopeActive = "B", true

	restore()
	// Resumed: our own scope is back, regardless of the clobber.
	if !currentScopeActive || currentScope != "A" {
		t.Errorf("after resume: scope=%q active=%v, want A/true", currentScope, currentScopeActive)
	}
}

// TestBootstrapScopeIsStable confirms bootstrapScope() ignores the event/explicit
// carriers — the halt slot and __gothic_set keying depend on this.
func TestBootstrapScopeIsStable(t *testing.T) {
	saveScopeGlobals(t)
	bootstrapScopeID = "mount"
	findScopeFn = func() string { return "event" }
	runInScope("explicit", func() {
		if got := bootstrapScope(); got != "mount" {
			t.Errorf("bootstrapScope() under explicit/event = %q, want mount", got)
		}
	})
}
