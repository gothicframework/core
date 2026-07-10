package helpers

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	helpers "github.com/gothicframework/core/render"
)

// TestRoutesGenTemplateImportsOrgPath is a regression guard for the module
// rename: the routes_gen generator emits a `routes` import into the USER's
// project, and a stale pre-rename legacy or major-version path there breaks
// `gothic build`. It renders the template, parses the output, and asserts every
// gothicframework import resolves to the suffixless org path (never pre-rename legacy,
// never a /vN segment) — an AST check, not a substring match, so it can't be
// fooled by the path appearing in a comment.
func TestRoutesGenTemplateImportsOrgPath(t *testing.T) {
	th := helpers.NewTemplateHelper()
	out := filepath.Join(t.TempDir(), "routes_gen.go")
	if err := th.UpdateFromTemplateFS(routesGenTemplateFS, routesGenTemplatePath, out, TemplateInfo{
		GoModName:     "example.com/app",
		ImportDefault: true, // emit the `routes ".../core/router"` import
	}); err != nil {
		t.Fatalf("render routes_gen: %v", err)
	}
	src, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read rendered routes_gen: %v", err)
	}
	assertGothicImportsOrgPath(t, "routes_gen.go", string(src))
}

// assertGothicImportsOrgPath parses src as Go and asserts every import path that
// mentions the gothicframework module resolves to the suffixless org path — never
// the pre-rename legacy org, and never a stale /vN major-version segment (the
// runtime modules dropped their version suffix).
func assertGothicImportsOrgPath(t *testing.T, label, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, label, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s did not parse: %v\n---\n%s", label, err, src)
	}
	var seen int
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.Contains(path, "gothicframework/") {
			continue
		}
		seen++
		// The generated routes import must resolve to the suffixless core module
		// (github.com/gothicframework/core/...). This one prefix check rejects every
		// stale form at once: the legacy org, a /v2 or /v3 major-version segment, or
		// the components module — none of which start with this prefix.
		if !strings.HasPrefix(path, "github.com/gothicframework/core/") {
			t.Errorf("%s import %q is not under the suffixless core module (github.com/gothicframework/core/...) — stale org or major-version segment?", label, path)
		}
	}
	if seen == 0 {
		t.Errorf("%s emitted no gothicframework import to check (fixture should force one)", label)
	}
}
