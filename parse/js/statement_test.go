package js_test

import (
	"errors"
	"github.com/yongjohnlee80/golib/parse/js"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
)

// Statements is a [parse.Parser], and saying so here rather than in the file
// itself keeps the assertion where a change to the interface will break a TEST
// instead of quietly dropping a capability callers discover by type assertion.
var _ parse.Parser[[]js.Stmt] = js.Statements{}

// renderStmt prints a statement tree back as source, with every if PARENTHESISED.
//
// The parentheses are the whole point, exactly as in renderExpr: an else bound
// to the wrong if produces a tree with the same node kinds, the same statements
// and the same identifiers, and prints identically to the correct one unless the
// nesting is made visible. Asserting on kinds cannot see that defect at all.
func renderStmt(s *js.Stmt) string {
	if s == nil {
		return "<nil>"
	}
	switch s.Kind {
	case js.StmtEmpty:
		return ";"
	case js.StmtBlock:
		return "{" + renderProgram(s.Body) + "}"
	case js.StmtExpr:
		return renderExpr(s.Value)
	case js.StmtReturn:
		if s.Value == nil {
			return "return"
		}
		return "return " + renderExpr(s.Value)
	case js.StmtDeclaration:
		parts := make([]string, 0, len(s.Decls))
		for _, d := range s.Decls {
			if d.Init == nil {
				parts = append(parts, d.Name)
				continue
			}
			parts = append(parts, d.Name+" = "+renderExpr(d.Init))
		}
		return s.Raw + " " + strings.Join(parts, ", ")
	case js.StmtIf:
		out := "(if " + renderExpr(s.Cond) + " " + renderStmt(s.Then)
		if s.Else != nil {
			out += " else " + renderStmt(s.Else)
		}
		return out + ")"
	}
	return "<" + s.Kind.String() + ">"
}

// renderProgram joins statements with a SPACE rather than a semicolon, because
// the empty statement is itself a semicolon and a semicolon separator makes
// `;;` and `;` print alike — an ambiguity in the instrument, which is the one
// place a test can least afford one.
func renderProgram(list []js.Stmt) string {
	parts := make([]string, 0, len(list))
	for i := range list {
		parts = append(parts, renderStmt(&list[i]))
	}
	return strings.Join(parts, " ")
}

// stmts asserts that each source parses to its stated program.
func stmts(t *testing.T, cases map[string]string) {
	t.Helper()
	var s js.Statements
	for src, want := range cases {
		list, err := s.Parse([]byte(src))
		if err != nil {
			t.Errorf("%s: %q: %v", s.FormatName(), src, err)
			continue
		}
		if got := renderProgram(list); got != want {
			t.Errorf("%s: %q\n got %s\nwant %s", s.FormatName(), src, got, want)
		}
	}
}

// stmtRefuses asserts that each source is rejected, and WHICH refusal it gets:
// "keep typing" or "this cannot work".
func stmtRefuses(t *testing.T, incomplete bool, sources ...string) {
	t.Helper()
	var s js.Statements
	for _, src := range sources {
		list, err := s.Parse([]byte(src))
		if err == nil {
			t.Errorf("%q parsed as %s, want a refusal", src, renderProgram(list))
			continue
		}
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v is not a parse.SyntaxError", src, err)
			continue
		}
		if se.Incomplete != incomplete {
			t.Errorf("%q: Incomplete = %v, want %v (%v)", src, se.Incomplete, incomplete, err)
		}
	}
}

func TestAProgramIsASequenceOfStatementsOfEverySupportedKind(t *testing.T) {
	stmts(t, map[string]string{
		"f();":                   "f()",
		"f(); g();":              "f() g()",
		";":                      ";",
		";;":                     "; ;",
		"{}":                     "{}",
		"{ f(); }":               "{f()}",
		"{ f(); g(); }":          "{f() g()}",
		"{ { f(); } }":           "{{f()}}",
		"let a = 1;":             "let a = 1",
		"const b = f(x);":        "const b = f(x)",
		"var c = a + b;":         "var c = (a + b)",
		"return;":                "return",
		"return a.b;":            "return a.b",
		"if (a) f();":            "(if a f())",
		"if (a) f(); else g();":  "(if a f() else g())",
		"if (a) { f(); }":        "(if a {f()})",
		"a ? f() : g();":         "(a ? f() : g())",
		"obj.method(1, 2)[k];":   "obj.method(1, 2)[k]",
		"let a = 1; return a.b;": "let a = 1 return a.b",
	})
}

