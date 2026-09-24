package js

import (
	"github.com/yongjohnlee80/golib/parse"

	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Statements parses a sequence of JavaScript statements — the body of a QML
// signal handler, and the layer that sits ON TOP of [Expression].
//
// Statements contain expressions and never the reverse, so this file adds a
// grammar above the expression one and reuses [ExprDialect] unchanged for
// everything below the statement level. A second dialect table for the same
// operators would drift from the first, and the drift would be invisible: both
// halves would keep parsing.
//
// # What it parses
//
//	Block       := '{' { Statement } '}'
//	Statement   := Block | Declaration | If | Return | ExpressionStmt | Empty
//	Declaration := ( 'let' | 'const' | 'var' ) Ident [ '=' Expression ]
//	               { ',' Ident [ '=' Expression ] } Terminator
//	If          := 'if' '(' Expression ')' Statement [ 'else' Statement ]
//	Return      := 'return' [ Expression ] Terminator
//	ExpressionStmt := Expression Terminator
//	Empty       := ';'
//
// A source with no statements at all is an empty program, not an error: an
// empty handler body is something a person writes on purpose.
//
// # What it does NOT parse
//
// Loops, switch, try, throw, and function and class declarations. Each is
// REFUSED BY NAME — `for` is told that the for loop is not implemented rather
// than being read as an identifier and then blamed on the parenthesis after it.
// The distinction matters because the second kind of message sends a reader
// looking at the wrong construct, and a reader who cannot tell "unsupported"
// from "malformed" rewrites working code.
//
// Adding one of them is a row LEAVING [unsupportedStmt] plus a kind and a parse
// function; [Stmt] already carries the condition, body and branch fields a loop
// or a switch needs.
//
// Assignment is not parsed either, because [Expression] does not parse it — but
// `x = 1` at statement position is refused BY NAME here, since it is the first
// thing a person writes in a handler body and the terminator message alone
// would send them looking for a semicolon they did not omit.
//
// # Terminators: an explicit ';' or a line break, and no automatic insertion
//
// JavaScript's automatic semicolon insertion is not implemented. A simple
// statement ends at a `;`, at a line break, at the `}` that closes its block, or
// at end of input — and a statement that ends at none of those is REFUSED
// rather than guessed at. `f() g()` on one line is an error here, where
// JavaScript's rules make it one too but by a route no reader can follow.
//
// The consequence is smaller than it first looks, because the expression parser
// consumes across line breaks whenever an expression CAN continue: `a\n+ b` is
// one statement, and a call argument list spanning five lines is one statement.
// A line break ends a statement only where the expression could not have gone on
// anyway. Where this parser and ASI genuinely differ is that ASI's restricted
// productions are not special-cased — they do not need to be, since `return`
// followed by a line break already ends the statement, which is what ASI was
// written to achieve.
//
// `{` at statement position opens a BLOCK, as in JavaScript. An object literal
// there has to be parenthesised.
//
// # Incomplete input
//
// Every construct cut off at end of input reports [parse.SyntaxError] with Incomplete
// set, so a watcher can hold the last good tree instead of blanking a screen
// between keystrokes. A WRONG character is not incomplete: more typing will not
// fix it, and a watcher told otherwise holds a stale tree for as long as the
// typo survives.
//
// # Diagnostics
//
// Errors from the statement layer name the format as "<dialect>-statements";
// errors from inside an expression keep "<dialect>-expression", because that is
// the sub-parser that refused and the reader is better served knowing which
// layer had the objection.
//
// The zero value parses [JavaScript].
type Statements struct {
	// Dialect selects the language below the statement level. The zero value
	// means [JavaScript].
	//
	// The statement grammar itself is not dialect-driven: `let` and `if` are
	// spelled the same in every C-family language that has them, and the parts
	// that genuinely differ are the ones this revision does not implement.
	Dialect *ExprDialect

	// MaxDepth bounds nesting. Zero means [DefaultExprMaxDepth].
	//
	// It is one budget for statements AND the expressions inside them, since
	// what has to stay bounded is the recursion, and the recursion alternates
	// between the two. An `else if` chain nests structurally, so a chain longer
	// than the limit is refused like any other deep nesting.
	MaxDepth int
}

// FormatName implements [parse.Named].
func (s Statements) FormatName() string { return s.dialect().Name + "-statements" }

func (s Statements) dialect() *ExprDialect {
	if s.Dialect == nil {
		return &JavaScript
	}
	return s.Dialect
}

// StmtKind classifies a node of the statement tree.
type StmtKind uint8

const (
	// StmtInvalid is the zero value and never appears in a parsed tree.
	StmtInvalid StmtKind = iota
	// StmtBlock is `{ … }`; Body holds the statements inside it.
	StmtBlock
	// StmtEmpty is a bare `;`. It is kept rather than dropped because a
	// consumer rewriting source needs to know it was written.
	StmtEmpty
	// StmtExpr is an expression evaluated for its effect; Value holds it.
	StmtExpr
	// StmtDeclaration is `let`, `const` or `var`. Raw holds which, and Decls
	// holds every name it binds, in order.
	StmtDeclaration
	// StmtIf is `if (Cond) Then [else Else]`.
	StmtIf
	// StmtReturn is `return [Value]`. Value is nil for a bare `return`, which
	// is a different statement from one returning `undefined` to any consumer
	// that reproduces the source.
	StmtReturn
)

// String renders the kind for diagnostics.
func (k StmtKind) String() string {
	switch k {
	case StmtBlock:
		return "block"
	case StmtEmpty:
		return "empty"
	case StmtExpr:
		return "expression"
	case StmtDeclaration:
		return "declaration"
	case StmtIf:
		return "if"
	case StmtReturn:
		return "return"
	default:
		return "invalid"
	}
}

// Stmt is one node of a parsed statement tree.
//
// The fields a node uses depend on its Kind, and [StmtKind]'s documentation
// says which. The set is deliberately a little wider than the grammar needs:
// Cond and Body are what a `while` would use, and Then and Else are what a
// `for` body would use, so the constructs this revision refuses can be added
// without reshaping the node every consumer already switches on.
type Stmt struct {
	Kind StmtKind
	Pos  parse.Position
	// Raw is the keyword as written — "let", "const", "var" — or the punctuation
	// that names the form: "{}" for a block, ";" for an empty statement.
	Raw string
	// Body holds the statements of a block.
	Body []Stmt
	// Cond is the test of an if.
	Cond *Expr
	// Then is the branch taken when Cond holds. It is a single statement, which
	// may be a block.
	Then *Stmt
	// Else is the branch taken otherwise, or nil when no `else` was written.
	Else *Stmt
	// Value is the expression of an expression statement, or of a return.
	Value *Expr
	// Decls holds the names a declaration binds, in the order written.
	Decls []Declarator
}

// Declarator is one name bound by a declaration.
type Declarator struct {
	// Name is the identifier as written.
	Name string
	Pos  parse.Position
	// Init is the initialiser, or nil when the name was declared without one.
	//
	// A name with no initialiser is still RECORDED. It binds the name, and a
	// consumer resolving references against the declarations cannot otherwise
	// tell a name declared and assigned later from a name never declared at all.
	Init *Expr
}

// Walk calls fn for s and every statement beneath it, parents before children.
// Returning false prunes that statement's subtree.
func (s *Stmt) Walk(fn func(*Stmt) bool) { s.walk(fn, nil) }

// WalkExprs calls fn for every expression s contains — its own, and those of
// every statement nested inside it — and for every node beneath each of them.
//
// This is the traversal dependency discovery is built on, and the reason it is
// one call rather than "walk the statements, then remember to walk each one's
// expressions": a name that appears only in a nested if's condition is reachable
// no other way, and a consumer that misses it reports a binding that depends on
// nothing and then never updates. Nothing fails at parse time when that happens,
// so the traversal has to be the thing that cannot be got wrong.
func (s *Stmt) WalkExprs(fn func(*Expr) bool) { s.walk(nil, fn) }

// WalkStmts walks a whole program, as [Stmt.Walk] walks one statement.
func WalkStmts(list []Stmt, fn func(*Stmt) bool) {
	for i := range list {
		list[i].walk(fn, nil)
	}
}

// WalkExprs walks every expression of a whole program, as [Stmt.WalkExprs]
// walks those of one statement.
func WalkExprs(list []Stmt, fn func(*Expr) bool) {
	for i := range list {
		list[i].walk(nil, fn)
	}
}

// walk is the single enumeration of a statement's children, over both kinds of
// child at once.
//
// Both public traversals route through it so that a statement kind gaining a
// field has ONE place to be added rather than two that can disagree — and a
// disagreement between them is silent, since each traversal looks complete on
// its own.
func (s *Stmt) walk(onStmt func(*Stmt) bool, onExpr func(*Expr) bool) {
	if s == nil {
		return
	}
	if onStmt != nil && !onStmt(s) {
		return
	}
	if onExpr != nil {
		s.Cond.Walk(onExpr)
		s.Value.Walk(onExpr)
		for i := range s.Decls {
			s.Decls[i].Init.Walk(onExpr)
		}
	}
	for i := range s.Body {
		s.Body[i].walk(onStmt, onExpr)
	}
	s.Then.walk(onStmt, onExpr)
	s.Else.walk(onStmt, onExpr)
}

// Parse reads every statement in src.
func (s Statements) Parse(src []byte) ([]Stmt, error) {
	sc := parse.NewScanner(src)
	x := &exprParser{sc: sc, max: s.MaxDepth, d: s.dialect()}
	if x.max <= 0 {
		x.max = DefaultExprMaxDepth
	}
	p := &stmtParser{sc: sc, x: x}

	var out []Stmt
	for {
		if err := x.space(); err != nil {
			return nil, err
		}
		if sc.Done() {
			return out, nil
		}
		st, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
}

// stmtParser parses statements over the SAME scanner and depth budget as the
// expression parser it delegates to, so the two cannot disagree about where the
// cursor is or how deep the recursion has gone.
type stmtParser struct {
	sc *parse.Scanner
	x  *exprParser
}

func (p *stmtParser) format() string { return p.x.d.Name + "-statements" }

func (p *stmtParser) enter(at parse.Position) error {
	p.x.depth++
	if p.x.depth > p.x.max {
		return parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: "a statement nested no deeper than the limit",
			Got:  "deeper nesting",
		}
	}
	return nil
}

func (p *stmtParser) leave() { p.x.depth-- }

// unsupportedStmt maps a leading keyword to the refusal a reader gets for it.
//
// It is a table because the list is the whole difference between this revision
// and the next one: implementing `while` means deleting its row and adding a
// parse function, and nothing else here has to know. Every entry names the
// CONSTRUCT, because "unexpected token" at the parenthesis after `for` reads as
// a bug in the source rather than a gap in the parser.
var unsupportedStmt = map[string]string{
	"for":      "a statement; the for loop is not implemented",
	"while":    "a statement; the while loop is not implemented",
	"do":       "a statement; the do/while loop is not implemented",
	"switch":   "a statement; switch is not implemented",
	"case":     "a statement; switch is not implemented, so a case label has nothing to belong to",
	"try":      "a statement; try/catch is not implemented",
	"catch":    "a statement; try/catch is not implemented",
	"finally":  "a statement; try/catch is not implemented",
	"throw":    "a statement; throw is not implemented",
	"function": "a statement; the function declaration is not implemented",
	"class":    "a statement; the class declaration is not implemented",
	"break":    "a statement; break is not implemented",
	"continue": "a statement; continue is not implemented",
	"else":     "a statement; an else must follow an if",
}

func (p *stmtParser) statement() (Stmt, error) {
	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	at := p.sc.Pos()
	r, ok := p.sc.Peek()
	if !ok {
		return Stmt{}, parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: "a statement", Got: "end of input", Incomplete: true,
		}
	}

	switch r {
	case '{':
		return p.block(at)
	case ';':
		p.sc.Take(";")
		return Stmt{Kind: StmtEmpty, Pos: at, Raw: ";"}, nil
	case '}':
		// Reached only outside any block, so there is nothing for it to close.
		// Saying so beats "want a statement": the reader has one brace too many,
		// not a missing statement.
		return Stmt{}, parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: "a statement; this } closes nothing", Got: parse.QuoteRune(r),
		}
	}

	// The keyword is matched as a WHOLE WORD by reading the identifier the
	// source actually has: `lettuce` and `iffy` start with keywords, and a
	// prefix test would turn each of them into a declaration or a condition.
	switch kw := p.peekWord(); kw {
	case "let", "const", "var":
		return p.declaration(at, kw)
	case "if":
		return p.ifStmt(at)
	case "return":
		return p.returnStmt(at)
	default:
		if want, ok := unsupportedStmt[kw]; ok {
			return Stmt{}, parse.SyntaxError{
				Format: p.format(), Pos: at, Want: want, Got: `"` + kw + `"`,
			}
		}
	}
	return p.exprStmt(at)
}

