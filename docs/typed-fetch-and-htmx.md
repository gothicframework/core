# Typed Fetch + HTMX from a WASM component

This is the usage guide for the two client-side tooling surfaces a Gothic TinyGo
component can call from its route's `ClientSideState`:

- **Fetch / Response** — structured HTTP (status, headers, typed body) with
  blocking, callback, and channel forms.
- **Typed JSON** — `Decode[T]` / `Encode[T]` (reflection-free, CLI-codegen'd) and
  `MapAny` (untyped).
- **HTMX** — a Go mirror of htmx 2.0.3 for swapping trusted HTML and wiring
  events.

Everything below is imported via the dot-import that every Gothic WASM page uses,
so the names are unqualified:

```go
import (
	routes "github.com/gothicframework/core/router"
	. "github.com/gothicframework/core/wasm"
)
```

Design context and the rejected alternatives are in
[ADR 0005](adr/0005-typed-fetch-and-htmx-wasm.md).

---

## When to use which — the render-vs-compute discriminator

| You want to… | Use | Why |
|---|---|---|
| Put server/BFF-rendered **HTML** in the DOM | `HTMX.Swap` / `HTMX.Ajax` | htmx runs scripts + `htmx.process`, so nested stateful components boot |
| **Compute** on structured data (branch on fields, sum, build a request body) | `Fetch` + `Decode[T]` (typed) or `MapAny` (untyped) | the bytes stay in the component; no HTML round trip |
| Call a **third-party** API needing a secret key or lacking CORS | a server-side **BFF** route, then `Fetch` your own route | same as any React app — keep the key server-side |

**XSS caveat:** `HTMX.Swap` executes `<script>` in the swapped markup (that is how
nested components boot). Only ever swap **trusted** HTML — your own templates or
your own BFF — never arbitrary third-party HTML.

---

## Fetch / Response

`Fetch` blocks the component (asyncify) but never blocks the browser. `Response`
exposes `Status`, `Headers`, `Body`, and helpers `.Text()`, `.Bytes()`, `.OK()`
(200–299), and `.MapAny()`.

```go
type FetchConfig struct {
	Method    string            // "GET" (default), "POST", "PUT", "DELETE"
	Headers   map[string]string // request headers
	Body      string            // text body
	BodyBytes []byte            // binary body — used when Body is empty
	Query     map[string]string // query params appended to the URL
}
```

### Blocking Fetch — status / headers / OK / Text

```go
ClientSideState: func() {
	CreateWasmFunc("loadUser", func() {
		resp, err := Fetch("/api/jsonEcho")
		if err != nil {
			SetText("status", "error:"+err.Error())
			return
		}
		SetText("status", strconv.Itoa(resp.Status))       // "200"
		SetText("ok", strconv.FormatBool(resp.OK()))       // "true"
		SetText("ctype", resp.Headers["content-type"])     // "application/json"
		SetText("body", resp.Text())                       // raw JSON string
	})
}
```

> **Cancellation is automatic.** Each component scope owns an `AbortController`;
> any request still in flight when the component tears down is aborted for you.
> There is nothing to wire up.

### FetchAsync — callback form (no goroutine)

The `done` callback runs in the calling component scope, so it can write the DOM
directly.

```go
CreateWasmFunc("loadAsync", func() {
	SetText("async-status", "pending")
	FetchAsync("/api/jsonEcho", FetchConfig{Method: "GET"}, func(resp Response, err error) {
		if err != nil {
			SetText("async-status", "error:"+err.Error())
			return
		}
		SetText("async-status", strconv.Itoa(resp.Status))
		SetText("async-ok", strconv.FormatBool(resp.OK()))
	})
})
```

### FetchChan — channel / fan-out

`FetchChan` returns `<-chan FetchResult` (`{ Response Response; Err error }`).
Kick off N requests, then collect them. Do the receive in a goroutine so the
click handler never blocks.

```go
const fanout = 4

CreateWasmFunc("fanOut", func() {
	SetText("chan-count", "pending")
	go func() {
		// Start N concurrent requests.
		chans := make([]<-chan FetchResult, fanout)
		for i := 0; i < fanout; i++ {
			chans[i] = FetchChan("/api/jsonEcho")
		}
		// Collect the results.
		okCount := 0
		statuses := ""
		for i := 0; i < fanout; i++ {
			res := <-chans[i]
			if res.Err == nil && res.Response.OK() {
				okCount++
			}
			statuses += strconv.Itoa(res.Response.Status) + " "
		}
		SetText("chan-count", strconv.Itoa(okCount))  // "4"
		SetText("chan-status", statuses)              // "200 200 200 200 "
	}()
})
```

*(Cribbed from the tested reference page `e2e-tests/src/pages/fetchresponse.templ`.)*

---

## The own-BFF-JSON pattern (shared struct, no drift)

Your API route emits JSON; your WASM client decodes it into the **same** struct
the server marshalled. One DTO, two importers, zero drift.

**Rule that makes or breaks the build:** the DTO must live in a **`net/http`-free
package**. A cross-package `Decode[T]`/`Encode[T]` forces the gothic CLI to import
`T`'s package into the WASM `main`, so TinyGo compiles it. If that package
(transitively) imports `net/http` — like an `api` package whose handlers take
`http.ResponseWriter` — the TinyGo build **fails**. Put shared DTOs in a leaf
package (`src/shared`): struct decls only, no `net/http`, no `reflect`, no
`encoding/json`.

**`src/shared/echo.go`** — the shared contract, imported by both sides:

```go
package shared

// TinyGo-safe: struct decls only. No net/http / reflect / encoding/json.

type EchoNested struct {
	Depth int    `json:"depth"`
	Label string `json:"label"`
}

type EchoStruct struct {
	Message  string     `json:"message"`
	Count    int        `json:"count"`
	UserName string     `json:"user_name"` // snake_case rename: key != Go field name
	Tags     []string   `json:"tags"`
	Nested   EchoNested `json:"nested"`
}
```

**`src/api/jsonEcho.go`** — the server handler emits that struct as JSON:

```go
package api

import (
	"encoding/json"
	"net/http"

	routes "github.com/gothicframework/core/router"
	"github.com/gothicframework/e2e-tests/src/shared"
)

var JsonEchoConfig = routes.ApiRouteConfig{HttpMethod: routes.GET, Type: routes.DYNAMIC}

func JsonEcho(w http.ResponseWriter, r *http.Request) {
	body, _ := json.Marshal(shared.EchoStruct{
		Message: "echo from gothic", Count: 42, UserName: "gothic_user",
		Tags: []string{"alpha", "beta", "gamma"},
		Nested: shared.EchoNested{Depth: 7, Label: "inner"},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}
```

The client then decodes into `shared.EchoStruct` — the same type — as shown next.

---

## Typed JSON — `Decode[T]`, `MapAny`

`Decode[T](resp)` returns a typed `T`; the CLI generates `_jsonDecode_T` at build
time by reading `T`'s fields and `json:` tags. `MapAny()` reads the same body
untyped.

**The type argument is mandatory and explicit** — `Decode[shared.EchoStruct](resp)`,
never `Decode(resp)`. `T` must be a struct. Missing keys / `null` → zero value;
nested structs, slices, and pointers are supported; unknown keys are ignored;
embedded fields are flattened (json promotion). Numbers decode through float64, so
an `int64` > 2^53 loses precision.

```go
CreateWasmFunc("decodeEcho", func() {
	resp, err := Fetch("/api/jsonEcho")
	if err != nil {
		SetText("message", "error:"+err.Error())
		return
	}

	// EXPLICIT, cross-package generic decode into the SHARED struct.
	v, err := Decode[shared.EchoStruct](resp)
	if err != nil {
		SetText("message", "decode-error:"+err.Error())
		return
	}
	SetText("message", v.Message)                     // "echo from gothic"
	SetText("count", strconv.Itoa(v.Count))           // "42"
	SetText("username", v.UserName)                   // "gothic_user" (user_name rename)
	if len(v.Tags) > 0 {
		SetText("tag0", v.Tags[0])                    // "alpha" (slice element)
	}
	SetText("nested-depth", strconv.Itoa(v.Nested.Depth)) // "7" (nested struct int)

	// Untyped read of the SAME body (object roots only; numbers are float64).
	m, err := resp.MapAny()
	if err != nil {
		SetText("map-message", "maperr:"+err.Error())
		return
	}
	if s, ok := m["message"].(string); ok {
		SetText("map-message", s)
	}
	if f, ok := m["count"].(float64); ok {
		SetText("map-count", strconv.Itoa(int(f)))
	}
})
```

*(Cribbed from `e2e-tests/src/pages/jsondecode.templ`.)*

---

## Typed JSON — `Encode[T]` (request body)

`Encode[T](v)` is symmetric to `Decode`: the CLI generates `_jsonEncode_T` to
marshal a typed struct into JSON bytes for a request body. **Explicit type
argument required.** nil slices/pointers/maps → `null`. (v1 note: `,omitempty` is
parsed for the key name but the field is always emitted — omitempty semantics are
ignored.)

Build a struct, `Encode` it, POST it, and `Decode` the echo to prove the
round trip:

```go
CreateWasmFunc("roundtrip", func() {
	sent := shared.EchoStruct{
		Message:  "round trip",
		Count:    99,
		UserName: "sent_user",
		Tags:     []string{"x", "y"},
		Nested:   shared.EchoNested{Depth: 3, Label: "rt"},
	}

	// Typed marshal → request body bytes.
	body := Encode[shared.EchoStruct](sent)

	resp, err := Fetch("/api/jsonEchoPost", FetchConfig{
		Method:    "POST",
		Headers:   map[string]string{"Content-Type": "application/json"},
		BodyBytes: body,
	})
	if err != nil {
		SetText("rt-message", "error:"+err.Error())
		return
	}

	echoed, err := Decode[shared.EchoStruct](resp)
	if err != nil {
		SetText("rt-message", "decode-error:"+err.Error())
		return
	}
	SetText("rt-message", echoed.Message)              // "round trip"
	SetText("rt-count", strconv.Itoa(echoed.Count))    // "99"
	SetText("rt-username", echoed.UserName)            // "sent_user" (rename survives)

	match := echoed.Message == sent.Message &&
		echoed.Count == sent.Count &&
		echoed.UserName == sent.UserName &&
		echoed.Nested.Depth == sent.Nested.Depth
	SetText("rt-ok", strconv.FormatBool(match))        // "true"
})
```

*(Cribbed from `e2e-tests/src/pages/jsondecode.templ` + `e2e-tests/src/api/jsonEchoPost.go`.)*

---

## HTMX from Go

`var HTMX` mirrors htmx 2.0.3. Swap strategies are bare consts (`InnerHTML`,
`OuterHTML`, `BeforeEnd`, `AfterBegin`, `BeforeBegin`, `AfterEnd`, `Delete`,
`None`). Events are `Evt`-prefixed `HtmxEvent` consts (`EvtAfterSwap`,
`EvtBeforeRequest`, `EvtLoad`, …); custom events are `HtmxEvent("htmx:whatever")`.
A handler receives `Event` (an alias for the DOM event / `JSValue`).

### Ajax and Swap — render trusted HTML, boot nested components

`HTMX.Swap` runs `<script>` tags and `htmx.process`, so a swapped fragment that
embeds a Gothic stateful component boots and becomes interactive after the swap.

```go
// Ajax: htmx fetches the fragment and swaps it; nested component boots.
CreateWasmFunc("doAjax", func() {
	HTMX.Ajax("GET", "/components/htmxfrag", AjaxOpts{Target: "#ajax-target"})
})

// Swap: fetch the fragment yourself, then swap the (trusted) HTML.
CreateWasmFunc("doSwap", func() {
	go func() {
		resp, err := Fetch("/components/htmxfrag")
		if err != nil {
			SetText("swap-status", "error:"+err.Error())
			return
		}
		HTMX.Swap("#swap-target", resp.Text(), InnerHTML) // runs scripts + htmx.process
		SetText("swap-status", strconv.Itoa(resp.Status))
	}()
})
```

`AjaxOpts` is `{ Target, Source string; Swap SwapStrategy; Values map[string]string }`.

### Event listeners — `On` (scoped) vs `OnGlobal` (page-wide)

`HTMX.On` is **subtree-scoped** (to the component's `[data-gothic-scope]` root)
**and lifetime-scoped** — its `js.Func` is released on teardown, so it is
leak-safe. `HTMX.OnGlobal` listens page-wide. `HTMX.Off(e, h)` removes all
`On`/`OnGlobal` regs for event `e` in the current scope (Go func values are not
comparable, so `h` is accepted for symmetry but ignored).

```go
ClientSideState: func() {
	swapCount := CreateObservable(0)
	globalCount := CreateObservable(0)
	Observe(func() { SetText("onswap-count", strconv.Itoa(swapCount.Get())) }, swapCount)
	Observe(func() { SetText("onglobal-count", strconv.Itoa(globalCount.Get())) }, globalCount)

	// Subtree-scoped afterSwap listener — auto-released on teardown.
	HTMX.On(EvtAfterSwap, func(e Event) {
		swapCount.Set(swapCount.Get() + 1)
	})
	// Page-wide afterSwap listener.
	HTMX.OnGlobal(EvtAfterSwap, func(e Event) {
		globalCount.Set(globalCount.Get() + 1)
	})
	// Custom event: listen for a cast HtmxEvent, act on it.
	HTMX.OnGlobal(HtmxEvent("gothic:ping"), func(e Event) {
		SetText("trigger-out", "pong")
	})
}
```

### Trigger, classes, find, values

```go
// Fire a custom htmx event on a target (the OnGlobal handler above writes "pong").
CreateWasmFunc("doTrigger", func() {
	HTMX.Trigger("#trigger-target", HtmxEvent("gothic:ping"))
})

// Class helpers: AddClass / RemoveClass / ToggleClass / TakeClass.
CreateWasmFunc("doClass", func() {
	HTMX.AddClass("#class-target", "hx-added")
})

// Find an element by selector; report whether it resolved.
CreateWasmFunc("doFind", func() {
	el := HTMX.Find("#needle")           // also: FindAll(sel) []JSValue, Closest(target, sel)
	SetText("find-out", strconv.FormatBool(el.Truthy()))
})

// Read the values htmx would submit from a form.
CreateWasmFunc("doValues", func() {
	vals := HTMX.Values("#my-form")      // map[string]any
	if s, ok := vals["foo"].(string); ok {
		SetText("values-out", s)
	}
})
```

Also available: `HTMX.Process(target)` (boot components under a subtree you
inserted yourself) and `HTMX.Remove(target)`.

*(Cribbed from `e2e-tests/src/pages/htmxwasm.templ`.)*

---

## Gotchas checklist

- **`Fetch` returns `Response`, not a string** — use `resp.Text()` / `resp.Bytes()`.
  `FetchBytes` no longer exists.
- **`Decode[T]` / `Encode[T]` need an explicit type arg** and `T` must be a
  struct — inferred calls are a build error.
- **DTOs go in a `net/http`-free package** (`src/shared`), imported by both the
  server handler and the WASM page. A DTO in a package that pulls `net/http`
  fails the TinyGo build.
- **Numbers decode through float64** — `int64` > 2^53 loses precision (`Decode`
  and `MapAny` alike).
- **`,omitempty` is a v1 no-op** for `Encode` — the field is always emitted.
- **`HTMX.Swap` runs scripts** — only swap trusted HTML.