func TestAnEmptySourceIsAnEmptyProgramRatherThanAnError(t *testing.T) {
	// A handler body with nothing in it is something a person writes on purpose,
	// so "" must not be the end-of-input refusal a single expression gets. A
	// parser that reused the expression entry point inherits that refusal and
	// makes every empty binding look broken.
	for _, src := range []string{"", "   ", "\n\n", "// just a comment\n", "/* nothing */"} {
		list, err := js.Statements{}.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if len(list) != 0 {
			t.Errorf("%q: got %d statements, want none", src, len(list))
		}
	}
}

func TestADeclarationKeepsEveryNameIncludingTheOnesWithNoInitialiser(t *testing.T) {
	// The name is what a declaration BINDS, so dropping the ones with no `=`
	// still yields a well-formed tree that parses, renders and evaluates — and a
	// consumer resolving references against it reports `a` as undeclared. A test
	// that only asserts `let a = 1` parses never touches that path.
	for src, want := range map[string]string{
		"let a;":                  "a",
		"let a, b;":               "a,b",
		"let a = 1, b;":           "a,b",
		"let a, b = 2;":           "a,b",
		"let a, b, c;":            "a,b,c",
		"var x = 1, y = 2, z = 3": "x,y,z",
		"const k = 1;":            "k",
	} {
		list, err := js.Statements{}.Parse([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		var names []string
		for _, d := range list[0].Decls {
			names = append(names, d.Name)
		}
		if strings.Join(names, ",") != want {
			t.Errorf("%q: declared %v, want %s", src, names, want)
		}
	}
	// And the initialiser is nil rather than some empty expression, so the two
	// cases are distinguishable at all.
	list, _ := js.Statements{}.Parse([]byte("let a, b = 2;"))
	if list[0].Decls[0].Init != nil {
		t.Error("a declarator with no initialiser carries one anyway")
	}
	if list[0].Decls[1].Init == nil {
		t.Error("a declarator with an initialiser lost it")
	}
}

func TestADeclarationListStopsAtTheCommaRatherThanReadingItAsAnOperator(t *testing.T) {
	// `let a = 1, b = 2` has to yield TWO names. An initialiser parsed as a
	// comma expression would swallow `b = 2` and produce one declaration whose
	// value is wrong in a way nothing downstream can detect.
	stmts(t, map[string]string{
		"let a = 1, b = 2;":     "let a = 1, b = 2",
		"let a = f(1, 2), b;":   "let a = f(1, 2), b",
		"let a = [1, 2], b = 3": "let a = [1, 2], b = 3",
	})
}

func TestElseBindsToTheNearestIfThatHasNotGotOneYet(t *testing.T) {
	// The dangling else. Attaching it to the OUTER if produces a tree with the
	// same kinds, the same nesting depth and the same identifiers, so only the
	// rendered shape tells them apart — and the difference is that `y()` runs in
	// the case the source says it must not.
	stmts(t, map[string]string{
		"if (a) if (b) x(); else y();":           "(if a (if b x() else y()))",
		"if (a) { if (b) x(); } else y();":       "(if a {(if b x())} else y())",
		"if (a) if (b) x(); else y(); else z()":  "(if a (if b x() else y()) else z())",
		"if (a) x(); else if (b) y(); else z();": "(if a x() else (if b y() else z()))",
		"if (a) if (b) if (c) x(); else y();":    "(if a (if b (if c x() else y())))",
	})
}

func TestAnElseMayBeSeparatedFromItsBranchByALineBreak(t *testing.T) {
	// The line break terminates the then-branch, and a parser that treated a
	// terminated branch as a finished if would leave the `else` to be read as a
	// statement of its own — refused, with a message about a construct the
	// reader did not write.
	stmts(t, map[string]string{
		"if (a) x()\nelse y()":               "(if a x() else y())",
		"if (a) {\n x()\n}\nelse {\n y()\n}": "(if a {x()} else {y()})",
		"if (a)\n x()\nelse\n y()":           "(if a x() else y())",
	})
}

func TestAStatementEndsAtASemicolonOrALineBreakAndIsOtherwiseRefused(t *testing.T) {
	// The documented choice, both halves. Without the line-break half, every
	// semicolon-free handler body is refused; without the refusal half, the
	// parser is doing automatic insertion by accident and `f() g()` becomes two
	// statements no reader asked for.
	stmts(t, map[string]string{
		"f()":                          "f()",
		"f();":                         "f()",
		"f()\ng()":                     "f() g()",
		"f();g()":                      "f() g()",
		"{ f() }":                      "{f()}",
		"{ f()\ng() }":                 "{f() g()}",
		"let a = 1\nlet b = 2":         "let a = 1 let b = 2",
		"return 1\n":                   "return 1",
		"f() // trailing\ng()":         "f() g()",
		"f() /* over\nthe line */ g()": "f() g()",
	})
	stmtRefuses(t, false, "f() g()", "let a = 1 let b = 2", "return 1 return 2", "f() /* same line */ g()")
}

func TestAnExpressionThatCanContinueAcrossALineBreakStillDoes(t *testing.T) {
	// The line-break rule must not cut an expression that has not finished. A
	// parser that ended a statement at every newline splits `a\n+ b` into two
	// statements — each of which parses — and silently changes what the source
	// computes.
	stmts(t, map[string]string{
		"a +\nb":         "(a + b)",
		"a\n+ b":         "(a + b)",
		"f(\n 1,\n 2\n)": "f(1, 2)",
		"a ?\n b :\n c":  "(a ? b : c)",
		"obj\n .a\n .b":  "obj.a.b",
	})
}

func TestReturnStopsAtALineBreakInsteadOfSwallowingTheNextLine(t *testing.T) {
	// `return` then a line then a value is the one place JavaScript's automatic
	// insertion changes MEANING rather than only punctuation, and a parser that
	// simply looked for an expression after `return` returns the wrong thing
	// while parsing perfectly.
	stmts(t, map[string]string{
		"return\nf();":        "return f()",
		"return;\nf();":       "return f()",
		"return f();":         "return f()",
		"{ return }":          "{return}",
		"return":              "return",
		"if (a) return; f();": "(if a return) f()",
	})
}

func TestWalkReachesEveryNestedStatementAndEveryExpressionInside(t *testing.T) {
	// The API the whole file exists for. A consumer doing dependency discovery
	// collects identifiers through this traversal, and a name it cannot reach is
	// a dependency that never fires — with no symptom at parse time, so only a
	// test naming the reachable set can catch it. `limit` is the discriminating
	// one: it appears ONLY in a nested if's condition, which is the field a
	// statement walk that recursed into bodies alone would skip.
	const src = `
let total = 0;
if (ready) {
  if (items.length > limit) {
    report(items[0].id);
  } else {
    log("none");
  }
}
return total;
`
	list, err := js.Statements{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	js.WalkExprs(list, func(e *js.Expr) bool {
		if e.Kind == js.ExprIdent {
			names = append(names, e.Raw)
		}
		return true
	})
	if got := strings.Join(names, ","); got != "ready,items,limit,report,items,log,total" {
		t.Errorf("identifiers = %s", got)
	}

	var kinds []string
	js.WalkStmts(list, func(s *js.Stmt) bool {
		kinds = append(kinds, s.Kind.String())
		return true
	})
	if got := strings.Join(kinds, ","); got != "declaration,if,block,if,block,expression,block,expression,return" {
		t.Errorf("statements = %s", got)
	}
}

func TestWalkReachesAnExpressionBuriedInANestedIfCondition(t *testing.T) {
	// Sharpened to one claim, because the traversal has one place per field and
	// the condition is the field most easily left out: the branches are obvious
	// children, the condition looks like part of the node itself.
	list, err := js.Statements{}.Parse([]byte("if (a) { if (deep.name) { g(); } }"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	js.WalkExprs(list, func(e *js.Expr) bool {
		if e.Kind == js.ExprIdent {
			names = append(names, e.Raw)
		}
		return true
	})
	if got := strings.Join(names, ","); got != "a,deep,g" {
		t.Errorf("identifiers = %s, want a,deep,g", got)
	}
}

func TestWalkReachesInsideTemplateSubstitutionsAndDeclarationInitialisers(t *testing.T) {
	// The statement walk delegates to the expression walk rather than
	// re-implementing it, so the parts of an expression that are easy to forget —
	// a template's substitutions above all — stay reachable. Re-implemented here,
	// they would be missing again, and again only a wrong answer later would say so.
	list, err := js.Statements{}.Parse([]byte("let msg = `hi ${who.name}`;\nif (msg) send(msg);"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	js.WalkExprs(list, func(e *js.Expr) bool {
		if e.Kind == js.ExprIdent {
			names = append(names, e.Raw)
		}
		return true
	})
	if got := strings.Join(names, ","); got != "who,msg,send,msg" {
		t.Errorf("identifiers = %s, want who,msg,send,msg", got)
	}
}

func TestReturningFalseFromAStatementWalkPrunesThatSubtree(t *testing.T) {
	// The documented pruning contract. A traversal that ignored the result would
	// still visit everything — passing any test that only counts what it reached
	// — and a consumer relying on pruning to skip a region would silently get the
	// whole tree.
	list, err := js.Statements{}.Parse([]byte("if (a) { f(); g(); } h();"))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	js.WalkStmts(list, func(s *js.Stmt) bool {
		seen = append(seen, s.Kind.String())
		return s.Kind != js.StmtIf
	})
	if got := strings.Join(seen, ","); got != "if,expression" {
		t.Errorf("visited %s, want if,expression", got)
	}
}

func TestEveryConstructCutOffAtEndOfInputIsIncomplete(t *testing.T) {
	// The load-bearing flag: a watcher holds its last good tree on Incomplete and
	// blanks the screen otherwise, so a construct that forgets to set it makes
	// the editor flicker on the way through every one of these prefixes. A suite
	// that only checked `{` misses the seven other paths that reach end of input.
	stmtRefuses(t, true,
		"{",
		"{ f();",
		"{ { f(); }",
		"if",
		"if (",
		"if (a",
		"if (a)",
		"if (a) f();\nelse",
		"if (a) f(); else",
		"if (a) { f();",
		"let",
		"let a =",
		"let a = 1,",
		"let a = 1, b =",
		"const",
		"var x = f(",
		"return a +",
		"return f(",
		"f(",
		"f(a,",
		"f(a).",
		"f(`a${",
	)
}

func TestAWrongCharacterIsNotIncomplete(t *testing.T) {
	// The other half, and the half that keeps Incomplete meaningful: if a typo
	// reported "keep typing", the watcher holds a stale tree for as long as the
	// typo survives, which is exactly the failure Incomplete was added to avoid.
	stmtRefuses(t, false,
		"}",
		"{ f(); } }",
		"if a) f();",
		"if (a f();",
		"if (a) else f();",
		"let 1 = 2;",
		"let a = ;",
		"let a,, b;",
		"let a = 1 b = 2;",
		"else f();",
		"f() g()",
		"@",
	)
}

func TestNestedBlocksReachTheNestingLimitRatherThanTheStack(t *testing.T) {
	// The recursion is unbounded in the source, not in the parser, so a file of
	// braces from a watcher would otherwise exhaust a goroutine stack — which
	// cannot be recovered. Nesting far past the limit is the only way to tell a
	// charge that is applied from one that is merely present.
	const deep = 100000
	src := strings.Repeat("{", deep) + strings.Repeat("}", deep)
	_, err := js.Statements{}.Parse([]byte(src))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("%v, want a parse.SyntaxError about the nesting limit", err)
	}
	if !strings.Contains(se.Want, "nested") {
		t.Errorf("Want = %q, want the nesting-limit message", se.Want)
	}
}

func TestNestedIfsReachTheNestingLimitRatherThanTheStack(t *testing.T) {
	// A separate path from the block one: an if recurses through its BRANCH, so
	// a depth charge added to blocks alone leaves this one unbounded, and a
	// suite that tested braces only would report the limit as enforced.
	const deep = 100000
	src := strings.Repeat("if (a) ", deep) + "f();"
	_, err := js.Statements{}.Parse([]byte(src))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("%v, want a parse.SyntaxError about the nesting limit", err)
	}
	if !strings.Contains(se.Want, "nested") {
		t.Errorf("Want = %q, want the nesting-limit message", se.Want)
	}
}

func TestTheNestingLimitIsTheOneTheCallerSetAndCountsExpressionsToo(t *testing.T) {
	// MaxDepth is read from the value rather than being a constant the parser
	// keeps to itself, and the budget is shared with the expressions inside —
	// which is what makes it a bound on the RECURSION rather than on one of the
	// two grammars that take turns driving it.
	shallow := js.Statements{MaxDepth: 3}
	if _, err := shallow.Parse([]byte("{{{ f(); }}}")); err == nil {
		t.Error("three nested blocks parsed under MaxDepth 3, want a refusal")
	}
	if _, err := shallow.Parse([]byte("{{ f(); }}")); err != nil {
		t.Errorf("two nested blocks refused under MaxDepth 3: %v", err)
	}
	if _, err := shallow.Parse([]byte("{ f((((1)))); }")); err == nil {
		t.Error("a deeply parenthesised expression inside a block escaped the budget")
	}
}

func TestAnUnsupportedConstructIsRefusedByNameRatherThanByItsPunctuation(t *testing.T) {
	// The difference between a reader who learns the parser has a gap and one
	// who goes looking for a syntax error in correct JavaScript. `for` read as
	// an identifier fails at the parenthesis after it, and the message then
	// describes a construct nobody wrote.
	for src, want := range map[string]string{
		"for (;;) f();":             "for loop",
		"while (a) f();":            "while loop",
		"do f(); while (a);":        "do/while loop",
		"switch (a) { }":            "switch",
		"case 1:":                   "switch",
		"try { f(); } catch (e) {}": "try/catch",
		"catch (e) {}":              "try/catch",
		"finally {}":                "try/catch",
		"throw new Error();":        "throw",
		"function f() {}":           "function declaration",
		"class C {}":                "class declaration",
		"break;":                    "break",
		"continue;":                 "continue",
		"else f();":                 "else",
	} {
		_, err := js.Statements{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v, want a parse.SyntaxError naming the construct", src, err)
			continue
		}
		if !strings.Contains(se.Want, want) {
			t.Errorf("%q: Want = %q, which does not name %q", src, se.Want, want)
		}
		if se.Incomplete {
			t.Errorf("%q: Incomplete is set, but the source is not unfinished", src)
		}
	}
}

func TestAnUnsupportedConstructIsRefusedWhereverAStatementIsAllowed(t *testing.T) {
	// Nested positions reach the same code only if there is one statement entry
	// point. A keyword table consulted at the top level alone lets `for` inside a
	// block or an if branch fall through to the expression parser, where the
	// message is about a parenthesis again.
	for _, src := range []string{"{ for (;;) f(); }", "if (a) for (;;) f();", "if (a) f(); else while (b) g();"} {
		_, err := js.Statements{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v, want a parse.SyntaxError", src, err)
			continue
		}
		if !strings.Contains(se.Want, "loop") {
			t.Errorf("%q: Want = %q, which does not name the loop", src, se.Want)
		}
	}
}

func TestAssignmentAtStatementPositionIsRefusedByNameNotAsAMissingSemicolon(t *testing.T) {
	// `x = 1` is the first statement anyone writes in a handler body, and the
	// expression grammar has no assignment — so without this the reader is told
	// a semicolon is missing at the `=` and goes looking for the one thing that
	// would not have helped. An `=` cannot reach here as anything else: `==`,
	// `===` and `<=` are all consumed by the expression parser first.
	for _, src := range []string{"x = 1;", "a.b = c;", "items[0] = v;", "if (a) x = 1;", "{ x = 1; }"} {
		_, err := js.Statements{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v, want a parse.SyntaxError naming assignment", src, err)
			continue
		}
		if !strings.Contains(se.Want, "assignment") {
			t.Errorf("%q: Want = %q, which does not name assignment", src, se.Want)
		}
	}
	// And the comparisons that merely LOOK like one still parse, which is what
	// makes this a statement about assignment rather than about the character.
	stmts(t, map[string]string{
		"a == b;":    "(a == b)",
		"a === b;":   "(a === b)",
		"a <= b;":    "(a <= b)",
		"f(a >= b);": "f((a >= b))",
	})
}

func TestADeclarationRefusesToBindAWordTheParserReadsAsAKeyword(t *testing.T) {
	// `let if = 1` otherwise yields a declaration binding the name "if" — a tree
	// that renders, walks and resolves, for source no JavaScript engine accepts.
	// The identifier lexer has no notion of reserved words, so the check has to
	// be made here, and a suite that only tried `let a = 1` never reaches it.
	for _, src := range []string{
		"let if = 1;", "let return = 1;", "let else = 1;", "let for = 1;",
		"var class = 1;", "const true = 1;", "let null = 1;", "let a, if = 1;",
	} {
		_, err := js.Statements{}.Parse([]byte(src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v, want a parse.SyntaxError", src, err)
			continue
		}
		if !strings.Contains(se.Want, "keyword") {
			t.Errorf("%q: Want = %q, which does not say the name is a keyword", src, se.Want)
		}
	}
	// A name that merely CONTAINS a keyword is still a name, so the check is on
	// the whole word rather than on a prefix.
	stmts(t, map[string]string{
		"let iffy = 1;":     "let iffy = 1",
		"let returned = 1;": "let returned = 1",
		"let truthy = 1;":   "let truthy = 1",
	})
}

func TestAKeywordIsMatchedAsAWholeWord(t *testing.T) {
	// `lettuce` begins with `let` and `iffy` with `if`. A keyword tested by
	// prefix turns each of them into the construct it starts with, and the
	// resulting error blames the character after the prefix — which is an
	// ordinary letter in an ordinary name.
	stmts(t, map[string]string{
		"lettuce();":      "lettuce()",
		"iffy(a);":        "iffy(a)",
		"returnValue();":  "returnValue()",
		"constant + 1;":   "(constant + 1)",
		"variable.x;":     "variable.x",
		"forEach(xs);":    "forEach(xs)",
		"classy();":       "classy()",
		"switcher();":     "switcher()",
		"elsewhere();":    "elsewhere()",
		"let letter = 1;": "let letter = 1",
		"iffy ? a : b;":   "(iffy ? a : b)",
	})
}

func TestTheDialectIsThreadedIntoTheExpressionsStatementsContain(t *testing.T) {
	// The dialect must reach the expression layer, not just the format name.
	// A statement parser that built its own default expression parser would
	// accept `===` under the C dialect and hand back a tree for source C cannot
	// express — and FormatName would still read "c-statements".
	c := js.Statements{Dialect: &js.C}
	if c.FormatName() != "c-statements" {
		t.Errorf("FormatName = %q, want c-statements", c.FormatName())
	}
	if _, err := c.Parse([]byte("f(a === b);")); err == nil {
		t.Error("the C dialect accepted ===")
	}
	if _, err := (js.Statements{}).Parse([]byte("f(a === b);")); err != nil {
		t.Errorf("the JavaScript dialect refused ===: %v", err)
	}
	if got := (js.Statements{}).FormatName(); got != "js-statements" {
		t.Errorf("FormatName = %q, want js-statements", got)
	}
}

func TestCommentsSeparateStatementsWithoutBecomingOne(t *testing.T) {
	// Comments are skipped by the expression parser's whitespace rule, which the
	// statement layer reuses — so the case worth pinning is the one where the
	// comment carries the line break the terminator rule needs.
	stmts(t, map[string]string{
		"f(); // done\ng();":       "f() g()",
		"/* lead */ f();":          "f()",
		"f() /* a */ /* b */\ng()": "f() g()",
		"{ // empty\n}":            "{}",
	})
	// An unterminated block comment is unfinished rather than wrong, at every
	// position a statement can start.
	stmtRefuses(t, true, "f(); /* unfinished", "/* unfinished", "{ /* unfinished")
}

func TestABraceAtStatementPositionOpensABlockNotAnObjectLiteral(t *testing.T) {
	// JavaScript reads it as a block too, so a parser that guessed "object
	// literal" here would accept source with a different meaning. The refusal of
	// `{a: 1}` is the evidence the block won: as a block it is a statement `a`
	// followed by a colon, which nothing can continue.
	stmts(t, map[string]string{
		"({a: 1});": "{a: 1}",
		"{ a; }":    "{a}",
	})
	stmtRefuses(t, false, "{a: 1}")
}
