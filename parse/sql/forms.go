package sql

import "github.com/yongjohnlee80/golib/parse"

// operatorChars are the bytes a PostgreSQL operator name may be built from.
const operatorChars = "+-*/<>=~!@#%^&|`?"

// operatorSpecials are the subset whose presence lets a multi-byte name END in
// `+` or `-`. Without one of these a trailing sign is not part of the name, which
// is how `1+-2` is a sum of 1 and -2 rather than an operator nobody defined.
const operatorSpecials = "~!@#%^&|`?"

func isOperatorByte(b byte) bool { return indexByte(operatorChars, b) >= 0 }
func hasSpecial(s []byte) bool {
	for _, b := range s {
		if indexByte(operatorSpecials, b) >= 0 {
			return true
		}
	}
	return false
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// PostgresOperator returns the form for a PostgreSQL operator NAME.
//
// # Why this is not a fixed set
//
// PostgreSQL does not have a list of operators; it has a grammar for operator
// names, and CREATE OPERATOR lets a schema add more. A preset that enumerated
// the built-in spellings would lex `@-`, `?|` and `#-` as two operators each and
// would never see a user-defined one at all — so a caller could not trust the
// token stream of any database that defines its own, which is most of the
// interesting ones.
//
// A name is a run of `+-*/<>=~!@#%^&|`+"`"+`?` with two restrictions, both of which
// exist because of what else those bytes spell:
//
//   - `--` and `/*` may not appear inside a name, because they open comments. So
//     `+--` is the operator `+` followed by a comment, not a three-byte name.
//   - a name longer than one byte may not END in `+` or `-` unless it also
//     contains one of `~!@#%^&|`+"`"+`?`. That is what makes `1+-2` a sum rather than
//     an operator, while `@-` stays one name because the `@` earns the trailing
//     sign.
//
// The restrictions need lookahead, so at a window edge the form defers rather
// than guessing: `+` followed by nothing yet could still become `+-@`, and
// answering early would mean answering differently depending on how the bytes
// arrived.
func PostgresOperator() parse.Form { return postgresOperator{} }

type postgresOperator struct{}

func (postgresOperator) Kind() parse.Kind { return parse.Operator }

func (postgresOperator) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	if !isOperatorByte(src[0]) {
		return 0, parse.NoMatch
	}
	// One byte is always a legal name, so the opener is decidable without the
	// boundary; how far it runs is End's business.
	return 1, parse.Matched
}

func (postgresOperator) End(src, openedWith []byte, boundary parse.InputBoundary) (int, error) {
	name := make([]byte, 0, len(openedWith)+len(src))
	name = append(name, openedWith...)

	stopped := false
	i := 0
	for i < len(src) {
		b := src[i]
		if !isOperatorByte(b) {
			stopped = true
			break
		}
		// A name may not CONTAIN the opening of a comment, so the byte that would
		// start one does not belong to the name either: in `1+/*c*/2` the name is
		// `+`, not `+/`, because the slash is the comment's.
		if last := name[len(name)-1]; (last == '-' && b == '-') || (last == '/' && b == '*') {
			if len(name) > len(openedWith) {
				name = name[:len(name)-1]
			}
			stopped = true
			break
		}
		name = append(name, b)
		i++
	}

	if !stopped && boundary == parse.MoreInput {
		// The run could still grow, and what arrives changes the answer: a later
		// special byte earns a trailing sign that would otherwise be trimmed.
		return 0, parse.ErrNeedMore
	}

	// A trailing sign only belongs to the name if the name earned it.
	if !hasSpecial(name) {
		for len(name) > 1 {
			if last := name[len(name)-1]; last == '+' || last == '-' {
				name = name[:len(name)-1]
				continue
			}
			break
		}
	}
	return len(name) - len(openedWith), nil
}

// MySQLDashComment returns the form for MySQL's `--` comment, which is NOT the
// same construct as the ANSI one.
//
// MySQL requires the second dash to be followed by whitespace or a control byte;
// without it the bytes are two minus signs. That is not a nicety — `balance--1`
// is a subtraction of a negative number, and reading it as a comment silently
// deletes the rest of the line from the token stream.
//
// With nothing after the second dash there is no following byte to satisfy the
// rule, so at end of input `--` is two operators rather than an empty comment.
func MySQLDashComment() parse.Form { return mysqlDashComment{} }

type mysqlDashComment struct{}

func (mysqlDashComment) Kind() parse.Kind { return parse.Comment }

func (mysqlDashComment) Starts(src []byte) (int, parse.Match) {
	for i, want := range []byte{'-', '-'} {
		if i >= len(src) {
			return 0, parse.Incomplete
		}
		if src[i] != want {
			return 0, parse.NoMatch
		}
	}
	// The third byte decides whether this is a comment at all.
	if len(src) < 3 {
		return 0, parse.Incomplete
	}
	if !isCommentGap(src[2]) {
		return 0, parse.NoMatch
	}
	return 2, parse.Matched
}

// isCommentGap reports the bytes MySQL accepts after `--`: whitespace, or any
// control byte.
func isCommentGap(b byte) bool { return b == ' ' || b == '\t' || b < 0x20 || b == 0x7f }

func (mysqlDashComment) End(src, openedWith []byte, boundary parse.InputBoundary) (int, error) {
	return lineBody(src, boundary)
}

// MySQLBlockComment returns the form for MySQL's `/* … */`, which deliberately
// DOES NOT match an executable version comment.
//
// `/*! … */` and `/*!50000 … */` are not comments in MySQL: the server executes
// what is inside them. Lexing that as a Comment would be the worst possible
// answer, because trivia is exactly what a consumer is invited to discard — an
// AST builder filters it in one line, and `/*!50000 DROP TABLE t */` would
// vanish on the way past. For anything examining SQL it must not.
//
// So this form refuses `/*!`, and the bytes inside are lexed as the ordinary
// tokens they are. The delimiters come through as operators, which is untidy and
// deliberately so: it is visible, and nothing executable is hidden inside a token
// a caller was told was safe to drop.
func MySQLBlockComment() parse.Form { return mysqlBlockComment{} }

type mysqlBlockComment struct{}

func (mysqlBlockComment) Kind() parse.Kind { return parse.Comment }

func (mysqlBlockComment) Starts(src []byte) (int, parse.Match) {
	for i, want := range []byte{'/', '*'} {
		if i >= len(src) {
			return 0, parse.Incomplete
		}
		if src[i] != want {
			return 0, parse.NoMatch
		}
	}
	// A third byte of `!` makes this executable, and executable is not trivia.
	if len(src) < 3 {
		return 0, parse.Incomplete
	}
	if src[2] == '!' {
		return 0, parse.NoMatch
	}
	return 2, parse.Matched
}

func (mysqlBlockComment) End(src, openedWith []byte, boundary parse.InputBoundary) (int, error) {
	// MySQL does not nest block comments: the first close ends it.
	for i := 0; i+1 < len(src); i++ {
		if src[i] == '*' && src[i+1] == '/' {
			return i + 2, nil
		}
	}
	if boundary == parse.MoreInput {
		return 0, parse.ErrNeedMore
	}
	return 0, &parse.UnterminatedError{Kind: parse.Comment, Open: string(openedWith)}
}

// lineBody is the shared tail of a to-end-of-line comment: the newline is the
// next token's, not the comment's, so a formatter keeps the line break.
func lineBody(src []byte, boundary parse.InputBoundary) (int, error) {
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			return i, nil
		}
	}
	if boundary == parse.MoreInput {
		return 0, parse.ErrNeedMore
	}
	return len(src), nil
}
