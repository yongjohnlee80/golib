package js_test

import (
	"errors"
	"github.com/yongjohnlee80/golib/parse/js"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// The dialect suite. Each exported dialect gets both halves of its declared
// subset: what it covers, and what it refuses. The second half is the one that
// earns the export — a dialect that quietly mis-parses the constructs it does
// not support is worse than no dialect at all, because the consumer gets a tree
// and no reason to doubt it.

func TestJavaScriptIsWhatTheZeroValueParses(t *testing.T) {
	// A zero Expression and an explicit &JavaScript must agree on a source that
	// uses the JS-only operators, so "the zero value means JavaScript" is checked
	// against behaviour rather than against the nil test in dialect().
	const src = "a ?? b?.c ** 2 === `t${x}`"
	zero, err := js.Expression{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	named, err := js.Expression{Dialect: &js.JavaScript}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if renderExpr(&zero) != renderExpr(&named) {
		t.Errorf("zero value gave %s, &JavaScript gave %s", renderExpr(&zero), renderExpr(&named))
	}
}

func TestCFollowsTheCPrecedenceLadder(t *testing.T) {
	// One case per adjacent pair, as for JavaScript. C's ladder is the one
	// JavaScript inherited, so the rows that matter are the ones where a dialect
	// copied from the wrong neighbour would still look plausible.
	shapes(t, &js.C, map[string]string{
		"a || b && c": "(a || (b && c))",
		"a && b | c":  "(a && (b | c))",
		"a | b ^ c":   "(a | (b ^ c))",
		"a ^ b & c":   "(a ^ (b & c))",
		"a & b == c":  "(a & (b == c))",
		"a == b < c":  "(a == (b < c))",
		"a < b << c":  "(a < (b << c))",
		"a << b + c":  "(a << (b + c))",
		"a + b * c":   "(a + (b * c))",
		"a ? b : c":   "(a ? b : c)",
	})
}

func TestCHasSizeofAndTheAddressAndIndirectionOperators(t *testing.T) {
	shapes(t, &js.C, map[string]string{
		"sizeof x":  "(sizeof x)",
		"sizeof(x)": "(sizeof x)",
		"*p":        "(* p)",
		"&x":        "(& x)",
		"*p + 1":    "((* p) + 1)",
		"&a.b":      "(& a.b)",
		"NULL":      "NULL",
		"sizeofx":   "sizeofx",
		"sizeof_t":  "sizeof_t",
		"a & *p":    "(a & (* p))",
	})
}

func TestCDoesNotHaveTheJavaScriptOnlyOperators(t *testing.T) {
	// Refusals, not silent reinterpretation. `===` is the one worth naming: a
	// dialect that fell back to `==` would accept the source and compare two
	// things loosely, which is the failure C programmers would never look for.
	refuses(t, &js.C, false, "a === b", "a !== b", "a ?? b", "a?.b", "a >>> b", "`t`")
	// `a?.b` is worth its own word: C HAS `?:`, so the refusal has to come from
	// the conditional finding no `:` rather than from `?.` being unknown. A
	// dialect that ignored the Optional flag would parse it as a member chain.
}

func TestCReadsADoubledStarAsIndirectionRatherThanExponentiation(t *testing.T) {
	// `2 ** 3` is `2 * (*3)` in C — a multiplication by a dereference. This is
	// the SAME source parsing differently per dialect, which is the strongest
	// evidence the dialect table is really consulted and not a decoration.
	shapes(t, &js.C, map[string]string{"2 ** 3": "(2 * (* 3))"})
	shapes(t, nil, map[string]string{"2 ** 3": "(2 ** 3)"})
}

func TestCRefusesWhatItsDeclaredSubsetExcludes(t *testing.T) {
	// Every construct the doc comment names as out of scope, pinned. Without
	// this the subset is a claim in prose that nothing checks, and the first one
	// to start mis-parsing does so quietly. The claim is "refused, never a wrong
	// tree" — which of the two refusals each gets is not part of it, since `a++`
	// legitimately looks like an unfinished `a + +…` to a parser with no
	// increment operator.
	refusesAnyhow(t, &js.C,
		"(int)x",       // cast
		"p->q",         // arrow member access
		"a, b",         // comma operator
		"a++",          // increment
		"a = b",        // assignment
		"a += b",       // compound assignment
		"(T){1}",       // compound literal
		"sizeof(int*)", // sizeof applied to a type
	)
}

func TestGoHasNoConditionalOperator(t *testing.T) {
	// Go's grammar has no `?:` at all, so this must be a refusal. A dialect flag
	// read the wrong way round would accept it and hand back a tree for source
	// the language cannot express.
	refuses(t, &js.Go, false, "a ? b : c", "a ? b : c ? d : e")
}

func TestGoHasAndNot(t *testing.T) {
	shapes(t, &js.Go, map[string]string{
		"a &^ b":      "(a &^ b)",
		"a &^ b &^ c": "((a &^ b) &^ c)",
		"a & b":       "(a & b)",
		"a && b":      "(a && b)",
		"a &^ b && c": "((a &^ b) && c)",
	})
}

func TestGoGroupsItsOperatorsIntoFiveLevelsNotTen(t *testing.T) {
	// The C ladder gives every bitwise operator a level of its own, which reads
	// as harmless and is not: Go puts `|` and `^` with `+`, and `&`, `&^`, `<<`
	// and `>>` with `*`. Under the C shape all five of these rows come out
	// right-leaning, and every one of them still evaluates — to a different
	// number. Testing only `a + b * c` cannot see any of it.
	shapes(t, &js.Go, map[string]string{
		"a | b + c":   "((a | b) + c)",
		"a ^ b - c":   "((a ^ b) - c)",
		"a + b | c":   "((a + b) | c)",
		"a & b * c":   "((a & b) * c)",
		"a << b * c":  "((a << b) * c)",
		"a * b &^ c":  "((a * b) &^ c)",
		"a | b == c":  "((a | b) == c)",
		"a + b < c":   "((a + b) < c)",
		"a || b && c": "(a || (b && c))",
	})
}

func TestGoSpellsTheNullLiteralNil(t *testing.T) {
	x := js.Expression{Dialect: &js.Go}
	e, err := x.Parse([]byte("nil"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != js.ExprNull {
		t.Errorf("nil parsed as %v, want null", e.Kind)
	}
	// And `null` is just a name in Go — a literal table shared between dialects
	// would make it a constant here and change what the tree means.
	e, err = x.Parse([]byte("null"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != js.ExprIdent {
		t.Errorf("null parsed as %v in Go, want an identifier", e.Kind)
	}
}

func TestGoDoesNotHaveTheJavaScriptOnlyOperators(t *testing.T) {
	refuses(t, &js.Go, false, "a === b", "a ?? b", "a?.b", "a >>> b", "a instanceof b", "typeof a")
	// `2 ** 3` is NOT among them: Go has no exponent operator but does have a
	// unary `*`, so the source is a multiplication by a dereference — the same
	// reading C gives it. Listing it as a refusal would be wrong, and the
	// dialects that share this reading are worth naming together.
	shapes(t, &js.Go, map[string]string{"2 ** 3": "(2 * (* 3))"})
}

func TestGoRefusesWhatItsDeclaredSubsetExcludes(t *testing.T) {
	refusesAnyhow(t, &js.Go,
		"T{a: 1}",  // composite literal
		"a.(int)",  // type assertion
		"a[i:j]",   // slice expression
		"<-ch",     // channel receive
		"f(xs...)", // variadic call
		"a = b",    // assignment
		"a := b",   // short variable declaration
	)
}

func TestEveryDialectRefusesTemplatesUnlessItDeclaresThem(t *testing.T) {
	// The flag, not the character. A parser that read backticks unconditionally
	// would accept them in C and Go, where a backtick is not a token at all.
	for _, d := range []*js.ExprDialect{&js.C, &js.Go} {
		if d.Templates {
			t.Fatalf("%s declares templates; this test has the wrong dialects", d.Name)
		}
	}
	refuses(t, &js.C, false, "`t`", "`a${x}`")
	refuses(t, &js.Go, false, "`t`", "`a${x}`")
	shapes(t, nil, map[string]string{"`t`": "`t`"})
}

func TestADialectWithoutOptionalChainingReadsQuestionDotAsAConditional(t *testing.T) {
	// C has `?:` and no `?.`, so `a ?.5 : 1` is an ordinary conditional whose
	// consequent is `.5`. A `?.` check that did not consult the dialect would
	// refuse valid C, and the message would blame a construct C does not have.
	shapes(t, &js.C, map[string]string{"a ?.5 : 1": "(a ? .5 : 1)"})
	shapes(t, &js.C, map[string]string{"a ? .5 : 1": "(a ? .5 : 1)"})
}

func TestANewDialectIsATableEntryAndNeedsNoParserChange(t *testing.T) {
	// The file's central design claim, tested rather than asserted: a dialect
	// with an operator spelling none of the built-in ones have — a pipeline `|>`
	// LOOSER than the `|` it starts with — parses correctly with no edit to the
	// parser. Under a guard that named `&` and `|` in code instead of deriving
	// the rule from the table, `a |> b` comes out as `a | (> b)`, or is refused;
	// none of the three shipped dialects can tell the difference, because none
	// of them has an operator that extends another across levels this way.
	pipeline := js.ExprDialect{
		Name: "pipe",
		Binary: [][]string{
			{"|>"},
			{"||"},
			{"|"},
			{"+", "-"},
		},
		Literals: map[string]js.ExprKind{"true": js.ExprBool},
	}
	shapes(t, &pipeline, map[string]string{
		"a |> b":      "(a |> b)",
		"a | b |> c":  "((a | b) |> c)",
		"a |> b | c":  "(a |> (b | c))",
		"a | b":       "(a | b)",
		"a || b":      "(a || b)",
		"a |> b || c": "(a |> (b || c))",
	})
}

func TestADialectWithNoBinaryOperatorsStillParsesPrimariesAndPostfix(t *testing.T) {
	// The binary ladder is a loop over a slice, and an empty slice is the case
	// that reveals whether the recursion's base is the table's length or a
	// hardcoded floor.
	bare := js.ExprDialect{Name: "bare"}
	shapes(t, &bare, map[string]string{
		"a":       "a",
		"a.b.c":   "a.b.c",
		"f(1, 2)": "f(1, 2)",
		"[1, 2]":  "[1, 2]",
		"{k: 1}":  "{k: 1}",
		"(a)":     "a",
		"'s'":     `"s"`,
		"a[0]":    "a[0]",
	})
	// With no operator table there is nothing to join two primaries, and no
	// unary, conditional, template or optional chain either.
	// `true` is NOT here: with no Literals table it is simply a name, which is
	// the behaviour a dialect gets by declaring nothing rather than a refusal.
	refuses(t, &bare, false, "a + b", "-a", "a ? b : c", "a?.b", "`t`")
	shapes(t, &bare, map[string]string{"true": "true"})
	if strings.Contains(js.Expression{Dialect: &bare}.FormatName(), "js") {
		t.Error("FormatName fell back to the default dialect instead of using the one given")
	}
}

// refusesAnyhow asserts only that each source is rejected, without pinning which
// of the two refusals it gets.
//
// It is for a dialect's declared out-of-scope list, where the promise is "a
// parse.SyntaxError, never a wrong tree". Insisting on a particular Incomplete value
// there would freeze an incidental diagnosis: `a++` has no increment operator to
// fail on, so it fails as an unfinished `a + +…`, and that is a fine thing for
// it to say.
func refusesAnyhow(t *testing.T, d *js.ExprDialect, sources ...string) {
	t.Helper()
	x := js.Expression{Dialect: d}
	for _, src := range sources {
		e, err := x.Parse([]byte(src))
		if err == nil {
			t.Errorf("%s: %q parsed as %s, want a refusal", x.FormatName(), src, renderExpr(&e))
			continue
		}
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%s: %q: %v is not a parse.SyntaxError", x.FormatName(), src, err)
		}
	}
}
