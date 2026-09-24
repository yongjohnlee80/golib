package parse_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// renderExpr prints a tree back as a fully parenthesised expression.
//
// Every precedence and associativity claim below is checked against this string
// rather than against node kinds, because a parser that binds `-a * b` as
// `-(a * b)` still produces a unary wrapping a binary — the right KINDS in the
// wrong SHAPE. Only the parentheses tell those two trees apart, and that exact
// defect was live in this file until the rendered form exposed it.
func renderExpr(e *parse.Expr) string {
	if e == nil {
		return "<nil>"
	}
	switch e.Kind {
	case parse.ExprNumber, parse.ExprBool, parse.ExprNull, parse.ExprUndefined, parse.ExprIdent:
		return e.Raw
	case parse.ExprString:
		return `"` + e.Raw + `"`
	case parse.ExprTemplate:
		var b strings.Builder
		b.WriteString("`")
		for i, chunk := range e.Chunks {
			b.WriteString(chunk)
			if i < len(e.Args) {
				b.WriteString("${" + renderExpr(&e.Args[i]) + "}")
			}
		}
		b.WriteString("`")
		return b.String()
	case parse.ExprUnary:
		return "(" + e.Raw + " " + renderExpr(e.Left) + ")"
	case parse.ExprBinary, parse.ExprLogical:
		return "(" + renderExpr(e.Left) + " " + e.Raw + " " + renderExpr(e.Right) + ")"
	case parse.ExprConditional:
		return "(" + renderExpr(e.Left) + " ? " + renderExpr(e.Right) + " : " + renderExpr(e.Alt) + ")"
	case parse.ExprMember:
		if e.Computed {
			return renderExpr(e.Left) + "[" + renderExpr(e.Right) + "]"
		}
		dot := "."
		if e.Optional {
			dot = "?."
		}
		return renderExpr(e.Left) + dot + e.Name
	case parse.ExprCall:
		return renderExpr(e.Left) + "(" + renderExprList(e.Args) + ")"
	case parse.ExprArray:
		return "[" + renderExprList(e.Args) + "]"
	case parse.ExprObject:
		parts := make([]string, 0, len(e.Props))
		for i := range e.Props {
			p := &e.Props[i]
			key := p.Key
			if p.Computed {
				key = "[" + renderExpr(p.KeyExpr) + "]"
			}
			parts = append(parts, key+": "+renderExpr(&p.Value))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "<" + e.Kind.String() + ">"
}

func renderExprList(args []parse.Expr) string {
	parts := make([]string, 0, len(args))
	for i := range args {
		parts = append(parts, renderExpr(&args[i]))
	}
	return strings.Join(parts, ", ")
}

// shapes asserts that each source renders as its stated tree, under dialect d.
// A nil dialect is JavaScript, which is also what the zero [parse.Expression]
// parses.
func shapes(t *testing.T, d *parse.ExprDialect, cases map[string]string) {
	t.Helper()
	x := parse.Expression{Dialect: d}
	for src, want := range cases {
		e, err := x.Parse([]byte(src))
		if err != nil {
			t.Errorf("%s: %q: %v", x.FormatName(), src, err)
			continue
		}
		if got := renderExpr(&e); got != want {
			t.Errorf("%s: %q\n got %s\nwant %s", x.FormatName(), src, got, want)
		}
	}
}

func shapesJS(t *testing.T, cases map[string]string) {
	t.Helper()
	shapes(t, nil, cases)
}

// refuses asserts that each source is rejected, and WHICH of the two refusals it
// gets: the "keep typing" one or the "this cannot work" one.
func refuses(t *testing.T, d *parse.ExprDialect, incomplete bool, sources ...string) {
	t.Helper()
	x := parse.Expression{Dialect: d}
	for _, src := range sources {
		e, err := x.Parse([]byte(src))
		if err == nil {
			t.Errorf("%s: %q parsed as %s, want a refusal", x.FormatName(), src, renderExpr(&e))
			continue
		}
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%s: %q: %v is not a SyntaxError", x.FormatName(), src, err)
			continue
		}
		if se.Incomplete != incomplete {
			t.Errorf("%s: %q: Incomplete = %v, want %v (%v)", x.FormatName(), src, se.Incomplete, incomplete, err)
		}
	}
}

func TestThePrecedenceLadderBindsEachLevelTighterThanTheOneAboveIt(t *testing.T) {
	// One case per ADJACENT pair of levels, so swapping any two neighbours has
	// somewhere to show up. A suite that only tested `a + b * c` would pass with
	// the bitwise, shift and equality levels in any order at all.
	shapesJS(t, map[string]string{
		"a ?? b || c":    "(a ?? (b || c))",
		"a || b && c":    "(a || (b && c))",
		"a && b | c":     "(a && (b | c))",
		"a | b ^ c":      "(a | (b ^ c))",
		"a ^ b & c":      "(a ^ (b & c))",
		"a & b == c":     "(a & (b == c))",
		"a == b < c":     "(a == (b < c))",
		"a < b << c":     "(a < (b << c))",
		"a << b + c":     "(a << (b + c))",
		"a + b * c":      "(a + (b * c))",
		"a * b ** c":     "(a * (b ** c))",
		"a ** -b":        "(a ** (- b))",
		"a ? b : c || d": "(a ? b : (c || d))",
	})
}

func TestBinaryOperatorsAreLeftAssociative(t *testing.T) {
	// Subtraction and division are the discriminating ones: with `+` or `&&` the
	// two associativities agree numerically, so a right-associative parser looks
	// correct to anything that evaluates rather than inspects the tree.
	shapesJS(t, map[string]string{
		"a - b - c":   "((a - b) - c)",
		"a / b / c":   "((a / b) / c)",
		"a << b >> c": "((a << b) >> c)",
		"a < b > c":   "((a < b) > c)",
		"a && b && c": "((a && b) && c)",
		"a ?? b ?? c": "((a ?? b) ?? c)",
	})
}

func TestExponentIsRightAssociative(t *testing.T) {
	shapesJS(t, map[string]string{
		"2 ** 3 ** 2":      "(2 ** (3 ** 2))",
		"2 ** 3 ** 2 ** 1": "(2 ** (3 ** (2 ** 1)))",
	})
}

func TestExponentDoesNotSwallowTheOperatorsLooserThanItself(t *testing.T) {
	// Right associativity comes for free if the right side is parsed as a WHOLE
	// expression, and that is how this parser had it: `2 ** 3 ** 2` was correct
	// while `2 ** 3 + 1` came out as `2 ** (3 + 1)`. An associativity test alone
	// cannot see that — the right side has to be probed with a LOOSER operator
	// following it.
	shapesJS(t, map[string]string{
		"2 ** 3 + 1":     "((2 ** 3) + 1)",
		"2 ** 3 * 4":     "((2 ** 3) * 4)",
		"2 ** 3 < 4":     "((2 ** 3) < 4)",
		"a ** b ? c : d": "((a ** b) ? c : d)",
		"a ** b ?? c":    "((a ** b) ?? c)",
	})
}

func TestPrefixOperatorsBindTighterThanEveryInfixOperator(t *testing.T) {
	// The same trap as the exponent, and the reason `typeof a === "x"` is here:
	// parsing the operand as a whole expression yields `typeof (a === "x")` — a
	// unary over a binary, the right node kinds, the wrong meaning, and the
	// string "boolean" at every evaluation.
	shapesJS(t, map[string]string{
		"-a * b":           "((- a) * b)",
		"-a + b":           "((- a) + b)",
		"!a && b":          "((! a) && b)",
		"!a ? b : c":       "((! a) ? b : c)",
		`typeof a === "x"`: `((typeof a) === "x")`,
		"~a | b":           "((~ a) | b)",
		"!a instanceof b":  "((! a) instanceof b)",
		"void a + b":       "((void a) + b)",
		"- a.b":            "(- a.b)",
		"-f(x)":            "(- f(x))",
		"a - -b":           "(a - (- b))",
	})
}

func TestPrefixOperatorsStackRightToLeft(t *testing.T) {
	shapesJS(t, map[string]string{
		"!!a":       "(! (! a))",
		"- - a":     "(- (- a))",
		"typeof !a": "(typeof (! a))",
		"!-~a":      "(! (- (~ a)))",
	})
}

func TestTheConditionalIsTheLoosestOperatorAndNestsToTheRight(t *testing.T) {
	shapesJS(t, map[string]string{
		"a ?? b ? c : d":    "((a ?? b) ? c : d)",
		"a ? b : c ? d : e": "(a ? b : (c ? d : e))",
		"a ? b ? c : d : e": "(a ? (b ? c : d) : e)",
		"a ? b + 1 : c * 2": "(a ? (b + 1) : (c * 2))",
		"a || b ? c : d":    "((a || b) ? c : d)",
	})
}

func TestASingleCharacterOperatorDoesNotStealTheFirstHalfOfADoubledOne(t *testing.T) {
	// `&` sits at a TIGHTER level than `&&`, so it is offered the source first
	// and will take one ampersand unless something stops it. Checking that
	// `a && b` parses is not enough: a parser without the guard yields
	// `a & (&b)`, which is still a tree — the shape has to be named.
	shapesJS(t, map[string]string{
		"a & b":      "(a & b)",
		"a && b":     "(a && b)",
		"a&&b":       "(a && b)",
		"a | b":      "(a | b)",
		"a || b":     "(a || b)",
		"a||b":       "(a || b)",
		"a & b && c": "((a & b) && c)",
		"a | b || c": "((a | b) || c)",
		"a && b & c": "(a && (b & c))",
	})
}

func TestTheLongestOperatorSpellingWins(t *testing.T) {
	// Each pair differs only by a trailing character, so reading the short form
	// first leaves a stray `=` or `>` that the next step either rejects with a
	// message about the wrong thing or, worse, absorbs as a prefix operator.
	shapesJS(t, map[string]string{
		"a >>> b": "(a >>> b)",
		"a >> b":  "(a >> b)",
		"a > b":   "(a > b)",
		"a >= b":  "(a >= b)",
		"a === b": "(a === b)",
		"a == b":  "(a == b)",
		"a !== b": "(a !== b)",
		"a != b":  "(a != b)",
		"a <= b":  "(a <= b)",
		"a << b":  "(a << b)",
		"a < b":   "(a < b)",
	})
}

func TestAssignmentIsNotAnExpression(t *testing.T) {
	// The boundary the package documents: a single `=` must not quietly become
	// an equality test, and must not parse at all.
	refuses(t, nil, false, "a = b", "a += b", "a |= b", "a >>= b")
}

func TestWordOperatorsDoNotMatchInsideALongerIdentifier(t *testing.T) {
	// The refusals are the discriminating half. Without the boundary check
	// `a instanceofb` reads as `a instanceof b` — a well-formed tree that
	// evaluates and is not what the source says. Asserting only that
	// `international` parses as a name exercises the identifier lexer, which was
	// never the part at risk, because `in` is not a prefix operator and so is
	// never offered the start of the expression.
	refuses(t, nil, false, "a instanceofb", "a inb", "a in_b", "a in9", "a in$b")
	shapesJS(t, map[string]string{
		"typeofx":       "typeofx",
		"voidmain":      "voidmain",
		"international": "international",
		"invalid":       "invalid",
		"instanceofit":  "instanceofit",
		"a.in":          "a.in",
		"a.instanceof":  "a.instanceof",
		"interest in b": "(interest in b)",
	})
}

func TestWordOperatorsApplyWhenTheyStandAlone(t *testing.T) {
	shapesJS(t, map[string]string{
		"a in b":           "(a in b)",
		"a instanceof b":   "(a instanceof b)",
		"typeof x":         "(typeof x)",
		"void 0":           "(void 0)",
		`"k" in obj`:       `("k" in obj)`,
		"a instanceof b.C": "(a instanceof b.C)",
		"typeof(x)":        "(typeof x)",
	})
}

func TestOptionalChainingIsNotTheConditionalOperator(t *testing.T) {
	shapesJS(t, map[string]string{
		"a?.b":      "a?.b",
		"a?.b?.c":   "a?.b?.c",
		"a.b?.c.d":  "a.b?.c.d",
		"a ?? b":    "(a ?? b)",
		"a ? b : c": "(a ? b : c)",
		"a?.b ?? c": "(a?.b ?? c)",
	})
}

func TestAQuestionMarkBeforeADecimalPointIsAConditionalNotAnOptionalChain(t *testing.T) {
	// `a?.5:1` is a conditional, because `?.` is only the chaining operator when
	// a digit does NOT follow. Matching two characters and stopping there rejects
	// valid source, and the rejection reads "want a name after ?." — a message
	// that sends the reader looking at the wrong construct entirely.
	shapesJS(t, map[string]string{
		"a ?.5 : 1": "(a ? .5 : 1)",
		"a?.5:1":    "(a ? .5 : 1)",
	})
}

func TestOptionalChainingWantsANameNotAnIndexOrACall(t *testing.T) {
	// `a?.[0]` and `a?.(1)` are real JavaScript that this revision does not
	// cover. Pinning the refusal is what keeps "not implemented" from being read
	// later as "accepted, and therefore correct".
	refuses(t, nil, false, "a?.[0]", "a?.(1)")
}

func TestPostfixChainsInTheOrderItIsWritten(t *testing.T) {
	shapesJS(t, map[string]string{
		"a.b.c(1,2)[x]": "a.b.c(1, 2)[x]",
		"f(x)(y)":       "f(x)(y)",
		"x[1][2]":       "x[1][2]",
		"a.b(c).d":      "a.b(c).d",
		"f()":           "f()",
		"a[b.c]":        "a[b.c]",
		"a[b ? c : d]":  "a[(b ? c : d)]",
	})
}

func TestGroupingReplacesThePrecedenceLadder(t *testing.T) {
	shapesJS(t, map[string]string{
		"(a + b) * c":   "((a + b) * c)",
		"(((a)))":       "a",
		"-(a * b)":      "(- (a * b))",
		"(a ? b : c).d": "(a ? b : c).d",
	})
	// The comma operator is not in the grammar, so a parenthesised pair is a
	// refusal rather than "the last one wins" — which is what a group that
	// merely stopped at the comma would quietly produce.
	refuses(t, nil, false, "(a, b)")
}

func TestNumericLiteralsAreKeptExactlyAsWritten(t *testing.T) {
	// Raw is UNDECODED on purpose, so the assertion is on the text: a parser
	// that normalised `0xFF` to 255 would satisfy any test that only checked the
	// kind, and would have already thrown away the consumer's choice of type.
	shapesJS(t, map[string]string{
		"1":          "1",
		"1e-5":       "1e-5",
		"1.5e+3":     "1.5e+3",
		"1.5E-3":     "1.5E-3",
		"0xFF":       "0xFF",
		"0b1010":     "0b1010",
		"0o17":       "0o17",
		"1_000":      "1_000",
		"1_000.5e-3": "1_000.5e-3",
		".5":         ".5",
		"1.":         "1.",
	})
}

func TestAnExponentSignJoinsTheNumberOnlyWhenItTouchesTheExponentMarker(t *testing.T) {
	// `1e-5` and `1e5-3` differ only in where the sign sits, and both start with
	// a digit and contain an `e` and a `-`. Testing `1e-5` alone leaves a lexer
	// that eats every sign after an exponent looking perfectly correct.
	shapesJS(t, map[string]string{
		"1e-5":   "1e-5",
		"1e5-3":  "(1e5 - 3)",
		"1e5+3":  "(1e5 + 3)",
		"5-3":    "(5 - 3)",
		"1.5-2":  "(1.5 - 2)",
		"1e-5-3": "(1e-5 - 3)",
	})
}

func TestARadixLiteralDoesNotTreatItsHexDigitEAsAnExponentMarker(t *testing.T) {
	// In `0x1e+5` the `e` is a hex digit and the `+` is addition — 35, not a
	// literal. Running the exponent rule over a radix literal joins all six
	// characters into one token, and nothing downstream can take it apart again.
	shapesJS(t, map[string]string{
		"0x1e+5": "(0x1e + 5)",
		"0x1E-2": "(0x1E - 2)",
		"0xe+1":  "(0xe + 1)",
		"0b1e+1": "(0b1e + 1)",
	})
}

func TestABareDotIsNotANumber(t *testing.T) {
	// A hard error rather than an incomplete one: `.` is wrong, not unfinished,
	// and a watcher told otherwise holds a stale tree for as long as the file
	// keeps that character.
	refuses(t, nil, false, ".", "..", ". ")
}

func TestStringEscapesAreDecodedAndTheQuotesAreDropped(t *testing.T) {
	// The escaped QUOTE cases are the ones that matter: a lexer that closed on
	// the first matching quote without honouring the backslash would still
	// produce a string node, just a truncated one, and the remaining characters
	// would be blamed on whatever came next.
	x := parse.Expression{}
	for src, want := range map[string]string{
		`'a\nb'`:  "a\nb",
		`'a\tb'`:  "a\tb",
		`'a\rb'`:  "a\rb",
		`'it\'s'`: "it's",
		`"a\"b"`:  `a"b`,
		`'a\\b'`:  `a\b`,
		`"plain"`: "plain",
		`''`:      "",
		`'a\0b'`:  "a\x00b",
		`'don"t'`: `don"t`,
		`"it's"`:  "it's",
	} {
		e, err := x.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if e.Kind != parse.ExprString || e.Raw != want {
			t.Errorf("%q: got %v %q, want string %q", src, e.Kind, e.Raw, want)
		}
	}
}

func TestATemplateSplitsIntoLiteralChunksAndParsedSubstitutions(t *testing.T) {
	shapesJS(t, map[string]string{
		"`plain`":           "`plain`",
		"``":                "``",
		"`hi ${x}`":         "`hi ${x}`",
		"`${x}`":            "`${x}`",
		"`${a + b * c}`":    "`${(a + (b * c))}`",
		"`a${x}b${y}c`":     "`a${x}b${y}c`",
		"`${x}${y}`":        "`${x}${y}`",
		"`${ {a: 1} }`":     "`${{a: 1}}`",
		"`${`inner ${z}`}`": "`${`inner ${z}`}`",
		"`cost $100`":       "`cost $100`",
		"`a\\${x}`":         "`a${x}`",
		"`${a ? b : c}`":    "`${(a ? b : c)}`",
		"`x` + `y`":         "(`x` + `y`)",
	})
}

func TestATemplateAlwaysHasOneMoreChunkThanSubstitution(t *testing.T) {
	// The alternation is the whole contract: a consumer reassembling the string
	// walks chunk, substitution, chunk, … and an off-by-one either drops the
	// tail or indexes out of range. An empty leading or trailing chunk still has
	// to be PRESENT, which is why `${x}` with nothing around it is here.
	for _, src := range []string{"``", "`a`", "`${x}`", "`a${x}`", "`${x}b`", "`a${x}b${y}c`"} {
		e, err := parse.Expression{}.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(e.Chunks) != len(e.Args)+1 {
			t.Errorf("%q: %d chunks and %d substitutions", src, len(e.Chunks), len(e.Args))
		}
		if e.Raw != "" {
			t.Errorf("%q: Raw = %q, but a template is not one string", src, e.Raw)
		}
	}
}

func TestWalkReachesTheIdentifiersInsideATemplateSubstitution(t *testing.T) {
	// This is the reason substitutions are parsed rather than kept as text. A
	// consumer building a dependency graph collects identifiers through Walk; a
	// template held as a single string contributes none, so the binding looks
	// like it depends on nothing and never updates. The failure has no symptom
	// at parse time at all — only a wrong answer later — so it has to be a test.
	e, err := parse.Expression{}.Parse([]byte("`Hello ${greeting.text}, ${count + 1} times`"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	e.Walk(func(n *parse.Expr) bool {
		if n.Kind == parse.ExprIdent {
			names = append(names, n.Raw)
		}
		return true
	})
	if strings.Join(names, ",") != "greeting,count" {
		t.Errorf("identifiers = %v, want greeting,count", names)
	}
}

func TestATemplateCutOffInsideASubstitutionIsIncomplete(t *testing.T) {
	// A template kept as text reports only the missing backtick, so every one of
	// these looks the same to a watcher. Parsed, each unfinished construct inside
	// the substitution reports itself — and `` `a${b} `` is the discriminating
	// one: the substitution is complete and only the template is not.
	refuses(t, nil, true, "`a${", "`a${b", "`a${b +", "`a${b}", "`a${'s", "`a${(b")
	// And a substitution containing something WRONG is wrong, not unfinished:
	// otherwise a watcher holds its stale tree for as long as the typo survives.
	refuses(t, nil, false, "`a${@}`", "`a${b c}`", "`a${.}`")
}

func TestASubstitutionCountsAgainstTheNestingLimit(t *testing.T) {
	// Text does not recurse, so a template held as a string has no depth to
	// charge. Parsed, it does — and `${`${…}`}` is the path that reaches the
	// parser without passing through any bracket, so it is the one that would
	// otherwise exhaust the stack.
	const deep = 100000
	src := strings.Repeat("`${", deep) + "a" + strings.Repeat("}`", deep)
	_, err := parse.Expression{}.Parse([]byte(src))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("%v, want a SyntaxError about the nesting limit", err)
	}
	if !strings.Contains(se.Want, "nested") {
		t.Errorf("Want = %q, want the nesting-limit message", se.Want)
	}
}

func TestParsingACallIsNotGrantingItAuthorityToRun(t *testing.T) {
	// The package makes no safety claim, so a consumer that must not execute
	// certain things enforces that over the TREE. This is the demonstration that
	// the separation works: the parser records `remove(id)` and a policy walking
	// the same tree refuses it, with no help from and no change to the parser.
	//
	// Assignment is not parsed at all, so the policy is shown over a call — the
	// construct that actually reaches a consumer, and the one that can mutate.
	tree, err := parse.Expression{}.Parse([]byte("ok ? read(id) : remove(id)"))
	if err != nil {
		t.Fatal(err)
	}
	refusedBy := func(e *parse.Expr, allowed map[string]bool) []string {
		var bad []string
		e.Walk(func(n *parse.Expr) bool {
			if n.Kind == parse.ExprCall && n.Left.Kind == parse.ExprIdent && !allowed[n.Left.Raw] {
				bad = append(bad, n.Left.Raw)
			}
			return true
		})
		return bad
	}
	if got := refusedBy(&tree, map[string]bool{"read": true}); strings.Join(got, ",") != "remove" {
		t.Errorf("a read-only policy refused %v, want remove", got)
	}
	if got := refusedBy(&tree, map[string]bool{"read": true, "remove": true}); len(got) != 0 {
		t.Errorf("a permissive policy refused %v, want nothing", got)
	}
	// And the parser itself refuses nothing of the sort: the tree exists either
	// way, which is the point — authority is the consumer's decision, not a
	// property the parse conferred.
	if _, err := (parse.Expression{}).Parse([]byte("drop(everything)")); err != nil {
		t.Errorf("the parser refused a mutating call: %v — it is not a policy", err)
	}
}

func TestLiteralKeywordsBecomeTheirOwnKindsRatherThanIdentifiers(t *testing.T) {
	// `nullish` and `trueish` are the discriminating rows: a literal table
	// consulted by prefix rather than by whole word turns every name that starts
	// with a keyword into a constant, which no shape test would reveal.
	x := parse.Expression{}
	for src, want := range map[string]parse.ExprKind{
		"true":       parse.ExprBool,
		"false":      parse.ExprBool,
		"null":       parse.ExprNull,
		"undefined":  parse.ExprUndefined,
		"nullish":    parse.ExprIdent,
		"trueish":    parse.ExprIdent,
		"falsey":     parse.ExprIdent,
		"undefined_": parse.ExprIdent,
	} {
		e, err := x.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if e.Kind != want || e.Raw != src {
			t.Errorf("%q: got %v %q, want %v", src, e.Kind, e.Raw, want)
		}
	}
}

func TestShortCircuitingOperatorsAreADifferentKindFromArithmeticOnes(t *testing.T) {
	// An evaluator that treats `&&` as an ordinary binary evaluates both sides,
	// which is the exact bug `a && a.b` is written to avoid. The kind is the only
	// place that distinction lives, so the rendered shape cannot check it.
	x := parse.Expression{}
	for src, want := range map[string]parse.ExprKind{
		"a && b": parse.ExprLogical,
		"a || b": parse.ExprLogical,
		"a ?? b": parse.ExprLogical,
		"a & b":  parse.ExprBinary,
		"a | b":  parse.ExprBinary,
		"a + b":  parse.ExprBinary,
		"a ** b": parse.ExprBinary,
	} {
		e, err := x.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if e.Kind != want {
			t.Errorf("%q: got %v, want %v", src, e.Kind, want)
		}
	}
}

func TestTrailingCommasAreAcceptedInArraysObjectsAndCalls(t *testing.T) {
	shapesJS(t, map[string]string{
		"[1, 2,]":       "[1, 2]",
		"f(1, 2,)":      "f(1, 2)",
		"{a: 1, b: 2,}": "{a: 1, b: 2}",
		"[1,]":          "[1]",
		"[]":            "[]",
		"{}":            "{}",
		"[ 1 , ]":       "[1]",
	})
}

func TestArrayElisionsAndDoubledCommasAreRefused(t *testing.T) {
	// `[,]` and `[1,,2]` are legal JavaScript holes this revision cannot
	// represent. Accepting them silently would drop an element and shift every
	// index after it — the one failure mode worse than refusing the file.
	refuses(t, nil, false, "[,]", "[1,,2]", "f(,)", "{,}", "[1 2]")
}

func TestObjectKeysMayBeNamesStringsOrComputed(t *testing.T) {
	shapesJS(t, map[string]string{
		"{a: 1}":                 "{a: 1}",
		"{'k': v}":               "{k: v}",
		`{"k": v}`:               "{k: v}",
		"{[a + b]: c}":           "{[(a + b)]: c}",
		"{a: 1, 'b': 2, [c]: 3}": "{a: 1, b: 2, [c]: 3}",
		"{if: 1}":                "{if: 1}",
		"{a: {b: {c: 1}}}":       "{a: {b: {c: 1}}}",
	})
}

func TestObjectShorthandIsRefusedByNamingTheMissingColon(t *testing.T) {
	// `{a}` is valid JavaScript that this revision does not support, so the
	// refusal has to say what is missing rather than "unexpected }" — the
	// difference between a reader typing `: a` and a reader filing a bug.
	e, err := parse.Expression{}.Parse([]byte("{a}"))
	if err == nil {
		t.Fatalf("parsed as %s, want a refusal", renderExpr(&e))
	}
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("%v is not a SyntaxError", err)
	}
	if !strings.Contains(se.Want, ":") {
		t.Errorf("Want = %q, which does not name the missing colon", se.Want)
	}
	if se.Incomplete {
		t.Error("Incomplete is set, but `{a}` is finished and wrong rather than unfinished")
	}
}

func TestCommentsSeparateTokensWithoutJoiningThem(t *testing.T) {
	shapesJS(t, map[string]string{
		"a /* c */ + b": "(a + b)",
		"a // c\n+ b":   "(a + b)",
		"/* c */ a":     "a",
		"a + b // tail": "(a + b)",
		"a/**/.b":       "a.b",
	})
	// A comment is whitespace, so it cannot weld two expressions into one; and a
	// comment cannot weld two halves of an operator either, which a test using
	// only spaces would never ask.
	refuses(t, nil, false, "a/**/b", "a &/**/& b", "a in/**/stanceof b")
}

func TestIncompleteInputIsTheOtherErrorFromWrongInput(t *testing.T) {
	// A watcher holds its last good tree on Incomplete and reports a problem on
	// anything else, so a path with the wrong flag either spams the user
	// mid-keystroke or hides a real syntax error until the file is saved. Every
	// construct with a closing half is listed, because the flag is set per PATH
	// and one missing case is one construct that behaves differently from all
	// the rest.
	refuses(t, nil, true,
		"", "   ", "a +", "a *", "a &&", "a ??", "a instanceof",
		"a ?", "a ? b", "a ? b :",
		"(", "(a", "(a + ", "a[", "a[1", "f(", "f(1", "f(1,",
		"[", "[1", "[1,", "{", "{a", "{a:", "{a: 1,", "{a: 1", "{[a",
		"'abc", `"abc`, "`abc", `'a\`, "`a\\", `'abc\n`,
		"/* c", "a + /* c", "a.", "a?.", "-", "!", "typeof", "2 **",
	)
	refuses(t, nil, false,
		"@", "a @ b", ".", "a ) b", "a } b", "f(1 2)", "{a: 1 b: 2}",
		"a.1", "a..b", "(a]", "[a)", ")", "}", "]", "a = b",
	)
}

func TestTheTwoErrorIdentitiesAreSiblingsAndNeverBoth(t *testing.T) {
	// A caller that gives up on ErrSyntax must not thereby give up on input that
	// was merely unfinished, so each identity is asserted BOTH ways: present and
	// absent. Checking only the positive lets an Unwrap that answered for both
	// pass, which would collapse the distinction the Incomplete flag exists for.
	_, err := parse.Expression{}.Parse([]byte("a +"))
	if !errors.Is(err, parse.ErrUnterminated) || errors.Is(err, parse.ErrSyntax) {
		t.Errorf("unfinished input: %v should be ErrUnterminated and NOT ErrSyntax", err)
	}
	_, err = parse.Expression{}.Parse([]byte("a @ b"))
	if !errors.Is(err, parse.ErrSyntax) || errors.Is(err, parse.ErrUnterminated) {
		t.Errorf("wrong input: %v should be ErrSyntax and NOT ErrUnterminated", err)
	}
}

func TestParseRequiresTheWholeInputToBeOneExpression(t *testing.T) {
	// Stopping at the first complete expression would accept `a b` as `a`, which
	// is how a parser used as a validator lets a typo through.
	refuses(t, nil, false, "a b", "1 2", "a, b", "a; b", "a + b c", "a b + c")
}

func TestErrorPositionsPointAtTheConstructRatherThanTheEndOfInput(t *testing.T) {
	// An unterminated string reports where the QUOTE is, not where the file ran
	// out, because that is where the reader has to go. A test that only checked
	// that an error occurred passes with every position left at 1:1.
	_, err := parse.Expression{}.Parse([]byte("a + 'unterminated"))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("%v is not a SyntaxError", err)
	}
	if se.Pos.Line != 1 || se.Pos.Column != 5 {
		t.Errorf("Pos = %s, want the opening quote at 1:5", se.Pos)
	}
}

