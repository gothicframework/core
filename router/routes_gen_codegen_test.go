package helpers

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
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

// TestRoutesGenQuotedHttpPath asserts that the template emits
// strconv.Quote'd paths (QuotedHttpPath) instead of raw HttpPath, so paths
// with special characters are safe in generated Go source code.
func TestRoutesGenQuotedHttpPath(t *testing.T) {
	th := helpers.NewTemplateHelper()
	out := filepath.Join(t.TempDir(), "routes_gen_quoted.go")

	info := TemplateInfo{
		GoModName:     "example.com/app",
		ImportDefault: true,
		Routes: []RouteTemplate{
			{
				FunctionName:      "Index",
				ConfigName:        "DefaultConfig",
				ConfigPackageName: "routes",
				PackageName:       "pages",
				HttpPath:          "/",
				QuotedHttpPath:    strconv.Quote("/"),
			},
			{
				FunctionName:      "BlogPost",
				ConfigName:        "BlogConfig",
				ConfigPackageName: "blog",
				PackageName:       "pages",
				HttpPath:          "/blog/{id}",
				QuotedHttpPath:    strconv.Quote("/blog/{id}"),
			},
		},
	}

	if err := th.UpdateFromTemplateFS(routesGenTemplateFS, routesGenTemplatePath, out, info); err != nil {
		t.Fatalf("render routes_gen: %v", err)
	}

	src, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read rendered routes_gen: %v", err)
	}
	gen := string(src)

	// The template must use the QuotedHttpPath value (which is already Go-quoted).
	// Verify QuotedHttpPath is used in the generated output. The template uses
	// {{.QuotedHttpPath}} which already includes surrounding double quotes.
	if !strings.Contains(gen, `routes.DefaultConfig.RegisterRoute(r,"/",pages.Index)`) {
		t.Errorf("expected quoted root path in template output, got:\n%s", gen)
	}
	if !strings.Contains(gen, `blog.BlogConfig.RegisterRoute(r,"/blog/{id}",pages.BlogPost)`) {
		t.Errorf("expected quoted /blog/{id} path in template output, got:\n%s", gen)
	}
}
