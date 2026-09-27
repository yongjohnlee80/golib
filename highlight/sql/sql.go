// Package sql highlights SQL, one line at a time, for an editor: PostgreSQL,
// SQLite and MySQL, each as its own definition, as KSyntaxHighlighting has
// "SQL (PostgreSQL)", "SQL (MySQL)" and "SQL".
//
// It lives here and not in parse/sql, whose kinds say what a run of bytes IS
// and never what it means — that package knows no keywords by design. A
// highlighter is exactly a judgement of meaning, made for a reader, so it is a
// definition beside the lexer, as KSyntaxHighlighting's are.
//
// It never refuses. Text being typed is unfinished most of the time: a block
// comment or a dollar-quoted body left open is a State carried to the next
// line, a string left open ends at the line, and anything it does not
// recognise is Normal.
//
// Styles are KSyntaxHighlighting's:
//
//	SELECT, FROM, JOIN, CREATE, GRANT…     Keyword
//	CASE, WHEN, THEN, IF, LOOP, RETURN…    ControlFlow
//	INTEGER, TEXT, VARCHAR, JSONB…         DataType
//	count(  now(  my_fn(                   Function
//	NULL, TRUE, FALSE, CURRENT_DATE…       Constant
//	'…'  E'…'  $$…$$                        String (escapes SpecialChar)
//	42  3.5  1e9  0x1f                      DecVal, Float, BaseN
//	-- …  /* … */  (# … in MySQL)           Comment (TODO, FIXME: Alert)
//	$1  ?  :name  @name                     Variable
//	= <> || :: …                            Operator
package sql

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse"
)

// Dialect is which SQL a highlighter reads.
type Dialect int

const (
	// PostgreSQL: nested block comments, dollar-quoted bodies, E'' escapes,
	// :: casts, $1 parameters.
	PostgreSQL Dialect = iota
	// SQLite: [bracketed] and `backticked` names, x'' blobs, ?, ?1, :name,
	// @name and $name parameters.
	SQLite
	// MySQL: `backticked` names, # comments, -- only before a space,
	// backslash escapes and double-quoted strings.
	MySQL
)

// Definitions are the three dialects as syntax definitions, named as
// KSyntaxHighlighting names them. PostgreSQL takes *.sql: a file does not say
// its dialect, and PostgreSQL's reading is the widest. SQLite and MySQL claim
// *.sqlite.sql and *.mysql.sql at a higher Priority, so those names reach them
// rather than the wider pattern.
func Definitions() []highlight.Definition {
	return []highlight.Definition{
		{Name: "SQL (PostgreSQL)", Extensions: []string{"*.sql", "*.pgsql"}, Highlighter: Highlighter(PostgreSQL)},
		{Name: "SQL (SQLite)", Extensions: []string{"*.sqlite.sql"}, Priority: 1, Highlighter: Highlighter(SQLite)},
		{Name: "SQL (MySQL)", Extensions: []string{"*.mysql.sql"}, Priority: 1, Highlighter: Highlighter(MySQL)},
	}
}

// Highlighter returns the highlighter for d. One serves any number of
// editors: what it keeps is the dollar-quote tags it has seen, shared and
// guarded.
func Highlighter(d Dialect) highlight.Highlighter {
	h := &highlighter{dialect: d, tags: map[string]highlight.State{}}
	return highlight.HighlighterFunc(h.line)
}

// States carried between lines. A block comment carries its depth (PostgreSQL
// nests them); a dollar-quoted body carries its tag, interned.
const (
	stateNone    highlight.State = 0
	stateComment highlight.State = 1 // + depth-1, below stateDollar
	stateDollar  highlight.State = 1 << 16
)

type highlighter struct {
	dialect Dialect
	mu      sync.Mutex
	tags    map[string]highlight.State
	names   []string
}

