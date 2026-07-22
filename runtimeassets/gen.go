//go:build ignore

// Command gen precomputes the brotli and gzip variants of every /_gothic/*
// runtime asset AT BUILD TIME and writes them under assets/, so the running
// server never spends cold-start CPU compressing them. Because this runs at
// release time, it uses the maximum ratio (brotli quality 11) for free.
//
// Run it via `go generate ./runtimeassets` whenever an underlying asset changes;
// the TestPrecompressedAssetsAreFresh drift test fails if the committed variants
// are stale.
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gothicframework/core/corewasm"
	"github.com/gothicframework/core/gothiccore"
	wasmexec "github.com/gothicframework/core/wasmexec"
)

const outDir = "assets"

func main() {
	if err := os.RemoveAll(outDir); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	// A committed placeholder guarantees `//go:embed all:assets` always has a
	// match even if (hypothetically) no variant were smaller than its source.
	if err := os.WriteFile(filepath.Join(outDir, ".gitkeep"), nil, 0o644); err != nil {
		log.Fatal(err)
	}

	assets := []struct {
		name string
		data []byte
	}{
		{gothiccore.FileName, gothiccore.Minified()},
		{corewasm.WASMFileName, corewasm.CoreWASM()},
		{corewasm.ExecFileName, corewasm.ExecJS()},
		{corewasm.BootFileName, corewasm.BootJS()},
		{"wasm_exec.js", wasmexec.Shim},
	}
	for _, a := range assets {
		writeVariant(a.name+".br", brotliMax(a.data), len(a.data))
		writeVariant(a.name+".gz", gzipMax(a.data), len(a.data))
	}
}

// writeVariant writes c under assets/<name> only when it actually shrank the
// input; a variant that is not smaller is simply omitted so the handler serves
// the raw bytes for it.
func writeVariant(name string, c []byte, rawLen int) {
	if len(c) == 0 || len(c) >= rawLen {
		fmt.Printf("  skip  %-28s (not smaller: %d >= %d)\n", name, len(c), rawLen)
		return
	}
	if err := os.WriteFile(filepath.Join(outDir, name), c, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  write %-28s %d -> %d bytes\n", name, rawLen, len(c))
}

func brotliMax(b []byte) []byte {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, brotli.BestCompression) // q11 — free at build time
	if _, err := w.Write(b); err != nil {
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

func gzipMax(b []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		log.Fatal(err)
	}
	// Zero the header so regenerating identical input yields identical bytes (no
	// spurious git diffs from an embedded timestamp).
	w.ModTime = time.Time{}
	w.OS = 255 // "unknown", per RFC 1952 — stable across platforms
	if _, err := w.Write(b); err != nil {
		log.Fatal(err)
	}
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}
