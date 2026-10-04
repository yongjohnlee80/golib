package sqlite

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// SQLite's full-text search is FTS5: an external-content table keyed by the
// content table's rowid, MATCH on the table's name, bm25 for rank and snippet
// for excerpts. Every identifier is quoted; the query text is bound; the
// snippet markers are quoted string constants.

// FullTextJoin implements dao.FullTexter.
func (d SqliteDialect) FullTextJoin(ix dao.FullTextIndex) string {
	return "INNER JOIN " + d.QuoteIdent(ix.Name) + " ON " + d.QuoteIdent(ix.Name) + ".\"rowid\" = " +
		d.QuoteIdent(ix.Table) + "." + d.QuoteIdent(ix.Key)
}

// FullTextMatch implements dao.FullTexter.
func (d SqliteDialect) FullTextMatch(ix dao.FullTextIndex, placeholder string) string {
	return d.QuoteIdent(ix.Name) + " MATCH " + placeholder
}

// FullTextRank implements dao.FullTexter: bm25 is lower for better matches, so
// ascending is best first.
func (d SqliteDialect) FullTextRank(ix dao.FullTextIndex, weights []float64) string {
	var b strings.Builder
	b.WriteString("bm25(")
	b.WriteString(d.QuoteIdent(ix.Name))
	for _, w := range weights {
		b.WriteString(", ")
		b.WriteString(strconv.FormatFloat(w, 'g', -1, 64))
	}
	b.WriteByte(')')
	return b.String()
}

// FullTextSnippet implements dao.FullTexter.
func (d SqliteDialect) FullTextSnippet(ix dao.FullTextIndex, column int, m dao.SnippetMarks) (string, error) {
	if m.Tokens < 1 || m.Tokens > 64 {
		return "", errs.Wrap(errs.ErrInvalidArgument, "sqlite: a snippet is 1 to 64 tokens, not %d", m.Tokens)
	}
	var marks [3]string
	for i, s := range []string{m.Open, m.Close, m.Ellipsis} {
		q, err := d.QuoteString(s)
		if err != nil {
			return "", err
		}
		marks[i] = q
	}
	return "snippet(" + d.QuoteIdent(ix.Name) + ", " + strconv.Itoa(column) + ", " +
		marks[0] + ", " + marks[1] + ", " + marks[2] + ", " + strconv.Itoa(m.Tokens) + ")", nil
}

var _ dao.FullTexter = SqliteDialect{}

var _ dao.FullTextMatcher = SqliteDialect{}
