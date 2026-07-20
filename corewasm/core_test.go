package corewasm

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestWriteEmitsThreeArtifacts verifies Write emits the wasm, the exec shim and
// the boot loader with the expected content.
func TestWriteEmitsThreeArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for name, want := range map[string][]byte{
		WASMFileName: coreWASM,
		ExecFileName: execJS,
		BootFileName: []byte(bootJS),
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(got) != len(want) {
			t.Errorf("%s: emitted %d bytes, want %d", name, len(got), len(want))
		}
	}
}

// TestCoreWasmIsValidModule guards that the committed core.wasm is a real
// WebAssembly binary (magic "\0asm", version 1) and not an empty/garbage file.
func TestCoreWasmIsValidModule(t *testing.T) {
	if len(coreWASM) < 8 {
		t.Fatalf("core.wasm too small: %d bytes", len(coreWASM))
	}
	wantMagic := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	for i, b := range wantMagic {
		if coreWASM[i] != b {
			t.Fatalf("core.wasm header byte %d = 0x%02x, want 0x%02x (not a valid wasm module)", i, coreWASM[i], b)
		}
	}
}

// TestBootHashTracksBinaries guards that the boot loader embeds both binary
// content hashes, so a framework upgrade that changes either binary changes the
// single ?v= the layout carries (transitive cache-bust).
func TestBootHashTracksBinaries(t *testing.T) {
	if !strings.Contains(bootJS, coreHash) {
		t.Errorf("boot loader does not embed coreHash %q — core upgrades would not cache-bust", coreHash)
	}
	if !strings.Contains(bootJS, execHash) {
		t.Errorf("boot loader does not embed execHash %q — exec upgrades would not cache-bust", execHash)
	}
	if !strings.Contains(BootAssetPath(), bootHash) {
		t.Errorf("BootAssetPath %q missing bootHash %q", BootAssetPath(), bootHash)
	}
}

// TestWriteIsMtimeStable is the acceptance criterion: because the static
// core sits in the hot-reload emission path (GenerateAll calls Write every build)
// but is NEVER recompiled, a second Write with unchanged content must leave every
// emitted file's modification time untouched — otherwise a hot-reload cycle would
// churn the core's mtime and defeat "not rebuilt on hot reload".
func TestWriteIsMtimeStable(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	files := []string{WASMFileName, ExecFileName, BootFileName}
	before := make(map[string]time.Time, len(files))
	for _, f := range files {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		before[f] = info.ModTime()
	}

	// Sleep so any rewrite would produce a distinguishable mtime on coarse
	// filesystem clocks.
	time.Sleep(20 * time.Millisecond)

	if err := Write(dir); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	for _, f := range files {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("stat %s (second): %v", f, err)
		}
		if got := info.ModTime(); !got.Equal(before[f]) {
			t.Errorf("%s mtime changed on idempotent re-emit: %v -> %v (Write is not content-idempotent)", f, before[f], got)
		}
	}
}

// repoRoot walks up from this test file to the module root (where go.mod lives).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find module root")
	return ""
}

// componentsRuntimeScripts locates the components module's runtimeScripts.templ.
// After the Part III multi-repo split it lives in the sibling `components` module
// not under `core`. When absent — a core-only build —
// the cross-module version-sync guard is skipped; it is re-exercised in the
// workspace/e2e gate once components is co-located.
func componentsRuntimeScripts(t *testing.T) []byte {
	t.Helper()
	root := repoRoot(t)
	for _, p := range []string{
		filepath.Join(root, "components", "runtimeScripts.templ"),
		filepath.Join(filepath.Dir(root), "components", "runtimeScripts.templ"),
	} {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	t.Skip("components module not co-located; cross-module version-sync guard runs in the workspace/e2e gate")
	return nil
}

// TestComponentReferencesVersionedBoot guards the wiring that replaced the old
// hardcoded-hash drift guard. The layout no longer references gothic-core-boot.js
// directly — the RuntimeScripts component emits it, building the URL from the LIVE
// corewasm.Version() rather than a hardcoded hash. Because the boot loader embeds
// the core.wasm and exec content hashes, Version() (bootHash) changes whenever any
// of the three static-core artifacts changes, so the emitted ?v= tracks all three
// automatically — no maintainer hash-bump needed. This asserts the component
// source still constructs the /_gothic/ boot URL from Version() (not a frozen
// literal), so a maintainer regenerating core.wasm can never leave a stale hash.
func TestComponentReferencesVersionedBoot(t *testing.T) {
	data := componentsRuntimeScripts(t)
	want := `"/_gothic/` + BootFileName + `?v=" + corewasm.Version()`
	if !strings.Contains(string(data), want) {
		t.Errorf("runtimeScripts.templ must build the boot-loader URL as %q (served from /_gothic/, versioned by the live boot hash)", want)
	}
}

// TestWriteRewritesOnContentChange verifies the flip side: when the on-disk
// content differs, Write DOES rewrite it (so a CLI upgrade actually replaces a
// stale core).
func TestWriteRewritesOnContentChange(t *testing.T) {
	dir := t.TempDir()
	bootPath := filepath.Join(dir, BootFileName)
	if err := os.WriteFile(bootPath, []byte("stale"), 0644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := Write(dir); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(bootPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != bootJS {
		t.Errorf("Write did not replace stale boot loader content")
	}
}