// tagState is the state that carries a dollar-quote tag across lines.
func (h *highlighter) tagState(tag string) highlight.State {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.tags[tag]; ok {
		return s
	}
	s := stateDollar + highlight.State(len(h.names))
	h.tags[tag] = s
	h.names = append(h.names, tag)
	return s
}

func (h *highlighter) tagOf(s highlight.State) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	i := int(s - stateDollar)
	if s < stateDollar || i >= len(h.names) {
		return "", false
	}
	return h.names[i], true
}

var (
	keywords = set(
		"ABORT", "ACTION", "ADD", "AFTER", "ALL", "ALTER", "ALWAYS", "ANALYZE", "AND", "ANY", "AS", "ASC",
		"ATTACH", "AUTOINCREMENT", "AUTO_INCREMENT", "BEFORE", "BEGIN", "BETWEEN", "BY", "CASCADE",
		"CAST", "CHECK", "COLLATE", "COLUMN", "COMMENT", "COMMIT", "CONFLICT", "CONSTRAINT", "COPY",
		"CONCURRENTLY", "CREATE", "CROSS", "CURRENT", "DATABASE", "DEFAULT", "DEFERRABLE", "DEFERRED", "DELETE",
		"DESC", "DESCRIBE", "DETACH", "DISTINCT", "DO", "DROP", "DUPLICATE", "EACH", "ENGINE", "ESCAPE", "EXCEPT", "EXCLUDE",
		"EXCLUSIVE", "EXISTS", "EXPLAIN", "EXTENSION", "FETCH", "FILTER", "FIRST", "FOLLOWING", "FOR",
		"FOREIGN", "FROM", "FULL", "FUNCTION", "GENERATED", "GLOB", "GRANT", "GROUP", "GROUPS",
		"HAVING", "IGNORE", "ILIKE", "IMMEDIATE", "IN", "INDEX", "INDEXED", "INHERITS", "INITIALLY",
		"INNER", "INSERT", "INSTEAD", "INTERSECT", "INTO", "IS", "ISNULL", "JOIN", "KEY", "LANGUAGE",
		"LAST", "LATERAL", "LEFT", "LIKE", "LIMIT", "LOCK", "MATCH", "MATERIALIZED", "NATURAL", "NO",
		"NOT", "NOTHING", "NOTNULL", "NULLS", "OF", "OFFSET", "ON", "ONLY", "OR", "ORDER", "OTHERS",
		"OUTER", "OVER", "OWNER", "PARTITION", "PRAGMA", "PRECEDING", "PRIMARY", "PROCEDURE",
		"RANGE", "RECURSIVE", "REFERENCES", "REFRESH", "REGEXP", "REINDEX", "RELEASE", "RENAME",
		"REPLACE", "RESTRICT", "RETURNING", "REVOKE", "RIGHT", "ROLE", "ROLLBACK", "ROW", "ROWS",
		"SAVEPOINT", "SCHEMA", "SELECT", "SEQUENCE", "SET", "SHOW", "SIMILAR", "SOME", "STORED", "STRICT",
		"TABLE", "TABLESPACE", "TEMP", "TEMPORARY", "TIES", "TO", "TRANSACTION", "TRIGGER", "TRUNCATE",
		"TYPE", "UNBOUNDED", "UNION", "UNIQUE", "UNLOGGED", "UPDATE", "USE", "USING", "VACUUM",
		"VALUES", "VIEW", "VIRTUAL", "WHERE", "WINDOW", "WITH", "WITHOUT")
	controlFlow = set("CASE", "WHEN", "THEN", "ELSE", "ELSIF", "ELSEIF", "END", "IF", "LOOP",
		"WHILE", "REPEAT", "UNTIL", "RETURN", "RETURNS", "EXIT", "CONTINUE", "RAISE", "PERFORM",
		"DECLARE", "EXECUTE", "CALL", "LEAVE", "ITERATE", "FOREACH")
	dataTypes = set(
		"BIGINT", "BIGSERIAL", "BINARY", "BIT", "BLOB", "BOOL", "BOOLEAN", "BYTEA", "CHAR",
		"CHARACTER", "CIDR", "DATE", "DATETIME", "DEC", "DECIMAL", "DOUBLE", "ENUM", "FLOAT",
		"FLOAT4", "FLOAT8", "INET", "INT", "INT2", "INT4", "INT8", "INTEGER", "INTERVAL", "JSON",
		"JSONB", "LONGTEXT", "MEDIUMINT", "MEDIUMTEXT", "MONEY", "NUMERIC", "NVARCHAR", "PRECISION",
		"REAL", "SERIAL", "SMALLINT", "SMALLSERIAL", "TEXT", "TIME", "TIMESTAMP", "TIMESTAMPTZ",
		"TIMETZ", "TINYINT", "TINYTEXT", "TSQUERY", "TSVECTOR", "UUID", "VARBINARY", "VARCHAR",
		"VARYING", "XML", "YEAR", "ZONE")
	constants = set("NULL", "TRUE", "FALSE", "UNKNOWN", "CURRENT_DATE", "CURRENT_TIME",
		"CURRENT_TIMESTAMP", "CURRENT_USER", "SESSION_USER", "LOCALTIME", "LOCALTIMESTAMP")
	alerts = []string{"TODO", "FIXME", "XXX", "HACK"}
)

