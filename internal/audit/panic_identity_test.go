package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// These are the controls for the panic budget's IDENTITY function, committed
// rather than run once in a shell. Every earlier design of that function was
// defeated by a mutation nobody had written down, so the mutations live here
// now: each case states two snippets that must — or must not — collide.
//
// A case that expects DIFFERENT identities is a guard against a category
// migrating onto code it was never written about. A case that expects the SAME
// identity is just as important: it pins what may be edited freely, and
// without those the honest fix would be to hash the whole file and force a
// re-read on every commit.

// identityOf parses src, finds its single panic, and returns func path +
// fingerprint exactly as the census computes them.
func identityOf(t *testing.T, src string) (string, string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	var fn, print string
	found := 0
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "panic" {
			return true
		}
		found++
		fp, gp := scopePath(fset, stack)
		var b strings.Builder
		for i, a := range call.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(render(fset, a))
		}
		fn, print = fp, fingerprint(b.String(), gp)
		return true
	})
	if found != 1 {
		t.Fatalf("fixture must contain exactly one panic, found %d:\n%s", found, src)
	}
	return fn, print
}

func wrap(body string) string {
	return "package p\n\nfunc F(x, y bool, n int, ch chan int) {\n" + body + "\n}\n"
}

type identityCase struct {
	name     string
	a, b     string
	wantSame bool
	why      string
}

func TestPanicIdentity_Cases(t *testing.T) {
	cases := []identityCase{
		{
			name:     "outer guard flipped",
			a:        wrap("\tif x {\n\t\tif y {\n\t\t\tpanic(\"same\")\n\t\t}\n\t}"),
			b:        wrap("\tif !x {\n\t\tif y {\n\t\t\tpanic(\"same\")\n\t\t}\n\t}"),
			wantSame: false,
			why:      "only the INNERMOST guard used to be hashed, so these collided (r1 MF1)",
		},
		{
			name:     "then/else swapped",
			a:        wrap("\tif x {\n\t\tpanic(\"same\")\n\t} else {\n\t\t_ = n\n\t}"),
			b:        wrap("\tif x {\n\t\t_ = n\n\t} else {\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "the edge taken is part of the control path, not decoration",
		},
		{
			name:     "switch subject changed",
			a:        wrap("\tswitch n {\n\tcase 1:\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tswitch n + 1 {\n\tcase 1:\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "the switch tag decides which case is reached",
		},
		{
			name:     "second expression of a multi-expression case",
			a:        wrap("\tswitch n {\n\tcase 1, 2:\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tswitch n {\n\tcase 1, 3:\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "only the FIRST case expression used to be hashed",
		},
		{
			name:     "default clause vs a real case",
			a:        wrap("\tswitch n {\n\tcase 1:\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tswitch n {\n\tdefault:\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "a default arm is a different path from a matched one",
		},
		{
			name:     "select arm changed",
			a:        wrap("\tselect {\n\tcase <-ch:\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tselect {\n\tcase ch <- 1:\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "select arms were not represented at all",
		},
		{
			name:     "nested loop condition changed",
			a:        wrap("\tfor i := 0; i < n; i++ {\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tfor i := 0; i < n+1; i++ {\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "a loop is a control ancestor like any other",
		},
		{
			name:     "range subject changed",
			a:        wrap("\tfor range []int{1} {\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tfor range []int{2} {\n\t\tpanic(\"same\")\n\t}"),
			wantSame: false,
			why:      "range's subject is part of the path",
		},
		{
			name:     "literal inner whitespace",
			a:        wrap("\tpanic(\"a  b\")"),
			b:        wrap("\tpanic(\"a b\")"),
			wantSame: false,
			why:      "normalising whitespace reached INSIDE literals and collided them (r1 MF2)",
		},
		{
			name:     "message reworded",
			a:        wrap("\tpanic(\"nil DataConn\")"),
			b:        wrap("\tpanic(\"Table is required\")"),
			wantSame: false,
			why:      "a reworded panic must retire its row so the reason is re-read",
		},
		{
			name:     "panic hoisted out of a closure",
			a:        wrap("\tdefer func() {\n\t\tpanic(\"same\")\n\t}()"),
			b:        wrap("\tpanic(\"same\")\n\t_ = x"),
			wantSame: false,
			why:      "a closure is its own scope; moving out of it changes reachability",
		},
		// ── and the cases that must NOT churn ──
		{
			name:     "syntactic whitespace outside literals",
			a:        wrap("\tif x  &&  y {\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\tif x && y {\n\t\tpanic(\"same\")\n\t}"),
			wantSame: true,
			why:      "the printer normalises syntax; only literal text is preserved verbatim",
		},
		{
			name:     "unrelated statements added before the panic",
			a:        wrap("\tif x {\n\t\tpanic(\"same\")\n\t}"),
			b:        wrap("\t_ = n\n\t_ = ch\n\tif x {\n\t\tpanic(\"same\")\n\t}"),
			wantSame: true,
			why:      "line shifts must not churn rows, or people regenerate without reading",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fnA, pA := identityOf(t, tc.a)
			fnB, pB := identityOf(t, tc.b)
			same := fnA == fnB && pA == pB
			if same != tc.wantSame {
				verb := "differ"
				if tc.wantSame {
					verb = "match"
				}
				t.Errorf("identities should %s but did not — %s\n  a: %s %s\n  b: %s %s",
					verb, tc.why, fnA, pA, fnB, pB)
			}
		})
	}
}

// TestPanicIdentity_ClosureIndexIsPerScope pins the closure path: two literals
// in the same parent scope get distinct indices, and inserting a literal
// BEFORE them shifts those indices — which is correct, they are different
// scopes — while a literal in an unrelated sibling statement does not.
func TestPanicIdentity_ClosureIndexIsPerScope(t *testing.T) {
	first := wrap("\tgo func() { _ = n }()\n\tdefer func() {\n\t\tpanic(\"same\")\n\t}()")
	only := wrap("\tdefer func() {\n\t\tpanic(\"same\")\n\t}()")
	fnA, _ := identityOf(t, first)
	fnB, _ := identityOf(t, only)
	if fnA == fnB {
		t.Errorf("a closure preceded by another sibling closure must not share its index: "+
			"%s vs %s", fnA, fnB)
	}
	if !strings.Contains(fnA, "func$") || !strings.Contains(fnB, "func$") {
		t.Errorf("closure segments missing from the path: %s / %s", fnA, fnB)
	}
	t.Logf("with a preceding sibling closure: %s; alone: %s", fnA, fnB)
}

// TestPanicIdentity_FingerprintGrammar pins the width the inventory validates.
func TestPanicIdentity_FingerprintGrammar(t *testing.T) {
	_, p := identityOf(t, wrap("\tpanic(\"x\")"))
	if !printRe.MatchString(p) {
		t.Errorf("fingerprint %q does not match the grammar the inventory enforces", p)
	}
}