func TestNodePositionsAreWhereTheirSubtreeStarts(t *testing.T) {
	// Across a newline, so a position that counted bytes instead of tracking
	// lines could not accidentally agree.
	e, err := parse.Expression{}.Parse([]byte("a +\n  b * c"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Pos.Line != 1 || e.Pos.Column != 1 {
		t.Errorf("root Pos = %s, want 1:1 — a binary starts where its left operand does", e.Pos)
	}
	if e.Right.Pos.Line != 2 || e.Right.Pos.Column != 3 {
		t.Errorf("right Pos = %s, want 2:3", e.Right.Pos)
	}
}

func TestNestingDeeperThanTheLimitIsAnErrorRatherThanAStackOverflow(t *testing.T) {
	// A stack overflow cannot be recovered, so this is the difference between a
	// bad file and a dead process. Each RECURSIVE path is exercised separately:
	// the limit is charged in a handful of places, and a path that reaches the
	// parser without passing through one of them has no limit at all. The counts
	// are far past the limit deliberately — at a few hundred a path with no check
	// still returns, and only a depth that actually exhausts the stack tells the
	// two apart.
	const deep = 200000
	for name, src := range map[string]string{
		"grouping":    strings.Repeat("(", deep) + "a" + strings.Repeat(")", deep),
		"array":       strings.Repeat("[", deep) + "a" + strings.Repeat("]", deep),
		"object":      strings.Repeat("{k:", deep) + "a" + strings.Repeat("}", deep),
		"object key":  strings.Repeat("{[", deep) + "a" + strings.Repeat("]:1}", deep),
		"call":        strings.Repeat("f(", deep) + "a" + strings.Repeat(")", deep),
		"index":       "a" + strings.Repeat("[", deep) + "b" + strings.Repeat("]", deep),
		"unary":       strings.Repeat("!", deep) + "a",
		"negation":    strings.Repeat("-", deep) + "a",
		"word unary":  strings.Repeat("typeof ", deep) + "a",
		"exponent":    strings.Repeat("2**", deep) + "2",
		"conditional": strings.Repeat("a?b:", deep) + "c",
	} {
		_, err := parse.Expression{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%s: %v, want a SyntaxError about the nesting limit", name, err)
			continue
		}
		if !strings.Contains(se.Want, "nested") {
			t.Errorf("%s: Want = %q, want the nesting-limit message", name, se.Want)
		}
	}
}

func TestTheDepthLimitIsASharpBoundaryThatTracksNesting(t *testing.T) {
	// "Deep input fails" is satisfied by a limit that is off by any amount, or
	// by one that does not move with the input at all. This finds the smallest
	// MaxDepth that accepts a fixed source and then asserts both edges: one less
	// refuses it, and that same limit refuses one more level of nesting. The
	// limit is never spelled out, because the number of charges per construct is
	// the parser's business and not a promise to a caller.
	const src = "((((a))))"
	limit := 0
	for d := 1; d <= parse.DefaultExprMaxDepth; d++ {
		if _, err := (parse.Expression{MaxDepth: d}).Parse([]byte(src)); err == nil {
			limit = d
			break
		}
	}
	if limit == 0 {
		t.Fatalf("no MaxDepth up to %d accepts %q", parse.DefaultExprMaxDepth, src)
	}
	if _, err := (parse.Expression{MaxDepth: limit - 1}).Parse([]byte(src)); err == nil {
		t.Errorf("MaxDepth %d accepted %q, but %d was the smallest that worked", limit-1, src, limit)
	}
	if _, err := (parse.Expression{MaxDepth: limit}).Parse([]byte("(" + src + ")")); err == nil {
		t.Errorf("MaxDepth %d accepted a strictly deeper expression too, so the limit does not track nesting", limit)
	}
}

func TestAZeroMaxDepthMeansTheDefaultRatherThanNoNestingAtAll(t *testing.T) {
	// A limit read straight from the field would reject every expression, since
	// the first charge already exceeds zero — so this is the difference between
	// "the zero value works" and "the zero value parses nothing".
	deep := strings.Repeat("(", 20) + "a" + strings.Repeat(")", 20)
	if _, err := (parse.Expression{}).Parse([]byte(deep)); err != nil {
		t.Errorf("the zero value rejected 20 levels of grouping: %v", err)
	}
	if _, err := (parse.Expression{MaxDepth: parse.DefaultExprMaxDepth}).Parse([]byte(deep)); err != nil {
		t.Errorf("MaxDepth = DefaultExprMaxDepth rejected the same source: %v", err)
	}
}

func TestWalkVisitsEveryNodeExactlyOnceIncludingObjectKeys(t *testing.T) {
	// A computed key is the node most easily forgotten, because it hangs off
	// ExprProperty rather than off Expr — a Walk that missed it still looks
	// complete on every other input. Counting alone would not catch a DOUBLE
	// visit either, so the visited nodes are collected in order and compared.
	e, err := parse.Expression{}.Parse([]byte("{a: 1, [k + 1]: f(2, [3])}"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[*parse.Expr]int{}
	var order []string
	e.Walk(func(n *parse.Expr) bool {
		seen[n]++
		order = append(order, renderExpr(n))
		return true
	})
	for n, count := range seen {
		if count != 1 {
			t.Errorf("%s visited %d times", renderExpr(n), count)
		}
	}
	want := []string{
		"{a: 1, [(k + 1)]: f(2, [3])}",
		"1",
		"(k + 1)", "k", "1",
		"f(2, [3])", "f", "2", "[3]", "3",
	}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Errorf("visited\n %v\nwant\n %v", order, want)
	}
}

func TestWalkReachesTheConditionalAlternative(t *testing.T) {
	// Alt is used by exactly one kind, so a Walk that forgot it passes every
	// test whose input contains no `?:` — and a consumer collecting identifiers
	// silently misses the whole else-branch of every binding that has one.
	e, err := parse.Expression{}.Parse([]byte("a ? b : c"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	e.Walk(func(n *parse.Expr) bool {
		if n.Kind == parse.ExprIdent {
			names = append(names, n.Raw)
		}
		return true
	})
	if strings.Join(names, ",") != "a,b,c" {
		t.Errorf("identifiers = %v, want a,b,c", names)
	}
}

func TestWalkStopsDescendingWithoutStoppingTheTraversal(t *testing.T) {
	// "Stops descending" is not "stops walking": the refused node's SIBLINGS
	// must still be visited, which a test that only counted the total cannot
	// distinguish from abandoning the rest of the tree.
	e, err := parse.Expression{}.Parse([]byte("(a + b) * (c + d)"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	e.Walk(func(n *parse.Expr) bool {
		if n.Raw == "+" && n.Left != nil && n.Left.Raw == "a" {
			return false
		}
		if n.Kind == parse.ExprIdent {
			names = append(names, n.Raw)
		}
		return true
	})
	if strings.Join(names, ",") != "c,d" {
		t.Errorf("identifiers = %v, want c,d — the refused subtree is skipped, its sibling is not", names)
	}
}

func TestWalkOnANilTreeDoesNothing(t *testing.T) {
	var e *parse.Expr
	e.Walk(func(*parse.Expr) bool { t.Error("visited a node of a nil tree"); return true })
}

func TestMemberAndCallNodesCarryTheirPartsWhereTheDocumentationSaysTheyDo(t *testing.T) {
	// Rendering cannot tell an optional member from a plain one if the consumer
	// reads the flag rather than the text, so the flags are asserted directly —
	// and the plain member beside it is asserted NOT to carry them, which is
	// what a flag set on the wrong node would look like.
	e, err := parse.Expression{}.Parse([]byte("a.b?.c[d](e)"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != parse.ExprCall || len(e.Args) != 1 || e.Args[0].Raw != "e" {
		t.Fatalf("outermost node is %v with args (%s), want a call of (e)", e.Kind, renderExprList(e.Args))
	}
	index := e.Left
	if index.Kind != parse.ExprMember || !index.Computed || index.Right.Raw != "d" {
		t.Fatalf("next node is %v computed=%v, want a computed member on d", index.Kind, index.Computed)
	}
	optional := index.Left
	if optional.Kind != parse.ExprMember || !optional.Optional || optional.Name != "c" || optional.Computed {
		t.Fatalf("got %v optional=%v computed=%v name=%q, want an optional member c",
			optional.Kind, optional.Optional, optional.Computed, optional.Name)
	}
	plain := optional.Left
	if plain.Kind != parse.ExprMember || plain.Optional || plain.Computed || plain.Name != "b" {
		t.Errorf("the `.b` member reports optional=%v computed=%v name=%q", plain.Optional, plain.Computed, plain.Name)
	}
	if plain.Left.Kind != parse.ExprIdent || plain.Left.Raw != "a" {
		t.Errorf("the chain does not bottom out at the identifier a: %v %q", plain.Left.Kind, plain.Left.Raw)
	}
}

func TestParsingIsRepeatableBecauseNothingIsCarriedBetweenCalls(t *testing.T) {
	// Expression is a value and holds no cursor, so one value must parse twice
	// identically — including the depth counter, which lives on the per-parse
	// state and would otherwise leak from the first call into the second.
	x := parse.Expression{MaxDepth: 8}
	first, err := x.Parse([]byte("((a + b))"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := x.Parse([]byte("((a + b))"))
	if err != nil {
		t.Fatalf("the second parse of the same source failed: %v", err)
	}
	if renderExpr(&first) != renderExpr(&second) {
		t.Errorf("%s then %s", renderExpr(&first), renderExpr(&second))
	}
}

func TestExprKindStringsAreStableForDiagnostics(t *testing.T) {
	for k, want := range map[parse.ExprKind]string{
		parse.ExprInvalid:     "invalid",
		parse.ExprNumber:      "number",
		parse.ExprString:      "string",
		parse.ExprTemplate:    "template",
		parse.ExprBool:        "bool",
		parse.ExprNull:        "null",
		parse.ExprUndefined:   "undefined",
		parse.ExprIdent:       "identifier",
		parse.ExprMember:      "member",
		parse.ExprCall:        "call",
		parse.ExprUnary:       "unary",
		parse.ExprBinary:      "binary",
		parse.ExprLogical:     "logical",
		parse.ExprConditional: "conditional",
		parse.ExprArray:       "array",
		parse.ExprObject:      "object",
	} {
		if got := k.String(); got != want {
			t.Errorf("ExprKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}
