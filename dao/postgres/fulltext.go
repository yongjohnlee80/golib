package postgres

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// PostgreSQL's full-text search keeps the document's tsvector as a column of
// the content table (FullTextIndex.Name on FullTextIndex.Table), so nothing is
// joined. It matches with @@ and ranks with ts_rank_cd, both against the query
// bound again at its placeholder; that second binding is why PostgreSQL does
// not implement dao.FullTexter, whose rank and snippet take none. Its snippet
// is ts_headline over the content column, against the query bound again too.
//
// The configuration renders as a quoted regconfig literal. It has already
// passed FullTextIndex.Validate, so it is an identifier, but it still goes
// through QuoteString rather than being pasted between quotes by hand.

// FullTextMatch implements dao.FullTextMatcher.
func (d PostgresDialect) FullTextMatch(ix dao.FullTextIndex, placeholder string) string {
	return d.tsvector(ix) + " @@ to_tsquery(" + d.regconfig(ix) + ", " + placeholder + ")"
}

// FullTextQueryRank implements dao.FullTextQueryRanker: ts_rank_cd is higher
// for better matches, so it is negated to put the best first when ascending.
// With weights, weight i goes into the slot of ix.Classes[i] in ts_rank_cd's
// {D, C, B, A} array, and a class no column has keeps PostgreSQL's default.
func (d PostgresDialect) FullTextQueryRank(ix dao.FullTextIndex, weights []float64, placeholder string) string {
	var b strings.Builder
	b.WriteString("-ts_rank_cd(")
	if len(weights) > 0 {
		b.WriteString(classWeights(ix.Classes, weights))
		b.WriteString(", ")
	}
	b.WriteString(d.tsvector(ix))
	b.WriteString(", to_tsquery(")
	b.WriteString(d.regconfig(ix))
	b.WriteString(", ")
	b.WriteString(placeholder)
	b.WriteString("))")
	return b.String()
}

// FullTextQuerySnippet implements dao.FullTextQuerySnippeter: ts_headline of
// the content column, one fragment of at most m.Tokens words (and at least
// half that), the matches between m.Open and m.Close. ts_headline separates
// fragments with a delimiter and marks no cut at the ends, so m.Ellipsis is
// not drawn. The marks go inside ts_headline's options as double-quoted
// values, which have no escape: a mark holding a double quote, a newline or
// a NUL is refused, as is a length outside 2 to 64 (MinWords must stay below
// MaxWords).
func (d PostgresDialect) FullTextQuerySnippet(ix dao.FullTextIndex, column int, m dao.SnippetMarks, placeholder string) (string, error) {
	if column < 0 || column >= len(ix.Columns) {
		return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: snippet column %d of %d", column, len(ix.Columns))
	}
	if m.Tokens < 2 || m.Tokens > 64 {
		return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: a snippet is 2 to 64 words, not %d", m.Tokens)
	}
	for _, s := range []string{m.Open, m.Close} {
		if s == "" || strings.ContainsAny(s, "\"\n\r\x00") {
			return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: a snippet mark is not empty and has no double quote, line break or NUL: %q", s)
		}
	}
	opts := "StartSel=\"" + m.Open + "\", StopSel=\"" + m.Close + "\", MaxWords=" + strconv.Itoa(m.Tokens) +
		", MinWords=" + strconv.Itoa(max(1, m.Tokens/2)) + ", MaxFragments=1"
	lit, err := d.QuoteString(opts)
	if err != nil {
		return "", err
	}
	return "ts_headline(" + d.regconfig(ix) + ", " + d.QuoteTable(ix.Table) + "." + d.QuoteIdent(ix.Columns[column]) +
		", to_tsquery(" + d.regconfig(ix) + ", " + placeholder + "), " + lit + ")", nil
}

// tsvector is the index's column on its content table.
func (d PostgresDialect) tsvector(ix dao.FullTextIndex) string {
	return d.QuoteTable(ix.Table) + "." + d.QuoteIdent(ix.Name)
}

// regconfig is the index's configuration as a typed literal: "simple" when it
// names none. An index that fails Validate never gets here through a DAO; if
// one is rendered directly it gets a NULL configuration, which matches and
// ranks nothing, rather than its text in the statement.
func (d PostgresDialect) regconfig(ix dao.FullTextIndex) string {
	if ix.Validate() != nil {
		return "NULL::regconfig"
	}
	cfg := ix.Config
	if cfg == "" {
		cfg = "simple"
	}
	q, err := d.QuoteString(cfg)
	if err != nil {
		return "NULL::regconfig"
	}
	return q + "::regconfig"
}

// pgDefaultWeights are ts_rank_cd's defaults, in its {D, C, B, A} order.
var pgDefaultWeights = [4]float64{0.1, 0.2, 0.4, 1.0}

// classWeights is the weights array literal: each weight at its column's
// class, the rest PostgreSQL's defaults.
func classWeights(classes []byte, weights []float64) string {
	slots := pgDefaultWeights
	for i, w := range weights {
		if i < len(classes) && classes[i] >= 'A' && classes[i] <= 'D' {
			slots['D'-classes[i]] = w
		}
	}
	parts := make([]string, len(slots))
	for i, w := range slots {
		s := strconv.FormatFloat(w, 'f', -1, 64)
		if !strings.ContainsAny(s, ".") {
			s += ".0"
		}
		parts[i] = s
	}
	return "'{" + strings.Join(parts, ", ") + "}'"
}

var (
	_ dao.FullTextMatcher        = PostgresDialect{}
	_ dao.FullTextQueryRanker    = PostgresDialect{}
	_ dao.FullTextQuerySnippeter = PostgresDialect{}
)
