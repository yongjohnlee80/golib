package parse

import (
	"strings"
	"unicode"
)

// Expression parses a single expression from a C-FAMILY language.
//
// It is a standalone format in its own right, not a helper for one consumer.
//
// # Why one parser rather than one per language
//
// C, C++, Java, C#, Go, JavaScript, TypeScript and PHP share an expression
// grammar: the same precedence ladder, the same member/index/call postfix
// chain, the same unary and conditional forms. What differs between them is
// almost entirely a TABLE — which operator spellings exist, which keywords are
// literals, whether templates or a conditional are allowed.
//
// So the dialect is [ExprDialect], and it is data. Adding C or Go is an entry
// in a table rather than a second parser that will drift from this one. That is
// the same reason the operator precedence itself is a table below: a language
// is a list of differences, and code that encodes those differences as control
// flow makes every one of them a place to get it wrong.
//
// QML property bindings are JavaScript expressions, so [QML] will use
// [JavaScript]; a full JavaScript parser adds statements and declarations ON
// TOP of this rather than beside it, because expressions are the part every
// other construct contains.
//
// # What it parses
//
// The full expression grammar, by precedence, loosest first:
//
//	Conditional  := Nullish [ '?' Assign ':' Assign ]
//	Nullish      := LogicalOr { '??' LogicalOr }
//	LogicalOr    := LogicalAnd { '||' LogicalAnd }
//	LogicalAnd   := BitOr { '&&' BitOr }
//	BitOr        := BitXor { '|' BitXor }
//	BitXor       := BitAnd { '^' BitAnd }
//	BitAnd       := Equality { '&' Equality }
//	Equality     := Relational { ( '===' | '!==' | '==' | '!=' ) Relational }
//	Relational   := Shift { ( '<=' | '>=' | '<' | '>' | 'instanceof' | 'in' ) Shift }
//	Shift        := Additive { ( '>>>' | '<<' | '>>' ) Additive }
//	Additive     := Multiplicative { ( '+' | '-' ) Multiplicative }
//	Multiplicative := Exponent { ( '*' | '/' | '%' ) Exponent }
//	Exponent     := Unary [ '**' Exponent ]            // RIGHT associative
//	Unary        := ( '!' | '-' | '+' | '~' | 'typeof' | 'void' ) Unary | Postfix
//	Postfix      := Primary { '.' Ident | '?.' Ident | '[' Expr ']' | '(' Args ')' }
//	Primary      := Number | String | Template | Bool | Null | Undefined
//	              | Ident | Array | Object | '(' Expr ')'
//
// # What it does NOT parse, and why that is a boundary rather than a gap
//
// Statements, declarations, function bodies and assignment. An expression
// evaluates to a value; a statement changes the world. Keeping the line here is
// what lets a consumer accept an expression from an untrusted file without
// accepting a program — and it is where a JavaScript parser would ADD, not
// where it would have to undo something.
//
// Arrow functions are excluded for the same reason: their body may be a block,
// which is statements.
//
// # Incomplete input
//
// Source that stops mid-expression reports [SyntaxError] with Incomplete set,
// so a caller watching a file being written can hold what it has instead of
// showing an error for every keystroke.
//
// The zero value parses [JavaScript].
type Expression struct {
	// Dialect selects the language. The zero value means [JavaScript].
	Dialect *ExprDialect

	// MaxDepth bounds nesting. Zero means [DefaultExprMaxDepth].
	//
	// The parser is recursive and an expression can nest without limit, so a
	// file from a watcher — or from anywhere less trusted — could otherwise
	// exhaust the goroutine stack, which cannot be recovered.
	MaxDepth int
}

// DefaultExprMaxDepth is the nesting limit [Expression] applies when MaxDepth is
// zero. Deep enough for any expression a person writes, shallow enough that the
// recursion cannot exhaust a stack.
const DefaultExprMaxDepth = 64

// FormatName implements [Named].
func (x Expression) FormatName() string { return x.dialect().Name + "-expression" }

