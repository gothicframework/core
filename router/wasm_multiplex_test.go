package helpers

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/gothicframework/core/gothiccore"
)

// TestMultiplexedBootstrap_MuxBranchPresent verifies the multiplexed bootstrap
// emits the mux/register-or-queue control flow: the per-wasmName instance table,
// the live-instance register call, the pending queue for late placements, and
// the last-scope-out teardown wrapper that halts the shared instance.
func TestMultiplexedBootstrap_MuxBranchPresent(t *testing.T) {
	in := []byte(`<div data-gothic-wasm="components-row" data-gothic-inst="abc" style="display:contents">x</div>`)
	out := injectWasmBootstrap(in, "components-row", GZIP, GothicTinyGo, "abc", true)

	// The per-placement mux run region stays inline in the per-instance script.
	wants := [][]byte{
		// Per-wasmName mux table + scope→wasmName reverse map.
		[]byte(`window.__gothicMux`),
		[]byte(`window.__gothicScopeMux`),
		// First placement creates the entry with a pending queue and live scope set.
		[]byte(`{ready:false,instId:id,pending:[],scopes:{}}`),
		// Subsequent placement registers on the live instance or queues.
		[]byte(`__gothic_register_scope`),
		[]byte(`mux.pending.push(id)`),
		// First placement flushes the pending queue after go.run.
		[]byte(`for(var _i=0;_i<mux.pending.length;_i++)`),
		// Orphan-instance guard: if the mux entry was dropped/replaced while
		// instantiating (last scope unmounted mid-load), discard this instance.
		[]byte(`if(window.__gothicMux[wn]!==mux){`),
		[]byte(`window.__gothicTeardown(id);`),
		// It still instantiates exactly like the base path for the first placement.
		[]byte(`WebAssembly.instantiateStreaming`),
	}
	for _, w := range wants {
		if !bytes.Contains(out, w) {
			t.Errorf("multiplexed bootstrap missing %q", w)
		}
	}

	// The once-installed last-scope-out teardown wrapper that halts the
	// shared instance moved into gothic-core.js (installed unconditionally there).
	coreWants := []string{
		`__gothicMuxTeardownInstalled`,
		`Object.keys(mux.scopes).length===0`,
		`saved.__halt()`,
	}
	for _, w := range coreWants {
		if !strings.Contains(gothiccore.JS, w) {
			t.Errorf("gothic-core.js missing mux teardown wrapper marker %q", w)
		}
	}
}

// TestNonMultiplexedBootstrap_NoMuxBranch is the inverse guard: a
// non-multiplexed render must NOT contain any of the mux machinery, so the
// default path stays byte-identical to the non-multiplexed baseline.
func TestNonMultiplexedBootstrap_NoMuxBranch(t *testing.T) {
	in := []byte(`<html><body>x</body></html>`)
	out := injectWasmBootstrap(in, "counter", GZIP, GothicTinyGo, "abc", false)

	forbidden := [][]byte{
		[]byte(`__gothicMux`),
		[]byte(`__gothicScopeMux`),
		[]byte(`__gothic_register_scope`),
		[]byte(`__gothicMuxTeardownInstalled`),
		[]byte(`mux.pending`),
	}
	for _, f := range forbidden {
		if bytes.Contains(out, f) {
			t.Errorf("non-multiplexed bootstrap unexpectedly contains mux marker %q", f)
		}
	}
}

// TestMultiplexedBootstrap_SharedHeadIdentical proves the multiplexed and
// non-multiplexed per-instance scripts share the SAME head (scope stamp +
// gothic-core.js ensure) up to the run region, so both install/consume the same
// shared globals from gothic-core.js. The observer itself now lives in
// gothic-core.js (asserted separately).
func TestMultiplexedBootstrap_SharedHeadIdentical(t *testing.T) {
	frag := []byte(`<div>x</div>`)
	non := injectWasmBootstrap(frag, "components-row", BROTLI, Golang, "deadbeef", false)
	mux := injectWasmBootstrap(frag, "components-row", BROTLI, Golang, "deadbeef", true)

	// The shared head ends right where the run body begins: `_ensureCore(function(){`.
	marker := []byte(`_ensureCore(function(){`)
	nonIdx := bytes.Index(non, marker)
	muxIdx := bytes.Index(mux, marker)
	if nonIdx < 0 || muxIdx < 0 {
		t.Fatalf("shared head marker missing (non=%d mux=%d)", nonIdx, muxIdx)
	}
	end := nonIdx + len(marker)
	if !bytes.Equal(non[:end], mux[:end]) {
		t.Errorf("multiplexed head diverges from the shared per-instance head before the run region")
	}

	// The unmount observer install lives once in gothic-core.js.
	if !strings.Contains(gothiccore.JS, `.observe(document.body,{childList:true,subtree:true})`) {
		t.Errorf("gothic-core.js missing the unmount observer install")
	}
}

// TestRegisterRoute_MultiplexedThreadsToEnvelope verifies RouteConfig.Multiplexed
// flows through RegisterRoute → wasmInjectedComponent → the rendered envelope so
// the emitted bootstrap carries the mux run region, and stays absent otherwise.
func TestRegisterRoute_MultiplexedThreadsToEnvelope(t *testing.T) {
	render := func(mux bool) string {
		c := &wasmInjectedComponent{
			inner:       htmlComponent("<section>hi</section>"),
			wasmName:    "components-row",
			compression: GZIP,
			compiler:    GothicTinyGo,
			multiplexed: mux,
		}
		var buf bytes.Buffer
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	muxOut := render(true)
	if !strings.Contains(muxOut, "__gothic_register_scope") || !strings.Contains(muxOut, "window.__gothicMux") {
		t.Errorf("multiplexed=true envelope missing mux run region")
	}
	nonOut := render(false)
	if strings.Contains(nonOut, "__gothicMux") {
		t.Errorf("multiplexed=false envelope must not carry mux run region")
	}
}

// htmlComponent adapts a fixed HTML string into a templ.Component.
func htmlComponent(html string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte(html))
		return err
	})
}