func set(words ...string) map[string]bool {
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}

// line is one line being highlighted: its scanner, and the spans so far.
type line struct {
	h     *highlighter
	text  string
	sc    *parse.Scanner
	spans []highlight.Span
}

func (l *line) at() int { return l.sc.Pos().Offset }

func (l *line) span(start, end int, st highlight.Style) {
	if end > start {
		l.spans = append(l.spans, highlight.Span{Start: start, End: end, Style: st})
	}
}

func (l *line) toEnd() {
	for !l.sc.Done() {
		l.sc.Next()
	}
}

func (h *highlighter) line(text string, previous highlight.State) ([]highlight.Span, highlight.State) {
	l := &line{h: h, text: text, sc: parse.NewScanner([]byte(text))}
	state := stateNone
	switch {
	case previous >= stateDollar:
		if tag, ok := h.tagOf(previous); ok {
			if !l.dollarBody(0, tag) {
				return l.spans, previous
			}
		}
	case previous >= stateComment:
		if depth := l.blockComment(0, int(previous-stateComment)+1); depth > 0 {
			return l.spans, stateComment + highlight.State(depth-1)
		}
	}
	for !l.sc.Done() {
		start := l.at()
		r, _ := l.sc.Next()
		switch {
		case unicode.IsSpace(r):
		case r == '-' && l.dashComment():
			l.toEnd()
			l.comment(start, l.at())
		case r == '#' && h.dialect == MySQL:
			l.toEnd()
			l.comment(start, l.at())
		case r == '/' && l.sc.Take("*"):
			if depth := l.blockComment(start, 1); depth > 0 {
				state = stateComment + highlight.State(depth-1)
			}
		case r == '\'':
			l.quoted(start, '\'', h.dialect == MySQL)
		case r == '"' && h.dialect == MySQL:
			l.quoted(start, '"', true)
		case r == '"':
			l.identifier('"')
		case r == '`' && h.dialect != PostgreSQL:
			l.identifier('`')
		case r == '[' && h.dialect == SQLite:
			l.identifier(']')
		case (r == 'E' || r == 'e') && h.dialect == PostgreSQL && l.peekIs('\''):
			l.sc.Next()
			l.quoted(start, '\'', true)
		case (r == 'x' || r == 'X' || r == 'b' || r == 'B') && l.peekIs('\''):
			l.sc.Next()
			l.quoted(start, '\'', false)
			l.restyle(start, highlight.BaseN)
		case r == '$' && h.dialect == PostgreSQL:
			if tag, ok := l.dollarOpen(start); ok {
				if !l.dollarBody(start, tag) {
					state = h.tagState(tag)
				}
			} else {
				l.parameter(start)
			}
		case r == '?' && h.dialect != PostgreSQL || (r == '@' || r == ':' || r == '$') && l.nameNext():
			// In PostgreSQL `?` is an operator (jsonb's), and `::` is a cast:
			// the second colon is no name, so it falls to the operator below.
			l.parameter(start)
		case r >= '0' && r <= '9' || r == '.' && l.digitNext():
			l.number(start, r)
		case r == '_' || unicode.IsLetter(r):
			l.word(start)
		case strings.ContainsRune("+-*/%=<>!&|^~:@#?", r):
			l.operator(start)
		default:
			// Punctuation — parentheses, commas, the statement's semicolon —
			// and anything else: Normal.
		}
	}
	return l.spans, state
}