func (x Expression) dialect() *ExprDialect {
	if x.Dialect == nil {
		return &JavaScript
	}
	return x.Dialect
}

// ExprDialect is the difference between one C-family expression language and
// another, as data.
//
// Every field is a list rather than a flag where it can be, because the next
// language is usually "the same, plus these two operators" — and a list absorbs
// that without anyone editing the parser.
type ExprDialect struct {
	// Name appears in diagnostics: "js", "c", "go".
	Name string
	// Binary holds the precedence levels, LOOSEST FIRST. Within a level the
	// longest spelling must come first, so `>>>` is never read as `>>` then `>`.
	Binary [][]string
	// Unary holds the prefix operators.
	Unary []string
	// Words are the operators spelled as words, which must not match a longer
	// identifier: `in` must not match inside `international`.
	Words []string
	// Literals maps a keyword to the kind it produces: "true" to ExprBool,
	// "null" or "nil" to ExprNull.
	Literals map[string]ExprKind
	// Conditional allows `a ? b : c`. Go has no such operator.
	Conditional bool
	// Optional allows `a?.b`.
	Optional bool
	// Templates allows backtick template literals.
	Templates bool
	// Exponent allows a right-associative `**`.
	Exponent bool
}

// JavaScript is the default dialect.
var JavaScript = ExprDialect{
	Name: "js",
	Binary: [][]string{
		{"??"},
		{"||"},
		{"&&"},
		{"|"},
		{"^"},
		{"&"},
		{"===", "!==", "==", "!="},
		{"<=", ">=", "<", ">", "instanceof", "in"},
		{">>>", "<<", ">>"},
		{"+", "-"},
		{"*", "/", "%"},
	},
	Unary: []string{"typeof", "void", "!", "~", "-", "+"},
	Words: []string{"instanceof", "in", "typeof", "void"},
	Literals: map[string]ExprKind{
		"true": ExprBool, "false": ExprBool,
		"null": ExprNull, "undefined": ExprUndefined,
	},
	Conditional: true, Optional: true, Templates: true, Exponent: true,
}

// C is the C and C++ expression dialect.
//
// It is here to prove the table carries a second language, and because the
// deltas are exactly the kind that would otherwise become a fork: no `===`, no
// `??`, no templates, no `**`, and `sizeof` joins the unary operators.
var C = ExprDialect{
	Name: "c",
	Binary: [][]string{
		{"||"},
		{"&&"},
		{"|"},
		{"^"},
		{"&"},
		{"==", "!="},
		{"<=", ">=", "<", ">"},
		{"<<", ">>"},
		{"+", "-"},
		{"*", "/", "%"},
	},
	Unary:       []string{"sizeof", "!", "~", "-", "+", "*", "&"},
	Words:       []string{"sizeof"},
	Literals:    map[string]ExprKind{"true": ExprBool, "false": ExprBool, "NULL": ExprNull},
	Conditional: true,
}

// Go is the Go expression dialect: no conditional operator, and `&^`.
var Go = ExprDialect{
	Name: "go",
	Binary: [][]string{
		{"||"},
		{"&&"},
		{"==", "!=", "<=", ">=", "<", ">"},
		{"|"},
		{"^"},
		{"&^", "&"},
		{"<<", ">>"},
		{"+", "-"},
		{"*", "/", "%"},
	},
	Unary:    []string{"!", "^", "-", "+", "*", "&"},
	Literals: map[string]ExprKind{"true": ExprBool, "false": ExprBool, "nil": ExprNull},
}

// ExprKind classifies a node of the expression tree.
type ExprKind uint8

