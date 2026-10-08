package code

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
)

// rows is n lines of f(i).
func rows(n int, f func(i int) string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(f(i))
		b.WriteString("\n")
	}
	return b.String()
}

// longDoc is a doc of n words.
func longDoc(n int) string { return strings.TrimSpace(strings.Repeat("words of doc ", n/3)) }

// oversized are declarations far over the default budget, in every language: a table, a type, an
// interface, a struct, an enum, and functions with docs longer than the budget.
func oversized() []fixture {
	return []fixture{
		{"go-table", Go{}, "pkg/g/tables.go", "package g\n\n// gbRanges are the grapheme break ranges.\nvar gbRanges = [][3]int{\n" +
			rows(2000, func(i int) string { return fmt.Sprintf("\t{0x%x, 0x%x, %d},", i*16, i*16+15, i%13) }) + "}\n"},
		{"go-type", Go{}, "pkg/g/conf.go", "package g\n\n// Conf is everything.\ntype Conf struct {\n" +
			rows(600, func(i int) string { return fmt.Sprintf("\tField%d string `json:\"field%d\"`", i, i) }) + "}\n"},
		{"go-doc", Go{}, "pkg/g/doc.go", "package g\n\n// Run " + longDoc(1500) +
			"\nfunc Run(ctx context.Context, n int) error {\n\treturn nil\n}\n"},
		{"ts-interface", TypeScript{}, "web/conf.ts", "/** Conf is everything. */\nexport interface Conf {\n" +
			rows(600, func(i int) string { return fmt.Sprintf("  field%d: string;", i) }) + "}\n"},
		{"ts-doc", TypeScript{}, "web/run.ts", "/** Run " + longDoc(1500) + " */\nexport function run(n: number): number {\n  return n;\n}\n"},
		{"py-doc", Python{}, "py/run.py", "def run(n):\n    \"\"\"Run " + longDoc(1500) + "\"\"\"\n    return n\n"},
		{"rs-struct", Rust{}, "src/conf.rs", "/// Conf is everything.\npub struct Conf {\n" +
			rows(600, func(i int) string { return fmt.Sprintf("    pub field%d: String,", i) }) + "}\n"},
		{"rs-enum", Rust{}, "src/kind.rs", "/// Kind is every kind.\npub enum Kind {\n" +
			rows(600, func(i int) string { return fmt.Sprintf("    Variant%d(u32),", i) }) + "}\n"},
		{"rs-doc", Rust{}, "src/run.rs", "/// Run " + longDoc(1500) + "\npub fn run(n: i32) -> i32 {\n    n\n}\n"},
	}
}

// No chunk's embedding is larger than the budget its body is held to, in any language, at any
// budget, the oversized declarations included.
func TestNoEmbedIsLargerThanItsBodysBudget(t *testing.T) {
	for _, f := range append(append(allFixtures(), bigFunctions()...), oversized()...) {
		for _, limit := range []int{0, 80, 40, 12} {
			cs, err := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src), Tokens: limit})
			if err != nil {
				t.Fatal(err)
			}
			partition(t, f.name, []byte(f.src), cs)
			lim := limit
			if lim == 0 {
				lim = chunk.DefaultTokens
			}
			for _, c := range cs {
				if n, b := chunk.Tokens([]byte(c.Embed)), budgetFor(c.Breadcrumb, lim); c.Embed != "" && n > b {
					t.Errorf("%s at %d: %q embeds %d tokens over a budget of %d", f.name, limit, c.Breadcrumb, n, b)
				}
			}
		}
	}
}