func (p *stmtParser) block(at parse.Position) (Stmt, error) {
	p.sc.Take("{")
	if err := p.enter(at); err != nil {
		return Stmt{}, err
	}
	defer p.leave()

	s := Stmt{Kind: StmtBlock, Pos: at, Raw: "{}"}
	for {
		if err := p.x.space(); err != nil {
			return Stmt{}, err
		}
		if p.sc.Done() {
			return Stmt{}, parse.SyntaxError{
				Format: p.format(), Pos: at,
				Want: "} to close the block opened here", Got: "end of input",
				Incomplete: true,
			}
		}
		if p.sc.Take("}") {
			return s, nil
		}
		inner, err := p.statement()
		if err != nil {
			return Stmt{}, err
		}
		s.Body = append(s.Body, inner)
	}
}

func (p *stmtParser) declaration(at parse.Position, kw string) (Stmt, error) {
	p.sc.Take(kw)
	s := Stmt{Kind: StmtDeclaration, Pos: at, Raw: kw}
	for {
		if err := p.x.space(); err != nil {
			return Stmt{}, err
		}
		nameAt := p.sc.Pos()
		name, ok := p.x.ident()
		if !ok {
			return Stmt{}, p.here(nameAt, "a name after "+kw)
		}
		if p.reserved(name) {
			// `let if = 1` otherwise binds the name "if", and nothing downstream
			// can tell that binding from a real one: a consumer resolving names
			// reports it, an evaluator looks for it, and the source it came from
			// is not JavaScript at all.
			return Stmt{}, parse.SyntaxError{
				Format: p.format(), Pos: nameAt,
				Want: "a name after " + kw + ", and " + name + " is a keyword",
				Got:  `"` + name + `"`,
			}
		}
		d := Declarator{Name: name, Pos: nameAt}

		if err := p.x.space(); err != nil {
			return Stmt{}, err
		}
		if eqAt := p.sc.Pos(); p.sc.Take("=") {
			init, err := p.expr(eqAt, "an initialiser after =")
			if err != nil {
				return Stmt{}, err
			}
			d.Init = &init
		}
		s.Decls = append(s.Decls, d)

		if err := p.x.space(); err != nil {
			return Stmt{}, err
		}
		if !p.sc.Take(",") {
			break
		}
	}
	if err := p.terminate(at, "the "+kw+" declaration"); err != nil {
		return Stmt{}, err
	}
	return s, nil
}