const (
	// ExprInvalid is the zero value and never appears in a parsed tree.
	ExprInvalid ExprKind = iota
	// ExprNumber is a numeric literal. Raw holds it as written, UNDECODED:
	// the consumer knows whether the target is an int, a float or something
	// else, and decoding here would pick one of those answers too early.
	ExprNumber
	// ExprString is a string literal. Raw holds the contents, unquoted.
	ExprString
	// ExprTemplate is a template literal. Raw holds the text between the
	// backticks; substitutions are not parsed in this revision and their `${}`
	// is left in Raw as written.
	ExprTemplate
	// ExprBool is true or false. Raw holds which.
	ExprBool
	// ExprNull is the null literal.
	ExprNull
	// ExprUndefined is the undefined literal.
	ExprUndefined
	// ExprIdent is a bare name. Raw holds it.
	ExprIdent
	// ExprMember is property access. Left is the object. For `a.b` Name holds
	// "b"; for `a[expr]` Right holds the index expression and Computed is set.
	// Optional holds whether it was written `?.`.
	ExprMember
	// ExprCall is an invocation. Left is the callee and Args the arguments.
	ExprCall
	// ExprUnary is a prefix operator. Raw holds it, Left holds the operand.
	ExprUnary
	// ExprBinary is an infix operator. Raw holds it, Left and Right the operands.
	ExprBinary
	// ExprLogical is &&, || or ??. It is separate from ExprBinary because these
	// SHORT-CIRCUIT: an evaluator must not treat them as ordinary operators
	// whose operands are both evaluated first.
	ExprLogical
	// ExprConditional is `Left ? Right : Alt`.
	ExprConditional
	// ExprArray is an array literal; Args holds the elements.
	ExprArray
	// ExprObject is an object literal; Props holds the entries.
	ExprObject
)

// String renders the kind for diagnostics.
func (k ExprKind) String() string {
	switch k {
	case ExprNumber:
		return "number"
	case ExprString:
		return "string"
	case ExprTemplate:
		return "template"
	case ExprBool:
		return "bool"
	case ExprNull:
		return "null"
	case ExprUndefined:
		return "undefined"
	case ExprIdent:
		return "identifier"
	case ExprMember:
		return "member"
	case ExprCall:
		return "call"
	case ExprUnary:
		return "unary"
	case ExprBinary:
		return "binary"
	case ExprLogical:
		return "logical"
	case ExprConditional:
		return "conditional"
	case ExprArray:
		return "array"
	case ExprObject:
		return "object"
	default:
		return "invalid"
	}
}

// Expr is one node of a parsed expression.
//
// The fields a node uses depend on its Kind, and [ExprKind]'s documentation
// says which. They are named rather than packed into a single operand slice
// because a reader of an evaluator should be able to tell an operand from an
// argument without counting.
type Expr struct {
	Kind ExprKind
	Pos  Position
	// Raw is the literal text, the identifier, or the operator symbol.
	Raw string
	// Left is the unary operand, the binary left, the member object, the call
	// callee, or the conditional test.
	Left *Expr
	// Right is the binary right, the computed member index, or the conditional
	// consequent.
	Right *Expr
	// Alt is the conditional alternative.
	Alt *Expr
	// Args holds call arguments or array elements.
	Args []Expr
	// Props holds object-literal entries.
	Props []ExprProperty
	// Name is the property name of a non-computed member access.
	Name string
	// Computed reports `a[b]` rather than `a.b`.
	Computed bool
	// Optional reports `?.` rather than `.`.
	Optional bool
}

// ExprProperty is one entry of an object literal.
type ExprProperty struct {
	// Key is the property name as written, unquoted.
	Key string
	// Computed reports `[expr]:` rather than a plain key; KeyExpr holds it.
	Computed bool
	KeyExpr  *Expr
	Value    Expr
	Pos      Position
}

// Walk calls fn for e and every node beneath it, parents before children.
//
// It exists because every consumer of an expression needs the same traversal —
// collecting identifiers, checking for forbidden constructs, rewriting — and a
// tree whose shape varies by Kind is exactly the thing each of them would
// otherwise get subtly wrong.
func (e *Expr) Walk(fn func(*Expr) bool) {
	if e == nil || !fn(e) {
		return
	}
	e.Left.Walk(fn)
	e.Right.Walk(fn)
	e.Alt.Walk(fn)
	for i := range e.Args {
		e.Args[i].Walk(fn)
	}
	for i := range e.Props {
		e.Props[i].KeyExpr.Walk(fn)
		e.Props[i].Value.Walk(fn)
	}
}