// dashComment reports whether the `-` just read opens a comment: `--`, and in
// MySQL only when a space, a control character or the end of the line follows.
func (l *line) dashComment() bool {
	if r, ok := l.sc.Peek(); !ok || r != '-' {
		return false
	}
	if l.h.dialect == MySQL {
		if r, ok := l.sc.PeekAt(1); ok && r > ' ' {
			return false
		}
	}
	l.sc.Next()
	return true
}

// blockComment colours from start through its close, counting nesting in
// PostgreSQL, and returns the depth still open at the end of the line: 0 when
// it closed.
func (l *line) blockComment(start, depth int) int {
	for !l.sc.Done() {
		switch {
		case l.sc.Take("*/"):
			if depth--; depth == 0 {
				l.comment(start, l.at())
				return 0
			}
		case l.h.dialect == PostgreSQL && l.sc.Take("/*"):
			depth++
		default:
			l.sc.Next()
		}
	}
	l.comment(start, l.at())
	return depth
}

// comment colours [start,end) as a comment, with TODO and its kind as Alert.
func (l *line) comment(start, end int) {
	body := l.text[start:end]
	at := 0
	for {
		i, word := -1, ""
		for _, a := range alerts {
			if j := strings.Index(body[at:], a); j >= 0 && (i < 0 || j < i) {
				i, word = j, a
			}
		}
		if i < 0 {
			l.span(start+at, end, highlight.Comment)
			return
		}
		l.span(start+at, start+at+i, highlight.Comment)
		l.span(start+at+i, start+at+i+len(word), highlight.Alert)
		at += i + len(word)
	}
}

// quoted colours a string opened at start through its closing quote or the
// end of the line. A doubled quote is the quote itself; with backslash, a
// backslash escapes the next character, coloured SpecialChar.
func (l *line) quoted(start int, quote rune, backslash bool) {
	from := start
	for !l.sc.Done() {
		at := l.at()
		r, _ := l.sc.Next()
		switch {
		case r == '\\' && backslash:
			l.span(from, at, highlight.String)
			if !l.sc.Done() {
				l.sc.Next()
			}
			l.span(at, l.at(), highlight.SpecialChar)
			from = l.at()
		case r == quote && l.peekIs(quote):
			l.sc.Next()
		case r == quote:
			l.span(from, l.at(), highlight.String)
			return
		}
	}
	l.span(from, l.at(), highlight.String)
}

// restyle gives every span from start on the style st: a blob or bit string
// is a number written in quotes.
func (l *line) restyle(start int, st highlight.Style) {
	for i := range l.spans {
		if l.spans[i].Start >= start {
			l.spans[i].Style = st
		}
	}
}

// identifier skips a quoted name through its closer. A name is Normal, as an
// unquoted one is: the quotes change its spelling, not what it is.
func (l *line) identifier(closer rune) {
	for !l.sc.Done() {
		r, _ := l.sc.Next()
		if r == closer {
			if closer != ']' && l.peekIs(closer) {
				l.sc.Next()
				continue
			}
			return
		}
	}
}