// Every fragment of an oversized table embeds within the budget and names the table; the first
// says its doc, its type and its first rows, and none holds the table.
func TestAnOversizedTableEmbedsItsHeadNotItsRows(t *testing.T) {
	f := oversized()[0]
	cs := goChunks(t, f.src, 0)
	var frags []search.Chunk
	body, embed := 0, 0
	for _, c := range cs {
		if strings.Contains(c.Breadcrumb, "gbRanges") {
			frags = append(frags, c)
			body += chunk.Tokens([]byte(c.Breadcrumb + "\n" + c.Body))
			embed += chunk.Tokens([]byte(c.EmbedText()))
		}
	}
	if len(frags) < 10 {
		t.Fatalf("the table made %d fragments", len(frags))
	}
	for i, c := range frags {
		if n, b := chunk.Tokens([]byte(c.Embed)), budgetFor(c.Breadcrumb, chunk.DefaultTokens); c.Embed == "" || n > b {
			t.Errorf("fragment %d embeds %d tokens (budget %d)", i, n, b)
		}
		if !strings.Contains(c.Embed, "gbRanges") {
			t.Errorf("fragment %d's embed does not name the table: %q", i, c.Embed)
		}
		if strings.Contains(c.Embed, "0x7cf0") {
			t.Errorf("fragment %d embeds the table's last row: %q", i, c.Embed)
		}
	}
	want := "pkg/x > var gbRanges\ngbRanges are the grapheme break ranges.\nvar gbRanges = [][3]int{{0x0, 0xf, 0}, {0x10, 0x1f, 1}, {0x20, 0x2f, 2}, …}"
	if frags[0].Embed != want {
		t.Errorf("first fragment embeds\n%q\nwant\n%q", frags[0].Embed, want)
	}
	if last := frags[len(frags)-1].Embed; !strings.HasPrefix(last, "pkg/x > var gbRanges\n{0x") || !strings.HasSuffix(last, elided) {
		t.Errorf("last fragment embeds %q", last)
	}
	if embed*10 > body {
		t.Errorf("the table embeds %d tokens against %d of body", embed, body)
	}
}

// A const or var embeds its names, type and values with a literal reduced to its first elements
// and a function literal to its signature; a short value stays as written.
func TestValuesEmbedTheirHeadNotTheirLiteral(t *testing.T) {
	src := `package g

// Limit is small.
const Limit = 5

var table = map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}

var hook = func(a int) error {
	if a > 0 {
		return nil
	}
	return nil
}

// Kinds.
const (
	// KindA is the first.
	KindA Kind = iota
	KindB
)

var name = "` + strings.Repeat("n", 120) + `"
`
	cs := goChunks(t, src, 0)
	want := map[string]string{
		"const Limit":        "pkg/x > const Limit\nLimit is small.\nconst Limit = 5",
		"var table":          `pkg/x > var table` + "\n" + `var table = map[string]int{"a": 1, "b": 2, "c": 3, …}`,
		"var hook":           "pkg/x > var hook\nvar hook = func(a int) error { … }",
		"const KindA, KindB": "pkg/x > const KindA, KindB\nKinds.\nconst (\nKindA is the first.\nKindA Kind = iota\nKindB\n)",
		"var name":           "pkg/x > var name\nvar name = \"" + strings.Repeat("n", 79) + "…",
	}
	for crumb, w := range want {
		if got := chunkOf(t, cs, crumb).Embed; got != w {
			t.Errorf("%s embeds\n%q\nwant\n%q", crumb, got, w)
		}
	}
}