// Parse reads one expression from src and requires that it be the whole of it.
func (x Expression) Parse(src []byte) (Expr, error) {
	p := &exprParser{sc: NewScanner(src), max: x.MaxDepth, d: x.dialect()}
	if p.max <= 0 {
		p.max = DefaultExprMaxDepth
	}
	_ = p.format()
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if p.sc.Done() {
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: p.sc.Pos(),
			Want: "an expression", Got: "end of input", Incomplete: true,
		}
	}
	e, err := p.expression()
	if err != nil {
		return Expr{}, err
	}
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if !p.sc.Done() {
		r, _ := p.sc.Peek()
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: p.sc.Pos(),
			Want: "end of the expression", Got: quoteRune(r),
		}
	}
	return e, nil
}

type exprParser struct {
	sc    *Scanner
	d     *ExprDialect
	max   int
	depth int
}

func (p *exprParser) format() string { return p.d.Name + "-expression" }

func (p *exprParser) enter(at Position) error {
	p.depth++
	if p.depth > p.max {
		return SyntaxError{
			Format: p.format(), Pos: at,
			Want: "an expression nested no deeper than the limit",
			Got:  "deeper nesting",
		}
	}
	return nil
}

func (p *exprParser) leave() { p.depth-- }

// space skips whitespace and comments. An unterminated block comment is
// INCOMPLETE rather than invalid: the writer has not finished typing it.
func (p *exprParser) space() error {
	for {
		for {
			r, ok := p.sc.Peek()
			if !ok || !unicode.IsSpace(r) {
				break
			}
			p.sc.Next()
		}
		if p.sc.HasPrefix("//") {
			for {
				r, ok := p.sc.Next()
				if !ok || r == '\n' {
					break
				}
			}
			continue
		}
		if p.sc.HasPrefix("/*") {
			at := p.sc.Pos()
			p.sc.Take("/*")
			for {
				if p.sc.Done() {
					return SyntaxError{
						Format: p.format(), Pos: at,
						Want: "*/ to close the comment opened here", Got: "end of input",
						Incomplete: true,
					}
				}
				if p.sc.Take("*/") {
					break
				}
				p.sc.Next()
			}
			continue
		}
		return nil
	}
}

func isLogical(op string) bool { return op == "&&" || op == "||" || op == "??" }

func (p *exprParser) expression() (Expr, error) { return p.conditional() }

func (p *exprParser) conditional() (Expr, error) {
	at := p.sc.Pos()
	if err := p.enter(at); err != nil {
		return Expr{}, err
	}
	defer p.leave()

	cond, err := p.binary(0)
	if err != nil {
		return Expr{}, err
	}
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	// `?.` is a member operator, not the start of a conditional.
	if !p.d.Conditional || !p.sc.HasPrefix("?") || p.sc.HasPrefix("?.") || p.sc.HasPrefix("??") {
		return cond, nil
	}
	qAt := p.sc.Pos()
	p.sc.Take("?")

	then, err := p.operand(qAt, "an expression after ?")
	if err != nil {
		return Expr{}, err
	}
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if !p.sc.Take(":") {
		return Expr{}, p.here(qAt, ": to complete the conditional started here")
	}
	alt, err := p.operand(qAt, "an expression after :")
	if err != nil {
		return Expr{}, err
	}
	return Expr{Kind: ExprConditional, Pos: cond.Pos, Raw: "?:",
		Left: &cond, Right: &then, Alt: &alt}, nil
}

