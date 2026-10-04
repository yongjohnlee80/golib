package query

import (
	"strings"

	"github.com/yongjohnlee80/golib/errs"
)

// ErrNoTerms is a query with nothing to match.
var ErrNoTerms = errs.Sentinel(errs.ErrInvalidArgument, "search: no terms to match")

// tsLexeme escapes a word for a quoted tsquery lexeme: inside the quotes a
// backslash escapes and a quote is doubled.
var tsLexeme = strings.NewReplacer(`\`, `\\`, `'`, `''`)

// TSQuery renders terms as a PostgreSQL tsquery, the text dao.Match and
// dao.RankQuery bind there: each term a quoted lexeme, a prefix term ending
// ":*", all joined with " & " so every term must match. Quoting keeps every
// operator character (&, |, !, :, parentheses) a character of the word, as
// FTS5 does for SQLite; the value is always bound, never inlined. No terms is
// [ErrNoTerms].
//
//	TSQuery([]Term{{Text: "rock&roll"}, {Text: "gui", Prefix: true}})
//	// 'rock&roll' & 'gui':*
func TSQuery(terms []Term) (string, error) {
	if len(terms) == 0 {
		return "", ErrNoTerms
	}
	parts := make([]string, len(terms))
	for i, t := range terms {
		p := "'" + tsLexeme.Replace(t.Text) + "'"
		if t.Prefix {
			p += ":*"
		}
		parts[i] = p
	}
	return strings.Join(parts, " & "), nil
}
