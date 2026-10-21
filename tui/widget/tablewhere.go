package widget

import (
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A WHERE BAR'S TEXT — a filter a user types over a table's loaded rows, in SQL's words:
//
//	where   := or
//	or      := and { OR and }
//	and     := not { AND not }
//	not     := { NOT } atom
//	atom    := ( or ) | field op literal | field IS [NOT] NULL | field [NOT] LIKE 'pattern'
//	op      := = | != | <> | < | <= | > | >=
//	field   := name | "quoted name" | #position
//	literal := number | 'string' | TRUE | FALSE
//
// NOT binds tighter than AND, and AND than OR. A name is a field's title, matched as ORDER BY's are
// (exactly, else case-insensitively when one field matches); a bare name runs to a space, a
// parenthesis, an operator or a quote, and one spelled like a keyword must be quoted. "" doubles a
// quote inside a quoted name, '' inside a string. #1 is the first field. A number is a decimal,
// with an optional sign, fraction and exponent. Keywords (AND OR NOT IS NULL LIKE TRUE FALSE) are
// case-insensitive. A comparison with NULL is refused: IS NULL is how to ask for NULL.
//
// The filter only reads the rows a table has loaded; nothing is sent anywhere.

// TableTruth is a predicate's answer for one row: one of SQL's three values. The order false <
// unknown < true is the one AND (the least) and OR (the greatest) take.
type TableTruth int8

const (
	TableFalse TableTruth = iota
	TableUnknown
	TableTrue
)

func (t TableTruth) String() string {
	switch t {
	case TableFalse:
		return "false"
	case TableTrue:
		return "true"
	}
	return "unknown"
}

// TablePredicate is a filter over a table's rows (TableCore.SetFilter). Eval is the predicate on
// source row r of src, read on the loop as Cell is; a row is shown only when it is TableTrue.
type TablePredicate interface {
	Eval(src TableSource, r int) TableTruth
}

// ParseTableWhere reads text as a filter over fields; text of only spaces is no filter, a nil
// predicate and no error.
//
// The predicate compares each cell by its kind when the source has kinds (TableKinds), as SQL
// does; a pairing not listed is unknown, so the row is not shown, and NOT of it is unknown too:
//
//	cell \ literal   number                             'string'   TRUE / FALSE
//	z (NULL)         unknown                            unknown    unknown
//	n                exact decimal; NaN, Infinity: unknown   text  unknown
//	b                unknown                            text       boolean
//	t                a number when its text is one      text       unknown
//	j                unknown                            text       unknown
//
// "text" is the cell's text against the string by strings.Compare, and an exact decimal is a
// big.Rat, never a float64. A source without kinds is all text: no cell is NULL, so IS NULL is true
// for none. IS [NOT] NULL is never unknown. LIKE matches a non-NULL cell's text, case-sensitive: %
// is any run of characters, _ one character, and \ makes the next character itself.
func ParseTableWhere(text string, fields []TableField) (TablePredicate, error) {
	p := whereParser{text: text, fields: fields}
	if err := p.next(); err != nil {
		return nil, err
	}
	if p.tok.kind == wtEnd {
		return nil, nil
	}
	root, err := p.or()
	if err != nil {
		return nil, err
	}
	switch p.tok.kind {
	case wtEnd:
		return &tableWhere{root: root, need: p.need}, nil
	case wtClose:
		return nil, p.fail(p.tok.at, "this ) closes nothing")
	}
	return nil, p.fail(p.tok.at, "expected AND or OR before %s", p.shown())
}

// tableWhere is a parsed filter: its tree, and how many fields it reads (the highest it names, plus
// one), so a core can drop it when the source has fewer.
type tableWhere struct {
	root whereNode
	need int
}

func (w *tableWhere) Eval(src TableSource, r int) TableTruth {
	k, _ := src.(TableKinds)
	return w.root.eval(src, k, r)
}

func (w *tableWhere) fieldsRead() int { return w.need }

// THE LEXER

type whereTokKind int

const (
	wtEnd whereTokKind = iota
	wtOpen
	wtClose
	wtOp
	wtName     // a bare name
	wtQuoted   // a "quoted name"
	wtPosition // #n
	wtNumber
	wtString
	wtKeyword
)

type whereTok struct {
	kind    whereTokKind
	at, end int      // the bytes it spans
	val     string   // a name's or a string's text, quotes undone; an operator; a keyword in upper case
	num     *big.Rat // a number's value
}

var whereKeywords = []string{"AND", "OR", "NOT", "IS", "NULL", "LIKE", "TRUE", "FALSE"}

type whereParser struct {
	text   string
	fields []TableField
	i      int // the lexer's place
	tok    whereTok
	need   int
}

func (p *whereParser) fail(at int, format string, args ...any) error {
	return barFail(at, format, args...)
}

// shown is the current token as the user wrote it, for a message.
func (p *whereParser) shown() string { return strconv.Quote(p.text[p.tok.at:p.tok.end]) }

// is reports whether the current token is the keyword kw.
func (p *whereParser) is(kw string) bool { return p.tok.kind == wtKeyword && p.tok.val == kw }

// next reads the token after the spaces at p.i into p.tok.
func (p *whereParser) next() error {
	for p.i < len(p.text) && orderSpace(p.text[p.i]) {
		p.i++
	}
	start := p.i
	t := whereTok{at: start}
	if p.i == len(p.text) {
		t.end = start
		p.tok = t
		return nil
	}
	s := p.text
	switch b := s[p.i]; {
	case b == '(':
		t.kind = wtOpen
		p.i++
	case b == ')':
		t.kind = wtClose
		p.i++
	case b == '=':
		t.kind, t.val = wtOp, "="
		p.i++
	case b == '!':
		if p.i+1 == len(s) || s[p.i+1] != '=' {
			return p.fail(start, "! is not an operator: write != or NOT")
		}
		t.kind, t.val = wtOp, "!="
		p.i += 2
	case b == '<' || b == '>':
		t.kind, t.val = wtOp, string(b)
		p.i++
		if p.i < len(s) && (s[p.i] == '=' || b == '<' && s[p.i] == '>') {
			t.val += string(s[p.i])
			p.i++
		}
	case b == '"' || b == '\'':
		val, err := p.quoted(b)
		if err != nil {
			return err
		}
		t.kind, t.val = wtQuoted, val
		if b == '\'' {
			t.kind = wtString
		}
	case b == '#':
		p.i++
		j := p.i
		for p.i < len(s) && s[p.i] >= '0' && s[p.i] <= '9' {
			p.i++
		}
		if p.i == j || p.i < len(s) && !whereStop(s[p.i]) {
			return p.fail(start, "%q is not a position: #1 is the first field", s[start:p.wordEnd()])
		}
		t.kind, t.val = wtPosition, s[j:p.i]
	case decimalStarts(s, p.i):
		p.i = decimalEnd(s, p.i)
		if p.i < len(s) && !whereStop(s[p.i]) {
			return p.fail(start, "%q is not a number", s[start:p.wordEnd()])
		}
		n, ok := parseDecimal(s[start:p.i])
		if !ok {
			return p.fail(start, "%s is out of range", s[start:p.i])
		}
		t.kind, t.num = wtNumber, n
	default:
		p.i = p.wordEnd()
		if p.i == start {
			r, _ := utf8.DecodeRuneInString(s[start:])
			return p.fail(start, "unexpected %q", r)
		}
		t.kind, t.val = wtName, s[start:p.i]
		for _, kw := range whereKeywords {
			if strings.EqualFold(t.val, kw) {
				t.kind, t.val = wtKeyword, kw
				break
			}
		}
	}
	t.end = p.i
	p.tok = t
	return nil
}

// wordEnd is where the bare word at p.i ends, without moving.
func (p *whereParser) wordEnd() int {
	j := p.i
	for j < len(p.text) && !whereStop(p.text[j]) {
		j++
	}
	return j
}

// quoted reads a name in double quotes or a string in single quotes, the quote doubled inside.
func (p *whereParser) quoted(q byte) (string, error) {
	start := p.i
	val, end, closed := readQuoted(p.text, p.i, q)
	p.i = end
	if closed {
		return val, nil
	}
	if q == '"' {
		return "", p.fail(start, "the quoted name is not closed")
	}
	return "", p.fail(start, "the string is not closed")
}

// whereStop reports whether b ends a bare word: a space, a parenthesis, an operator's first byte,
// a quote, or punctuation the grammar has no use for.
func whereStop(b byte) bool {
	return orderSpace(b) || strings.IndexByte(`()=!<>'",;`, b) >= 0
}

// THE PARSER

func (p *whereParser) or() (whereNode, error) {
	left, err := p.and()
	for err == nil && p.is("OR") {
		if err = p.next(); err != nil {
			break
		}
		var right whereNode
		if right, err = p.and(); err == nil {
			left = &whereOr{left, right}
		}
	}
	return left, err
}

func (p *whereParser) and() (whereNode, error) {
	left, err := p.not()
	for err == nil && p.is("AND") {
		if err = p.next(); err != nil {
			break
		}
		var right whereNode
		if right, err = p.not(); err == nil {
			left = &whereAnd{left, right}
		}
	}
	return left, err
}

func (p *whereParser) not() (whereNode, error) {
	if !p.is("NOT") {
		return p.atom()
	}
	if err := p.next(); err != nil {
		return nil, err
	}
	x, err := p.not()
	if err != nil {
		return nil, err
	}
	return &whereNot{x}, nil
}

func (p *whereParser) atom() (whereNode, error) {
	switch p.tok.kind {
	case wtOpen:
		open := p.tok.at
		if err := p.next(); err != nil {
			return nil, err
		}
		x, err := p.or()
		if err != nil {
			return nil, err
		}
		switch p.tok.kind {
		case wtEnd:
			return nil, p.fail(open, "the parenthesis is not closed")
		case wtClose:
			return x, p.next()
		}
		return nil, p.fail(p.tok.at, "expected AND, OR or ) before %s", p.shown())
	case wtName, wtQuoted, wtPosition:
	case wtEnd:
		return nil, p.fail(p.tok.at, "a condition is missing")
	case wtKeyword:
		return nil, p.fail(p.tok.at, "expected a field, not %s: quote a field of that name", p.tok.val)
	default:
		return nil, p.fail(p.tok.at, "expected a field before %s", p.shown())
	}
	col, err := p.field()
	if err != nil {
		return nil, err
	}
	name := p.text[p.tok.at:p.tok.end]
	if err := p.next(); err != nil {
		return nil, err
	}
	switch {
	case p.tok.kind == wtOp:
		op := p.tok.val
		if err := p.next(); err != nil {
			return nil, err
		}
		lit, err := p.literal(op)
		if err != nil {
			return nil, err
		}
		return &whereCompare{col: col, op: op, lit: lit}, nil
	case p.is("IS"):
		if err := p.next(); err != nil {
			return nil, err
		}
		x := &whereNull{col: col}
		if p.is("NOT") {
			x.not = true
			if err := p.next(); err != nil {
				return nil, err
			}
		}
		if !p.is("NULL") {
			return nil, p.expected("NULL after IS")
		}
		return x, p.next()
	case p.is("LIKE"), p.is("NOT"):
		x := &whereLike{col: col, not: p.is("NOT")}
		if err := p.next(); err != nil {
			return nil, err
		}
		if x.not {
			if !p.is("LIKE") {
				return nil, p.expected("LIKE after NOT")
			}
			if err := p.next(); err != nil {
				return nil, err
			}
		}
		if p.tok.kind != wtString {
			return nil, p.expected("a 'pattern' in single quotes after LIKE")
		}
		pat, msg := compileLike(p.tok.val)
		if msg != "" {
			return nil, p.fail(p.tok.at, "%s", msg)
		}
		x.pat = pat
		return x, p.next()
	case p.tok.kind == wtEnd:
		return nil, p.fail(p.tok.at, "%s needs an operator, IS or LIKE after it", name)
	}
	return nil, p.fail(p.tok.at, "expected an operator, IS or LIKE before %s", p.shown())
}

// expected is an error at the current token that what was due is not there.
func (p *whereParser) expected(what string) error {
	if p.tok.kind == wtEnd {
		return p.fail(p.tok.at, "expected %s", what)
	}
	return p.fail(p.tok.at, "expected %s, not %s", what, p.shown())
}

// field resolves the current token, a name or a position, to a field.
func (p *whereParser) field() (int, error) {
	t := p.tok
	col := 0
	if t.kind == wtPosition {
		var ok bool
		if col, ok = fieldAtPosition(t.val, len(p.fields)); !ok {
			return 0, p.fail(t.at, "no field #%s: the table has %d", t.val, len(p.fields))
		}
	} else {
		var msg string
		if col, msg = fieldNamed(p.fields, t.val, "#"); msg != "" {
			return 0, p.fail(t.at, "%s", msg)
		}
	}
	p.need = max(p.need, col+1)
	return col, nil
}

// literal reads the value a comparison op compares with.
func (p *whereParser) literal(op string) (whereLit, error) {
	t := p.tok
	var lit whereLit
	switch {
	case t.kind == wtNumber:
		lit = whereLit{kind: litNumber, num: t.num}
	case t.kind == wtString:
		lit = whereLit{kind: litString, s: t.val}
	case p.is("TRUE"), p.is("FALSE"):
		lit = whereLit{kind: litBool, b: t.val == "TRUE"}
	case p.is("NULL"):
		switch op {
		case "=":
			return lit, p.fail(t.at, "= NULL is never true: write IS NULL")
		case "!=", "<>":
			return lit, p.fail(t.at, "%s NULL is never true: write IS NOT NULL", op)
		}
		return lit, p.fail(t.at, "%s NULL is never true: write IS NULL or IS NOT NULL", op)
	case t.kind == wtEnd:
		return lit, p.fail(t.at, "a value is missing after %s", op)
	case t.kind == wtQuoted:
		return lit, p.fail(t.at, "%s is a field's name: write a string in single quotes", p.shown())
	default:
		return lit, p.fail(t.at, "expected a number, a 'string', TRUE or FALSE, not %s", p.shown())
	}
	return lit, p.next()
}

// THE PREDICATE

type whereNode interface {
	eval(src TableSource, kinds TableKinds, r int) TableTruth
}

// SQL's three-valued logic: AND is the lesser, OR the greater, NOT swaps true and false and leaves
// unknown.
type (
	whereAnd struct{ l, r whereNode }
	whereOr  struct{ l, r whereNode }
	whereNot struct{ x whereNode }
)

func (w *whereAnd) eval(src TableSource, k TableKinds, r int) TableTruth {
	a := w.l.eval(src, k, r)
	if a == TableFalse {
		return TableFalse
	}
	return min(a, w.r.eval(src, k, r))
}

func (w *whereOr) eval(src TableSource, k TableKinds, r int) TableTruth {
	a := w.l.eval(src, k, r)
	if a == TableTrue {
		return TableTrue
	}
	return max(a, w.r.eval(src, k, r))
}

func (w *whereNot) eval(src TableSource, k TableKinds, r int) TableTruth {
	return TableTrue - w.x.eval(src, k, r)
}

const (
	litNumber = iota
	litString
	litBool
)

type whereLit struct {
	kind int
	num  *big.Rat
	s    string
	b    bool
}

// whereCompare is `field op literal`.
type whereCompare struct {
	col int
	op  string
	lit whereLit
}

func (w *whereCompare) eval(src TableSource, k TableKinds, r int) TableTruth {
	kind, text := whereCell(src, k, r, w.col)
	if kind == TableCellNull {
		return TableUnknown
	}
	var c int
	switch w.lit.kind {
	case litString:
		c = strings.Compare(text, w.lit.s)
	case litNumber:
		if kind != TableCellNumber && kind != TableCellText {
			return TableUnknown
		}
		n, ok := parseDecimal(strings.TrimSpace(text))
		if !ok {
			return TableUnknown
		}
		c = n.Cmp(w.lit.num)
	case litBool:
		if kind != TableCellBool {
			return TableUnknown
		}
		b, err := strconv.ParseBool(strings.TrimSpace(text))
		if err != nil {
			return TableUnknown
		}
		c = boolOrder(b) - boolOrder(w.lit.b)
	}
	var holds bool
	switch w.op {
	case "=":
		holds = c == 0
	case "!=", "<>":
		holds = c != 0
	case "<":
		holds = c < 0
	case "<=":
		holds = c <= 0
	case ">":
		holds = c > 0
	case ">=":
		holds = c >= 0
	}
	return truthOf(holds)
}

// whereNull is `field IS [NOT] NULL`: only a z cell is NULL, so without kinds none is.
type whereNull struct {
	col int
	not bool
}

func (w *whereNull) eval(_ TableSource, k TableKinds, r int) TableTruth {
	null := k != nil && k.Kind(r, w.col) == TableCellNull
	return truthOf(null != w.not)
}

// whereLike is `field [NOT] LIKE 'pattern'`.
type whereLike struct {
	col int
	not bool
	pat likePattern
}

func (w *whereLike) eval(src TableSource, k TableKinds, r int) TableTruth {
	kind, text := whereCell(src, k, r, w.col)
	if kind == TableCellNull {
		return TableUnknown
	}
	return truthOf(w.pat.match(text) != w.not)
}

func truthOf(b bool) TableTruth {
	if b {
		return TableTrue
	}
	return TableFalse
}

// whereCell is a cell's kind and text; without kinds, and for a kind byte the core does not know,
// the kind is text.
func whereCell(src TableSource, k TableKinds, r, col int) (TableCellKind, string) {
	text := src.Cell(r, col)
	if k == nil {
		return TableCellText, text
	}
	switch kind := k.Kind(r, col); kind {
	case TableCellNull, TableCellNumber, TableCellBool, TableCellJSON:
		return kind, text
	}
	return TableCellText, text
}

// LIKE

// likePattern is a LIKE pattern cut at its %s: each segment is a run of characters, each one rune
// or any rune (_). The first segment is anchored at the text's start and the last at its end; the
// ones between are placed, in order, at their leftmost fit. A segment is never placed twice, so a
// match takes at most the text's length times the pattern's, whatever the pattern, with no
// backtracking.
type likePattern struct {
	segs [][]likeRune // len 1: no %, the whole text must fit the one segment
}

type likeRune struct {
	r   rune
	any bool
}

// compileLike reads a pattern; msg says why it cannot be one.
func compileLike(pattern string) (likePattern, string) {
	segs := [][]likeRune{nil}
	escaped := false
	for _, r := range pattern {
		last := len(segs) - 1
		switch {
		case escaped:
			segs[last] = append(segs[last], likeRune{r: r})
			escaped = false
		case r == '\\':
			escaped = true
		case r == '%':
			segs = append(segs, nil)
		case r == '_':
			segs[last] = append(segs[last], likeRune{any: true})
		default:
			segs[last] = append(segs[last], likeRune{r: r})
		}
	}
	if escaped {
		return likePattern{}, `the pattern ends in \: write \\ for a backslash`
	}
	return likePattern{segs: segs}, ""
}

func (p likePattern) match(s string) bool {
	text := []rune(s)
	if len(p.segs) == 1 {
		return len(text) == len(p.segs[0]) && likeAt(p.segs[0], text, 0)
	}
	first, last := p.segs[0], p.segs[len(p.segs)-1]
	if len(first)+len(last) > len(text) || !likeAt(first, text, 0) || !likeAt(last, text, len(text)-len(last)) {
		return false
	}
	pos, end := len(first), len(text)-len(last)
	for _, seg := range p.segs[1 : len(p.segs)-1] {
		at := -1
		for i := pos; i+len(seg) <= end; i++ {
			if likeAt(seg, text, i) {
				at = i
				break
			}
		}
		if at < 0 {
			return false
		}
		pos = at + len(seg)
	}
	return true
}

// likeAt reports whether seg fits text at i; the caller has checked it is long enough.
func likeAt(seg []likeRune, text []rune, i int) bool {
	for j, c := range seg {
		if !c.any && text[i+j] != c.r {
			return false
		}
	}
	return true
}

// DECIMALS

// decimalExpMax bounds a decimal's exponent: a big.Rat of 1e999999999 would take a gigabyte. It is
// far past any float64 and any number a cell holds in digits (those have no exponent).
const decimalExpMax = 10000

// decimalStarts reports whether a decimal starts at s[i]: a digit, or a point or a sign before one.
func decimalStarts(s string, i int) bool {
	digit := func(j int) bool { return j < len(s) && s[j] >= '0' && s[j] <= '9' }
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	return digit(i) || i < len(s) && s[i] == '.' && digit(i+1)
}

// decimalEnd is where the decimal at s[i] ends: [sign] digits [. digits] [e [sign] digits], at
// least one digit before the exponent.
func decimalEnd(s string, i int) int {
	digits := func() int {
		j := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i - j
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	n := digits()
	if i < len(s) && s[i] == '.' {
		i++
		n += digits()
	}
	if n == 0 {
		return start
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		mark := i
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			i = mark
		}
	}
	return i
}

// parseDecimal is s as an exact decimal, when the whole of s is one and its exponent is in range.
// Other syntaxes big.Rat reads (a fraction, a base prefix) are not decimals.
func parseDecimal(s string) (*big.Rat, bool) {
	if s == "" || decimalEnd(s, 0) != len(s) {
		return nil, false
	}
	if e := strings.IndexAny(s, "eE"); e >= 0 {
		exp := strings.TrimLeft(strings.TrimLeft(s[e+1:], "+-"), "0")
		if len(exp) > 5 {
			return nil, false
		}
		if n, _ := strconv.Atoi("0" + exp); n > decimalExpMax {
			return nil, false
		}
	}
	return new(big.Rat).SetString(s)
}