// binary parses one precedence level and everything tighter than it.
func (p *exprParser) binary(level int) (Expr, error) {
	if level >= len(p.d.Binary) {
		return p.unary()
	}
	left, err := p.binary(level + 1)
	if err != nil {
		return Expr{}, err
	}
	for {
		if err := p.space(); err != nil {
			return Expr{}, err
		}
		op, opAt, ok := p.takeOperator(p.d.Binary[level])
		if !ok {
			return left, nil
		}
		right, err := p.binaryOperand(level, opAt, op)
		if err != nil {
			return Expr{}, err
		}
		kind := ExprBinary
		if isLogical(op) {
			kind = ExprLogical
		}
		l, r := left, right
		left = Expr{Kind: kind, Pos: l.Pos, Raw: op, Left: &l, Right: &r}
	}
}

func (p *exprParser) binaryOperand(level int, opAt Position, op string) (Expr, error) {
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if p.sc.Done() {
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: opAt,
			Want: "an expression after " + op, Got: "end of input", Incomplete: true,
		}
	}
	return p.binary(level + 1)
}

func (p *exprParser) operand(at Position, want string) (Expr, error) {
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if p.sc.Done() {
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: at,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	return p.expression()
}

// takeOperator consumes the first of ops that is present, longest spelling
// first — so `>>>` is never read as `>>` followed by `>`, and `===` is never
// read as `==` followed by `=`.
func (p *exprParser) takeOperator(ops []string) (string, Position, bool) {
	at := p.sc.Pos()
	for _, op := range ops {
		if !p.sc.HasPrefix(op) {
			continue
		}
		if p.isWord(op) {
			// `instanceof` and `in` are words: `international` must not match.
			if r, ok := p.sc.PeekAt(len(op)); ok && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$') {
				continue
			}
		} else if op == "&" || op == "|" {
			// Do not steal the first character of && or ||.
			if r, ok := p.sc.PeekAt(1); ok && (r == '&' || r == '|') {
				continue
			}
		} else if op == "?" {
			continue
		}
		p.sc.Take(op)
		return op, at, true
	}
	return "", at, false
}

func (p *exprParser) isWord(op string) bool {
	for _, w := range p.d.Words {
		if w == op {
			return true
		}
	}
	return false
}

func (p *exprParser) unary() (Expr, error) {
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	at := p.sc.Pos()
	for _, op := range p.d.Unary {
		if !p.sc.HasPrefix(op) {
			continue
		}
		if p.isWord(op) {
			if r, ok := p.sc.PeekAt(len(op)); ok && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$') {
				continue
			}
		}
		p.sc.Take(op)
		if err := p.enter(at); err != nil {
			return Expr{}, err
		}
		operand, err := p.operand(at, "an expression after "+op)
		p.leave()
		if err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprUnary, Pos: at, Raw: op, Left: &operand}, nil
	}
	return p.exponent()
}

// exponent is RIGHT associative: 2 ** 3 ** 2 is 2 ** (3 ** 2).
func (p *exprParser) exponent() (Expr, error) {
	base, err := p.postfix()
	if err != nil {
		return Expr{}, err
	}
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if !p.d.Exponent || !p.sc.HasPrefix("**") {
		return base, nil
	}
	at := p.sc.Pos()
	p.sc.Take("**")
	rhs, err := p.operand(at, "an expression after **")
	if err != nil {
		return Expr{}, err
	}
	b := base
	return Expr{Kind: ExprBinary, Pos: b.Pos, Raw: "**", Left: &b, Right: &rhs}, nil
}

