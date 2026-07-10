package gothiccore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVersionStable pins the cache-buster hash to the JS content: it is 16 hex
// chars and deterministic. A change to JS changes it (and must be mirrored in the
// layout — see TestLayoutReferencesCurrentHash).
func TestVersionStable(t *testing.T) {
	v := Version()
	if len(v) != 16 {
		t.Fatalf("expected a 16-char hash, got %q (%d)", v, len(v))
	}
	if Version() != v {
		t.Errorf("Version() must be deterministic")
	}
	if AssetPath() != "/_gothic/"+FileName+"?v="+v {
		t.Errorf("unexpected AssetPath %q", AssetPath())
	}
}

// TestWriteEmitsAsset verifies Write emits the JS byte-for-byte.
func TestWriteEmitsAsset(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "public")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "public", FileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != JS {
		t.Errorf("emitted asset does not match JS const")
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
// (populated in Phase 30), not under `core`. When absent — a core-only build —
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
	t.Skip("components module not co-located (populated in Phase 30); cross-module version-sync guard runs in the workspace/e2e gate")
	return nil
}

// TestComponentReferencesVersionedAsset guards the wiring that replaced the old
// hardcoded-hash drift guard. The layout no longer references gothic-core.js
// directly — the RuntimeScripts component emits it, building the URL from the
// LIVE gothiccore.Version() rather than a hardcoded hash, so the cache-buster can
// never drift out of sync with the asset content. This asserts the component
// source still constructs the /_gothic/ URL from Version() (not a frozen literal).
func TestComponentReferencesVersionedAsset(t *testing.T) {
	data := componentsRuntimeScripts(t)
	want := `"/_gothic/` + FileName + `?v=" + gothiccore.Version()`
	if !strings.Contains(string(data), want) {
		t.Errorf("runtimeScripts.templ must build the gothic-core.js URL as %q (served from /_gothic/, versioned by the live content hash)", want)
	}
}