// dollarOpen reads the rest of a dollar-quote opener that began with the `$`
// at start — `$$` or `$tag$` — and returns its tag. On anything else it
// consumes nothing more.
func (l *line) dollarOpen(start int) (string, bool) {
	i := start + 1
	for i < len(l.text) {
		c := l.text[i]
		if c == '$' {
			tag := l.text[start+1 : i]
			if tag != "" && tag[0] >= '0' && tag[0] <= '9' {
				return "", false // $1 is a parameter
			}
			for l.at() <= i {
				l.sc.Next()
			}
			return tag, true
		}
		if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return "", false
		}
		i++
	}
	return "", false
}

// dollarBody colours a dollar-quoted body from start through `$tag$`, and
// reports whether it closed on this line.
func (l *line) dollarBody(start int, tag string) bool {
	closer := "$" + tag + "$"
	for !l.sc.Done() {
		if l.sc.Take(closer) {
			l.span(start, l.at(), highlight.String)
			return true
		}
		l.sc.Next()
	}
	l.span(start, l.at(), highlight.String)
	return false
}

// parameter colours a bind parameter that began at start: ?, ?1, $1, :name,
// @name, $name.
func (l *line) parameter(start int) {
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		l.sc.Next()
	}
	l.span(start, l.at(), highlight.Variable)
}

func (l *line) peekIs(want rune) bool {
	r, ok := l.sc.Peek()
	return ok && r == want
}

// nameNext reports whether a name or a digit follows: what makes `:x`, `@x`
// and `$1` parameters and a lone `:` or `@` an operator.
func (l *line) nameNext() bool {
	r, ok := l.sc.Peek()
	return ok && (r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
}

func (l *line) digitNext() bool {
	r, ok := l.sc.Peek()
	return ok && r >= '0' && r <= '9'
}

// number colours a numeric literal whose first character was first.
func (l *line) number(start int, first rune) {
	style := highlight.DecVal
	if first == '0' {
		if r, ok := l.sc.Peek(); ok && strings.ContainsRune("xXbBoO", r) {
			l.sc.Next()
			style = highlight.BaseN
		}
	}
	if first == '.' {
		style = highlight.Float
	}
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		switch {
		case style == highlight.BaseN && (unicode.IsDigit(r) || strings.ContainsRune("abcdefABCDEF_", r)):
		case unicode.IsDigit(r) || r == '_':
		case r == '.' && style == highlight.DecVal:
			style = highlight.Float
		case (r == 'e' || r == 'E') && style != highlight.BaseN:
			style = highlight.Float
			l.sc.Next()
			if r, ok := l.sc.Peek(); ok && (r == '+' || r == '-') {
				l.sc.Next()
			}
			continue
		default:
			l.span(start, l.at(), style)
			return
		}
		l.sc.Next()
	}
	l.span(start, l.at(), style)
}

// word colours a keyword, type, constant or function name that began at
// start. SQL's words are case-insensitive, and so is this.
func (l *line) word(start int) {
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		l.sc.Next()
	}
	w := strings.ToUpper(l.text[start:l.at()])
	switch {
	case controlFlow[w]:
		l.span(start, l.at(), highlight.ControlFlow)
	case constants[w]:
		l.span(start, l.at(), highlight.Constant)
	case dataTypes[w]:
		l.span(start, l.at(), highlight.DataType)
	case keywords[w]:
		l.span(start, l.at(), highlight.Keyword)
	case l.nextNonSpace() == '(':
		l.span(start, l.at(), highlight.Function)
	}
}

func (l *line) nextNonSpace() rune {
	i := l.at()
	for i < len(l.text) && (l.text[i] == ' ' || l.text[i] == '\t') {
		i++
	}
	if i >= len(l.text) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.text[i:])
	return r
}

// operator colours a run of operator characters that began at start.
func (l *line) operator(start int) {
	for !l.sc.Done() {
		r, _ := l.sc.Peek()
		if !strings.ContainsRune("+*/%=<>!&|^~:", r) {
			break
		}
		l.sc.Next()
	}
	l.span(start, l.at(), highlight.Operator)
}