// postfix reads a primary and every member access, index and call that follows,
// left to right — so `a.b(c).d` chains in the order it is written.
func (p *exprParser) postfix() (Expr, error) {
	e, err := p.primary()
	if err != nil {
		return Expr{}, err
	}
	for {
		if err := p.space(); err != nil {
			return Expr{}, err
		}
		switch {
		case p.d.Optional && p.sc.HasPrefix("?."):
			at := p.sc.Pos()
			p.sc.Take("?.")
			name, ok := p.ident()
			if !ok {
				return Expr{}, p.here(at, "a name after ?.")
			}
			obj := e
			e = Expr{Kind: ExprMember, Pos: obj.Pos, Raw: name, Name: name,
				Left: &obj, Optional: true}

		case p.sc.HasPrefix(".") && !p.sc.HasPrefix(".."):
			at := p.sc.Pos()
			p.sc.Take(".")
			name, ok := p.ident()
			if !ok {
				return Expr{}, p.here(at, "a name after .")
			}
			obj := e
			e = Expr{Kind: ExprMember, Pos: obj.Pos, Raw: name, Name: name, Left: &obj}

		case p.sc.HasPrefix("["):
			at := p.sc.Pos()
			p.sc.Take("[")
			idx, err := p.operand(at, "an index expression after [")
			if err != nil {
				return Expr{}, err
			}
			if err := p.space(); err != nil {
				return Expr{}, err
			}
			if !p.sc.Take("]") {
				return Expr{}, p.closer(at, "] to close the index opened here")
			}
			obj := e
			e = Expr{Kind: ExprMember, Pos: obj.Pos, Raw: "[]",
				Left: &obj, Right: &idx, Computed: true}

		case p.sc.HasPrefix("("):
			at := p.sc.Pos()
			p.sc.Take("(")
			args, err := p.list(at, ")", "the argument list")
			if err != nil {
				return Expr{}, err
			}
			callee := e
			e = Expr{Kind: ExprCall, Pos: callee.Pos, Raw: "()", Left: &callee, Args: args}

		default:
			return e, nil
		}
	}
}

func (p *exprParser) primary() (Expr, error) {
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	at := p.sc.Pos()
	r, ok := p.sc.Peek()
	if !ok {
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: at,
			Want: "an expression", Got: "end of input", Incomplete: true,
		}
	}

	switch {
	case r == '(':
		p.sc.Take("(")
		if err := p.enter(at); err != nil {
			return Expr{}, err
		}
		inner, err := p.operand(at, "an expression after (")
		p.leave()
		if err != nil {
			return Expr{}, err
		}
		if err := p.space(); err != nil {
			return Expr{}, err
		}
		if !p.sc.Take(")") {
			return Expr{}, p.closer(at, ") to close the group opened here")
		}
		return inner, nil

	case r == '[':
		p.sc.Take("[")
		if err := p.enter(at); err != nil {
			return Expr{}, err
		}
		elems, err := p.list(at, "]", "the array")
		p.leave()
		if err != nil {
			return Expr{}, err
		}
		return Expr{Kind: ExprArray, Pos: at, Raw: "[]", Args: elems}, nil

	case r == '{':
		return p.object(at)

	case r == '"' || r == '\'':
		return p.stringLiteral(at, r)

	case r == '`' && p.d.Templates:
		return p.template(at)

	case r >= '0' && r <= '9', r == '.':
		return p.number(at)

	case isExprIdentStart(r):
		name, _ := p.ident()
		if kind, ok := p.d.Literals[name]; ok {
			return Expr{Kind: kind, Pos: at, Raw: name}, nil
		}
		return Expr{Kind: ExprIdent, Pos: at, Raw: name}, nil
	}

	return Expr{}, SyntaxError{
		Format: p.format(), Pos: at,
		Want: "an expression", Got: quoteRune(r),
	}
}

// list reads a comma-separated sequence up to close.
func (p *exprParser) list(openAt Position, close, what string) ([]Expr, error) {
	var out []Expr
	if err := p.space(); err != nil {
		return nil, err
	}
	if p.sc.Take(close) {
		return out, nil
	}
	for {
		e, err := p.operand(openAt, "an expression in "+what)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if err := p.space(); err != nil {
			return nil, err
		}
		if p.sc.Done() {
			return nil, p.closer(openAt, close+" to close "+what+" opened here")
		}
		if p.sc.Take(close) {
			return out, nil
		}
		if p.sc.Take(",") {
			if err := p.space(); err != nil {
				return nil, err
			}
			// A trailing comma is legal in JavaScript.
			if p.sc.Take(close) {
				return out, nil
			}
			continue
		}
		r, _ := p.sc.Peek()
		return nil, SyntaxError{
			Format: p.format(), Pos: p.sc.Pos(),
			Want: ", or " + close + " in " + what, Got: quoteRune(r),
		}
	}
}

