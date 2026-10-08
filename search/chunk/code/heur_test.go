package code

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

func chunksOf(t *testing.T, c search.Chunker, path, src string) []search.Chunk {
	t.Helper()
	cs, err := c.Chunk(search.Doc{Path: path, Text: []byte(src)})
	if err != nil {
		t.Fatal(err)
	}
	partition(t, path, []byte(src), cs)
	return cs
}

func noCrumb(t *testing.T, cs []search.Chunk, word string) {
	t.Helper()
	for _, c := range cs {
		if strings.Contains(c.Breadcrumb, word) {
			t.Errorf("a unit named from inside a string or comment: %q", c.Breadcrumb)
		}
	}
}

func TestTypeScriptUnits(t *testing.T) {
	cs := chunksOf(t, TypeScript{}, "web/app.ts", tsSample)
	if !strings.HasPrefix(cs[0].Body, "import { a }") || cs[0].Breadcrumb != "web/app.ts" {
		t.Errorf("header: %q", cs[0].Body)
	}
	add := chunkOf(t, cs, "app.ts > add")
	if !strings.HasPrefix(add.Body, "/** Adds two numbers. */") {
		t.Errorf("add's JSDoc is not in its unit: %q", add.Body)
	}
	if !strings.Contains(add.Embed, "Adds two numbers.") || !strings.Contains(add.Embed, "export function add(x: number, y: number): number") || strings.Contains(add.Embed, "return x + y") {
		t.Errorf("add's Embed: %q", add.Embed)
	}
	shape := chunkOf(t, cs, "app.ts > Shape")
	if !strings.Contains(shape.Body, "@Component") || !strings.Contains(shape.Body, "A shape.") || !strings.Contains(shape.Body, `private name = "s";`) {
		t.Errorf("Shape's head: %q", shape.Body)
	}
	area := chunkOf(t, cs, "Shape > area")
	if !strings.HasPrefix(area.Body, "/** The area. */") || !strings.Contains(area.Body, `"}"`) || !strings.Contains(area.Embed, "The area.") {
		t.Errorf("area: %q / %q", area.Body, area.Embed)
	}
	if mk := chunkOf(t, cs, "Shape > make"); !strings.HasSuffix(mk.Body, "}\n}") {
		t.Errorf("the class's closing brace is not in its last unit: %q", mk.Body)
	}
	for _, crumb := range []string{"app.ts > Point", "app.ts > Pair", "app.ts > double"} {
		chunkOf(t, cs, crumb)
	}
	if d := chunkOf(t, cs, "app.ts > double"); !strings.Contains(d.Embed, "Doubles.") || strings.Contains(d.Embed, "n * 2") {
		t.Errorf("double's Embed: %q", d.Embed)
	}
	if last := cs[len(cs)-1]; !strings.HasSuffix(last.Breadcrumb, "(glue)") || last.Body != `console.log("top-level");` {
		t.Errorf("top-level statement: %q %q", last.Breadcrumb, last.Body)
	}
}

func TestJavaScriptStringsAndCommentsNeverMakeUnits(t *testing.T) {
	cs := chunksOf(t, TypeScript{}, "web/s.js", jsStrings)
	real := chunkOf(t, cs, "s.js > real")
	if !strings.Contains(real.Body, `"} function fake() {"`) || !strings.HasSuffix(real.Body, "return s + `}`;\n}") {
		t.Errorf("real: %q", real.Body)
	}
	noCrumb(t, cs, "fake")
	noCrumb(t, cs, "Fake")
}

func TestPythonUnits(t *testing.T) {
	cs := chunksOf(t, Python{}, "py/m.py", pySample)
	top := chunkOf(t, cs, "m.py > top")
	if !strings.HasPrefix(top.Body, "@decorator\n@other(") || !strings.HasSuffix(top.Body, "return b") {
		t.Errorf("top's span: %q", top.Body)
	}
	if !strings.Contains(top.Embed, "Top adds.") || !strings.Contains(top.Embed, "def top(a, b: int) -> int") || strings.Contains(top.Embed, "return a") {
		t.Errorf("top's Embed: %q", top.Embed)
	}
	box := chunkOf(t, cs, "m.py > Box")
	if !strings.Contains(box.Body, `"""A box."""`) || !strings.Contains(box.Body, "size = 3") || !strings.Contains(box.Embed, "A box.") {
		t.Errorf("Box's head: %q / %q", box.Body, box.Embed)
	}
	if o := chunkOf(t, cs, "Box > open"); !strings.Contains(o.Embed, "Opens it.") || strings.Contains(o.Embed, "self.size") {
		t.Errorf("open's Embed: %q", o.Embed)
	}
	cl := chunkOf(t, cs, "Box > close")
	if !strings.Contains(cl.Body, "class NotAClass:") || !strings.HasSuffix(cl.Body, "return s") {
		t.Errorf("close: %q", cl.Body)
	}
	noCrumb(t, cs, "NotAClass")
	noCrumb(t, cs, "not_a_def")
	if last := cs[len(cs)-1]; last.Body != `print("done")` {
		t.Errorf("the module statement after the class: %q", last.Body)
	}
}

