package code

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// bigFunctions are an oversized function in each language, split at statements under a budget
// of 80 tokens.
func bigFunctions() []fixture {
	var g, ts, py, rs strings.Builder
	g.WriteString("package x\n\n// Big does a lot.\nfunc Big(n int) error {\n")
	ts.WriteString("/** Big. */\nfunction big(n) {\n")
	py.WriteString("def big(n):\n    \"\"\"Big.\"\"\"\n")
	rs.WriteString("/// Big.\nfn big(n: i32) -> i32 {\n")
	for i := 0; i < 40; i++ {
		g.WriteString("\tv := step(n)\n\tif err := check(v); err != nil {\n\t\treturn err\n\t}\n")
		ts.WriteString("  const v = step(n);\n  if (v) { log(v); }\n")
		py.WriteString("    v = step(n)\n    if v:\n        log(v)\n")
		rs.WriteString("    let v = step(n);\n    if v > 0 { log(v); }\n")
	}
	g.WriteString("\treturn nil\n}\n")
	ts.WriteString("  return n;\n}\n")
	py.WriteString("    return n\n")
	rs.WriteString("    n\n}\n")
	return []fixture{
		{"go-big", Go{}, "pkg/x/big.go", g.String()},
		{"js-big", TypeScript{}, "a.js", ts.String()},
		{"py-big", Python{}, "a.py", py.String()},
		{"rs-big", Rust{}, "a.rs", rs.String()},
	}
}

// functionEmbeds lists the Embed of every function and method chunk in the fixtures, and of every
// fragment of the oversized functions: what a function embeds, in every language.
func functionEmbeds(t *testing.T) string {
	t.Helper()
	functions := map[string][]string{
		"go":         {"Thing > Get", "Thing > Set", "x > Map"},
		"ts":         {"> add", "Shape > area", "Shape > make", "> double"},
		"js-strings": {"> real"},
		"py":         {"> top", "Box > open", "Box > close"},
		"py-strings": {"> real"},
		"rs":         {"Shape > area", "> fmt", "> new", "> later", "> answer", "> callback"},
		"rs-strings": {"> real", "> second"},
	}
	var b strings.Builder
	list := func(name string, cs []search.Chunk, keep func(string) bool) {
		for _, c := range cs {
			if keep(c.Breadcrumb) {
				fmt.Fprintf(&b, "== %s | %s\n%s\n", name, c.Breadcrumb, c.EmbedText())
			}
		}
	}
	for _, f := range allFixtures() {
		cs, err := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src)})
		if err != nil {
			t.Fatal(err)
		}
		list(f.name, cs, func(crumb string) bool {
			for _, s := range functions[f.name] {
				if strings.HasSuffix(crumb, s) {
					return true
				}
			}
			return false
		})
	}
	for _, f := range bigFunctions() {
		cs, err := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src), Tokens: 80})
		if err != nil {
			t.Fatal(err)
		}
		list(f.name, cs, func(crumb string) bool { return strings.Contains(crumb, "ig") })
	}
	return b.String()
}

// A function's Embed is its breadcrumb, doc and signature, and a later fragment's the signature
// with its comments and the identifiers it declares and calls: the golden holds them as they were
// before embeds were bounded, and they must not change.
func TestFunctionEmbedsMatchTheGolden(t *testing.T) {
	got := functionEmbeds(t)
	path := filepath.Join("testdata", "function-embeds.golden")
	if os.Getenv("CODECHUNK_UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("function embeds differ from %s:\n%s", path, firstDiff(string(want), got))
	}
	if n := strings.Count(got, "\n== "); n < 30 {
		t.Errorf("the golden lists only %d embeds", n+1)
	}
}

// firstDiff shows the first line where a and b differ.
func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < max(len(al), len(bl)); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  want %q\n  got  %q", i+1, x, y)
		}
	}
	return "(equal)"
}