// ifStmt parses an if and, where one follows, its else.
//
// The else is taken HERE, by the innermost if that has not yet found one, which
// is what makes it bind to the nearest if: `if (a) if (b) x(); else y();` runs
// y only when a holds and b does not. Leaving the else for an outer call to
// claim produces a tree of exactly the same node kinds that runs y in the case
// the source excludes, and no test that counts nodes can see the difference.
func (p *stmtParser) ifStmt(at parse.Position) (Stmt, error) {
	p.sc.Take("if")
	if err := p.enter(at); err != nil {
		return Stmt{}, err
	}
	defer p.leave()

	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	if !p.sc.Take("(") {
		return Stmt{}, p.here(at, "( after if")
	}
	cond, err := p.expr(at, "a condition after if (")
	if err != nil {
		return Stmt{}, err
	}
	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	if !p.sc.Take(")") {
		return Stmt{}, p.closer(at, ") to close the if condition opened here")
	}

	then, err := p.branch(at, "a statement after if (…)")
	if err != nil {
		return Stmt{}, err
	}
	s := Stmt{Kind: StmtIf, Pos: at, Raw: "if", Cond: &cond, Then: &then}

	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	if p.peekWord() == "else" {
		elseAt := p.sc.Pos()
		p.sc.Take("else")
		alt, err := p.branch(elseAt, "a statement after else")
		if err != nil {
			return Stmt{}, err
		}
		s.Else = &alt
	}
	return s, nil
}

