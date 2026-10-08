package code

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
)

// docLines is a long doc of "documentation words", n lines with the given comment prefix.
func docLines(prefix string, n int) string {
	return rows(n, func(int) string { return prefix + "documentation words documentation words" })
}

// tailCase is a unit whose embedding ends with a tail boundTail keeps: a function's signature,
// or the head line of a class or impl, under a doc far longer than any budget it is chunked at.
type tailCase struct {
	name string
	c    search.Chunker
	path string
	src  string
	unit string // a word of the unit's breadcrumb
	tail string
}

func tailCases() []tailCase {
	return []tailCase{
		{"go func", Go{}, "pkg/p/run.go", "package p\n\n// Run runs.\n" + docLines("// ", 60) +
			"func Run(ctx context.Context, name string, opts ...Option) (Result, error) {\n\treturn nil, nil\n}\n",
			"Run", "func Run(ctx context.Context, name string, opts ...Option) (Result, error)"},
		{"ts function", TypeScript{}, "web/run.ts", "/**\n * Run runs.\n" + docLines(" * ", 60) + " */\n" +
			"export function run(ctx: Context, name: string, opts: Options): Promise<Result> {\n  return go(ctx);\n}\n",
			"run", "export function run(ctx: Context, name: string, opts: Options): Promise<Result>"},
		{"ts class", TypeScript{}, "web/shape.ts", "/**\n * Shape draws.\n" + docLines(" * ", 60) + " */\n" +
			"export class Shape extends Base implements Drawable {\n  area(): number { return 1; }\n}\n",
			"Shape", "export class Shape extends Base implements Drawable"},
		{"py def", Python{}, "py/run.py", "def run(ctx, name, opts, *args, **kwargs):\n    \"\"\"Run runs.\n" +
			docLines("    ", 60) + "    \"\"\"\n    return go(ctx)\n",
			"run", "def run(ctx, name, opts, *args, **kwargs)"},
		{"py class", Python{}, "py/shape.py", "class Shape(Base, Drawable, metaclass=Meta):\n    \"\"\"Shape draws.\n" +
			docLines("    ", 60) + "    \"\"\"\n\n    def area(self):\n        return 1\n",
			"Shape", "class Shape(Base, Drawable, metaclass=Meta)"},
		{"rs fn", Rust{}, "src/run.rs", "/// Run runs.\n" + docLines("/// ", 60) +
			"pub fn run(ctx: &Context, name: &str, opts: Options) -> Result<Output, Error> {\n    go(ctx)\n}\n",
			"run", "pub fn run(ctx: &Context, name: &str, opts: Options) -> Result<Output, Error>"},
		{"rs impl", Rust{}, "src/point.rs", "/// Point formats.\n" + docLines("/// ", 60) +
			"impl fmt::Display for Point {\n    fn fmt(&self) -> i32 { 1 }\n}\n",
			"Display", "impl fmt::Display for Point"},
	}
}

// Under a doc too long for any budget, the tail survives whatever room the budget leaves: with
// room for something before it, the embedding ends with it whole; with room for it alone (the
// budget it exactly, or a token or two over), it is the whole embedding and the doc is dropped;
// with less room than it needs, it is cut at a word, and no doc text takes its place. Every gap
// from three tokens short to four over is met, in every language's tail.
func TestTheTailSurvivesEveryBudget(t *testing.T) {
	for _, tc := range tailCases() {
		tl := chunk.Tokens([]byte(tc.tail))
		met := map[int]bool{}
		for limit := 1; limit <= 150; limit++ {
			cs, err := tc.c.Chunk(search.Doc{Path: tc.path, Text: []byte(tc.src), Tokens: limit})
			if err != nil {
				t.Fatal(err)
			}
			var c search.Chunk
			for _, x := range cs {
				if strings.Contains(x.Breadcrumb, tc.unit) && x.Embed != "" {
					c = x
					break
				}
			}
			if c.Embed == "" {
				continue
			}
			b := budgetFor(c.Breadcrumb, limit)
			d := b - tl
			if d < -3 || d > 4 {
				continue
			}
			met[d] = true
			e := c.Embed
			if n := chunk.Tokens([]byte(e)); n > b {
				t.Errorf("%s, limit %d (gap %d): %d tokens over a budget of %d", tc.name, limit, d, n, b)
			}
			switch {
			case d >= 3:
				if !strings.HasSuffix(e, "\n"+tc.tail) {
					t.Errorf("%s, limit %d (gap %d): does not end with the tail: %q", tc.name, limit, d, e)
				}
			case d >= 0:
				if e != tc.tail {
					t.Errorf("%s, limit %d (gap %d): want the tail alone, got %q", tc.name, limit, d, e)
				}
			default:
				words := strings.TrimSuffix(strings.TrimSuffix(e, elided), " ")
				if strings.Contains(e, "documentation") || !strings.HasSuffix(e, elided) || !strings.HasPrefix(tc.tail, words) {
					t.Errorf("%s, limit %d (gap %d): want the tail cut at a word, got %q", tc.name, limit, d, e)
				}
			}
		}
		for d := -3; d <= 4; d++ {
			if !met[d] {
				t.Errorf("%s: no limit leaves a gap of %d tokens", tc.name, d)
			}
		}
	}
}
