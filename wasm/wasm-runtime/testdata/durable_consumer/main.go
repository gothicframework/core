//go:build js && wasm

// Command durable_consumer is a compile-only fixture proving a DURABLE consumer's
// main() builds under GOOS=js GOARCH=wasm. It mirrors the
// shape of a generated ClientSideState body that opts a component into the core's
// page-session durable cache: create observables, DurableObserve them (rehydrates
// from the core across teardown→re-mount, persists changes), wire the DOM, expose
// a callback, then park on the halt channel. It is under testdata/ so it is not
// part of any normal package build; a host test invokes `go build` on it.
package main

import (
	"strconv"

	. "github.com/gothicframework/core/wasm/wasm-runtime/runtime"
)

func main() {
	count := CreateObservable(0)
	note := CreateObservable("")

	// Opt into durability. No-op unless this placement carries a
	// data-gothic-durable-key; when present, count/note rehydrate from the core
	// before first paint and persist for the page session.
	DurableObserve("count", count, strconv.Itoa,
		func(s string) int { n, _ := strconv.Atoi(s); return n })
	DurableObserve("note", note,
		func(s string) string { return s },
		func(s string) string { return s })

	Observe(func() {
		SetText("count-out", strconv.Itoa(count.Get()))
	}, count)

	Observe(func() {
		SetText("note-out", note.Get())
	}, note)

	CreateWasmFunc("inc", func() {
		count.Set(count.Get() + 1)
	})
	CreateWasmStringFunc("setNote", func(s string) {
		note.Set(s)
	})

	// A component may branch on whether it is durable.
	if DurableKey() != "" {
		ConsoleLog("durable mode active")
	}

	select {
	case <-GothicHaltChan():
		return
	}
}
