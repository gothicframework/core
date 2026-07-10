//go:build !js || !wasm

package runtime_test

import (
	"os"
	"os/exec"
	"testing"
)

// TestDurableConsumerBuildsUnderJSWasm is the Phase-18 Go-level build gate,
// mirroring Phase-17's TestPhase17ConsumerBuildsUnderJSWasm: it compiles the
// hand-written durable consumer fixture (testdata/durable_consumer) with the
// standard Go js/wasm toolchain. The durable runtime files are
// `//go:build js && wasm`, which GOOS=js GOARCH=wasm satisfies exactly like
// TinyGo's -target=wasm — so a green build here proves the durable consumer and
// the runtime helpers it calls (DurableObserve, DurableKey, and the unexported
// register/write/listen/online helpers) link and type-check for the WASM target.
//
// The end-to-end BEHAVIOR (mutate → swap-away → swap-back → restored, cleared
// values surviving, multiplexed per-row) is proven by the Phase-21
// wasm-durable-cache.spec.ts Playwright suite on TestGothic — NOT here.
//
// Skipped when the Go toolchain is unavailable (matches the rest of the suite,
// which runs in toolchain-less CI).
func TestDurableConsumerBuildsUnderJSWasm(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH; skipping js/wasm build gate")
	}

	const pkg = "github.com/gothicframework/core/wasm/wasm-runtime/testdata/durable_consumer"
	cmd := exec.Command("go", "build", "-o", os.DevNull, pkg)
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("GOOS=js GOARCH=wasm go build of durable consumer failed: %v\n%s", err, b)
	}
}
