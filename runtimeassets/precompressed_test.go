package runtimeassets

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
)

// TestPrecompressedAssetsAreFresh guards the build-time-compressed variants: every
// embedded .br/.gz must decompress back to the CURRENT raw asset bytes. If an asset
// changed (e.g. a regenerated core.wasm) without re-running
// `go generate ./runtimeassets`, the committed variant is stale and this fails —
// the same drift-guard idea as corewasm/gothiccore Version(), applied to the
// precompressed forms.
func TestPrecompressedAssetsAreFresh(t *testing.T) {
	for _, a := range All() {
		if a.Brotli != nil {
			got, err := io.ReadAll(brotli.NewReader(bytes.NewReader(a.Brotli)))
			if err != nil {
				t.Errorf("%s: embedded .br failed to decode: %v", a.Name, err)
			} else if !bytes.Equal(got, a.Bytes) {
				t.Errorf("%s: embedded .br is STALE (decodes to %d bytes, asset is %d) — run `go generate ./runtimeassets`", a.Name, len(got), len(a.Bytes))
			}
		}
		if a.Gzip != nil {
			zr, err := gzip.NewReader(bytes.NewReader(a.Gzip))
			if err != nil {
				t.Errorf("%s: embedded .gz header invalid: %v", a.Name, err)
				continue
			}
			got, err := io.ReadAll(zr)
			if err != nil {
				t.Errorf("%s: embedded .gz failed to decode: %v", a.Name, err)
			} else if !bytes.Equal(got, a.Bytes) {
				t.Errorf("%s: embedded .gz is STALE — run `go generate ./runtimeassets`", a.Name)
			}
		}
	}
}

// TestPrecompressedVariantsExist ensures the big win — the ~1.9 MB core wasm — is
// actually served precompressed (not silently falling back to raw because a
// variant went missing).
func TestPrecompressedVariantsExist(t *testing.T) {
	wasm, ok := Get("gothic-core.wasm")
	if !ok {
		t.Fatal("gothic-core.wasm not registered")
	}
	if wasm.Brotli == nil || wasm.Gzip == nil {
		t.Errorf("gothic-core.wasm must have both precompressed variants; got br=%v gz=%v — run `go generate ./runtimeassets`", wasm.Brotli != nil, wasm.Gzip != nil)
	}
	if len(wasm.Brotli) >= len(wasm.Bytes) {
		t.Errorf("gothic-core.wasm brotli variant (%d) is not smaller than raw (%d)", len(wasm.Brotli), len(wasm.Bytes))
	}
}