// A function whose doc is longer than the budget keeps its signature whole, in every language:
// the doc is cut instead. Every fragment of it embeds the signature.
func TestALongDocNeverCostsAFunctionItsSignature(t *testing.T) {
	sigs := map[string]string{
		"go-doc": "func Run(ctx context.Context, n int) error",
		"ts-doc": "export function run(n: number): number",
		"py-doc": "def run(n)",
		"rs-doc": "pub fn run(n: i32) -> i32",
	}
	for _, f := range oversized() {
		sig, ok := sigs[f.name]
		if !ok {
			continue
		}
		cs, _ := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src)})
		found := 0
		for _, c := range cs {
			if !strings.Contains(c.Breadcrumb, "un") || c.Embed == "" {
				continue
			}
			if found++; found == 1 {
				if !strings.HasSuffix(c.Embed, sig) {
					t.Errorf("%s: the embed does not end with the signature: %q", f.name, c.Embed[max(0, len(c.Embed)-120):])
				}
				if !strings.Contains(c.Embed, "words of doc") || !strings.Contains(c.Embed, elided) {
					t.Errorf("%s: the doc is not kept and cut: %q", f.name, c.Embed[:min(200, len(c.Embed))])
				}
			} else if !strings.Contains(c.Embed, sig) {
				t.Errorf("%s: fragment %d lacks the signature: %q", f.name, found, c.Embed)
			}
			if n, b := chunk.Tokens([]byte(c.Embed)), budgetFor(c.Breadcrumb, chunk.DefaultTokens); n > b {
				t.Errorf("%s: %d tokens over %d", f.name, n, b)
			}
		}
		if found == 0 {
			t.Errorf("%s: no function chunk", f.name)
		}
	}
}

// An oversized type, interface, struct or enum embeds within the budget in every fragment, each
// naming the declaration, and no fragment repeats the declaration whole.
func TestOversizedDeclarationsEmbedEachFragmentsHead(t *testing.T) {
	names := map[string]string{"go-type": "Conf", "ts-interface": "Conf", "rs-struct": "Conf", "rs-enum": "Kind"}
	for _, f := range oversized() {
		name, ok := names[f.name]
		if !ok {
			continue
		}
		cs, _ := f.c.Chunk(search.Doc{Path: f.path, Text: []byte(f.src)})
		frags := 0
		for _, c := range cs {
			if !strings.Contains(c.Breadcrumb, name) {
				continue
			}
			frags++
			if !strings.Contains(c.Embed, name) {
				t.Errorf("%s: a fragment's embed does not name %s: %q", f.name, name, c.Embed)
			}
			if strings.Contains(c.Embed, "599") || strings.Count(c.Embed, "\n") > previewLines+2 && frags > 1 {
				t.Errorf("%s: fragment %d embeds more than its head: %q", f.name, frags, c.Embed)
			}
		}
		if frags < 5 {
			t.Errorf("%s: %d fragments", f.name, frags)
		}
	}
}

// A function with no statements to cut at is cut at lines, and every fragment keeps its doc and
// signature: a fragment's own lines are control flow, which a function's embedding leaves out.
func TestAFunctionCutAtLinesKeepsItsDocAndSignature(t *testing.T) {
	src := "package g\n\n// Pick picks.\nfunc Pick(n int) string {\n\tswitch n {\n" +
		rows(300, func(i int) string { return fmt.Sprintf("\tcase %d:\n\t\treturn \"v%d\"", i, i) }) + "\t}\n}\n"
	cs := goChunks(t, src, 0)
	var frags []search.Chunk
	for _, c := range cs {
		if strings.Contains(c.Breadcrumb, "Pick") {
			frags = append(frags, c)
		}
	}
	if len(frags) < 3 {
		t.Fatalf("%d fragments", len(frags))
	}
	want := "pkg/x > Pick\nPick picks.\nfunc Pick(n int) string"
	for i, c := range frags {
		if c.Embed != want {
			t.Errorf("fragment %d embeds %q", i, c.Embed)
		}
	}
}

// A unit that embeds its body (a file header with a long package doc) embeds each fragment's body:
// prose is what it is about.
func TestALongHeaderEmbedsItsBody(t *testing.T) {
	src := "// Package g is documented at length.\n" + rows(400, func(int) string { return "// words of doc words of doc" }) +
		"package g\n\nfunc F() {}\n"
	cs := goChunks(t, src, 0)
	n := 0
	for _, c := range cs {
		if c.Breadcrumb == "pkg/x" {
			n++
			if c.Embed != "" {
				t.Errorf("header fragment %d embeds %q, not its body", n, c.Embed)
			}
		}
	}
	if n < 3 {
		t.Fatalf("the header made %d fragments", n)
	}
}