func (p *exprParser) object(at Position) (Expr, error) {
	p.sc.Take("{")
	if err := p.enter(at); err != nil {
		return Expr{}, err
	}
	defer p.leave()

	e := Expr{Kind: ExprObject, Pos: at, Raw: "{}"}
	if err := p.space(); err != nil {
		return Expr{}, err
	}
	if p.sc.Take("}") {
		return e, nil
	}
	for {
		if err := p.space(); err != nil {
			return Expr{}, err
		}
		if p.sc.Done() {
			return Expr{}, p.closer(at, "} to close the object opened here")
		}
		propAt := p.sc.Pos()
		var prop ExprProperty
		prop.Pos = propAt

		switch r, _ := p.sc.Peek(); {
		case r == '[':
			p.sc.Take("[")
			k, err := p.operand(propAt, "a computed key after [")
			if err != nil {
				return Expr{}, err
			}
			if err := p.space(); err != nil {
				return Expr{}, err
			}
			if !p.sc.Take("]") {
				return Expr{}, p.closer(propAt, "] to close the computed key opened here")
			}
			prop.Computed, prop.KeyExpr = true, &k
		case r == '"' || r == '\'':
			s, err := p.stringLiteral(propAt, r)
			if err != nil {
				return Expr{}, err
			}
			prop.Key = s.Raw
		default:
			name, ok := p.ident()
			if !ok {
				return Expr{}, p.here(propAt, "a property name")
			}
			prop.Key = name
		}

		if err := p.space(); err != nil {
			return Expr{}, err
		}
		if !p.sc.Take(":") {
			return Expr{}, p.here(propAt, ": after the property name")
		}
		v, err := p.operand(propAt, "a value after :")
		if err != nil {
			return Expr{}, err
		}
		prop.Value = v
		e.Props = append(e.Props, prop)

		if err := p.space(); err != nil {
			return Expr{}, err
		}
		if p.sc.Done() {
			return Expr{}, p.closer(at, "} to close the object opened here")
		}
		if p.sc.Take("}") {
			return e, nil
		}
		if p.sc.Take(",") {
			if err := p.space(); err != nil {
				return Expr{}, err
			}
			if p.sc.Take("}") {
				return e, nil
			}
			continue
		}
		r, _ := p.sc.Peek()
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: p.sc.Pos(),
			Want: ", or } in the object", Got: quoteRune(r),
		}
	}
}

func (p *exprParser) stringLiteral(at Position, quote rune) (Expr, error) {
	p.sc.Next() // opening quote
	var b strings.Builder
	for {
		r, ok := p.sc.Next()
		if !ok {
			return Expr{}, SyntaxError{
				Format: p.format(), Pos: at,
				Want: "a closing quote for the string opened here", Got: "end of input",
				Incomplete: true,
			}
		}
		if r == '\\' {
			esc, ok := p.sc.Next()
			if !ok {
				return Expr{}, SyntaxError{
					Format: p.format(), Pos: at,
					Want: "a character after the escape", Got: "end of input",
					Incomplete: true,
				}
			}
			b.WriteRune(unescape(esc))
			continue
		}
		if r == quote {
			return Expr{Kind: ExprString, Pos: at, Raw: b.String()}, nil
		}
		b.WriteRune(r)
	}
}

