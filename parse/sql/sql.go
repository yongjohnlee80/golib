package sql

import "github.com/yongjohnlee80/golib/parse"

// PostgreSQL returns the lexical forms of PostgreSQL, in precedence order.
//
// Assumes standard_conforming_strings is on, which has been the default since
// 9.1: an ordinary '…' literal treats a backslash as an ordinary byte, and the
// E'…' form is the one that gives it escape meaning. Dollar-quoting is included,
// and its tag rule is [PostgresTag].
//
// Deliberately not covered, because each needs a decision rather than a guess:
// U&'…' unicode escapes, B'…' and X'…' bit and hex strings, and operator names
// inside OPERATOR(...). A caller who needs one adds a form to the slice.
func PostgreSQL() []parse.Form {
	return []parse.Form{
		// Comments first: `/*` must beat the `/` operator, `--` must beat `-`.
		parse.BlockComment("/*", "*/", true), // PostgreSQL nests them
		parse.LineComment("--"),

		// Escape strings before the word run, which would otherwise take the E.
		parse.QuoteForm("E'", "'", parse.QuoteOpts{Doubling: true, Escape: '\\'}),
		parse.QuoteForm("e'", "'", parse.QuoteOpts{Doubling: true, Escape: '\\'}),

		// Dollar quoting. The tag rule is this package's, which is what keeps the
		// core from having to know that $1 is a parameter here.
		parse.DelimitedForm('$', '$', parse.DelimitedOpts{
			TagByte:    PostgresTag,
			AllowEmpty: true, // $$body$$
		}),

		parse.QuoteForm("'", "'", parse.QuoteOpts{Doubling: true}),
		parse.QuoteForm(`"`, `"`, parse.QuoteOpts{Doubling: true, Kind: parse.Ident}),

		parse.RunForm(parse.Space, IsSpace),

		// Numbers before the word run and before punctuation: 1e5 is one number,
		// and a leading dot in .5 is part of one rather than punctuation.
		Number(),
		parse.RunForm(parse.Word, PostgresWordByte),

		// The cast and assignment tokens, which are NOT operator names: `:` is
		// not one of PostgreSQL's operator bytes, so the grammar below cannot
		// reach them.
		parse.SetForm(parse.Operator, "::", ":="),

		// Not a list: PostgreSQL has a GRAMMAR for operator names, and
		// CREATE OPERATOR lets a schema add more.
		PostgresOperator(),
		parse.SetForm(parse.Terminator, ";"),
		parse.SetForm(parse.Punct, "(", ")", ",", ".", "[", "]", ":"),

		// Total coverage: whatever is left is one byte of punctuation.
		parse.ByteForm(parse.Punct),
	}
}

// MySQL returns the lexical forms of MySQL, in precedence order.
//
// Assumes the default SQL mode: a backslash escapes inside both '…' and "…", and
// a double-quoted run is a STRING rather than an identifier. Under ANSI_QUOTES it
// would be an identifier instead — that is a session mode, not a dialect
// property, so a caller who runs in that mode swaps the one form rather than
// getting a flag here.
//
// Two MySQL-specific forms rather than the generic ones, because MySQL's
// versions of these constructs are genuinely different: `--` is a comment only
// when whitespace or a control byte follows it, so `balance--1` stays a
// subtraction; and `/*! … */` is refused as a comment because the server EXECUTES
// its contents, which must not reach a caller as trivia it was invited to drop.
// See [MySQLDashComment] and [MySQLBlockComment].
func MySQL() []parse.Form {
	return []parse.Form{
		// MySQL does not nest block comments, and /*! … */ is NOT one: the
		// server executes what is inside it, so it must not arrive as trivia.
		MySQLBlockComment(),
		// `--` is a comment only when whitespace or a control byte follows it.
		MySQLDashComment(),
		parse.LineComment("#"),

		parse.QuoteForm("'", "'", parse.QuoteOpts{Doubling: true, Escape: '\\'}),
		parse.QuoteForm(`"`, `"`, parse.QuoteOpts{Doubling: true, Escape: '\\'}),
		parse.QuoteForm("`", "`", parse.QuoteOpts{Doubling: true, Kind: parse.Ident}),

		parse.RunForm(parse.Space, IsSpace),

		Number(),
		parse.RunForm(parse.Word, MySQLWordByte),

		parse.SetForm(parse.Operator,
			"<=>", // the null-safe equality, and it must beat <= and <
			"->>", "->", ":=", "<<", ">>", "&&", "||",
			"<>", "<=", ">=", "!=",
			"+", "-", "*", "/", "%", "^", "=", "<", ">", "~", "&", "|", "!",
		),
		parse.SetForm(parse.Terminator, ";"),
		parse.SetForm(parse.Punct, "(", ")", ",", ".", "[", "]"),

		parse.ByteForm(parse.Punct),
	}
}

// IsSpace reports the bytes SQL treats as whitespace between tokens.
func IsSpace(_ int, b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

// PostgresWordByte reports whether b may appear at index within an unquoted
// PostgreSQL identifier. A leading digit is not an identifier's business — that
// is a number — and `$` is legal after the first byte only.
//
// Bytes at or above 0x80 are accepted anywhere, which is how a UTF-8 identifier
// gets through a byte-oriented predicate without this package deciding what an
// encoding means.
func PostgresWordByte(index int, b byte) bool {
	if isLetter(b) || b == '_' || b >= 0x80 {
		return true
	}
	if index == 0 {
		return false
	}
	return isDigit(b) || b == '$'
}

// MySQLWordByte is the same idea for MySQL, which additionally allows `$` as a
// first byte.
func MySQLWordByte(index int, b byte) bool {
	if isLetter(b) || b == '_' || b == '$' || b >= 0x80 {
		return true
	}
	if index == 0 {
		return false
	}
	return isDigit(b)
}

// PostgresTag reports whether b is legal at index within a dollar-quoting tag.
//
// It is POSITION-AWARE because the ordinary rule needs it: a leading digit is
// illegal while a trailing one is fine, so the same predicate must reject `$1$`
// — where `$1` is a parameter, not a quote — and accept `$a1$`. No single
// func(byte) bool can do both, and writing this charset into the core would have
// been the core naming a dialect.
func PostgresTag(index int, b byte) bool {
	if isLetter(b) || b == '_' || b >= 0x80 {
		return true
	}
	if index == 0 {
		return false
	}
	return isDigit(b)
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isDigit(b byte) bool  { return b >= '0' && b <= '9' }