func (p *stmtParser) returnStmt(at parse.Position) (Stmt, error) {
	p.sc.Take("return")
	s := Stmt{Kind: StmtReturn, Pos: at, Raw: "return"}

	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	// A bare `return` is complete, so end of input here is NOT incomplete — and
	// a line break ends it too, which is the one place JavaScript's automatic
	// insertion changes meaning rather than only punctuation.
	if p.sc.Done() || p.sc.HasPrefix(";") || p.sc.HasPrefix("}") || p.brokeLine(at) {
		return s, p.terminate(at, "return")
	}
	v, err := p.expr(at, "an expression after return")
	if err != nil {
		return Stmt{}, err
	}
	s.Value = &v
	return s, p.terminate(at, "the returned expression")
}

func (p *stmtParser) exprStmt(at parse.Position) (Stmt, error) {
	e, err := p.x.expression()
	if err != nil {
		return Stmt{}, err
	}
	if err := p.terminate(at, "the expression statement"); err != nil {
		return Stmt{}, err
	}
	return Stmt{Kind: StmtExpr, Pos: at, Value: &e}, nil
}

// branch reads the single statement an if or an else governs.
func (p *stmtParser) branch(at parse.Position, want string) (Stmt, error) {
	if err := p.x.space(); err != nil {
		return Stmt{}, err
	}
	if p.sc.Done() {
		return Stmt{}, parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	return p.statement()
}

// expr reads an expression a statement requires, reporting the end of input as
// INCOMPLETE against the keyword that promised one.
func (p *stmtParser) expr(at parse.Position, want string) (Expr, error) {
	if err := p.x.space(); err != nil {
		return Expr{}, err
	}
	if p.sc.Done() {
		return Expr{}, parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	return p.x.expression()
}

// terminate consumes the end of a simple statement, or refuses.
func (p *stmtParser) terminate(start parse.Position, what string) error {
	if err := p.x.space(); err != nil {
		return err
	}
	if p.sc.Take(";") {
		return nil
	}
	// A closing brace ends the last statement of a block, and end of input ends
	// the last statement of a file: both are finished statements, not unfinished
	// ones, so neither is Incomplete.
	if p.sc.Done() || p.sc.HasPrefix("}") || p.brokeLine(start) {
		return nil
	}
	// An `=` left here can only be an assignment: every operator spelled with one
	// that the expression grammar HAS — `==`, `===`, `<=` — was consumed before
	// the expression parser gave up. Saying so matters more for assignment than
	// for any of the refused statements, because `x = 1` is what a person writes
	// first in a handler body, and "want ; or a line break" sends them looking
	// for a missing semicolon they did not omit. `=>` is excluded because the
	// missing construct there is the arrow function, not the assignment.
	if p.sc.HasPrefix("=") && !p.sc.HasPrefix("=>") {
		return parse.SyntaxError{
			Format: p.format(), Pos: p.sc.Pos(),
			Want: "a statement; assignment is not implemented", Got: `"="`,
		}
	}
	r, _ := p.sc.Peek()
	return parse.SyntaxError{
		Format: p.format(), Pos: p.sc.Pos(),
		Want: "; or a line break after " + what, Got: parse.QuoteRune(r),
	}
}

// brokeLine reports whether a line break separates what has been parsed so far
// from the cursor.
//
// It looks BACKWARDS, because by the time an expression returns, the expression
// parser's own trailing-space skip has already consumed the line break this
// rule rests on — and the cursor's line number cannot answer the question, since
// the expression may itself have spanned several lines. Scanning back from the
// cursor stops at the first character of real source, so the start bound is only
// a floor, never the answer.
func (p *stmtParser) brokeLine(start parse.Position) bool {
	return breaksLine(p.sc.Slice(start.Offset, p.sc.Pos().Offset))
}

func breaksLine(gap []byte) bool {
	for len(gap) > 0 {
		r, size := utf8.DecodeLastRune(gap)
		switch {
		case r == '\n':
			return true
		case unicode.IsSpace(r):
			gap = gap[:len(gap)-size]
		case r == '/' && len(gap) >= 4 && gap[len(gap)-2] == '*':
			// A block comment already skipped as whitespace. The search goes
			// THROUGH it rather than stopping, because the break may be inside
			// it — `f() /*\n*/ g()` is two statements on two lines however it
			// looks to a scan that gives up at the first non-space byte.
			open := bytes.LastIndex(gap[:len(gap)-2], []byte("/*"))
			if open < 0 {
				return false
			}
			if bytes.IndexByte(gap[open:], '\n') >= 0 {
				return true
			}
			gap = gap[:open]
		default:
			return false
		}
	}
	return false
}

// reserved reports whether a word is one this parser reads as something other
// than a name.
//
// It is derived from the tables that already decide those readings — the
// statement keywords, the refused constructs, and the dialect's own literals —
// rather than from a list of JavaScript's reserved words. A separate list would
// be a second opinion about the same question, and the two would disagree the
// moment either table changed.
func (p *stmtParser) reserved(w string) bool {
	switch w {
	case "let", "const", "var", "if", "else", "return":
		return true
	}
	if _, ok := unsupportedStmt[w]; ok {
		return true
	}
	_, ok := p.x.d.Literals[w]
	return ok
}

// peekWord returns the identifier at the cursor WITHOUT consuming it, so a word
// that turns out not to be a keyword can still be read as the start of an
// expression.
func (p *stmtParser) peekWord() string {
	r, ok := p.sc.Peek()
	if !ok || !isExprIdentStart(r) {
		return ""
	}
	var b strings.Builder
	for i := 0; ; i++ {
		r, ok := p.sc.PeekAt(i)
		if !ok || !isExprIdentPart(r) {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// here reports a failure at the cursor, wanting what. End of input is
// INCOMPLETE; a wrong character is not.
func (p *stmtParser) here(at parse.Position, want string) error {
	if p.sc.Done() {
		return parse.SyntaxError{
			Format: p.format(), Pos: at,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	r, _ := p.sc.Peek()
	return parse.SyntaxError{Format: p.format(), Pos: p.sc.Pos(), Want: want, Got: parse.QuoteRune(r)}
}

// closer reports a missing closing delimiter, pointing at where it was opened.
func (p *stmtParser) closer(openAt parse.Position, want string) error {
	if p.sc.Done() {
		return parse.SyntaxError{
			Format: p.format(), Pos: openAt,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	r, _ := p.sc.Peek()
	return parse.SyntaxError{Format: p.format(), Pos: p.sc.Pos(), Want: want, Got: parse.QuoteRune(r)}
}