func (p *exprParser) template(at Position) (Expr, error) {
	p.sc.Next() // opening backtick
	var b strings.Builder
	for {
		r, ok := p.sc.Next()
		if !ok {
			return Expr{}, SyntaxError{
				Format: p.format(), Pos: at,
				Want: "a closing backtick for the template opened here", Got: "end of input",
				Incomplete: true,
			}
		}
		if r == '\\' {
			esc, ok := p.sc.Next()
			if !ok {
				return Expr{}, SyntaxError{
					Format: p.format(), Pos: at,
					Want: "a character after the escape", Got: "end of input",
					Incomplete: true,
				}
			}
			b.WriteRune(unescape(esc))
			continue
		}
		if r == '`' {
			return Expr{Kind: ExprTemplate, Pos: at, Raw: b.String()}, nil
		}
		b.WriteRune(r)
	}
}

func unescape(r rune) rune {
	switch r {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case '0':
		return 0
	default:
		return r
	}
}

// number reads a numeric literal WITHOUT decoding it. Raw holds it as written,
// for the same reason the rest of this package leaves values undecoded: the
// consumer knows whether the target is an int, a float or a ratio, and choosing
// here would answer that too early.
func (p *exprParser) number(at Position) (Expr, error) {
	start := at.Offset
	seenDot, seenExp := false, false
	for {
		r, ok := p.sc.Peek()
		if !ok {
			break
		}
		switch {
		case r >= '0' && r <= '9':
		case r == '.' && !seenDot && !seenExp:
			seenDot = true
		case (r == 'e' || r == 'E') && !seenExp:
			seenExp = true
		case (r == '+' || r == '-') && seenExp && p.sc.Pos().Offset > start &&
			isExpSign(p.sc, start):
		case r == 'x' || r == 'X' || r == 'b' || r == 'B' || r == 'o' || r == 'O',
			r >= 'a' && r <= 'f', r >= 'A' && r <= 'F', r == '_':
			// Hex, binary, octal digits and separators. Validity is the
			// consumer's business, as with every other literal here.
		default:
			goto done
		}
		p.sc.Next()
	}
done:
	raw := string(p.sc.Slice(start, p.sc.Pos().Offset))
	if raw == "." || raw == "" {
		return Expr{}, SyntaxError{
			Format: p.format(), Pos: at,
			Want: "a number", Got: quoteRune('.'),
		}
	}
	return Expr{Kind: ExprNumber, Pos: at, Raw: raw}, nil
}

// isExpSign reports whether the character just before the cursor is the
// exponent marker, so `1e-5` reads as one number and `a-5` does not.
func isExpSign(sc *Scanner, start int) bool {
	off := sc.Pos().Offset
	if off <= start {
		return false
	}
	prev := sc.Slice(off-1, off)
	return len(prev) == 1 && (prev[0] == 'e' || prev[0] == 'E')
}

func (p *exprParser) ident() (string, bool) {
	r, ok := p.sc.Peek()
	if !ok || !isExprIdentStart(r) {
		return "", false
	}
	start := p.sc.Pos().Offset
	p.sc.Next()
	for {
		r, ok := p.sc.Peek()
		if !ok || !isExprIdentPart(r) {
			break
		}
		p.sc.Next()
	}
	return string(p.sc.Slice(start, p.sc.Pos().Offset)), true
}

func isExprIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_' || r == '$'
}

func isExprIdentPart(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$'
}

// here reports a failure at the cursor, wanting what.
func (p *exprParser) here(at Position, want string) error {
	if p.sc.Done() {
		return SyntaxError{
			Format: p.format(), Pos: at,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	r, _ := p.sc.Peek()
	return SyntaxError{Format: p.format(), Pos: p.sc.Pos(), Want: want, Got: quoteRune(r)}
}

// closer reports a missing closing delimiter. Running out of input is
// INCOMPLETE — the writer has not finished — while finding the wrong character
// is an ordinary error.
func (p *exprParser) closer(openAt Position, want string) error {
	if p.sc.Done() {
		return SyntaxError{
			Format: p.format(), Pos: openAt,
			Want: want, Got: "end of input", Incomplete: true,
		}
	}
	r, _ := p.sc.Peek()
	return SyntaxError{Format: p.format(), Pos: p.sc.Pos(), Want: want, Got: quoteRune(r)}
}