func TestPythonStringsAndCommentsNeverMakeUnits(t *testing.T) {
	cs := chunksOf(t, Python{}, "py/s.py", pyStrings)
	real := chunkOf(t, cs, "s.py > real")
	if !strings.Contains(real.Body, "# def fake():") || !strings.HasSuffix(real.Body, "return s") {
		t.Errorf("real: %q", real.Body)
	}
	noCrumb(t, cs, "inside")
	noCrumb(t, cs, "fake")
}

func TestRustUnits(t *testing.T) {
	cs := chunksOf(t, Rust{}, "src/lib.rs", rsSample)
	if !strings.HasPrefix(cs[0].Body, "//! Crate doc.\nuse std::fmt;") {
		t.Errorf("header: %q", cs[0].Body)
	}
	p := chunkOf(t, cs, "Point (struct)")
	if !strings.HasPrefix(p.Body, "/// A point.\n#[derive(Debug, Clone)]") || !strings.Contains(p.Embed, "A point.") || !strings.Contains(p.Embed, "x: i32") {
		t.Errorf("Point: %q / %q", p.Body, p.Embed)
	}
	for _, crumb := range []string{"Unit (struct)", "Kind (enum)", "> trait Shape", "> impl fmt::Display for Point", "for Point > fmt", "> impl Point", "impl Point > new", "impl Point > later", "lib.rs > answer", "lib.rs > callback"} {
		chunkOf(t, cs, crumb)
	}
	f := chunkOf(t, cs, "for Point > fmt")
	if !strings.HasPrefix(f.Body, "/// Formats.") || !strings.Contains(f.Embed, "Formats.") || strings.Contains(f.Embed, "write!") {
		t.Errorf("fmt: %q / %q", f.Body, f.Embed)
	}
	if h := chunkOf(t, cs, "> impl Point"); !strings.Contains(h.Body, "const ORIGIN") {
		t.Errorf("impl Point's head lacks its associated const: %q", h.Body)
	}
	if l := chunkOf(t, cs, "impl Point > later"); !strings.HasSuffix(l.Body, "}\n}") {
		t.Errorf("the impl's closing brace is not in its last unit: %q", l.Body)
	}
	if last := cs[len(cs)-1]; !strings.HasPrefix(last.Body, "macro_rules!") {
		t.Errorf("the macro is not glue: %q", last.Body)
	}
}

func TestRustStringsCharsAndCommentsNeverMakeUnits(t *testing.T) {
	cs := chunksOf(t, Rust{}, "src/s.rs", rsStrings)
	real := chunkOf(t, cs, "s.rs > real")
	if !strings.Contains(real.Body, "let c = '}';") || !strings.Contains(real.Body, "nested */ } */\n}") {
		t.Errorf("real: %q", real.Body)
	}
	chunkOf(t, cs, "s.rs > second")
	noCrumb(t, cs, "fake")
}

func TestHeuristicOversizedUnitsSplitAtStatements(t *testing.T) {
	var ts, py, rs strings.Builder
	ts.WriteString("/** Big. */\nfunction big(n) {\n")
	py.WriteString("def big(n):\n    \"\"\"Big.\"\"\"\n")
	rs.WriteString("/// Big.\nfn big(n: i32) -> i32 {\n")
	for i := 0; i < 40; i++ {
		ts.WriteString("  const v = step(n);\n  if (v) { log(v); }\n")
		py.WriteString("    v = step(n)\n    if v:\n        log(v)\n")
		rs.WriteString("    let v = step(n);\n    if v > 0 { log(v); }\n")
	}
	ts.WriteString("  return n;\n}\n")
	py.WriteString("    return n\n")
	rs.WriteString("    n\n}\n")
	for _, c := range []struct {
		ch   search.Chunker
		path string
		src  string
		sig  string
	}{
		{TypeScript{}, "a.js", ts.String(), "function big(n)"},
		{Python{}, "a.py", py.String(), "def big(n)"},
		{Rust{}, "a.rs", rs.String(), "fn big(n: i32) -> i32"},
	} {
		cs, _ := c.ch.Chunk(search.Doc{Path: c.path, Text: []byte(c.src), Tokens: 80})
		partition(t, c.path, []byte(c.src), cs)
		if len(cs) < 3 {
			t.Fatalf("%s: no split (%d chunks)", c.path, len(cs))
		}
		for i, f := range cs {
			if !strings.Contains(f.Breadcrumb, c.sig) {
				t.Errorf("%s fragment %d breadcrumb lacks the signature: %q", c.path, i, f.Breadcrumb)
			}
			if i > 0 && (!strings.Contains(f.Embed, "step") || !strings.Contains(f.Embed, c.sig)) {
				t.Errorf("%s fragment %d Embed: %q", c.path, i, f.Embed)
			}
		}
		if !strings.Contains(cs[0].Embed, "Big.") {
			t.Errorf("%s first fragment's Embed lacks the doc: %q", c.path, cs[0].Embed)
		}
	}
}
